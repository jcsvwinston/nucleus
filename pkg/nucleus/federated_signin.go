// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package nucleus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

// The framework builds the identity providers declared in auth_federated
// and owns the flow — the anti-forgery state, the pending sign-in, the
// refusal of a callback that does not carry it back — and it used to stop
// there: the two routes were the application's to write (ADR-028). Every
// application wrote the same two handlers, and until it did, the callback
// URL the startup log tells the operator to register answered 404.
//
// FederatedSignIn is those two handlers, written once: what `nucleus add
// oidc` mounts. An application whose sign-in ends somewhere else — linking
// the identity to an account of its own, issuing a token, a landing page
// that depends on who signed in — keeps writing its own pair, as before.

// FederatedSignInConfig is the configuration of the FederatedSignIn module,
// bound from `modules.federated.*` in nucleus.yml.
type FederatedSignInConfig struct {
	// Redirect is the path the browser is sent to after a successful
	// sign-in, e.g. "/". Empty answers 200 with the signed-in identity as
	// JSON. It has to be a path of this application ("/…"): the callback
	// is reachable by anyone, and an absolute address would make it an
	// open redirect.
	Redirect string `koanf:"redirect"`
}

// The session keys the FederatedSignIn callback writes the signed-in
// identity under. A handler reads them with the session manager (or
// Context.SessionGetString).
const (
	// SessionKeyFederatedInstance is the auth_federated instance the person
	// signed in through ("corp").
	SessionKeyFederatedInstance = "federated_instance"
	// SessionKeyFederatedUserID is the identity provider's subject for the
	// person.
	SessionKeyFederatedUserID = "federated_user_id"
	// SessionKeyFederatedUsername is the username the provider reported.
	SessionKeyFederatedUsername = "federated_username"
	// SessionKeyFederatedEmail is the email address the provider reported.
	SessionKeyFederatedEmail = "federated_email"
)

// FederatedSignInModuleName is the name FederatedSignIn mounts under: its
// configuration is `modules.federated.*`.
const FederatedSignInModuleName = "federated"

// federatedStateCookie carries the anti-forgery state token from the start
// route to the callback. It is not the provider's state and the provider
// never sees it.
const federatedStateCookie = "nucleus_federated_state"

// FederatedSignIn returns the module that serves the sign-in routes of
// every identity provider declared in auth_federated:
//
//	GET  /auth/<name>/start      auth.FederatedStartPath(name)
//	GET  /auth/<name>/callback   auth.FederatedCallbackPath(name)
//	POST /auth/<name>/callback   (providers that answer with a form post)
//
// The start route begins the flow and redirects the browser to the identity
// provider; the anti-forgery state rides in an HttpOnly cookie scoped to
// the instance's routes. The callback completes the flow — the framework
// refuses a callback without that state before the provider is consulted —
// then rotates the session token and records the identity in the session
// under the SessionKeyFederated* keys, and answers with the identity as JSON
// or redirects to FederatedSignInConfig.Redirect.
//
// On the default stack the module grants the anonymous subject these routes
// and nothing else: the person signing in has no session yet. With no
// instance declared it mounts nothing and says so at startup.
//
//	nucleus.New().
//	    FromConfigFile("nucleus.yml").
//	    Mount(nucleus.FederatedSignIn()).
//	    Start()
func FederatedSignIn() ModuleSpec {
	var (
		set      *auth.FederatedSet
		sessions *auth.SessionManager
		logger   = slog.Default()
		secure   bool
	)
	return Module[FederatedSignInConfig]{
		Name: FederatedSignInModuleName,
		OnStart: func(_ context.Context, rt Runtime, cfg FederatedSignInConfig) error {
			if err := checkSignInRedirect(cfg.Redirect); err != nil {
				return err
			}
			r, ok := rt.(runtime)
			if !ok || r.core == nil {
				return errors.New("federated: FederatedSignIn serves the identity providers a nucleus application builds, and this runtime is not one")
			}
			set = r.core.AuthFederated
			if r.core.Config != nil {
				secure = strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.core.Config.PublicBaseURL)), "https://")
			}
			sessions = rt.Session()
			logger = rt.Logger()
			if set == nil {
				rt.Logger().Warn("nucleus: FederatedSignIn is mounted and auth_federated declares no identity provider; no sign-in route is served")
				return nil
			}
			if enforcer := rt.Authorizer(); enforcer != nil {
				for _, name := range set.Names() {
					for _, p := range []string{auth.FederatedStartPath(name), auth.FederatedCallbackPath(name)} {
						if err := enforcer.AddPolicy("anonymous", p, "*"); err != nil {
							return fmt.Errorf("federated: allow the sign-in route %s: %w", p, err)
						}
					}
				}
			}
			rt.Logger().Info("nucleus: federated sign-in routes mounted", "instances", strings.Join(set.Names(), " "))
			return nil
		},
		Routes: func(r Router, cfg FederatedSignInConfig) {
			if set == nil {
				return
			}
			for _, name := range set.Names() {
				r.Get(auth.FederatedStartPath(name), federatedStart(set, logger, name, secure))
				callback := federatedCallback(set, sessions, logger, name, cfg.Redirect, secure)
				r.Get(auth.FederatedCallbackPath(name), callback)
				r.Post(auth.FederatedCallbackPath(name), callback)
			}
		},
	}.Build()
}

// checkSignInRedirect refuses a redirect that is not a path of this
// application.
func checkSignInRedirect(redirect string) error {
	if redirect == "" {
		return nil
	}
	if !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") || strings.ContainsAny(redirect, "\\\r\n") {
		return fmt.Errorf("federated: modules.%s.redirect must be a path of this application (\"/…\"), got %q: an absolute address would make the sign-in callback an open redirect",
			FederatedSignInModuleName, redirect)
	}
	return nil
}

// federatedCookiePath scopes the state cookie to one instance's two routes.
func federatedCookiePath(instance string) string {
	return strings.TrimSuffix(auth.FederatedStartPath(instance), "start")
}

func federatedStart(set *auth.FederatedSet, logger *slog.Logger, instance string, secure bool) Handler {
	return func(c *Context) error {
		redirectURL, state, err := set.Begin(c.Request.Context(), instance)
		if err != nil {
			// The provider could not be reached or refused to start: the
			// person can do nothing about it, the log says why.
			logger.Error("nucleus: federated sign-in could not start", "instance", instance, "error", err)
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "sign-in is unavailable"})
		}
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     federatedStateCookie,
			Value:    state,
			Path:     federatedCookiePath(instance),
			HttpOnly: true,
			Secure:   secure,
			// Lax, not Strict: the callback is a top-level navigation coming
			// back from another site, and a Strict cookie would not ride it.
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(auth.DefaultFederatedPendingTTL.Seconds()),
		})
		return c.Redirect(http.StatusFound, redirectURL)
	}
}

func federatedCallback(set *auth.FederatedSet, sessions *auth.SessionManager, logger *slog.Logger, instance, redirect string, secure bool) Handler {
	return func(c *Context) error {
		ctx := c.Request.Context()
		var state string
		if cookie, err := c.Request.Cookie(federatedStateCookie); err == nil {
			state = cookie.Value
		}
		// The state is single use whatever happens next.
		http.SetCookie(c.Writer, &http.Cookie{Name: federatedStateCookie, Value: "", Path: federatedCookiePath(instance), MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
		if state == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "the sign-in was not started here, or took too long: start it again"})
		}
		user, err := set.Complete(ctx, instance, state, c.Request)
		if err != nil {
			logger.Warn("nucleus: federated sign-in refused", "instance", instance, "error", err)
			return c.JSON(http.StatusUnauthorized, map[string]string{"error": "sign-in failed"})
		}
		if sessions == nil || !sessions.HasSession(ctx) {
			return errors.New("federated: the callback did not pass through the session middleware, so there is nowhere to record the sign-in")
		}
		// Rotate first: a session fixed before the sign-in must not be the
		// one that carries the identity after it.
		if err := sessions.RenewToken(ctx); err != nil {
			return fmt.Errorf("federated: rotate the session: %w", err)
		}
		sessions.Put(ctx, SessionKeyFederatedInstance, instance)
		sessions.Put(ctx, SessionKeyFederatedUserID, user.ID)
		sessions.Put(ctx, SessionKeyFederatedUsername, user.Username)
		sessions.Put(ctx, SessionKeyFederatedEmail, user.Email)
		if redirect != "" {
			return c.Redirect(http.StatusSeeOther, redirect)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"instance": instance,
			"id":       user.ID,
			"username": user.Username,
			"email":    user.Email,
			"roles":    user.AllRoles(),
		})
	}
}

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
//	GET  /auth/<name>/metadata   auth.FederatedMetadataPath(name), for a
//	                             provider that publishes service metadata
//	                             (SAML)
//
// The start route begins the flow and redirects the browser to the identity
// provider; the anti-forgery state rides in an HttpOnly cookie scoped to
// the instance's routes — SameSite=Lax, or SameSite=None with Secure for a
// provider whose identity provider answers with a cross-site form post
// (SAML) when public_base_url is https, because a Lax cookie does not ride
// that post. The callback completes the flow — the framework refuses a
// callback without that state before the provider is consulted — then
// rotates the session token and records the identity in the session under
// the SessionKeyFederated* keys, and answers with the identity as JSON or
// redirects to FederatedSignInConfig.Redirect. With csrf_enabled the
// callbacks are exempted from the CSRF check: an identity provider's form
// post cannot carry the application's token, and the state is what ties
// the callback to the browser that started it.
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
					paths := []string{auth.FederatedStartPath(name), auth.FederatedCallbackPath(name)}
					if set.PublishesServiceMetadata(name) {
						paths = append(paths, auth.FederatedMetadataPath(name))
					}
					for _, p := range paths {
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
				sameSite := federatedStateSameSite(set, name, secure)
				r.Get(auth.FederatedStartPath(name), federatedStart(set, logger, name, secure, sameSite))
				callback := federatedCallback(set, sessions, logger, name, cfg.Redirect, secure, sameSite)
				r.Get(auth.FederatedCallbackPath(name), callback)
				r.Post(auth.FederatedCallbackPath(name), callback)
				if set.PublishesServiceMetadata(name) {
					r.Get(auth.FederatedMetadataPath(name), federatedMetadata(set, logger, name))
				}
			}
		},
	}.Build()
}

// federatedCallbackCSRFExemptions exempts the callback of every declared
// instance from the CSRF check when FederatedSignIn is mounted. An identity
// provider returns the browser with a form POST from its own site (SAML's
// HTTP-POST binding, OIDC's form_post), which cannot carry the
// application's CSRF token; what ties that request to the browser that
// started the sign-in is the state cookie the callback checks before the
// provider is consulted, and what ties it to this application is the
// provider's own verification. What is exempted is each callback path — the
// CSRF middleware matches by prefix, and nothing is served below a callback
// — never the /auth/ prefix other routes live under. It runs before app.New, the
// last moment an exemption can take effect, and logs what it exempts, as
// the module exemptions do.
func federatedCallbackCSRFExemptions(specs map[string]ModuleSpec, instances []auth.FederatedInstance, logger *slog.Logger) []string {
	mounted := false
	for _, spec := range specs {
		if spec != nil && spec.Name() == FederatedSignInModuleName {
			mounted = true
		}
	}
	if !mounted || len(instances) == 0 {
		return nil
	}
	var out []string
	for _, inst := range instances {
		name := strings.ToLower(strings.TrimSpace(inst.Name))
		if name == "" {
			continue
		}
		out = append(out, auth.FederatedCallbackPath(name))
	}
	if logger != nil && len(out) > 0 {
		logger.Info("nucleus: federated sign-in callbacks exempted from CSRF (an identity provider's form post cannot carry the token; the sign-in's state cookie is checked instead)",
			"paths", strings.Join(out, " "))
	}
	return out
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

// federatedStateSameSite is the SameSite attribute of an instance's state
// cookie. Lax by default: the callback is a top-level navigation coming
// back from another site, and a Strict cookie would not ride it. A Lax
// cookie does not ride a cross-site form POST either, which is how a SAML
// identity provider returns the browser, so such an instance gets None —
// which browsers accept only with Secure, so only over https. Over plain
// http the cookie stays Lax, and the sign-in works with an identity
// provider on the same site (a local one) and not with a remote one.
func federatedStateSameSite(set *auth.FederatedSet, instance string, secure bool) http.SameSite {
	if secure && set.CallbackIsCrossSiteFormPost(instance) {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

// federatedMetadata serves the document a provider publishes for its
// identity provider (SAML service-provider metadata). It is mounted only for
// an instance whose provider publishes one.
func federatedMetadata(set *auth.FederatedSet, logger *slog.Logger, instance string) Handler {
	return func(c *Context) error {
		contentType, body, ok, err := set.ServiceMetadata(c.Request.Context(), instance)
		switch {
		case err != nil:
			logger.Error("nucleus: federated service metadata unavailable", "instance", instance, "error", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "metadata is unavailable"})
		case !ok:
			return c.JSON(http.StatusNotFound, map[string]string{"error": "not found"})
		}
		c.Writer.Header().Set("Content-Type", contentType)
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write(body)
		return nil
	}
}

func federatedStart(set *auth.FederatedSet, logger *slog.Logger, instance string, secure bool, sameSite http.SameSite) Handler {
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
			// Lax, not Strict, and None for a cross-site form post over
			// https: see federatedStateSameSite.
			SameSite: sameSite,
			MaxAge:   int(auth.DefaultFederatedPendingTTL.Seconds()),
		})
		return c.Redirect(http.StatusFound, redirectURL)
	}
}

func federatedCallback(set *auth.FederatedSet, sessions *auth.SessionManager, logger *slog.Logger, instance, redirect string, secure bool, sameSite http.SameSite) Handler {
	return func(c *Context) error {
		ctx := c.Request.Context()
		var state string
		if cookie, err := c.Request.Cookie(federatedStateCookie); err == nil {
			state = cookie.Value
		}
		// The state is single use whatever happens next.
		http.SetCookie(c.Writer, &http.Cookie{Name: federatedStateCookie, Value: "", Path: federatedCookiePath(instance), MaxAge: -1, HttpOnly: true, Secure: secure, SameSite: sameSite})
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

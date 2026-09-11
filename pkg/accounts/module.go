package accounts

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Routes are the paths this module serves. They are constants because an
// application links to them, a policy file names them, and a test asks for
// them — three places that must not drift apart.
const (
	RouteRegister       = "/auth/register"
	RouteVerifyEmail    = "/auth/verify-email"
	RouteLogin          = "/auth/login"
	RouteLogout         = "/auth/logout"
	RoutePasswordReset  = "/auth/password/reset"
	RoutePasswordChange = "/auth/password/change"
	RouteMagicLink      = "/auth/magic-link"
)

// Module mounts the account flows on the application's router.
//
// The handlers are thin on purpose: every decision that matters — what a
// failed login reveals, how long a token lives, whether a link can be used
// twice — is in Service, where it is tested once instead of in each
// application's copy of these seven handlers.
// The module takes the application's OWN session manager at start-up
// rather than whatever the caller happened to build the service with. That
// is not a convenience: a service holding a DIFFERENT manager from the one
// mounted as middleware writes into a session the request never carries,
// and the first symptom is a 500 on sign-in — which is exactly how this
// was found.
func Module(service *Service) nucleus.ModuleSpec {
	return nucleus.Module[struct{}]{
		Name: "accounts",
		OnStart: func(_ context.Context, rt nucleus.Runtime, _ struct{}) error {
			if service == nil {
				return errors.New("accounts: nil service")
			}
			if sm := rt.Session(); sm != nil {
				service.UseSessions(sm)
			}
			return nil
		},
		Routes: func(r nucleus.Router, _ struct{}) {
			r.Post(RouteRegister, handleRegister(service))
			r.Post(RouteVerifyEmail, handleVerifyEmail(service))
			r.Get(RouteVerifyEmail, handleVerifyEmail(service))
			r.Post(RouteLogin, handleLogin(service))
			r.Post(RouteLogout, handleLogout(service))
			r.Post(RoutePasswordReset, handlePasswordReset(service))
			r.Put(RoutePasswordReset, handlePasswordResetConfirm(service))
			r.Post(RoutePasswordChange, handlePasswordChange(service))
			r.Post(RouteMagicLink, handleMagicLinkRequest(service))
			r.Get(RouteMagicLink, handleMagicLinkConsume(service))
		},
	}.Build()
}

type registerRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleRegister always answers 202. The response says what was done for
// the CALLER — "if that address can be registered, a link is on its way" —
// and nothing about whether it already existed, which is the enumeration
// oracle every registration form ships by accident.
func handleRegister(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		var req registerRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		if err := s.Register(c.Request.Context(), req.Email, req.Username, req.Password); err != nil {
			if errors.Is(err, ErrWeakPassword) {
				// A weak password IS reported: the caller chose it, so
				// nothing about anyone else is disclosed, and refusing
				// without saying why is how a form becomes unusable.
				return c.JSON(http.StatusUnprocessableEntity, errorBody(err.Error()))
			}
			return err
		}
		return c.JSON(http.StatusAccepted, map[string]string{
			"status": "if that address can be registered, a confirmation link is on its way",
		})
	}
}

func handleVerifyEmail(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		token := tokenFrom(c)
		if token == "" {
			return c.JSON(http.StatusBadRequest, errorBody("a token is required"))
		}
		account, err := s.VerifyEmail(c.Request.Context(), token)
		if err != nil {
			return tokenError(c, err)
		}
		return c.JSON(http.StatusOK, map[string]any{"email": account.Email, "verified": true})
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleLogin(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		var req loginRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}

		ctx := c.Request.Context()
		account, err := s.Login(ctx, req.Email, req.Password)
		switch {
		case errors.Is(err, ErrAccountLocked):
			// 429 and not 401: the caller is being rate limited, and a
			// client that retries on 401 would hammer a locked account.
			return c.JSON(http.StatusTooManyRequests, errorBody("too many attempts, try again later"))
		case errors.Is(err, ErrInvalidCredentials):
			return c.JSON(http.StatusUnauthorized, errorBody("invalid credentials"))
		case errors.Is(err, ErrEmailNotVerified):
			return c.JSON(http.StatusForbidden, errorBody("confirm your email address first"))
		case errors.Is(err, ErrAccountDisabled):
			return c.JSON(http.StatusForbidden, errorBody("this account is disabled"))
		case err != nil:
			return err
		}

		if s.sessions != nil {
			if err := s.StartSession(ctx, account); err != nil {
				return err
			}
		}
		return c.JSON(http.StatusOK, map[string]any{"id": account.ID, "email": account.Email})
	}
}

func handleLogout(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		if err := s.Logout(c.Request.Context()); err != nil {
			return err
		}
		return c.NoContent()
	}
}

type emailRequest struct {
	Email string `json:"email"`
}

func handlePasswordReset(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		var req emailRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		if err := s.RequestPasswordReset(c.Request.Context(), req.Email); err != nil {
			return err
		}
		return c.JSON(http.StatusAccepted, map[string]string{
			"status": "if that address is registered, a reset link is on its way",
		})
	}
}

type resetConfirmRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func handlePasswordResetConfirm(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		var req resetConfirmRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		if _, err := s.ResetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
			if errors.Is(err, ErrWeakPassword) {
				return c.JSON(http.StatusUnprocessableEntity, errorBody(err.Error()))
			}
			return tokenError(c, err)
		}
		return c.NoContent()
	}
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handlePasswordChange requires a session: the account is the one signed
// in, never one named in the body. A handler that took an account id from
// the request would let anyone change anyone's password with a valid
// current password they already knew.
func handlePasswordChange(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		if s.sessions == nil {
			return c.JSON(http.StatusNotImplemented, errorBody("this deployment has no session manager"))
		}
		ctx := c.Request.Context()
		accountID := s.sessions.GetString(ctx, SessionKeyAccountID)
		if accountID == "" {
			return c.JSON(http.StatusUnauthorized, errorBody("sign in first"))
		}

		var req changePasswordRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		err := s.ChangePassword(ctx, accountID, req.CurrentPassword, req.NewPassword)
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return c.JSON(http.StatusUnauthorized, errorBody("the current password is wrong"))
		case errors.Is(err, ErrWeakPassword):
			return c.JSON(http.StatusUnprocessableEntity, errorBody(err.Error()))
		case err != nil:
			return err
		}
		return c.NoContent()
	}
}

func handleMagicLinkRequest(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		var req emailRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		if err := s.RequestMagicLink(c.Request.Context(), req.Email); err != nil {
			return err
		}
		return c.JSON(http.StatusAccepted, map[string]string{
			"status": "if that address is registered, a sign-in link is on its way",
		})
	}
}

func handleMagicLinkConsume(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		token := tokenFrom(c)
		if token == "" {
			return c.JSON(http.StatusBadRequest, errorBody("a token is required"))
		}
		ctx := c.Request.Context()
		account, err := s.ConsumeMagicLink(ctx, token)
		if err != nil {
			return tokenError(c, err)
		}
		if s.sessions != nil {
			if err := s.StartSession(ctx, account); err != nil {
				return err
			}
		}
		return c.JSON(http.StatusOK, map[string]any{"id": account.ID, "email": account.Email})
	}
}

// tokenFrom reads the token from the query string (a link the user clicked)
// or from a JSON body (a form the SPA posted).
func tokenFrom(c *nucleus.Context) string {
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		return token
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := c.BindJSON(&body); err == nil {
		return strings.TrimSpace(body.Token)
	}
	return ""
}

// tokenError answers every token failure the same way. Telling an unknown
// token from an expired or already-used one tells an attacker which guess
// was closer.
func tokenError(c *nucleus.Context, err error) error {
	if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrNotFound) {
		return c.JSON(http.StatusBadRequest, errorBody("invalid or expired token"))
	}
	if errors.Is(err, ErrAccountDisabled) {
		return c.JSON(http.StatusForbidden, errorBody("this account is disabled"))
	}
	return err
}

func errorBody(message string) map[string]any {
	return map[string]any{"error": map[string]string{"message": message}}
}

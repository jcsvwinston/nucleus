package accounts

import (
	"errors"
	"net/http"
	"time"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// Second-factor routes.
const (
	RouteTOTP          = "/auth/mfa/totp"
	RouteMFAVerify     = "/auth/mfa/verify"
	RouteRecoveryCodes = "/auth/mfa/recovery-codes"
)

// ReauthWindow is how fresh a sign-in must be to enrol or remove a second
// factor. Fifteen minutes is long enough not to be a nuisance inside one
// sitting, and short enough that a session left open on a shared machine
// is not a way to take the account over.
const ReauthWindow = 15 * time.Minute

func mfaRoutes(r nucleus.Router, service *Service) {
	r.Post(RouteTOTP, handleTOTPBegin(service))
	r.Put(RouteTOTP, handleTOTPConfirm(service))
	r.Delete(RouteTOTP, handleTOTPDisable(service))
	r.Post(RouteMFAVerify, handleMFAVerify(service))
	r.Post(RouteRecoveryCodes, handleRecoveryCodes(service))
}

// signedIn returns the account id in the session, or "" when there is none.
func signedIn(s *Service, c *nucleus.Context) string {
	ctx := c.Request.Context()
	if s.sessions == nil || !s.sessions.HasSession(ctx) {
		return ""
	}
	return s.sessions.GetString(ctx, SessionKeyAccountID)
}

// requireFreshSignIn is the guard every factor change goes through.
func requireFreshSignIn(s *Service, c *nucleus.Context) (string, error) {
	accountID := signedIn(s, c)
	if accountID == "" {
		return "", c.JSON(http.StatusUnauthorized, errorBody("sign in first"))
	}
	if err := s.RequireFreshAuth(c.Request.Context(), ReauthWindow); err != nil {
		// 403 with a reason a client can act on: re-prompt for the
		// password rather than send the user to a login page that will
		// tell them they are already signed in.
		return "", c.JSON(http.StatusForbidden, map[string]any{
			"error": map[string]string{
				"code":    "REAUTHENTICATION_REQUIRED",
				"message": "confirm your password again to change a second factor",
			},
		})
	}
	return accountID, nil
}

func handleTOTPBegin(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		accountID, err := requireFreshSignIn(s, c)
		if accountID == "" {
			return err
		}
		secret, uri, err := s.BeginTOTPEnrolment(c.Request.Context(), accountID)
		if err != nil {
			return mfaError(c, err)
		}
		// The secret is returned ONCE, to be shown as a QR code. It is
		// not readable again: the stored copy is encrypted and the
		// endpoint never decrypts it for a caller.
		return c.JSON(http.StatusOK, map[string]any{"secret": secret, "uri": uri})
	}
}

type codeRequest struct {
	Code string `json:"code"`
}

func handleTOTPConfirm(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		accountID, err := requireFreshSignIn(s, c)
		if accountID == "" {
			return err
		}
		var req codeRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		codes, err := s.ConfirmTOTPEnrolment(c.Request.Context(), accountID, req.Code)
		if err != nil {
			return mfaError(c, err)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"recovery_codes": codes,
			"note":           "store these now: they are shown once and cannot be recovered",
		})
	}
}

func handleTOTPDisable(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		accountID, err := requireFreshSignIn(s, c)
		if accountID == "" {
			return err
		}
		if err := s.DisableTOTP(c.Request.Context(), accountID); err != nil {
			return mfaError(c, err)
		}
		return c.NoContent()
	}
}

// handleMFAVerify completes a sign-in that stopped at the password step.
func handleMFAVerify(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		var req codeRequest
		if err := c.BindJSON(&req); err != nil {
			return c.JSON(http.StatusBadRequest, errorBody("request body must be valid JSON"))
		}
		account, err := s.CompleteSecondFactor(c.Request.Context(), req.Code)
		switch {
		case errors.Is(err, ErrAccountLocked):
			return c.JSON(http.StatusTooManyRequests, errorBody("too many attempts, try again later"))
		case errors.Is(err, ErrInvalidCode), errors.Is(err, ErrInvalidCredentials):
			return c.JSON(http.StatusUnauthorized, errorBody("invalid code"))
		case err != nil:
			return mfaError(c, err)
		}
		return c.JSON(http.StatusOK, map[string]any{"id": account.ID, "email": account.Email})
	}
}

func handleRecoveryCodes(s *Service) nucleus.Handler {
	return func(c *nucleus.Context) error {
		accountID, err := requireFreshSignIn(s, c)
		if accountID == "" {
			return err
		}
		codes, err := s.RegenerateRecoveryCodes(c.Request.Context(), accountID)
		if err != nil {
			return mfaError(c, err)
		}
		return c.JSON(http.StatusOK, map[string]any{
			"recovery_codes": codes,
			"note":           "the previous codes no longer work",
		})
	}
}

func mfaError(c *nucleus.Context, err error) error {
	switch {
	case errors.Is(err, ErrMFAUnavailable):
		// 501 and not 500: the deployment did not configure this, which
		// is a different thing from a failure.
		return c.JSON(http.StatusNotImplemented, errorBody(err.Error()))
	case errors.Is(err, ErrMFANotEnrolled):
		return c.JSON(http.StatusNotFound, errorBody("no second factor is enrolled"))
	case errors.Is(err, ErrInvalidCode):
		return c.JSON(http.StatusUnauthorized, errorBody("invalid code"))
	case errors.Is(err, ErrReauthenticationRequired):
		return c.JSON(http.StatusForbidden, errorBody("confirm your password again"))
	case errors.Is(err, ErrNotFound):
		return c.JSON(http.StatusNotFound, errorBody("no such account"))
	default:
		return err
	}
}

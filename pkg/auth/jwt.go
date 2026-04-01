package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	gferrors "github.com/jcsvwinston/GoFrame/pkg/errors"
	"github.com/jcsvwinston/GoFrame/pkg/observe"
)

type jwtCtxKey struct{}

// Claims holds the JWT payload with user identity information.
type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// JWTManager handles JWT token generation and validation.
type JWTManager struct {
	secret []byte
	expiry time.Duration
	issuer string
}

// NewJWTManager creates a JWT manager with the given secret and token expiry duration.
func NewJWTManager(secret string, expiry time.Duration, issuer ...string) *JWTManager {
	iss := "goframe"
	if len(issuer) > 0 && issuer[0] != "" {
		iss = issuer[0]
	}
	return &JWTManager{
		secret: []byte(secret),
		expiry: expiry,
		issuer: iss,
	}
}

// Generate creates a signed JWT token for the given user claims.
func (m *JWTManager) Generate(userID, username, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.expiry)),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("auth.JWTManager.Generate: %w", err)
	}
	return signed, nil
}

// Validate parses and validates a JWT token string. Returns the claims if valid.
func (m *JWTManager) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth.JWTManager.Validate: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("auth.JWTManager.Validate: invalid token claims")
	}
	return claims, nil
}

// Middleware returns an HTTP middleware that extracts and validates the JWT token
// from the Authorization header (Bearer scheme). On success, the claims are stored
// in the request context and can be retrieved via ClaimsFromContext.
func (m *JWTManager) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				gferrors.WriteError(w, gferrors.Unauthorized("missing authorization header"), nil)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				gferrors.WriteError(w, gferrors.Unauthorized("invalid authorization format, expected: Bearer <token>"), nil)
				return
			}

			claims, err := m.Validate(parts[1])
			if err != nil {
				gferrors.WriteError(w, gferrors.Unauthorized("invalid or expired token"), nil)
				return
			}

			ctx := context.WithValue(r.Context(), jwtCtxKey{}, claims)
			ctx = observe.CtxWithUserID(ctx, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext extracts JWT claims from the request context.
// Returns nil, false if no claims are present.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(jwtCtxKey{}).(*Claims)
	return claims, ok
}

// OptionalJWTMiddleware is like Middleware but does not reject requests without
// a token. If a valid token is present, claims are added to the context.
// If no token or an invalid token is present, the request proceeds without claims.
func (m *JWTManager) OptionalJWTMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
					if claims, err := m.Validate(parts[1]); err == nil {
						ctx := context.WithValue(r.Context(), jwtCtxKey{}, claims)
						ctx = observe.CtxWithUserID(ctx, claims.UserID)
						r = r.WithContext(ctx)
					}
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

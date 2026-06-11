package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

const (
	defaultAdminUsersTable = "nucleus_admin_users"

	adminSessionUserIDKey    = "__nucleus_admin_user_id"
	adminSessionUsernameKey  = "__nucleus_admin_username"
	adminSessionEmailKey     = "__nucleus_admin_email"
	adminSessionSuperuserKey = "__nucleus_admin_superuser"

	// dummyPasswordHash is a real bcrypt hash — cost 12, the same cost
	// auth.HashPassword stamps on stored credentials — of an unguessable
	// throwaway string. The unknown-username branch of handleLoginPOST
	// burns a compare against it so both rejection paths cost one bcrypt
	// verification: without it, unknown-username returned in microseconds
	// while wrong-password ran the full compare — a ~100-300 ms timing
	// oracle that let callers enumerate valid usernames despite identical
	// status and body (fleetdesk finding #17).
	dummyPasswordHash = "$2a$12$SOaD6FOzjSQzKXcT4CnGTuP7JjxYDbQ.noFqvqZaLfOSBUPF7TxSO"
)

// DatabaseAdminAuth is the default admin auth provider wired by pkg/app.
// Behavior:
// - Admin is always protected: login is required.
type DatabaseAdminAuth struct {
	db      *sql.DB
	session *auth.SessionManager
	prefix  string
	table   string
}

// NewDatabaseAdminAuth creates a DB-backed AdminAuth provider that validates
// credentials against nucleus_admin_users (same table used by createuser).
func NewDatabaseAdminAuth(sqlDB *sql.DB, session *auth.SessionManager, prefix string) *DatabaseAdminAuth {
	return &DatabaseAdminAuth{
		db:      sqlDB,
		session: session,
		prefix:  normalizeAdminPrefix(prefix),
		table:   defaultAdminUsersTable,
	}
}

func normalizeAdminPrefix(raw string) string {
	return NormalizePrefix(raw)
}

// Authenticate returns an authenticated admin user from server-side session.
func (a *DatabaseAdminAuth) Authenticate(r *http.Request) (*auth.User, error) {
	if a == nil || a.db == nil {
		return nil, errors.New("admin auth provider is not configured")
	}
	if r == nil {
		return nil, errors.New("missing request")
	}

	if a.session == nil {
		return nil, errors.New("admin session manager is not configured")
	}
	if !sessionContextReady(a.session, r.Context()) {
		return nil, errors.New("admin session context is not available")
	}

	userID := strings.TrimSpace(a.session.GetString(r.Context(), adminSessionUserIDKey))
	if userID == "" {
		return nil, errors.New("admin authentication required")
	}

	record, found, err := a.findUserByID(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	if !found {
		_ = a.session.Destroy(r.Context())
		return nil, errors.New("admin session is no longer valid")
	}
	return record.toUser(), nil
}

// Authorize currently allows all actions for authenticated admin users.
func (a *DatabaseAdminAuth) Authorize(user *auth.User, _ string, _ string) bool {
	return user != nil && strings.TrimSpace(user.ID) != ""
}

// LoginHandler renders the login page (GET) and validates credentials (POST).
func (a *DatabaseAdminAuth) LoginHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			a.handleLoginGET(w, r)
		case http.MethodPost:
			a.handleLoginPOST(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
	})
}

type adminLoginUserRecord struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	IsSuperuser  bool
}

func (u adminLoginUserRecord) toUser() *auth.User {
	return &auth.User{
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		Role:        "admin",
		IsSuperuser: u.IsSuperuser,
	}
}

func (a *DatabaseAdminAuth) handleLoginGET(w http.ResponseWriter, r *http.Request) {
	next := a.sanitizeNext(r.URL.Query().Get("next"))
	a.renderLoginPage(w, http.StatusOK, next, "", "")
}

func (a *DatabaseAdminAuth) handleLoginPOST(w http.ResponseWriter, r *http.Request) {
	next := a.sanitizeNext(r.URL.Query().Get("next"))
	if err := r.ParseForm(); err != nil {
		a.renderLoginPage(w, http.StatusBadRequest, next, "Invalid form payload.", "")
		return
	}

	formNext := strings.TrimSpace(r.FormValue("next"))
	if formNext != "" {
		next = a.sanitizeNext(formNext)
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || strings.TrimSpace(password) == "" {
		a.renderLoginPage(w, http.StatusBadRequest, next, "Username and password are required.", "")
		return
	}

	record, found, err := a.findUserByLogin(r.Context(), username)
	if err != nil {
		http.Error(w, "admin login query failed", http.StatusInternalServerError)
		return
	}

	if found {
		if !auth.CheckPassword(password, record.PasswordHash) {
			a.renderLoginPage(w, http.StatusUnauthorized, next, "Invalid credentials.", "")
			return
		}
	} else {
		// Equalize timing with the found-branch compare above; the result
		// is deliberately discarded (see dummyPasswordHash).
		auth.CheckPassword(password, dummyPasswordHash)
		a.renderLoginPage(w, http.StatusUnauthorized, next, "Invalid credentials.", "")
		return
	}

	if a.session == nil || !sessionContextReady(a.session, r.Context()) {
		http.Error(w, "session middleware is not configured for admin login", http.StatusInternalServerError)
		return
	}

	if err := a.session.RenewToken(r.Context()); err != nil {
		http.Error(w, "unable to renew session token", http.StatusInternalServerError)
		return
	}
	a.session.Put(r.Context(), adminSessionUserIDKey, record.ID)
	a.session.Put(r.Context(), adminSessionUsernameKey, record.Username)
	a.session.Put(r.Context(), adminSessionEmailKey, record.Email)
	if record.IsSuperuser {
		a.session.Put(r.Context(), adminSessionSuperuserKey, "1")
	} else {
		a.session.Put(r.Context(), adminSessionSuperuserKey, "0")
	}

	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (a *DatabaseAdminAuth) renderLoginPage(w http.ResponseWriter, status int, next, errorMsg, infoMsg string) {
	adminPrefix := a.prefix
	if adminPrefix == "" {
		adminPrefix = DefaultPrefix
	}

	// Login responses must never be cached: a cached 401 would replay a
	// stale error banner on a later clean navigation.
	w.Header().Set("Cache-Control", "no-store")

	if loginUIContent, ok := adminUIBuildFS(); ok {
		content, err := fs.ReadFile(loginUIContent, "index.html")
		if err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(status)
			out := injectAdminPrefix(content, adminPrefix)
			out = injectLoginMessage(out, errorMsg, infoMsg)
			_, _ = w.Write(out)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(fallbackLoginPage(adminPrefix, next, errorMsg, infoMsg)))
}

func fallbackLoginPage(adminPrefix, next, errorMsg, infoMsg string) string {
	var message string
	if errorMsg != "" {
		message = `<p role="alert">` + html.EscapeString(errorMsg) + `</p>`
	} else if infoMsg != "" {
		message = `<p>` + html.EscapeString(infoMsg) + `</p>`
	}
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <meta name="nucleus-admin-prefix" content="` + html.EscapeString(adminPrefix) + `">
  <title>Nucleus Admin Login</title>
  <style>
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; font-family: system-ui, sans-serif; background: #f6f7f9; color: #15171a; }
    main { width: min(360px, calc(100vw - 32px)); background: #fff; border: 1px solid #d8dde6; border-radius: 8px; padding: 24px; box-shadow: 0 8px 24px rgba(15, 23, 42, 0.08); }
    h1 { margin: 0 0 20px; font-size: 1.25rem; }
    label { display: block; margin-top: 14px; font-weight: 600; }
    input { box-sizing: border-box; width: 100%; margin-top: 6px; padding: 10px 12px; border: 1px solid #b9c0cc; border-radius: 6px; font: inherit; }
    button { width: 100%; margin-top: 20px; padding: 11px 12px; border: 0; border-radius: 6px; background: #1f6feb; color: #fff; font: inherit; font-weight: 700; cursor: pointer; }
    p[role="alert"] { padding: 10px 12px; border-radius: 6px; background: #fff1f2; color: #9f1239; }
  </style>
</head>
<body>
  <main>
    <h1>Nucleus Admin</h1>
    ` + message + `
    <form method="post">
      <input type="hidden" name="next" value="` + html.EscapeString(next) + `">
      <label for="username">Username or email</label>
      <input id="username" name="username" autocomplete="username" required>
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
      <button type="submit">Sign in</button>
    </form>
  </main>
</body>
</html>`
}

func (a *DatabaseAdminAuth) sanitizeNext(raw string) string {
	fallback := a.prefix + "/"
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	if strings.Contains(value, "://") {
		return fallback
	}
	if !strings.HasPrefix(value, a.prefix) {
		return fallback
	}
	return value
}

func (a *DatabaseAdminAuth) findUserByID(ctx context.Context, id string) (adminLoginUserRecord, bool, error) {
	users, tableReady, err := a.listUsers(ctx)
	if err != nil {
		return adminLoginUserRecord{}, false, err
	}
	if !tableReady {
		return adminLoginUserRecord{}, false, nil
	}

	target := strings.TrimSpace(id)
	for _, user := range users {
		if strings.TrimSpace(user.ID) == target {
			return user, true, nil
		}
	}
	return adminLoginUserRecord{}, false, nil
}

func (a *DatabaseAdminAuth) findUserByLogin(ctx context.Context, login string) (adminLoginUserRecord, bool, error) {
	users, tableReady, err := a.listUsers(ctx)
	if err != nil {
		return adminLoginUserRecord{}, false, err
	}
	if !tableReady {
		return adminLoginUserRecord{}, false, nil
	}

	target := strings.TrimSpace(login)
	for _, user := range users {
		if strings.EqualFold(strings.TrimSpace(user.Username), target) || strings.EqualFold(strings.TrimSpace(user.Email), target) {
			return user, true, nil
		}
	}
	return adminLoginUserRecord{}, false, nil
}

func (a *DatabaseAdminAuth) listUsers(ctx context.Context) ([]adminLoginUserRecord, bool, error) {
	if a == nil || a.db == nil {
		return nil, false, errors.New("admin database is not configured")
	}

	query := fmt.Sprintf("SELECT id, username, email, password_hash, is_superuser FROM %s", a.table)
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		if isAdminUserTableMissing(err) {
			return nil, false, nil
		}
		return nil, true, fmt.Errorf("query admin users: %w", err)
	}
	defer rows.Close()

	users := make([]adminLoginUserRecord, 0, 8)
	for rows.Next() {
		var u adminLoginUserRecord
		var superRaw interface{}
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &superRaw); err != nil {
			return nil, true, fmt.Errorf("scan admin user row: %w", err)
		}
		u.ID = strings.TrimSpace(u.ID)
		u.Username = strings.TrimSpace(u.Username)
		u.Email = strings.TrimSpace(u.Email)
		u.IsSuperuser = parseAdminSuperuserValue(superRaw)
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("iterate admin users: %w", err)
	}

	return users, true, nil
}

func parseAdminSuperuserValue(raw interface{}) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int8:
		return v != 0
	case int16:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case uint:
		return v != 0
	case uint8:
		return v != 0
	case uint16:
		return v != 0
	case uint32:
		return v != 0
	case uint64:
		return v != 0
	case []byte:
		return parseAdminSuperuserString(string(v))
	case string:
		return parseAdminSuperuserString(v)
	default:
		return parseAdminSuperuserString(fmt.Sprintf("%v", raw))
	}
}

func parseAdminSuperuserString(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

func isAdminUserTableMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such table") ||
		strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "unknown table") ||
		strings.Contains(msg, "invalid object name") ||
		strings.Contains(msg, "ora-00942")
}

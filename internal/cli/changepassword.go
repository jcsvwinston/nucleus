package cli

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/auth"
)

func runChangePassword(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("changepassword", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	usernameFlag := fs.String("username", "", "Username to update")
	password := fs.String("password", "", "New password (plaintext)")
	noInput := fs.Bool("no-input", false, "Disable interactive prompts")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	username, err := resolveChangePasswordUsername(*usernameFlag, fs.Args())
	if err != nil {
		return err
	}
	if err := validateUsername(username); err != nil {
		return err
	}

	if !*noInput && strings.TrimSpace(*password) == "" {
		reader := bufio.NewReader(stdin)
		line, err := promptLine(reader, stdout, "Password: ")
		if err != nil {
			return err
		}
		*password = line
	}

	if strings.TrimSpace(*password) == "" {
		return fmt.Errorf("password is required (use --no-input with --password in CI)")
	}
	if err := validatePassword(*password); err != nil {
		return err
	}

	cfg, database, _, cleanup, err := newDatabaseWithAlias(*configPath, *databaseAlias)
	if err != nil {
		return err
	}
	defer cleanup()

	// Refuse before writing a hash nobody will read.
	//
	// With an auth_backends chain configured, the panel authenticates
	// through it. If the local backend is not in that chain, the local
	// password_hash is never consulted — and this command used to write it,
	// print "Password updated" and exit 0 anyway (QCD-FW-27). The operator
	// walks away believing access is restored. Exiting 0 without producing
	// the effect the user believes they got is bad everywhere; on the
	// command someone reaches for when they are locked out it is the worst
	// place for it.
	if err := refuseChangePasswordUnderChain(cfg.AuthBackends); err != nil {
		return err
	}

	sqlDB, err := database.SqlDB()
	if err != nil {
		return fmt.Errorf("open sql handle: %w", err)
	}
	// The nucleus_admin_users table is owned by the orbit module
	// (ADR-019). Require that orbit has already initialised the schema so
	// a missing table surfaces as an actionable "orbit not installed"
	// error instead of a raw SQL "no such table" failure.
	if err := requireOrbitAdminSchema(sqlDB, database.System(), "changepassword"); err != nil {
		return err
	}

	userID, err := findAdminUserIDByUsername(sqlDB, database.System(), username)
	if err != nil {
		return err
	}
	if userID == "" {
		return fmt.Errorf("admin user %q not found", username)
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	update := fmt.Sprintf(
		"UPDATE %s SET password_hash = %s, updated_at = %s WHERE id = %s",
		adminUsersTable,
		quoteSQLString(hash),
		quoteSQLString(nowRFC3339()),
		quoteSQLString(userID),
	)
	if _, err := sqlDB.Exec(update); err != nil {
		return fmt.Errorf("update admin password: %w", err)
	}

	return writeCommandStatus(stdout, "changepassword", "ok", fmt.Sprintf("Password updated: %s", username), map[string]interface{}{
		"username": username,
	})
}

func resolveChangePasswordUsername(usernameFlag string, positional []string) (string, error) {
	username := strings.TrimSpace(usernameFlag)
	if username == "" {
		if len(positional) != 1 {
			return "", fmt.Errorf("usage: nucleus changepassword [--config nucleus.yml] [--password xxx] [--no-input] <username>")
		}
		username = strings.TrimSpace(positional[0])
	} else if len(positional) > 0 {
		return "", fmt.Errorf("changepassword accepts either positional username or --username, not both")
	}

	if username == "" {
		return "", fmt.Errorf("username cannot be empty")
	}
	return username, nil
}

// findAdminUserIDByUsernameSQL builds the changepassword lookup for the
// given dialect. See selectOneAdminUserIDSQL for the mssql TOP 1 branch.
func findAdminUserIDByUsernameSQL(dialect, username string) string {
	where := fmt.Sprintf("username = %s", quoteSQLString(username))
	return selectOneAdminUserIDSQL(dialect, where)
}

func findAdminUserIDByUsername(sqlDB *sql.DB, dialect, username string) (string, error) {
	query := findAdminUserIDByUsernameSQL(dialect, username)
	var id string
	if err := sqlDB.QueryRow(query).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("lookup admin user by username: %w", err)
	}
	return id, nil
}

// localAuthBackendName is the name App gives the application's own user
// table when WithUserProvider does not override it.
const localAuthBackendName = "local"

// refuseChangePasswordUnderChain reports why the local password hash would
// be ignored, or nil when it is still consulted.
//
// The rule is narrow on purpose. A chain that CONTAINS the local backend
// still reads the hash — after the rejection-ends-the-attempt fix that is
// the break-glass path, which makes it more important, not less — so
// refusing there would break the recovery this command exists for.
func refuseChangePasswordUnderChain(backends []string) error {
	if len(backends) == 0 {
		return nil
	}
	for _, b := range backends {
		if strings.EqualFold(strings.TrimSpace(b), localAuthBackendName) {
			return nil
		}
	}
	return fmt.Errorf(
		"refusing to write a password hash nothing will read: auth_backends is configured as [%s] and does not include %q, "+
			"so the panel authenticates through that chain and never consults the local password_hash. "+
			"Change the password in the identity source the chain names, or add %q to auth_backends "+
			"(if your UserProvider is registered under another name, list THAT name there) and run this again",
		strings.Join(backends, ", "), localAuthBackendName, localAuthBackendName)
}

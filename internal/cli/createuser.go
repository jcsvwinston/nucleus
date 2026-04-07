package cli

import (
	"bufio"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/jcsvwinston/GoFrame/pkg/auth"
)

const adminUsersTable = "goframe_admin_users"

var usernameRegex = regexp.MustCompile(`^[A-Za-z0-9._-]{3,64}$`)

func runCreateUser(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("createuser", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	username := fs.String("username", "", "Username")
	email := fs.String("email", "", "Email")
	password := fs.String("password", "", "Password (plaintext)")
	superuser := fs.Bool("superuser", true, "Create as superuser")
	noInput := fs.Bool("no-input", false, "Disable interactive prompts")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("createuser does not accept positional arguments")
	}

	if !*noInput {
		if err := promptMissingUserFields(stdin, stdout, username, email, password); err != nil {
			return err
		}
	}

	if strings.TrimSpace(*username) == "" || strings.TrimSpace(*email) == "" || strings.TrimSpace(*password) == "" {
		return fmt.Errorf("username, email and password are required (use --no-input with explicit flags in CI)")
	}
	if err := validateUsername(*username); err != nil {
		return err
	}
	if err := validateEmail(*email); err != nil {
		return err
	}
	if err := validatePassword(*password); err != nil {
		return err
	}

	_, database, _, cleanup, err := newDatabaseWithAlias(*configPath, *databaseAlias)
	if err != nil {
		return err
	}
	defer cleanup()

	sqlDB, err := database.SqlDB()
	if err != nil {
		return fmt.Errorf("open sql handle: %w", err)
	}

	if err := ensureAdminUsersTable(sqlDB); err != nil {
		return err
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	now := nowRFC3339()
	existingID, err := findExistingAdminUserID(sqlDB, *username, *email)
	if err != nil {
		return err
	}

	if existingID == "" {
		id := newUserID()
		insert := fmt.Sprintf(
			"INSERT INTO %s (id, username, email, password_hash, is_superuser, created_at, updated_at) VALUES (%s, %s, %s, %s, %d, %s, %s)",
			adminUsersTable,
			quoteSQLString(id),
			quoteSQLString(*username),
			quoteSQLString(*email),
			quoteSQLString(hash),
			boolToInt(*superuser),
			quoteSQLString(now),
			quoteSQLString(now),
		)
		if _, err := sqlDB.Exec(insert); err != nil {
			return fmt.Errorf("insert admin user: %w", err)
		}
		fmt.Fprintf(stdout, "Admin user created: %s\n", *username)
		return nil
	}

	update := fmt.Sprintf(
		"UPDATE %s SET username = %s, email = %s, password_hash = %s, is_superuser = %d, updated_at = %s WHERE id = %s",
		adminUsersTable,
		quoteSQLString(*username),
		quoteSQLString(*email),
		quoteSQLString(hash),
		boolToInt(*superuser),
		quoteSQLString(now),
		quoteSQLString(existingID),
	)
	if _, err := sqlDB.Exec(update); err != nil {
		return fmt.Errorf("update admin user: %w", err)
	}
	fmt.Fprintf(stdout, "Admin user updated: %s\n", *username)
	return nil
}

func promptMissingUserFields(stdin io.Reader, stdout io.Writer, username, email, password *string) error {
	reader := bufio.NewReader(stdin)
	if strings.TrimSpace(*username) == "" {
		line, err := promptLine(reader, stdout, "Username: ")
		if err != nil {
			return err
		}
		*username = line
	}
	if strings.TrimSpace(*email) == "" {
		line, err := promptLine(reader, stdout, "Email: ")
		if err != nil {
			return err
		}
		*email = line
	}
	if strings.TrimSpace(*password) == "" {
		line, err := promptLine(reader, stdout, "Password: ")
		if err != nil {
			return err
		}
		*password = line
	}
	return nil
}

func promptLine(reader *bufio.Reader, out io.Writer, label string) (string, error) {
	fmt.Fprint(out, label)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func validateUsername(username string) error {
	if !usernameRegex.MatchString(username) {
		return fmt.Errorf("invalid username %q (allowed: letters, digits, ., _, -, length 3-64)", username)
	}
	return nil
}

func validateEmail(email string) error {
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("invalid email %q", email)
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	return nil
}

func ensureAdminUsersTable(sqlDB *sql.DB) error {
	if err := validateSQLIdentifier(adminUsersTable); err != nil {
		return err
	}

	query := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
	id VARCHAR(64) PRIMARY KEY,
	username VARCHAR(191) NOT NULL UNIQUE,
	email VARCHAR(191) NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	is_superuser INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)
`, adminUsersTable)
	if _, err := sqlDB.Exec(query); err != nil {
		return fmt.Errorf("ensure admin users table: %w", err)
	}
	return nil
}

func findExistingAdminUserID(sqlDB *sql.DB, username, email string) (string, error) {
	query := fmt.Sprintf(
		"SELECT id FROM %s WHERE username = %s OR email = %s LIMIT 1",
		adminUsersTable,
		quoteSQLString(username),
		quoteSQLString(email),
	)
	var id string
	if err := sqlDB.QueryRow(query).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("find existing admin user: %w", err)
	}
	return id, nil
}

func newUserID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("u_%d", time.Now().UnixNano())
	}
	return "u_" + time.Now().UTC().Format("20060102150405") + "_" + hex.EncodeToString(buf)
}

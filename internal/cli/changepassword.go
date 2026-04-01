package cli

import (
	"bufio"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/jcsvwinston/GoFrame/pkg/auth"
)

func runChangePassword(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("changepassword", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
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

	_, database, cleanup, err := newDatabase(*configPath)
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

	userID, err := findAdminUserIDByUsername(sqlDB, username)
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

	fmt.Fprintf(stdout, "Password updated: %s\n", username)
	return nil
}

func resolveChangePasswordUsername(usernameFlag string, positional []string) (string, error) {
	username := strings.TrimSpace(usernameFlag)
	if username == "" {
		if len(positional) != 1 {
			return "", fmt.Errorf("usage: goframe changepassword [--config goframe.yaml] [--password xxx] [--no-input] <username>")
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

func findAdminUserIDByUsername(sqlDB *sql.DB, username string) (string, error) {
	query := fmt.Sprintf(
		"SELECT id FROM %s WHERE username = %s LIMIT 1",
		adminUsersTable,
		quoteSQLString(username),
	)
	var id string
	if err := sqlDB.QueryRow(query).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("lookup admin user by username: %w", err)
	}
	return id, nil
}

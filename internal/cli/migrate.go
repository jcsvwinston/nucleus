package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jcsvwinston/GoFrame/pkg/db"
)

func runMigrate(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	migrationsPath := fs.String("migrations", "migrations", "Migrations directory")
	force := fs.Bool("force", false, "Force destructive actions (recommended in CI)")
	yes := fs.Bool("yes", false, "Auto-confirm destructive actions without prompt")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	rest := fs.Args()
	action := "up"
	if len(rest) > 0 {
		action = strings.ToLower(rest[0])
		rest = rest[1:]
	}

	if action == "create" {
		if len(rest) != 1 {
			return fmt.Errorf("migrate create requires a migration name")
		}
		migrator := db.NewMigrator(nil, *migrationsPath, newSilentLogger())
		if err := migrator.Create(rest[0]); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Migration files created for %q\n", rest[0])
		return nil
	}

	if !isMigrateActionSupported(action) {
		return fmt.Errorf("unknown migrate action %q", action)
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	migrator, cleanup, err := newMigrator(*configPath, *migrationsPath)
	if err != nil {
		return err
	}
	defer cleanup()

	switch action {
	case "up":
		if len(rest) > 1 {
			return fmt.Errorf("migrate up accepts at most one optional argument (steps)")
		}
		steps, err := parseOptionalPositiveInt(rest, 1<<31-1)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		if err := migrator.Steps(steps); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Migrations applied")
		return nil

	case "down":
		if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "migrate down"); err != nil {
			return err
		}
		steps, err := parseOptionalPositiveInt(rest, 1)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		if err := migrator.Steps(-steps); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Rolled back %d migration(s)\n", steps)
		return nil

	case "steps":
		if len(rest) != 1 {
			return fmt.Errorf("migrate steps requires exactly one integer argument")
		}
		n, err := strconv.Atoi(rest[0])
		if err != nil {
			return fmt.Errorf("invalid steps value %q", rest[0])
		}
		if n == 0 {
			return fmt.Errorf("steps cannot be zero")
		}
		if n < 0 {
			if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "migrate steps (rollback)"); err != nil {
				return err
			}
		}
		if err := migrator.Steps(n); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Applied steps: %d\n", n)
		return nil

	case "status":
		if len(rest) != 0 {
			return fmt.Errorf("migrate status does not accept extra arguments")
		}
		status, err := migrator.Status()
		if err != nil {
			return err
		}
		if len(status) == 0 {
			fmt.Fprintln(stdout, "No migration files found")
			return nil
		}
		for _, s := range status {
			state := "pending"
			at := "-"
			if s.Applied {
				state = "applied"
				if s.AppliedAt != nil {
					at = s.AppliedAt.UTC().Format("2006-01-02T15:04:05Z")
				}
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", s.ID, state, at)
		}
		return nil

	case "reset":
		if len(rest) != 0 {
			return fmt.Errorf("migrate reset does not accept extra arguments")
		}
		if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "migrate reset"); err != nil {
			return err
		}
		status, err := migrator.Status()
		if err != nil {
			return err
		}
		toRollback := countApplied(status)
		if toRollback == 0 {
			fmt.Fprintln(stdout, "No applied migrations to rollback")
			return nil
		}
		if err := migrator.Steps(-toRollback); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "Rolled back %d migration(s)\n", toRollback)
		return nil

	case "refresh":
		if len(rest) != 0 {
			return fmt.Errorf("migrate refresh does not accept extra arguments")
		}
		if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "migrate refresh"); err != nil {
			return err
		}
		status, err := migrator.Status()
		if err != nil {
			return err
		}
		toRollback := countApplied(status)
		if toRollback > 0 {
			if err := migrator.Steps(-toRollback); err != nil {
				return err
			}
		}
		if err := migrator.Up(); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "Migrations refreshed")
		return nil

	default:
		return fmt.Errorf("unsupported migrate action %q", action)
	}
}

func isMigrateActionSupported(action string) bool {
	switch action {
	case "up", "down", "steps", "status", "reset", "refresh":
		return true
	default:
		return false
	}
}

func countApplied(status []db.MigrationStatus) int {
	count := 0
	for _, s := range status {
		if s.Applied {
			count++
		}
	}
	return count
}

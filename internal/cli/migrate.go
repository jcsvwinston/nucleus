package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/db"
)

func runMigrate(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to nucleus config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
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
	if len(rest) == 0 {
		// No implicit action: `nucleus migrate` used to run `up` silently,
		// so a typo in the action (or a bare invocation meant as a status
		// check) mutated the schema. The action is always spelled out.
		return fmt.Errorf("migrate requires an action: up, down, steps, status, drift, reset, refresh or create (see nucleus migrate --help)")
	}
	action := strings.ToLower(rest[0])
	rest = rest[1:]

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

	migrator, _, cleanup, err := newMigratorWithAlias(*configPath, *migrationsPath, *databaseAlias)
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
		// The directory on disk is the host's plan; modules that applied
		// their embedded migrations at start wrote `<module>/<id>` rows to
		// the same ledger and have no directory here. Both are shown, the
		// module rows grouped by their namespace, so the status is the
		// database's, not one directory's.
		ledger, err := migrator.Applied()
		if err != nil {
			return err
		}
		moduleRows := moduleLedgerRows(ledger)
		if len(status) == 0 && len(moduleRows) == 0 {
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
		for _, row := range moduleRows {
			fmt.Fprintf(stdout, "%s\tapplied\t%s\n", row.ID, row.AppliedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
		return nil

	case "drift":
		if len(rest) != 0 {
			return fmt.Errorf("migrate drift does not accept extra arguments")
		}
		drift, err := migrator.Drift()
		if err != nil {
			return err
		}
		if len(drift) == 0 {
			fmt.Fprintln(stdout, "No drift detected")
			return nil
		}
		for _, d := range drift {
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", d.ID, d.Kind, d.AppliedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
		// Non-zero exit so CI gates can detect drift programmatically.
		return fmt.Errorf("%d migration(s) drifted from disk", len(drift))

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
	case "up", "down", "steps", "status", "drift", "reset", "refresh":
		return true
	default:
		return false
	}
}

// moduleLedgerRows keeps the namespaced ledger rows (a module's embedded
// migrations, applied through its Runtime at start) grouped by namespace in
// sorted order. Unscoped rows are the host's own and already appear through
// Status — or through `migrate drift` when their file is gone.
func moduleLedgerRows(ledger []db.AppliedMigration) []db.AppliedMigration {
	rows := make([]db.AppliedMigration, 0, len(ledger))
	for _, row := range ledger {
		if row.Namespace != "" {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
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

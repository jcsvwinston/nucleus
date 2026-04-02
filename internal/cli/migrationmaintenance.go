package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type optimizeMigrationStats struct {
	OriginalStatements  int
	OptimizedStatements int
	RemovedStatements   int
}

func runOptimizeMigration(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("optimizemigration", flag.ContinueOnError)
	fs.SetOutput(stderr)

	migrationsPath := fs.String("migrations", "migrations", "Migrations directory")
	down := fs.Bool("down", false, "Optimize rollback SQL (.down.sql) instead of apply SQL (.up.sql)")
	write := fs.Bool("write", false, "Write optimized SQL back to the migration file")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) != 1 {
		return fmt.Errorf("usage: goframe optimizemigration [--migrations migrations] [--down] [--write] <migration_id_or_name>")
	}

	pairs, err := loadMigrationPairs(*migrationsPath)
	if err != nil {
		return err
	}
	mig, err := resolveMigrationRef(fs.Args()[0], pairs)
	if err != nil {
		return err
	}

	targetPath := mig.UpPath
	if *down {
		if mig.DownPath == "" {
			return fmt.Errorf("migration %q does not have a .down.sql file", mig.ID)
		}
		targetPath = mig.DownPath
	}

	body, err := os.ReadFile(targetPath)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", targetPath, err)
	}

	optimizedSQL, stats := optimizeMigrationSQL(string(body))
	if !*write {
		fmt.Fprint(stdout, optimizedSQL)
		return nil
	}

	if normalizeLineEndings(string(body)) == normalizeLineEndings(optimizedSQL) {
		fmt.Fprintf(stdout, "Migration already optimized: %s (%d statement(s))\n", targetPath, stats.OptimizedStatements)
		return nil
	}

	if err := os.WriteFile(targetPath, []byte(optimizedSQL), 0644); err != nil {
		return fmt.Errorf("write optimized migration %s: %w", targetPath, err)
	}

	fmt.Fprintf(
		stdout,
		"Migration optimized: %s (statements %d -> %d, removed %d)\n",
		targetPath,
		stats.OriginalStatements,
		stats.OptimizedStatements,
		stats.RemovedStatements,
	)
	return nil
}

func runSquashMigrations(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("squashmigrations", flag.ContinueOnError)
	fs.SetOutput(stderr)

	migrationsPath := fs.String("migrations", "migrations", "Migrations directory")
	fromRef := fs.String("from", "", "First migration in squash range (inclusive)")
	toRef := fs.String("to", "", "Last migration in squash range (inclusive)")
	name := fs.String("name", "", "Squashed migration name (default: squashed_<from>_to_<to>)")
	write := fs.Bool("write", false, "Write squashed migration files")
	archiveOld := fs.Bool("archive-old", false, "Move source migrations to migrations/.squashed/<new_id>/ after writing")
	force := fs.Bool("force", false, "Overwrite output files if they already exist")
	dryRun := fs.Bool("dry-run", false, "Print squash plan without writing files")
	printSQL := fs.Bool("print-sql", false, "Print generated up/down SQL in dry-run mode")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("squashmigrations does not accept positional arguments")
	}
	if strings.TrimSpace(*fromRef) == "" || strings.TrimSpace(*toRef) == "" {
		return fmt.Errorf("squashmigrations requires --from and --to")
	}

	pairs, err := loadMigrationPairs(*migrationsPath)
	if err != nil {
		return err
	}
	fromMig, err := resolveMigrationRef(*fromRef, pairs)
	if err != nil {
		return err
	}
	toMig, err := resolveMigrationRef(*toRef, pairs)
	if err != nil {
		return err
	}

	startIdx, endIdx, err := findMigrationRangeIndices(pairs, fromMig.ID, toMig.ID)
	if err != nil {
		return err
	}
	selected := pairs[startIdx : endIdx+1]

	squashName := toSnakeCase(*name)
	if squashName == "" {
		squashName = defaultSquashName(fromMig.ID, toMig.ID)
	}
	newID := time.Now().Format("20060102150405") + "_" + squashName
	upOutPath := filepath.Join(*migrationsPath, newID+".up.sql")
	downOutPath := filepath.Join(*migrationsPath, newID+".down.sql")

	upScript, downScript, err := buildSquashedSQL(selected)
	if err != nil {
		return err
	}

	shouldDryRun := *dryRun || !*write
	if shouldDryRun {
		fmt.Fprintf(
			stdout,
			"DRY-RUN\tSQUASHMIGRATIONS\tfrom=%s\tto=%s\tcount=%d\tnew_id=%s\n",
			fromMig.ID, toMig.ID, len(selected), newID,
		)
		fmt.Fprintf(stdout, "DRY-RUN\tOUTPUT\tUP\t%s\n", upOutPath)
		fmt.Fprintf(stdout, "DRY-RUN\tOUTPUT\tDOWN\t%s\n", downOutPath)
		if *archiveOld {
			fmt.Fprintf(stdout, "DRY-RUN\tARCHIVE\t%s\n", filepath.Join(*migrationsPath, ".squashed", newID))
		}
		if *printSQL {
			fmt.Fprintln(stdout, "-- SQUASHED UP SQL --")
			fmt.Fprint(stdout, upScript)
			fmt.Fprintln(stdout, "-- SQUASHED DOWN SQL --")
			fmt.Fprint(stdout, downScript)
		}
		return nil
	}

	if err := ensureDir(*migrationsPath); err != nil {
		return err
	}
	if err := writeFileIfNotExists(upOutPath, upScript, *force); err != nil {
		return err
	}
	if err := writeFileIfNotExists(downOutPath, downScript, *force); err != nil {
		return err
	}

	if *archiveOld {
		archiveDir := filepath.Join(*migrationsPath, ".squashed", newID)
		if err := ensureDir(archiveDir); err != nil {
			return err
		}
		for _, mig := range selected {
			if err := moveFileToDir(mig.UpPath, archiveDir, *force); err != nil {
				return err
			}
			if mig.DownPath != "" {
				if err := moveFileToDir(mig.DownPath, archiveDir, *force); err != nil {
					return err
				}
			}
		}
		fmt.Fprintf(stdout, "Squashed migrations written: %s and %s (archived %d migration(s) to %s)\n", upOutPath, downOutPath, len(selected), archiveDir)
		return nil
	}

	fmt.Fprintf(stdout, "Squashed migrations written: %s and %s\n", upOutPath, downOutPath)
	fmt.Fprintln(stdout, "Warning: source migrations are still present; archive or remove them to avoid duplicate application.")
	return nil
}

func optimizeMigrationSQL(sqlScript string) (string, optimizeMigrationStats) {
	statements := splitSQLStatements(sqlScript)
	seen := make(map[string]struct{}, len(statements))
	optimized := make([]string, 0, len(statements))

	stats := optimizeMigrationStats{}
	for _, stmt := range statements {
		trimmed := strings.TrimSpace(stmt)
		cleaned := stripLeadingSQLComments(trimmed)
		if cleaned == "" {
			continue
		}
		stats.OriginalStatements++
		key := normalizeSQLStatementKey(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		optimized = append(optimized, cleaned)
	}

	stats.OptimizedStatements = len(optimized)
	stats.RemovedStatements = stats.OriginalStatements - stats.OptimizedStatements

	if len(optimized) == 0 {
		return "-- no-op migration\n", stats
	}
	return renderSQLStatements(optimized), stats
}

func isCommentOnlySQL(stmt string) bool {
	trimmed := strings.TrimSpace(stmt)
	if trimmed == "" {
		return true
	}
	if strings.HasPrefix(trimmed, "--") {
		return true
	}
	return strings.HasPrefix(trimmed, "/*") && strings.HasSuffix(trimmed, "*/")
}

func stripLeadingSQLComments(stmt string) string {
	trimmed := strings.TrimSpace(stmt)
	for trimmed != "" {
		switch {
		case strings.HasPrefix(trimmed, "--"):
			newline := strings.Index(trimmed, "\n")
			if newline < 0 {
				return ""
			}
			trimmed = strings.TrimSpace(trimmed[newline+1:])
		case strings.HasPrefix(trimmed, "/*"):
			closing := strings.Index(trimmed, "*/")
			if closing < 0 {
				return ""
			}
			trimmed = strings.TrimSpace(trimmed[closing+2:])
		default:
			return trimmed
		}
	}
	return ""
}

func normalizeSQLStatementKey(stmt string) string {
	trimmed := strings.TrimSpace(stmt)
	trimmed = strings.TrimSuffix(trimmed, ";")
	parts := strings.Fields(strings.ToLower(trimmed))
	return strings.Join(parts, " ")
}

func normalizeLineEndings(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(s)
}

func findMigrationRangeIndices(pairs []migrationPair, fromID, toID string) (int, int, error) {
	start := -1
	end := -1
	for i, pair := range pairs {
		if pair.ID == fromID {
			start = i
		}
		if pair.ID == toID {
			end = i
		}
	}
	if start < 0 || end < 0 {
		return 0, 0, fmt.Errorf("unable to locate migration range")
	}
	if start > end {
		return 0, 0, fmt.Errorf("--from migration must be older than or equal to --to migration")
	}
	return start, end, nil
}

func defaultSquashName(fromID, toID string) string {
	from := migrationIDSuffix(fromID)
	to := migrationIDSuffix(toID)
	return toSnakeCase("squashed_" + from + "_to_" + to)
}

func migrationIDSuffix(id string) string {
	if idx := strings.Index(id, "_"); idx >= 0 && idx+1 < len(id) {
		return id[idx+1:]
	}
	return id
}

func buildSquashedSQL(selected []migrationPair) (string, string, error) {
	upStatements := make([]string, 0, len(selected)*4)
	for _, mig := range selected {
		raw, err := os.ReadFile(mig.UpPath)
		if err != nil {
			return "", "", fmt.Errorf("read up migration %s: %w", mig.ID, err)
		}
		upStatements = append(upStatements, extractSQLStatementsForSquash(string(raw))...)
	}
	upScript, _ := optimizeMigrationSQL(renderSQLStatements(upStatements))

	downStatements := make([]string, 0, len(selected)*4)
	for i := len(selected) - 1; i >= 0; i-- {
		mig := selected[i]
		if mig.DownPath == "" {
			continue
		}
		raw, err := os.ReadFile(mig.DownPath)
		if err != nil {
			return "", "", fmt.Errorf("read down migration %s: %w", mig.ID, err)
		}
		downStatements = append(downStatements, extractSQLStatementsForSquash(string(raw))...)
	}
	downScript, _ := optimizeMigrationSQL(renderSQLStatements(downStatements))

	return upScript, downScript, nil
}

func extractSQLStatementsForSquash(script string) []string {
	rawStatements := splitSQLStatements(script)
	out := make([]string, 0, len(rawStatements))
	for _, stmt := range rawStatements {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" || isCommentOnlySQL(trimmed) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func moveFileToDir(srcPath, dstDir string, force bool) error {
	if strings.TrimSpace(srcPath) == "" {
		return nil
	}
	dstPath := filepath.Join(dstDir, filepath.Base(srcPath))
	if force {
		_ = os.Remove(dstPath)
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("move %s -> %s: %w", srcPath, dstPath, err)
	}
	return nil
}

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runSeed(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	databaseAlias := fs.String("database", "", "Database alias to use (defaults to database_default)")
	seedsDir := fs.String("seeds", "seeds", "Directory containing .sql seed files")
	singleFile := fs.String("file", "", "Run a single seed file (absolute path or relative to --seeds)")
	dryRun := fs.Bool("dry-run", false, "Print seed execution plan without running SQL")
	force := fs.Bool("force", false, "Force seed execution in production/CI")
	yes := fs.Bool("yes", false, "Auto-confirm seed execution without prompt")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("seed does not accept positional arguments")
	}

	files, err := resolveSeedFiles(*seedsDir, *singleFile)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintln(stdout, "No seed files found")
		return nil
	}

	cfg, database, _, cleanup, err := newDatabaseWithAlias(*configPath, *databaseAlias)
	if err != nil {
		return err
	}
	defer cleanup()

	sqlDB, err := database.SqlDB()
	if err != nil {
		return fmt.Errorf("open sql handle: %w", err)
	}

	if !*dryRun {
		if err := requireDangerousApproval(cfg, stdin, stdout, *force, *yes, "seed"); err != nil {
			return err
		}
	}

	executed := 0
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read seed file %s: %w", file, err)
		}

		if *dryRun {
			fmt.Fprintf(stdout, "DRY-RUN\t%s\n", file)
			executed++
			continue
		}

		if err := executeSQLScript(sqlDB, string(body)); err != nil {
			return fmt.Errorf("execute seed %s: %w", file, err)
		}
		fmt.Fprintf(stdout, "APPLIED\t%s\n", file)
		executed++
	}

	if *dryRun {
		fmt.Fprintf(stdout, "Planned %d seed file(s)\n", executed)
	} else {
		fmt.Fprintf(stdout, "Executed %d seed file(s)\n", executed)
	}
	return nil
}

func resolveSeedFiles(seedsDir, singleFile string) ([]string, error) {
	if singleFile != "" {
		path := singleFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(seedsDir, singleFile)
		}
		if !strings.HasSuffix(strings.ToLower(path), ".sql") {
			return nil, fmt.Errorf("seed file must use .sql extension: %s", path)
		}
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("seed file not found: %s", path)
		}
		return []string{path}, nil
	}

	if err := os.MkdirAll(seedsDir, 0755); err != nil {
		return nil, fmt.Errorf("create seeds directory %s: %w", seedsDir, err)
	}
	entries, err := os.ReadDir(seedsDir)
	if err != nil {
		return nil, fmt.Errorf("read seeds directory %s: %w", seedsDir, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}
		files = append(files, filepath.Join(seedsDir, name))
	}
	sort.Strings(files)
	return files, nil
}

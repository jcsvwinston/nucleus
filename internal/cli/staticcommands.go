package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type staticCopyPlanItem struct {
	relativePath string
	sourcePath   string
	targetPath   string
}

func runCollectStatic(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("collectstatic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "collectstatic")

	configPath := fs.String("config", "", "Path to nucleus config file")
	outputDir := fs.String("output", "", "Destination directory (defaults to config static_root)")
	sourceRaw := fs.String("source", "", "Comma-separated source directories")
	noDefaultSources := fs.Bool("no-default-sources", false, "Disable default static source discovery")
	clearOutput := fs.Bool("clear", false, "Delete output directory before collecting")
	dryRun := fs.Bool("dry-run", false, "Print copy plan without writing files")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("collectstatic does not accept positional arguments")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	resolvedOutput := strings.TrimSpace(*outputDir)
	if resolvedOutput == "" {
		resolvedOutput = strings.TrimSpace(cfg.StaticRoot)
	}
	if resolvedOutput == "" {
		return fmt.Errorf("static output directory is required (set --output or config static_root)")
	}
	resolvedOutput = filepath.Clean(resolvedOutput)

	if *clearOutput && isDangerousStaticOutput(resolvedOutput) {
		return fmt.Errorf("refusing to clear dangerous output path %q", resolvedOutput)
	}

	sources, err := resolveStaticSources(*sourceRaw, !*noDefaultSources, resolvedOutput)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("no static source directories found (use --source or create static directories)")
	}

	plan, duplicates, err := buildCollectStaticPlan(sources, resolvedOutput)
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		fmt.Fprintf(stdout, "No static files to collect from %d source directory(ies)\n", len(sources))
		return nil
	}

	for _, dup := range duplicates {
		fmt.Fprintf(stderr, "warning: duplicate static path %q ignored (kept %s, skipped %s)\n", dup.relativePath, dup.sourcePath, dup.targetPath)
	}

	if *dryRun {
		if *clearOutput {
			fmt.Fprintf(stdout, "DRY-RUN\tCOLLECTSTATIC\tclear=%s\n", resolvedOutput)
		}
		for _, item := range plan {
			fmt.Fprintf(stdout, "DRY-RUN\tCOLLECTSTATIC\tcopy=%s\tto=%s\n", item.sourcePath, item.targetPath)
		}
		fmt.Fprintf(stdout, "Planned collectstatic: files=%d output=%s sources=%d\n", len(plan), resolvedOutput, len(sources))
		return nil
	}

	if *clearOutput {
		if err := os.RemoveAll(resolvedOutput); err != nil {
			return fmt.Errorf("clear output directory %s: %w", resolvedOutput, err)
		}
	}
	if err := ensureDir(resolvedOutput); err != nil {
		return err
	}

	for _, item := range plan {
		if err := copyStaticFile(item.sourcePath, item.targetPath); err != nil {
			return err
		}
	}

	fmt.Fprintf(stdout, "Collected static files: %d (output=%s sources=%d)\n", len(plan), resolvedOutput, len(sources))
	return nil
}

func runFindStatic(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("findstatic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "findstatic")

	configPath := fs.String("config", "", "Path to nucleus config file")
	sourceRaw := fs.String("source", "", "Comma-separated source directories")
	noDefaultSources := fs.Bool("no-default-sources", false, "Disable default static source discovery")
	first := fs.Bool("first", false, "Return only the first match for each query")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	queries := fs.Args()
	if len(queries) == 0 {
		return fmt.Errorf("findstatic requires at least one asset path (example: nucleus findstatic app.css)")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	sources, err := resolveStaticSources(*sourceRaw, !*noDefaultSources, strings.TrimSpace(cfg.StaticRoot))
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("no static source directories found (use --source or create static directories)")
	}

	type staticQueryResult struct {
		Query   string   `json:"query"`
		Matches []string `json:"matches"`
	}
	results := make([]staticQueryResult, 0, len(queries))

	missing := 0
	for _, query := range queries {
		q := strings.TrimSpace(query)
		if q == "" {
			return fmt.Errorf("findstatic queries cannot be empty")
		}
		matches, err := findStaticMatches(sources, q, *first)
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			missing++
		}
		results = append(results, staticQueryResult{Query: q, Matches: matches})
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			return err
		}
	} else {
		for _, result := range results {
			fmt.Fprintf(stdout, "%s\n", result.Query)
			if len(result.Matches) == 0 {
				fmt.Fprintln(stdout, "  (not found)")
				continue
			}
			for _, path := range result.Matches {
				fmt.Fprintf(stdout, "  %s\n", path)
			}
		}
	}

	if missing > 0 {
		return fmt.Errorf("findstatic could not resolve %d query(s)", missing)
	}
	return nil
}

func isDangerousStaticOutput(path string) bool {
	normalized := filepath.Clean(strings.TrimSpace(path))
	switch normalized {
	case "", ".", string(filepath.Separator):
		return true
	default:
		return false
	}
}

func resolveStaticSources(sourceRaw string, includeDefaults bool, outputDir string) ([]string, error) {
	candidates := make([]string, 0, 8)
	if includeDefaults {
		candidates = append(candidates, "static", filepath.Join("internal", "web", "static"))
		globbed, err := filepath.Glob(filepath.Join("internal", "*", "web", "static"))
		if err != nil {
			return nil, fmt.Errorf("discover static sources: %w", err)
		}
		sort.Strings(globbed)
		candidates = append(candidates, globbed...)
	}

	for _, raw := range strings.Split(sourceRaw, ",") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		candidates = append(candidates, trimmed)
	}

	outputAbs := ""
	if strings.TrimSpace(outputDir) != "" {
		abs, err := filepath.Abs(filepath.Clean(outputDir))
		if err != nil {
			return nil, fmt.Errorf("resolve output directory %s: %w", outputDir, err)
		}
		outputAbs = abs
	}

	seen := make(map[string]struct{}, len(candidates))
	sources := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		cleaned := filepath.Clean(candidate)
		if cleaned == "" || cleaned == "." {
			continue
		}

		info, err := os.Stat(cleaned)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read static source %s: %w", cleaned, err)
		}
		if !info.IsDir() {
			continue
		}

		abs, err := filepath.Abs(cleaned)
		if err != nil {
			return nil, fmt.Errorf("resolve static source %s: %w", cleaned, err)
		}
		if outputAbs != "" && abs == outputAbs {
			continue
		}
		if _, exists := seen[abs]; exists {
			continue
		}
		seen[abs] = struct{}{}
		sources = append(sources, cleaned)
	}

	return sources, nil
}

func buildCollectStaticPlan(sources []string, outputDir string) ([]staticCopyPlanItem, []staticCopyPlanItem, error) {
	plannedByRel := make(map[string]staticCopyPlanItem, 64)
	plan := make([]staticCopyPlanItem, 0, 64)
	duplicates := make([]staticCopyPlanItem, 0, 8)

	for _, sourceDir := range sources {
		err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}

			rel, err := filepath.Rel(sourceDir, path)
			if err != nil {
				return fmt.Errorf("compute relative static path for %s: %w", path, err)
			}
			rel = filepath.Clean(rel)
			if rel == "." {
				return nil
			}
			key := filepath.ToSlash(rel)
			target := filepath.Join(outputDir, rel)

			item := staticCopyPlanItem{
				relativePath: key,
				sourcePath:   path,
				targetPath:   target,
			}

			if existing, exists := plannedByRel[key]; exists {
				duplicates = append(duplicates, staticCopyPlanItem{
					relativePath: key,
					sourcePath:   existing.sourcePath,
					targetPath:   path,
				})
				return nil
			}

			plannedByRel[key] = item
			plan = append(plan, item)
			return nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("scan static source %s: %w", sourceDir, err)
		}
	}

	sort.Slice(plan, func(i, j int) bool {
		return plan[i].relativePath < plan[j].relativePath
	})
	return plan, duplicates, nil
}

func copyStaticFile(sourcePath, targetPath string) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open static source %s: %w", sourcePath, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat static source %s: %w", sourcePath, err)
	}

	if err := ensureDir(filepath.Dir(targetPath)); err != nil {
		return err
	}
	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("create static target %s: %w", targetPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy static %s -> %s: %w", sourcePath, targetPath, err)
	}
	return nil
}

func findStaticMatches(sources []string, query string, firstOnly bool) ([]string, error) {
	query = filepath.Clean(filepath.FromSlash(strings.TrimSpace(query)))
	if query == "" || query == "." {
		return nil, fmt.Errorf("invalid static query %q", query)
	}

	out := make([]string, 0, len(sources))
	seen := map[string]struct{}{}
	globQuery := hasGlobMeta(query)

	for _, sourceDir := range sources {
		candidates := make([]string, 0, 4)
		if globQuery {
			pattern := filepath.Join(sourceDir, query)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				return nil, fmt.Errorf("glob static query %q in %s: %w", query, sourceDir, err)
			}
			candidates = append(candidates, matches...)
		} else {
			candidates = append(candidates, filepath.Join(sourceDir, query))
		}

		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("read static file %s: %w", candidate, err)
			}
			if !info.Mode().IsRegular() {
				continue
			}

			abs, err := filepath.Abs(candidate)
			if err != nil {
				return nil, fmt.Errorf("resolve static match %s: %w", candidate, err)
			}
			if _, exists := seen[abs]; exists {
				continue
			}
			seen[abs] = struct{}{}
			out = append(out, filepath.Clean(candidate))
			if firstOnly {
				return out, nil
			}
		}
	}

	sort.Strings(out)
	return out, nil
}

func hasGlobMeta(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

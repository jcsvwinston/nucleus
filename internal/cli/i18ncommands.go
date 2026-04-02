package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const defaultMessagesDomain = "messages"

var messageExtractPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:_|T|Tr|Translate|I18n)\(\s*"((?:\\.|[^"\\])*)"\s*\)`),
	regexp.MustCompile(`\{\{\s*(?:t|trans|i18n)\s+"((?:\\.|[^"\\])*)"\s*\}\}`),
}

type messageLocation struct {
	File string
	Line int
}

type messageCatalogEntry struct {
	ID        string
	Locations []messageLocation
}

type compileMessageJob struct {
	poPath   string
	jsonPath string
	locale   string
}

type compiledMessages struct {
	Locale  string            `json:"locale"`
	Domain  string            `json:"domain"`
	Entries map[string]string `json:"entries"`
}

func runMakeMessages(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("makemessages", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	locale := fs.String("locale", "", "Locale code (defaults to config default_locale)")
	domain := fs.String("domain", defaultMessagesDomain, "Messages domain (file name without extension)")
	inputRoot := fs.String("input", ".", "Root directory to scan for translatable strings")
	localesPathFlag := fs.String("locales-path", "", "Locales root path override (defaults to config locales_path)")
	outputPath := fs.String("output", "", "Output .po file path (optional)")
	extensionsRaw := fs.String("extensions", ".go,.html,.tmpl,.templ", "Comma-separated file extensions to scan")
	dryRun := fs.Bool("dry-run", false, "Print extraction summary without writing files")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("makemessages does not accept positional arguments")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	resolvedLocale := strings.TrimSpace(*locale)
	if resolvedLocale == "" {
		resolvedLocale = strings.TrimSpace(cfg.DefaultLocale)
	}
	if resolvedLocale == "" {
		return fmt.Errorf("locale is required (set --locale or config default_locale)")
	}

	resolvedDomain := strings.TrimSpace(*domain)
	if resolvedDomain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	resolvedInput := strings.TrimSpace(*inputRoot)
	if resolvedInput == "" {
		resolvedInput = "."
	}

	extensions, err := parseMessageExtensions(*extensionsRaw)
	if err != nil {
		return err
	}

	entries, scannedFiles, err := collectMessageCatalog(resolvedInput, extensions)
	if err != nil {
		return err
	}

	localesPath := strings.TrimSpace(*localesPathFlag)
	if localesPath == "" {
		localesPath = strings.TrimSpace(cfg.LocalesPath)
	}
	if localesPath == "" {
		localesPath = "locales"
	}

	targetPath := strings.TrimSpace(*outputPath)
	if targetPath == "" {
		targetPath = filepath.Join(localesPath, resolvedLocale, "LC_MESSAGES", resolvedDomain+".po")
	}

	if *dryRun {
		fmt.Fprintf(stdout, "DRY-RUN\tMAKEMESSAGES\tlocale=%s\tdomain=%s\tmessages=%d\tfiles=%d\toutput=%s\n", resolvedLocale, resolvedDomain, len(entries), scannedFiles, targetPath)
		return nil
	}

	if err := writePOCatalog(targetPath, resolvedLocale, resolvedDomain, entries); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Message catalog written: %s (%d message(s), %d file(s) scanned)\n", targetPath, len(entries), scannedFiles)
	return nil
}

func runCompileMessages(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("compilemessages", flag.ContinueOnError)
	fs.SetOutput(stderr)

	configPath := fs.String("config", "", "Path to goframe config file")
	locale := fs.String("locale", "", "Compile only one locale (default: compile all locales)")
	domain := fs.String("domain", defaultMessagesDomain, "Messages domain to compile")
	localesPathFlag := fs.String("locales-path", "", "Locales root path override (defaults to config locales_path)")
	outputPath := fs.String("output", "", "Output .json file path (only when compiling one locale)")
	dryRun := fs.Bool("dry-run", false, "Print compilation plan without writing files")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("compilemessages does not accept positional arguments")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	resolvedDomain := strings.TrimSpace(*domain)
	if resolvedDomain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	localesPath := strings.TrimSpace(*localesPathFlag)
	if localesPath == "" {
		localesPath = strings.TrimSpace(cfg.LocalesPath)
	}
	if localesPath == "" {
		localesPath = "locales"
	}

	jobs, err := discoverCompileMessageJobs(localesPath, strings.TrimSpace(*locale), resolvedDomain)
	if err != nil {
		return err
	}

	overrideOutput := strings.TrimSpace(*outputPath)
	if overrideOutput != "" {
		if len(jobs) != 1 {
			return fmt.Errorf("--output can only be used when exactly one catalog is compiled")
		}
		jobs[0].jsonPath = overrideOutput
	}

	totalEntries := 0
	for _, job := range jobs {
		raw, err := os.ReadFile(job.poPath)
		if err != nil {
			return fmt.Errorf("read PO catalog %s: %w", job.poPath, err)
		}
		entries, err := parsePOCatalog(raw)
		if err != nil {
			return fmt.Errorf("parse PO catalog %s: %w", job.poPath, err)
		}

		compiled := buildCompiledMessages(job.locale, resolvedDomain, entries)
		totalEntries += len(compiled.Entries)

		if *dryRun {
			fmt.Fprintf(stdout, "DRY-RUN\tCOMPILEMESSAGES\tlocale=%s\tdomain=%s\tentries=%d\tinput=%s\toutput=%s\n", job.locale, resolvedDomain, len(compiled.Entries), job.poPath, job.jsonPath)
			continue
		}

		if err := ensureDir(filepath.Dir(job.jsonPath)); err != nil {
			return err
		}
		payload, err := json.MarshalIndent(compiled, "", "  ")
		if err != nil {
			return fmt.Errorf("encode compiled catalog %s: %w", job.jsonPath, err)
		}
		payload = append(payload, '\n')
		if err := os.WriteFile(job.jsonPath, payload, 0644); err != nil {
			return fmt.Errorf("write compiled catalog %s: %w", job.jsonPath, err)
		}
		fmt.Fprintf(stdout, "Compiled messages: %s (%d entries)\n", job.jsonPath, len(compiled.Entries))
	}

	if *dryRun {
		fmt.Fprintf(stdout, "Planned compile: %d catalog(s), %d entries\n", len(jobs), totalEntries)
	}

	return nil
}

func parseMessageExtensions(raw string) (map[string]struct{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("extensions cannot be empty")
	}

	out := make(map[string]struct{})
	for _, token := range strings.Split(raw, ",") {
		ext := strings.ToLower(strings.TrimSpace(token))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if len(ext) < 2 {
			return nil, fmt.Errorf("invalid extension %q", token)
		}
		out[ext] = struct{}{}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one valid extension is required")
	}
	return out, nil
}

func collectMessageCatalog(root string, extensions map[string]struct{}) ([]messageCatalogEntry, int, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}

	type locSet map[string]messageLocation
	entryByID := make(map[string]locSet)
	scannedFiles := 0

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if shouldSkipMessageDir(path, root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := extensions[ext]; !ok {
			return nil
		}

		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		scannedFiles++

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			relPath = path
		}
		relPath = filepath.ToSlash(relPath)

		hits := extractMessageHits(string(body), relPath)
		for _, hit := range hits {
			if strings.TrimSpace(hit.ID) == "" {
				continue
			}
			locKey := fmt.Sprintf("%s:%d", hit.File, hit.Line)
			locations := entryByID[hit.ID]
			if locations == nil {
				locations = make(locSet)
				entryByID[hit.ID] = locations
			}
			locations[locKey] = messageLocation{File: hit.File, Line: hit.Line}
		}
		return nil
	})
	if err != nil {
		return nil, scannedFiles, fmt.Errorf("scan input directory: %w", err)
	}

	ids := make([]string, 0, len(entryByID))
	for id := range entryByID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]messageCatalogEntry, 0, len(ids))
	for _, id := range ids {
		locationMap := entryByID[id]
		locations := make([]messageLocation, 0, len(locationMap))
		for _, loc := range locationMap {
			locations = append(locations, loc)
		}
		sort.Slice(locations, func(i, j int) bool {
			if locations[i].File == locations[j].File {
				return locations[i].Line < locations[j].Line
			}
			return locations[i].File < locations[j].File
		})
		out = append(out, messageCatalogEntry{ID: id, Locations: locations})
	}

	return out, scannedFiles, nil
}

func shouldSkipMessageDir(path, root string) bool {
	if path == root {
		return false
	}

	name := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	switch name {
	case ".git", "vendor", "node_modules", "dist", "tmp":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}

func extractMessageHits(content, relPath string) []messageCatalogEntryHit {
	hits := make([]messageCatalogEntryHit, 0, 16)
	for _, pattern := range messageExtractPatterns {
		matches := pattern.FindAllStringSubmatchIndex(content, -1)
		for _, m := range matches {
			if len(m) < 4 {
				continue
			}
			raw := content[m[2]:m[3]]
			id := unescapeExtractedMessage(raw)
			if strings.TrimSpace(id) == "" {
				continue
			}
			line := 1 + strings.Count(content[:m[0]], "\n")
			hits = append(hits, messageCatalogEntryHit{
				ID:   id,
				File: relPath,
				Line: line,
			})
		}
	}
	return hits
}

type messageCatalogEntryHit struct {
	ID   string
	File string
	Line int
}

func unescapeExtractedMessage(raw string) string {
	decoded, err := strconv.Unquote(`"` + raw + `"`)
	if err != nil {
		return raw
	}
	return decoded
}

func writePOCatalog(path, locale, domain string, entries []messageCatalogEntry) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Generated by goframe makemessages.\n")
	b.WriteString("# Domain: " + domain + "\n")
	b.WriteString("\n")
	b.WriteString("msgid \"\"\n")
	b.WriteString("msgstr \"\"\n")
	b.WriteString("\"Project-Id-Version: goframe\\n\"\n")
	b.WriteString("\"Language: " + escapePOString(locale) + "\\n\"\n")
	b.WriteString("\"MIME-Version: 1.0\\n\"\n")
	b.WriteString("\"Content-Type: text/plain; charset=UTF-8\\n\"\n")
	b.WriteString("\"Content-Transfer-Encoding: 8bit\\n\"\n")
	b.WriteString("\n")

	for _, entry := range entries {
		if len(entry.Locations) > 0 {
			b.WriteString("#: ")
			for i, loc := range entry.Locations {
				if i > 0 {
					b.WriteString(" ")
				}
				b.WriteString(fmt.Sprintf("%s:%d", loc.File, loc.Line))
			}
			b.WriteString("\n")
		}
		b.WriteString("msgid \"" + escapePOString(entry.ID) + "\"\n")
		b.WriteString("msgstr \"\"\n\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

func escapePOString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func discoverCompileMessageJobs(localesPath, locale, domain string) ([]compileMessageJob, error) {
	if strings.TrimSpace(domain) == "" {
		return nil, fmt.Errorf("domain cannot be empty")
	}
	localesPath = strings.TrimSpace(localesPath)
	if localesPath == "" {
		localesPath = "locales"
	}

	if locale != "" {
		poPath := filepath.Join(localesPath, locale, "LC_MESSAGES", domain+".po")
		if _, err := os.Stat(poPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("PO catalog not found: %s", poPath)
			}
			return nil, fmt.Errorf("stat PO catalog %s: %w", poPath, err)
		}
		return []compileMessageJob{{
			poPath:   poPath,
			jsonPath: strings.TrimSuffix(poPath, ".po") + ".json",
			locale:   locale,
		}}, nil
	}

	targetName := domain + ".po"
	jobs := make([]compileMessageJob, 0, 8)
	err := filepath.WalkDir(localesPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if d.Name() != targetName {
			return nil
		}
		jobs = append(jobs, compileMessageJob{
			poPath:   path,
			jsonPath: strings.TrimSuffix(path, ".po") + ".json",
			locale:   localeFromPOPath(localesPath, path),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover PO catalogs: %w", err)
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("no PO catalogs found under %s for domain %q", localesPath, domain)
	}

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].poPath < jobs[j].poPath })
	return jobs, nil
}

func localeFromPOPath(localesPath, poPath string) string {
	rel, err := filepath.Rel(localesPath, poPath)
	if err != nil {
		return "unknown"
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 3 && strings.EqualFold(parts[1], "LC_MESSAGES") {
		return parts[0]
	}
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "unknown"
}

func parsePOCatalog(raw []byte) (map[string]string, error) {
	entries := make(map[string]string)

	var (
		currentID  string
		currentStr string
		haveEntry  bool
		mode       string
	)

	flush := func() {
		if !haveEntry {
			return
		}
		if currentID != "" {
			entries[currentID] = currentStr
		}
		currentID = ""
		currentStr = ""
		haveEntry = false
		mode = ""
	}

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			flush()
		case strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "msgid "):
			if haveEntry {
				flush()
			}
			value, err := parsePOQuotedLiteral(strings.TrimSpace(strings.TrimPrefix(line, "msgid")))
			if err != nil {
				return nil, err
			}
			currentID = value
			currentStr = ""
			haveEntry = true
			mode = "id"
		case strings.HasPrefix(line, "msgstr "):
			value, err := parsePOQuotedLiteral(strings.TrimSpace(strings.TrimPrefix(line, "msgstr")))
			if err != nil {
				return nil, err
			}
			if !haveEntry {
				haveEntry = true
			}
			currentStr = value
			mode = "str"
		case strings.HasPrefix(line, "msgstr["):
			closing := strings.Index(line, "]")
			if closing <= 0 || closing+1 >= len(line) {
				continue
			}
			value, err := parsePOQuotedLiteral(strings.TrimSpace(line[closing+1:]))
			if err != nil {
				return nil, err
			}
			if currentStr == "" {
				currentStr = value
			}
			mode = "str"
		case strings.HasPrefix(line, "\""):
			value, err := parsePOQuotedLiteral(line)
			if err != nil {
				return nil, err
			}
			if !haveEntry {
				continue
			}
			if mode == "id" {
				currentID += value
			} else if mode == "str" {
				currentStr += value
			}
		default:
			continue
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan PO content: %w", err)
	}
	flush()

	return entries, nil
}

func parsePOQuotedLiteral(token string) (string, error) {
	if !strings.HasPrefix(token, "\"") {
		return "", fmt.Errorf("invalid PO token %q", token)
	}
	value, err := strconv.Unquote(token)
	if err != nil {
		return "", fmt.Errorf("invalid PO string %q: %w", token, err)
	}
	return value, nil
}

func buildCompiledMessages(locale, domain string, entries map[string]string) compiledMessages {
	out := make(map[string]string, len(entries))
	for id, translated := range entries {
		if strings.TrimSpace(id) == "" {
			continue
		}
		value := translated
		if value == "" {
			value = id
		}
		out[id] = value
	}
	return compiledMessages{
		Locale:  locale,
		Domain:  domain,
		Entries: out,
	}
}

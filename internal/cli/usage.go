package cli

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
)

// usageRow is one named item in a usage section: a positional argument, a
// subcommand (with its own arguments spelled in Name) or a value a flag
// accepts. Help is a single line.
type usageRow struct {
	Name string
	Help string
}

// usageSection is a free-form named list rendered after the subcommands —
// the checks `doctor --check` accepts, the templates `new --template`
// knows. It carries data the flag package cannot express: a flag's usage
// string is prose, a section is a list a completion script can offer.
type usageSection struct {
	Title string
	Rows  []usageRow
}

// usageSpec is the grammar of one CLI command as DATA: synopsis lines,
// positionals, subcommands and examples. Each command installs it on its
// flag.FlagSet (installUsage) so `nucleus <cmd> --help` and `nucleus help
// <cmd>` print the grammar in front of the flags instead of the bare
// flag.PrintDefaults dump, which never listed a subcommand or a positional:
// `migrate --help` did not say that up, down, steps, status, drift, reset,
// refresh and create exist, and `testserver --help` did not say it takes a
// fixture path.
//
// Keeping the grammar as a table rather than as prose inside each command
// is deliberate: the same rows are what a shell completion generator needs
// to complete `nucleus migrate <TAB>` — data that flag.FlagSet cannot
// provide.
type usageSpec struct {
	// Synopsis lines, each a full invocation starting with "nucleus ".
	Synopsis []string
	// Description is one short paragraph under the synopsis.
	Description string
	// Positionals documents the non-flag arguments in synopsis order.
	Positionals []usageRow
	// SubcommandsTitle names the subcommand section ("Actions" for
	// migrate); empty means "Subcommands".
	SubcommandsTitle string
	// Subcommands documents the first positional's accepted words.
	Subcommands []usageRow
	// Sections are extra named lists (values a flag accepts).
	Sections []usageSection
	// Examples are full invocations starting with "nucleus ".
	Examples []string
	// Notes are short paragraphs printed last.
	Notes []string
}

// commandUsages is the per-command grammar table, keyed by primary command
// name. Commands absent from the table keep flag.PrintDefaults as their
// whole help; the goal is for every command with positionals or
// subcommands to have a row here.
var commandUsages = map[string]usageSpec{
	"migrate": {
		Synopsis:         []string{"nucleus migrate [flags] [<action> [args]]"},
		Description:      "Apply and manage SQL migrations against the configured database. Without an action, up runs.",
		SubcommandsTitle: "Actions",
		Subcommands: []usageRow{
			{Name: "up [n]", Help: "Apply every pending migration, or only the next n"},
			{Name: "down [n]", Help: "Roll back the last migration, or the last n (destructive: --force or --yes in production)"},
			{Name: "steps <n>", Help: "Apply n migrations forward, or roll back |n| when n is negative"},
			{Name: "status", Help: "List every migration file with its applied/pending state"},
			{Name: "drift", Help: "Report applied migrations whose .up.sql file is missing on disk; exits non-zero on drift"},
			{Name: "reset", Help: "Roll back every applied migration (destructive)"},
			{Name: "refresh", Help: "Roll back every applied migration, then apply all of them again (destructive)"},
			{Name: "create <name>", Help: "Write an empty <timestamp>_<name>.up.sql/.down.sql pair; needs no database"},
		},
		Examples: []string{
			"nucleus migrate --config nucleus.yml",
			"nucleus migrate --config nucleus.yml status",
			"nucleus migrate --config nucleus.yml down 1",
			"nucleus migrate create add_users_table",
		},
	},
	"routes": {
		Synopsis:    []string{"nucleus routes [flags]"},
		Description: "List the HTTP routes of an application built from configuration only: framework-owned routes, not the modules compiled into your binary.",
		Examples: []string{
			"nucleus routes --config nucleus.yml",
			"nucleus routes --config nucleus.yml --path /admin --json",
		},
		Notes: []string{
			"With env: development, booting your own binary (go run .) logs one \"module route mounted\" line per module route — that log is the full table.",
		},
	},
	"serve": {
		Synopsis:    []string{"nucleus serve [flags]"},
		Description: "Start an HTTP server built from configuration only. Modules compiled into your binary are not mounted; to serve your application, run your own binary (go run .).",
		Examples: []string{
			"nucleus serve --config nucleus.yml",
			"nucleus serve --config nucleus.yml --port 9090 --without-defaults",
		},
	},
	"new": {
		Synopsis:    []string{"nucleus new <project_name> [flags]"},
		Description: "Create a new project scaffold: go.mod pinned to this release, a composition-root main.go, nucleus.yml and an empty migrations directory. No feature code is generated.",
		Positionals: []usageRow{
			{Name: "<project_name>", Help: "Directory created under --out; also the default module path suffix (example.com/<project_name>)"},
		},
		Sections: []usageSection{{
			Title: "Templates (--template)",
			Rows: []usageRow{
				{Name: "mvc", Help: "Full-stack: default subsystems, rbac_policy.csv and the sqlite driver import (default)"},
				{Name: "api", Help: "Core-only: WithoutDefaults(), no admin, storage, mail or authz"},
			},
		}},
		Examples: []string{
			"nucleus new blog --module github.com/acme/blog",
			"nucleus new svc --template api --port 9090",
		},
	},
	"startapp": {
		Synopsis:    []string{"nucleus startapp <name> [flags]"},
		Description: "Create an app scaffold inside an existing project: model, controller, service, repository, contract, tasks, a server-rendered page and a mountable module, plus the migration for the configured dialect.",
		Positionals: []usageRow{
			{Name: "<name>", Help: "App name; the model is <Name>, the table and the routes are its plural"},
		},
		Examples: []string{
			"nucleus startapp billing --out .",
			"nucleus startapp billing --skip-migration",
		},
	},
	"shell": {
		Synopsis:    []string{"nucleus shell [flags]", "nucleus shell -c <sql> [flags]", "nucleus shell [flags] < script.sql"},
		Description: "Execute SQL against the configured database: one statement with -c, a script from stdin, or an interactive prompt on a terminal.",
		Examples: []string{
			"nucleus shell --config nucleus.yml",
			"nucleus shell --config nucleus.yml -c \"SELECT count(*) FROM users\"",
			"nucleus shell --config nucleus.yml --sandbox < report.sql",
		},
	},
	"seed": {
		Synopsis:    []string{"nucleus seed [flags]"},
		Description: "Execute the .sql seed files of --seeds in name order, or one file with --file. Production runs need --force or --yes.",
		Examples: []string{
			"nucleus seed --config nucleus.yml --dry-run",
			"nucleus seed --config nucleus.yml --file 001_users.sql",
		},
	},
	"loaddata": {
		Synopsis:    []string{"nucleus loaddata [flags] <fixture.json>", "nucleus loaddata --file <fixture.json> [flags]"},
		Description: "Import JSON fixtures into database tables, in foreign-key order.",
		Positionals: []usageRow{
			{Name: "<fixture.json>", Help: "Fixture file to load (alternative to --file; not both)"},
		},
		Examples: []string{
			"nucleus loaddata --config nucleus.yml fixtures.json",
			"nucleus loaddata --config nucleus.yml --truncate --tables users,roles fixtures.json",
		},
	},
	"testserver": {
		Synopsis:    []string{"nucleus testserver [flags] <fixture.json>", "nucleus testserver --fixture <fixture.json> [flags]"},
		Description: "Load a fixture, then start a server built from configuration only (like serve: your modules are not mounted).",
		Positionals: []usageRow{
			{Name: "<fixture.json>", Help: "Fixture file loaded before the server starts (alternative to --fixture; not both)"},
		},
		Examples: []string{
			"nucleus testserver --config nucleus.yml fixtures.json",
			"nucleus testserver --config nucleus.yml --dry-run fixtures.json",
		},
	},
	"findstatic": {
		Synopsis:    []string{"nucleus findstatic [flags] <asset>..."},
		Description: "Resolve each asset path against the discovered static source directories. Exits non-zero when any query has no match.",
		Positionals: []usageRow{
			{Name: "<asset>...", Help: "One or more asset paths relative to a static root; glob patterns (*, ?, [...]) are accepted"},
		},
		Examples: []string{
			"nucleus findstatic --config nucleus.yml app.css",
			"nucleus findstatic --config nucleus.yml --first --json 'img/*.png'",
		},
	},
	"collectstatic": {
		Synopsis:    []string{"nucleus collectstatic [flags]"},
		Description: "Copy every file of the static source directories into --output (or the configured static_root), first source wins on duplicate paths.",
		Examples: []string{
			"nucleus collectstatic --config nucleus.yml --output public/assets",
			"nucleus collectstatic --config nucleus.yml --dry-run",
		},
	},
	"doctor": {
		Synopsis:    []string{"nucleus doctor [flags]"},
		Description: "Run diagnostic checks for framework subsystems; exits non-zero when any check fails.",
		Sections: []usageSection{{
			Title: "Checks (--check)",
			Rows: []usageRow{
				{Name: "tasks", Help: "Configured jobs provider; with asynq, queue reachability over Redis"},
				{Name: "outbox", Help: "Outbox dispatcher and pending events"},
				{Name: "storage", Help: "Storage backend configuration; a live probe only when targeted explicitly"},
				{Name: "observability", Help: "OpenTelemetry exporters and metrics"},
				{Name: "tenancy", Help: "Multi-tenant configuration and isolation"},
				{Name: "rbac", Help: "RBAC policy file and enforcer"},
				{Name: "security", Help: "High-risk misconfiguration: CORS, trusted proxies, signing key, CSRF"},
				{Name: "auth", Help: "Authentication chain: backend order, per-backend configuration, break-glass path"},
			},
		}},
		Examples: []string{
			"nucleus doctor --config nucleus.yml",
			"nucleus doctor --config nucleus.yml --check security",
			"nucleus doctor --config nucleus.yml --json",
		},
	},
	"openapi": {
		Synopsis:    []string{"nucleus openapi [flags]"},
		Description: "Export the experimental OpenAPI document built by internal/contracts of the project (created by generate resource or startapp).",
		Examples: []string{
			"nucleus openapi --out openapi.json",
			"nucleus openapi --project ./svc --out -",
		},
	},
}

// installUsage sets fs.Usage to render the command's usageSpec followed by
// the flag defaults, on the flag set's own output. A command without a row
// in commandUsages keeps the flag package's default usage.
func installUsage(fs *flag.FlagSet, name string) {
	spec, ok := commandUsages[name]
	if !ok {
		return
	}
	fs.Usage = func() {
		spec.render(fs.Output(), fs)
	}
}

// usageError builds the error a command returns when its positionals are
// wrong: the first synopsis line, so the message and the help never
// disagree.
func usageError(name string) error {
	spec, ok := commandUsages[name]
	if !ok || len(spec.Synopsis) == 0 {
		return fmt.Errorf("usage: nucleus %s --help", name)
	}
	return fmt.Errorf("usage: %s", spec.Synopsis[0])
}

// render writes the grammar and, when fs is not nil, the flag defaults.
func (u usageSpec) render(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintln(w, "Usage:")
	for _, line := range u.Synopsis {
		fmt.Fprintf(w, "  %s\n", line)
	}
	if u.Description != "" {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, u.Description)
	}
	renderUsageRows(w, "Arguments", u.Positionals)
	title := u.SubcommandsTitle
	if title == "" {
		title = "Subcommands"
	}
	renderUsageRows(w, title, u.Subcommands)
	for _, section := range u.Sections {
		renderUsageRows(w, section.Title, section.Rows)
	}
	if fs != nil && countFlags(fs) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Flags:")
		fs.PrintDefaults()
	}
	if len(u.Examples) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "Examples:")
		for _, ex := range u.Examples {
			fmt.Fprintf(w, "  %s\n", ex)
		}
	}
	for _, note := range u.Notes {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, note)
	}
}

func renderUsageRows(w io.Writer, title string, rows []usageRow) {
	if len(rows) == 0 {
		return
	}
	width := 0
	for _, r := range rows {
		if len(r.Name) > width {
			width = len(r.Name)
		}
	}
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "%s:\n", title)
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s  %s\n", width, r.Name, r.Help)
	}
}

func countFlags(fs *flag.FlagSet) int {
	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	return n
}

// usageSpecNames returns the sorted command names that carry a usageSpec.
func usageSpecNames() []string {
	names := make([]string, 0, len(commandUsages))
	for name := range commandUsages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// subcommandWords returns the bare first word of every subcommand row
// ("up [n]" -> "up"), the set a dispatcher or a completion script matches.
func (u usageSpec) subcommandWords() []string {
	out := make([]string, 0, len(u.Subcommands))
	for _, row := range u.Subcommands {
		if word := strings.Fields(row.Name); len(word) > 0 {
			out = append(out, word[0])
		}
	}
	return out
}

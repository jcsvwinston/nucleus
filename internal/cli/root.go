package cli

import (
	"fmt"
	"io"
	"sort"
)

type commandSpec struct {
	name    string
	summary string
	run     func(args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Version is injected at build time in releases.
var Version = "dev"

var commandSpecs = []commandSpec{
	{name: "changepassword", summary: "Update an admin user's password", run: runChangePassword},
	{name: "clearsessions", summary: "Delete expired or all session rows", run: runClearSessions},
	{name: "compilemessages", summary: "Compile .po message catalogs into JSON bundles", run: runCompileMessages},
	{name: "collectstatic", summary: "Collect static assets into configured static_root", run: runCollectStatic},
	{name: "createcachetable", summary: "Create SQL table used by database-backed cache", run: runCreateCacheTable},
	{name: "createuser", summary: "Create or update an admin user", run: runCreateUser},
	{name: "config", summary: "Print the effective configuration with per-key source", run: runConfig},
	{name: "diffsettings", summary: "Show configuration differences from defaults", run: runDiffSettings},
	{name: "doctor", summary: "Run diagnostic checks for framework subsystems", run: runDoctor},
	{name: "dumpdata", summary: "Export DB rows as JSON fixtures", run: runDumpData},
	{name: "wizard", summary: "Interactive wizard for complex commands", run: runWizard},
	{name: "findstatic", summary: "Find static assets across discovered source directories", run: runFindStatic},
	{name: "flush", summary: "Delete all data from database tables (keeps migration history)", run: runFlush},
	{name: "generate", summary: "Generate model, handler, or migration scaffolds", run: runGenerate},
	{name: "health", summary: "Check configured dependencies health", run: runHealth},
	{name: "inspectdb", summary: "Inspect DB schema and generate Go model structs", run: runInspectDB},
	{name: "loaddata", summary: "Import JSON fixtures into DB tables", run: runLoadData},
	{name: "mailproviders", summary: "List registered and external mail providers", run: runMailProviders},
	{name: "makemessages", summary: "Extract translatable strings into .po catalogs", run: runMakeMessages},
	{name: "ogrinspect", summary: "Inspect geospatial tables and generate Go model structs", run: runOGRInspect},
	{name: "optimizemigration", summary: "Optimize SQL statements in one migration file", run: runOptimizeMigration},
	{name: "plugin", summary: "Inspect and validate plugin providers/capabilities", run: runPlugin},
	{name: "remove_stale_contenttypes", summary: "Delete stale rows from content types table", run: runRemoveStaleContentTypes},
	{name: "sendtestemail", summary: "Send a test email through configured mail provider", run: runSendTestEmail},
	{name: "serve", summary: "Start the HTTP server", run: runServe},
	{name: "migrate", summary: "Apply and manage SQL migrations", run: runMigrate},
	{name: "sqlmigrate", summary: "Print SQL for a migration file", run: runSQLMigrate},
	{name: "sqlflush", summary: "Print SQL statements used by flush", run: runSQLFlush},
	{name: "sqlsequencereset", summary: "Print SQL statements to reset table sequences", run: runSQLSequenceReset},
	{name: "new", summary: "Create a new MVC + API + Admin project scaffold", run: runNew},
	{name: "openapi", summary: "Export the experimental OpenAPI project contract", run: runOpenAPI},
	{name: "squashmigrations", summary: "Squash a migration range into a single migration", run: runSquashMigrations},
	{name: "startapp", summary: "Create an app scaffold in an existing project", run: runStartApp},
	{name: "seed", summary: "Execute SQL seed files", run: runSeed},
	{name: "shell", summary: "Execute SQL interactively or via -c", run: runShell},
	{name: "test", summary: "Run Go tests with project-friendly defaults", run: runTest},
	{name: "testserver", summary: "Load fixture data and start a local server", run: runTestServer},
	{name: "routes", summary: "List registered HTTP routes", run: runRoutes},
}

var commandByName = buildCommandMap()

func buildCommandMap() map[string]commandSpec {
	out := make(map[string]commandSpec, len(commandSpecs))
	for _, c := range commandSpecs {
		out[c.name] = c
	}
	return out
}

// ContractPrimaryCommandNames returns the sorted list of primary CLI command
// names that are considered part of the root command surface contract.
func ContractPrimaryCommandNames() []string {
	names := make([]string, 0, len(commandSpecs))
	for _, spec := range commandSpecs {
		names = append(names, spec.name)
	}
	sort.Strings(names)
	return names
}

// Run executes the CLI command dispatch and returns a process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	parsedArgs, outputOpts, err := parseGlobalOutputOptions(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	args = parsedArgs

	previousOutputOpts := currentOutputOptions()
	setOutputOptions(outputOpts)
	defer setOutputOptions(previousOutputOpts)

	if len(args) == 0 {
		printRootUsage(stdout)
		return 0
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "help", "-h", "--help":
		if len(rest) == 0 {
			printRootUsage(stdout)
			return 0
		}
		targetName := resolveHelpCommand(rest[0])
		target, ok := commandByName[targetName]
		if !ok {
			fmt.Fprintf(stderr, "error: unknown command %q\n", rest[0])
			printRootUsage(stderr)
			return 2
		}
		if err := target.run([]string{"--help"}, stdin, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "nucleus %s\n", Version)
		return 0
	}

	resolvedCmd, resolvedArgs, err := canonicalizeCommand(cmd, rest)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	cmd = resolvedCmd
	rest = resolvedArgs

	target, ok := commandByName[cmd]
	if !ok {
		handled, code, err := runExternalCommand(cmd, rest, stdin, stdout, stderr)
		if handled {
			if err != nil {
				fmt.Fprintf(stderr, "error: %v\n", err)
				return 1
			}
			return code
		}
		fmt.Fprintf(stderr, "error: unknown command %q\n", cmd)
		printRootUsage(stderr)
		return 2
	}

	if err := target.run(rest, stdin, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func printRootUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  nucleus [global options] <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Global options:")
	fmt.Fprintln(w, "  --output plain|pretty|json")
	fmt.Fprintln(w, "  --color auto|always|never")
	fmt.Fprintln(w, "  --symbols / --no-symbols")
	fmt.Fprintln(w, "  --json                    Shorthand for --output json")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Commands:")

	cmds := make([]commandSpec, len(commandSpecs))
	copy(cmds, commandSpecs)
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].name < cmds[j].name })
	for _, c := range cmds {
		fmt.Fprintf(w, "  %-10s %s\n", c.name, c.summary)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Django-style aliases:")
	for _, name := range sortedAliasNames() {
		alias := commandAliases[name]
		fmt.Fprintf(w, "  %-15s %s\n", name, alias.summary)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  nucleus new blog --module github.com/acme/blog")
	fmt.Fprintln(w, "  nucleus serve --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus runserver 0.0.0.0:8080")
	fmt.Fprintln(w, "  nucleus startapp billing --out .")
	fmt.Fprintln(w, "  nucleus migrate --config nucleus.yml status")
	fmt.Fprintln(w, "  nucleus sqlmigrate --migrations migrations 20260401120000_add_users")
	fmt.Fprintln(w, "  nucleus sqlflush --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus flush --config nucleus.yml --yes")
	fmt.Fprintln(w, "  nucleus diffsettings --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus doctor --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus doctor --config nucleus.yml --check tasks")
	fmt.Fprintln(w, "  nucleus doctor --config nucleus.yml --json")
	fmt.Fprintln(w, "  nucleus wizard --type inspectdb")
	fmt.Fprintln(w, "  nucleus wizard --type new")
	fmt.Fprintln(w, "  nucleus wizard --type startapp")
	fmt.Fprintln(w, "  nucleus createcachetable --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus clearsessions --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus remove_stale_contenttypes --config nucleus.yml --dry-run")
	fmt.Fprintln(w, "  nucleus mailproviders --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin list --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin doctor --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus plugin test --provider sendgrid --capability mail.send")
	fmt.Fprintln(w, "  nucleus makemessages --config nucleus.yml --locale es --input .")
	fmt.Fprintln(w, "  nucleus compilemessages --config nucleus.yml --locale es")
	fmt.Fprintln(w, "  nucleus collectstatic --config nucleus.yml --output public/assets")
	fmt.Fprintln(w, "  nucleus findstatic --config nucleus.yml app.css")
	fmt.Fprintln(w, "  nucleus optimizemigration --migrations migrations add_users_table")
	fmt.Fprintln(w, "  nucleus squashmigrations --migrations migrations --from init --to add_users --name baseline --write")
	fmt.Fprintln(w, "  nucleus sendtestemail --config nucleus.yml --to dev@example.com --dry-run")
	fmt.Fprintln(w, "  nucleus check --deploy --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus sqlsequencereset --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus inspectdb --config nucleus.yml --output internal/models/inspected.go")
	fmt.Fprintln(w, "  nucleus ogrinspect --config nucleus.yml --output internal/models/geospatial.go")
	fmt.Fprintln(w, "  nucleus openapi --out openapi.json")
	fmt.Fprintln(w, "  nucleus dumpdata --config nucleus.yml --output fixtures.json")
	fmt.Fprintln(w, "  nucleus loaddata --config nucleus.yml fixtures.json")
	fmt.Fprintln(w, "  nucleus testserver --config nucleus.yml --dry-run fixtures.json")
	fmt.Fprintln(w, "  nucleus changepassword admin --config nucleus.yml --password newsecret123 --no-input")
	fmt.Fprintln(w, "  nucleus showmigrations --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus generate model User")
	fmt.Fprintln(w, "  nucleus test --run TestRun_MigrateLifecycle ./cmd/nucleus")
	fmt.Fprintln(w, "  nucleus routes --config nucleus.yml")
	fmt.Fprintln(w, "  nucleus health --config nucleus.yml")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Extensions:")
	fmt.Fprintln(w, "  External commands on PATH are supported as nucleus-<name>.")
	fmt.Fprintln(w, "  Example: nucleus foo -> executes nucleus-foo")
}

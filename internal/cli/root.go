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
	{name: "serve", summary: "Start the HTTP server", run: runServe},
	{name: "migrate", summary: "Apply and manage SQL migrations", run: runMigrate},
	{name: "sqlmigrate", summary: "Print SQL for a migration file", run: runSQLMigrate},
	{name: "sqlflush", summary: "Print SQL statements used by flush", run: runSQLFlush},
	{name: "sqlsequencereset", summary: "Print SQL statements to reset table sequences", run: runSQLSequenceReset},
	{name: "flush", summary: "Delete all data from database tables (keeps migration history)", run: runFlush},
	{name: "inspectdb", summary: "Inspect DB schema and generate Go model structs", run: runInspectDB},
	{name: "dumpdata", summary: "Export DB rows as JSON fixtures", run: runDumpData},
	{name: "loaddata", summary: "Import JSON fixtures into DB tables", run: runLoadData},
	{name: "new", summary: "Create a new MVC + API + Admin project scaffold", run: runNew},
	{name: "startapp", summary: "Create an app scaffold in an existing project", run: runStartApp},
	{name: "createuser", summary: "Create or update an admin user", run: runCreateUser},
	{name: "seed", summary: "Execute SQL seed files", run: runSeed},
	{name: "shell", summary: "Execute SQL interactively or via -c", run: runShell},
	{name: "generate", summary: "Generate model, handler, or migration scaffolds", run: runGenerate},
	{name: "test", summary: "Run Go tests with project-friendly defaults", run: runTest},
	{name: "routes", summary: "List registered HTTP routes", run: runRoutes},
	{name: "health", summary: "Check configured dependencies health", run: runHealth},
}

var commandByName = buildCommandMap()

func buildCommandMap() map[string]commandSpec {
	out := make(map[string]commandSpec, len(commandSpecs))
	for _, c := range commandSpecs {
		out[c.name] = c
	}
	return out
}

// Run executes the CLI command dispatch and returns a process exit code.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
		fmt.Fprintf(stdout, "goframe %s\n", Version)
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
	fmt.Fprintln(w, "  goframe <command> [options]")
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
	fmt.Fprintln(w, "  goframe new blog --module github.com/acme/blog")
	fmt.Fprintln(w, "  goframe serve --config goframe.yaml")
	fmt.Fprintln(w, "  goframe runserver 0.0.0.0:8080")
	fmt.Fprintln(w, "  goframe startapp billing --out .")
	fmt.Fprintln(w, "  goframe migrate --config goframe.yaml status")
	fmt.Fprintln(w, "  goframe sqlmigrate --migrations migrations 20260401120000_add_users")
	fmt.Fprintln(w, "  goframe sqlflush --config goframe.yaml")
	fmt.Fprintln(w, "  goframe flush --config goframe.yaml --yes")
	fmt.Fprintln(w, "  goframe sqlsequencereset --config goframe.yaml")
	fmt.Fprintln(w, "  goframe inspectdb --config goframe.yaml --output internal/models/inspected.go")
	fmt.Fprintln(w, "  goframe dumpdata --config goframe.yaml --output fixtures.json")
	fmt.Fprintln(w, "  goframe loaddata --config goframe.yaml fixtures.json")
	fmt.Fprintln(w, "  goframe showmigrations --config goframe.yaml")
	fmt.Fprintln(w, "  goframe generate model User")
	fmt.Fprintln(w, "  goframe test --run TestRun_MigrateLifecycle ./cmd/goframe")
	fmt.Fprintln(w, "  goframe routes --config goframe.yaml")
	fmt.Fprintln(w, "  goframe health --config goframe.yaml")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Extensions:")
	fmt.Fprintln(w, "  External commands on PATH are supported as goframe-<name>.")
	fmt.Fprintln(w, "  Example: goframe foo -> executes goframe-foo")
}

package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/app"
	"github.com/jcsvwinston/nucleus/pkg/plugins"
)

// externalCommandPrefix names the executables `nucleus <name>` dispatches
// to. The capability plugins share the prefix (`nucleus-plugin-<provider>`)
// and are not commands: `nucleus plugin list` lists them.
const externalCommandPrefix = "nucleus-"

// externalCommand is a `nucleus-<name>` executable found on PATH.
type externalCommand struct {
	Name       string `json:"name"`
	BinaryPath string `json:"binary_path"`
	// Shadowed is set when a built-in command or alias has the same name:
	// `nucleus <name>` runs the built-in, never this executable.
	Shadowed bool `json:"shadowed,omitempty"`
	// Refused is why the configuration's plugins policy does not let
	// `nucleus <name>` run it.
	Refused string `json:"refused,omitempty"`
}

// discoverExternalCommands lists the `nucleus-<name>` executables on
// pathEnv, the first of each name winning the way exec.LookPath resolves
// it, sorted by name.
func discoverExternalCommands(pathEnv string, policy plugins.Policy) []externalCommand {
	seen := map[string]bool{}
	var out []externalCommand
	for _, dir := range filepath.SplitList(pathEnv) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), externalCommandPrefix) ||
				strings.HasPrefix(entry.Name(), plugins.GenericBinaryPrefix) {
				continue
			}
			name := strings.TrimPrefix(entry.Name(), externalCommandPrefix)
			if runtime.GOOS == "windows" {
				name = strings.TrimSuffix(name, ".exe")
			}
			if name == "" || seen[name] {
				continue
			}
			full := filepath.Join(dir, entry.Name())
			if ok, err := plugins.IsExecutableFile(full, entry); err != nil || !ok {
				continue
			}
			seen[name] = true
			cmd := externalCommand{Name: name, BinaryPath: full, Shadowed: isBuiltinCommandName(name)}
			if refused := policy.AllowCommand(name); refused != nil {
				cmd.Refused = refused.Error()
			}
			out = append(out, cmd)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// isBuiltinCommandName reports whether `nucleus <name>` resolves to a
// built-in command or alias before the external dispatch is consulted. It
// is assigned in init: the command table refers to the plugin commands,
// which refer to this, and a function that read the table directly would
// make the table's initialisation depend on itself.
var isBuiltinCommandName func(name string) bool

func init() {
	isBuiltinCommandName = func(name string) bool {
		switch name {
		case "help", "version":
			return true
		}
		if _, ok := commandByName[name]; ok {
			return true
		}
		_, ok := commandAliases[name]
		return ok
	}
}

// dispatchPolicy is the plugins policy `nucleus <name>` enforces before it
// runs `nucleus-<name>`: the configuration's `plugins` block, read the way
// every command reads it — the file NUCLEUS_CONFIG names, or nucleus.yml in
// the working directory when there is one. An external command receives its
// arguments untouched, so a `--config` among them is the command's, not
// this lookup's.
//
// The allowlist is opt-in (DEP-2026-014), so a configuration that does not
// load does not stop a command from running — unless it declares a
// `plugins` block: then the allowlist it declares cannot be read, and the
// command is refused rather than run unchecked.
func dispatchPolicy() (plugins.Policy, error) {
	path := strings.TrimSpace(os.Getenv("NUCLEUS_CONFIG"))
	cfg, err := app.LoadConfig(path)
	if err == nil {
		return cfg.Plugins.Policy(), nil
	}
	if configDeclaresPlugins(path, os.Environ()) {
		return plugins.Policy{}, fmt.Errorf("the configuration declares a plugins block and does not load, so its allowlist cannot be applied: %w", err)
	}
	return plugins.Policy{}, nil
}

var pluginsBlock = regexp.MustCompile(`(?m)^plugins\s*:`)

// configDeclaresPlugins reports whether the configuration file at path (or
// nucleus.yml when path is empty) or the environment declares the plugins
// block. It reads the file as text, because it is asked when the file does
// not load.
func configDeclaresPlugins(path string, environ []string) bool {
	for _, kv := range environ {
		name, val, ok := strings.Cut(kv, "=")
		if ok && val != "" && strings.HasPrefix(strings.ToUpper(name), "NUCLEUS_PLUGINS__") {
			return true
		}
	}
	if path == "" {
		path = "nucleus.yml"
	}
	raw, err := os.ReadFile(path)
	return err == nil && pluginsBlock.Match(raw)
}

func runExternalCommand(name string, args []string, stdin io.Reader, stdout, stderr io.Writer) (handled bool, exitCode int, err error) {
	binary := externalCommandPrefix + name
	path, lookErr := exec.LookPath(binary)
	if lookErr != nil {
		return false, 0, nil
	}
	policy, err := dispatchPolicy()
	if err != nil {
		return true, 1, fmt.Errorf("%s not run: %w", binary, err)
	}
	if refused := policy.AllowCommand(name); refused != nil {
		return true, 1, fmt.Errorf("%w — list it under plugins.commands to run it", refused)
	}

	cmd := exec.Command(path, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return true, exitErr.ExitCode(), nil
		}
		return true, 1, runErr
	}

	return true, 0, nil
}

// printExternalCommands is the part of the root usage that lists the
// external commands found on PATH.
func printExternalCommands(w io.Writer) {
	fmt.Fprintln(w, "Extensions:")
	fmt.Fprintln(w, "  External commands on PATH are supported as nucleus-<name>.")
	fmt.Fprintln(w, "  Example: nucleus foo -> executes nucleus-foo")
	fmt.Fprintln(w, "  The plugins.commands key of the configuration, when set, lists the ones that may run.")
	found := discoverExternalCommands(os.Getenv("PATH"), plugins.Policy{})
	var runnable []externalCommand
	for _, c := range found {
		if !c.Shadowed {
			runnable = append(runnable, c)
		}
	}
	if len(runnable) == 0 {
		fmt.Fprintln(w, "  None found on PATH.")
		return
	}
	fmt.Fprintln(w, "  Found on PATH:")
	for _, c := range runnable {
		fmt.Fprintf(w, "    %-12s %s\n", c.Name, c.BinaryPath)
	}
}

package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
)

// runConfig dispatches the `config` subcommands. Today only `print` is
// implemented — the ADR-010 Phase 3 effective-config inspection. The
// `config schema` JSON-Schema emission (ADR-010 §2) is a separate
// follow-up and is not yet wired here.
func runConfig(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nucleus config print --effective --config <path> [--config <path>…] [--json]")
		return errors.New("config: missing subcommand (supported: print)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "print":
		return runConfigPrint(rest, stdin, stdout, stderr)
	default:
		return fmt.Errorf("config: unknown subcommand %q (supported: print)", sub)
	}
}

// repeatedString collects a repeatable string flag (`--config a --config b`).
type repeatedString []string

func (r *repeatedString) String() string { return strings.Join(*r, ",") }
func (r *repeatedString) Set(v string) error {
	*r = append(*r, v)
	return nil
}

// runConfigPrint implements `nucleus config print --effective`: it merges
// the given config files exactly as FromConfigFile would and prints every
// effective key with its value and source. Sensitive values are redacted
// via the canonical observe.DefaultRedactedKeys() list (ADR-010 §5).
func runConfigPrint(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("config print", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var paths repeatedString
	fs.Var(&paths, "config", "Path to a nucleus config file (repeatable; merged left to right)")
	// `print` only supports the effective view today; --effective is
	// accepted for fidelity with the ADR-010 §5 command form and so a
	// future non-effective view can be added without breaking callers.
	_ = fs.Bool("effective", false, "Print the effective configuration with per-key source")
	asJSON := fs.Bool("json", false, "Print output as JSON")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if len(fs.Args()) > 0 {
		return fmt.Errorf("config print does not accept positional arguments")
	}
	if len(paths) == 0 {
		return errors.New("config print requires at least one --config path")
	}

	ec, err := nucleus.LoadEffective([]string(paths))
	if err != nil {
		return err
	}

	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ec)
	}

	for _, v := range ec.Values {
		fmt.Fprintf(stdout, "%s = %s [%s]\n", v.Key, formatEffectiveValue(v.Value), formatConfigSource(v.Source))
	}
	return nil
}

// formatConfigSource renders a ConfigSource as `kind` (for defaults) or
// `kind:path` (for files), e.g. `default`, `yaml:config/nucleus.yaml`.
func formatConfigSource(s nucleus.ConfigSource) string {
	if s.Path == "" {
		return s.Kind
	}
	return s.Kind + ":" + s.Path
}

func formatEffectiveValue(v any) string {
	switch vv := v.(type) {
	case string:
		if vv == "" {
			return `""`
		}
		return vv
	default:
		return fmt.Sprint(vv)
	}
}

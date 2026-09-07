// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

// completionShells are the shells a script is generated for, in the order
// the help lists them.
var completionShells = []usageRow{
	{Name: "bash", Help: "Source the output, or install it under bash-completion's completions directory"},
	{Name: "zsh", Help: "Write the output to a file named _nucleus on your fpath, then run compinit"},
	{Name: "fish", Help: "Write the output to ~/.config/fish/completions/nucleus.fish"},
}

// runCompletion prints a completion script for one shell. The script is
// generated from the command table (commandSpecs), the Django-style
// aliases (commandAliases), each command's grammar (commandUsages: the
// subcommands a first positional accepts, the values a flag accepts) and
// each command's flags, read from its own --help — the same data the help
// screens render, so the completion cannot name a word the binary rejects.
func runCompletion(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	fs.SetOutput(stderr)
	installUsage(fs, "completion")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return usageError("completion")
	}
	model := buildCompletionModel()
	switch rest[0] {
	case "bash":
		return model.renderBash(stdout)
	case "zsh":
		return model.renderZsh(stdout)
	case "fish":
		return model.renderFish(stdout)
	}
	return fmt.Errorf("unknown shell %q: expected bash, zsh or fish", rest[0])
}

// completionFlag is one flag of a command as the completion needs it: the
// name without dashes, its help, whether it takes a value, and the values
// it accepts when the command's grammar lists them.
type completionFlag struct {
	Name       string
	Help       string
	TakesValue bool
	Values     []usageRow
}

// completionCommand is one word the first position accepts — a primary
// command, an alias, or the help and version pseudo-commands — with what
// follows it.
type completionCommand struct {
	Name        string
	Summary     string
	Subcommands []usageRow
	Flags       []completionFlag
	// CompletesCommands is set for help: its argument is a command name.
	CompletesCommands bool
}

// completionModel is the whole surface as data; each shell renders it.
type completionModel struct {
	Commands []completionCommand
	Globals  []completionFlag
}

// globalCompletionFlags mirrors the "Global options" block of the root
// usage (parseGlobalOutputOptions); TestCompletionGlobalsMatchRootUsage
// keeps the two in step.
var globalCompletionFlags = []completionFlag{
	{Name: "output", Help: "Output style", TakesValue: true, Values: []usageRow{{Name: "plain"}, {Name: "pretty"}, {Name: "json"}}},
	{Name: "color", Help: "Color mode", TakesValue: true, Values: []usageRow{{Name: "auto"}, {Name: "always"}, {Name: "never"}}},
	{Name: "symbols", Help: "Print status symbols"},
	{Name: "no-symbols", Help: "Do not print status symbols"},
	{Name: "json", Help: "Shorthand for --output json"},
}

// completionCommandTable hands the completion the command table. It is set
// in init rather than read directly because the table lists runCompletion,
// and an initializer that reads the table from runCompletion's call graph
// is an initialization cycle; init functions run after every package
// variable is initialized and are outside that analysis.
var completionCommandTable func() map[string]commandSpec

func init() {
	completionCommandTable = func() map[string]commandSpec { return commandByName }
}

// buildCompletionModel assembles the model from the CLI's own tables.
func buildCompletionModel() completionModel {
	table := completionCommandTable()
	names := make([]string, 0, len(table))
	for name := range table {
		names = append(names, name)
	}
	sort.Strings(names)

	byName := map[string]completionCommand{}
	var commands []completionCommand
	for _, name := range names {
		spec := table[name]
		cmd := completionCommand{Name: name, Summary: spec.summary, Flags: completionFlagsOf(name)}
		if usage, ok := commandUsages[name]; ok {
			cmd.Subcommands = completionSubcommands(usage)
			attachSectionValues(cmd.Flags, usage)
		}
		byName[name] = cmd
		commands = append(commands, cmd)
	}
	for _, name := range sortedAliasNames() {
		alias := commandAliases[name]
		target := byName[alias.command]
		cmd := completionCommand{Name: name, Summary: alias.summary, Flags: target.Flags}
		// A plain alias is its target; a rewriting alias supplies the
		// target's subcommand itself, so only the flags are offered.
		if alias.rewrite == nil {
			cmd.Subcommands = target.Subcommands
		}
		commands = append(commands, cmd)
	}
	commands = append(commands,
		completionCommand{Name: "help", Summary: "Print the help of a command", CompletesCommands: true},
		completionCommand{Name: "version", Summary: "Print the CLI version"},
	)
	sort.SliceStable(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })
	return completionModel{Commands: commands, Globals: globalCompletionFlags}
}

// completionSubcommands turns the grammar rows into bare words with their
// help ("up [n]" -> "up").
func completionSubcommands(u usageSpec) []usageRow {
	var out []usageRow
	for _, row := range u.Subcommands {
		if word := strings.Fields(row.Name); len(word) > 0 {
			out = append(out, usageRow{Name: word[0], Help: row.Help})
		}
	}
	return out
}

// sectionFlagRE reads the flag a usage section documents from its title:
// "Checks (--check)" -> check.
var sectionFlagRE = regexp.MustCompile(`\(--([A-Za-z0-9_-]+)\)\s*$`)

// attachSectionValues gives a flag the values its command's grammar lists
// for it, so `doctor --check <TAB>` offers the checks.
func attachSectionValues(flags []completionFlag, u usageSpec) {
	for _, section := range u.Sections {
		m := sectionFlagRE.FindStringSubmatch(section.Title)
		if m == nil {
			continue
		}
		for i := range flags {
			if flags[i].Name == m[1] {
				flags[i].Values = section.Rows
			}
		}
	}
}

// helpFlagRE matches a flag line of flag.PrintDefaults: two spaces, a
// dash, the name, and optionally the value's type or placeholder.
var helpFlagRE = regexp.MustCompile(`^  -([A-Za-z0-9][A-Za-z0-9_-]*)(?: (\S.*))?$`)

// completionFlagsOf reads a command's flags from its own help screen,
// which every command prints on --help (TestEveryCommandHelpExitsZero):
// the flag package's defaults are the one place the flags exist as text,
// and a flag line is unmistakable — two spaces, a dash, the name, the type
// when the flag takes a value, then the help on the next line.
func completionFlagsOf(name string) []completionFlag {
	spec, ok := completionCommandTable()[name]
	if !ok {
		return nil
	}
	var out, errOut bytes.Buffer
	if err := spec.run([]string{"--help"}, strings.NewReader(""), &out, &errOut); err != nil {
		return nil
	}
	lines := strings.Split(out.String()+errOut.String(), "\n")
	var flags []completionFlag
	for i, line := range lines {
		m := helpFlagRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		f := completionFlag{Name: m[1], TakesValue: m[2] != ""}
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "    \t") {
			f.Help = strings.TrimSpace(lines[i+1])
		}
		flags = append(flags, f)
	}
	sort.SliceStable(flags, func(i, j int) bool { return flags[i].Name < flags[j].Name })
	return flags
}

// pathFlag reports whether a value flag most likely takes a path, so the
// shells that can complete files do.
func (f completionFlag) pathFlag() bool {
	if !f.TakesValue || len(f.Values) > 0 {
		return false
	}
	for _, hint := range []string{"config", "file", "dir", "path", "out", "output", "migrations", "seeds", "fixture", "input", "project", "template"} {
		if f.Name == hint || strings.HasSuffix(f.Name, "-"+hint) || strings.HasPrefix(f.Name, hint+"-") {
			return true
		}
	}
	return false
}

func (m completionModel) commandWords() []string {
	words := make([]string, 0, len(m.Commands))
	for _, c := range m.Commands {
		words = append(words, c.Name)
	}
	return words
}

// helpTargets are the words `help` accepts: every command and alias.
func (m completionModel) helpTargets() []string {
	var words []string
	for _, c := range m.Commands {
		if c.Name != "help" && c.Name != "version" {
			words = append(words, c.Name)
		}
	}
	return words
}

func flagWords(flags []completionFlag) []string {
	words := make([]string, 0, len(flags))
	for _, f := range flags {
		words = append(words, "--"+f.Name)
	}
	return words
}

func rowWords(rows []usageRow) []string {
	words := make([]string, 0, len(rows))
	for _, r := range rows {
		words = append(words, r.Name)
	}
	return words
}

const completionHeader = "# nucleus %s completion, generated by `nucleus completion %s`. Do not edit: regenerate after upgrading the CLI.\n"

// renderBash writes a completion function for bash. The command word is
// the first argument that is not a global option (or the value of one).
func (m completionModel) renderBash(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, completionHeader, "bash", "bash")
	b.WriteString(`_nucleus_completion() {
    local cur prev cmd i
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    cmd=""
    for ((i = 1; i < COMP_CWORD; i++)); do
        case "${COMP_WORDS[i]}" in
`)
	for _, g := range m.Globals {
		if g.TakesValue {
			fmt.Fprintf(&b, "            --%s|-%s) ((i++)) ;;\n", g.Name, g.Name)
		}
	}
	b.WriteString(`            -*) ;;
            *) cmd="${COMP_WORDS[i]}"; break ;;
        esac
    done
    if [[ -z "$cmd" ]]; then
        case "$prev" in
`)
	for _, g := range m.Globals {
		if len(g.Values) > 0 {
			fmt.Fprintf(&b, "            --%s|-%s) COMPREPLY=($(compgen -W %q -- \"$cur\")); return ;;\n", g.Name, g.Name, strings.Join(rowWords(g.Values), " "))
		}
	}
	fmt.Fprintf(&b, "        esac\n        if [[ \"$cur\" == -* ]]; then\n            COMPREPLY=($(compgen -W %q -- \"$cur\"))\n            return\n        fi\n", strings.Join(flagWords(m.Globals), " "))
	fmt.Fprintf(&b, "        COMPREPLY=($(compgen -W %q -- \"$cur\"))\n        return\n    fi\n    case \"$cmd\" in\n", strings.Join(m.commandWords(), " "))
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "        %s)\n", c.Name)
		if c.CompletesCommands {
			fmt.Fprintf(&b, "            COMPREPLY=($(compgen -W %q -- \"$cur\"))\n            return ;;\n", strings.Join(m.helpTargets(), " "))
			continue
		}
		valueFlags := false
		for _, f := range c.Flags {
			if len(f.Values) > 0 {
				valueFlags = true
			}
		}
		if valueFlags {
			b.WriteString("            case \"$prev\" in\n")
			for _, f := range c.Flags {
				if len(f.Values) > 0 {
					fmt.Fprintf(&b, "                --%s|-%s) COMPREPLY=($(compgen -W %q -- \"$cur\")); return ;;\n", f.Name, f.Name, strings.Join(rowWords(f.Values), " "))
				}
			}
			b.WriteString("            esac\n")
		}
		if len(c.Flags) > 0 {
			fmt.Fprintf(&b, "            if [[ \"$cur\" == -* ]]; then\n                COMPREPLY=($(compgen -W %q -- \"$cur\"))\n                return\n            fi\n", strings.Join(flagWords(c.Flags), " "))
		}
		if len(c.Subcommands) > 0 {
			fmt.Fprintf(&b, "            COMPREPLY=($(compgen -W %q -- \"$cur\"))\n", strings.Join(rowWords(c.Subcommands), " "))
		}
		b.WriteString("            return ;;\n")
	}
	b.WriteString(`    esac
}
complete -o default -F _nucleus_completion nucleus
`)
	_, err := io.WriteString(w, b.String())
	return err
}

// zshText makes a help text safe inside a single-quoted zsh _arguments or
// _describe spec: the quote is escaped the zsh way, the characters those
// specs parse (colon, brackets, backslash) are replaced.
func zshText(s string) string {
	r := strings.NewReplacer("'", "'\\''", ":", " -", "[", "(", "]", ")", "\\", "")
	return r.Replace(s)
}

// zshFlagSpec renders one flag for _arguments.
func zshFlagSpec(f completionFlag) string {
	spec := fmt.Sprintf("'--%s[%s]", f.Name, zshText(f.Help))
	switch {
	case len(f.Values) > 0:
		spec += ":value:(" + strings.Join(rowWords(f.Values), " ") + ")"
	case f.pathFlag():
		spec += ":file:_files"
	case f.TakesValue:
		spec += ":value:"
	}
	return spec + "'"
}

// renderZsh writes a #compdef function for zsh.
func (m completionModel) renderZsh(w io.Writer) error {
	var b strings.Builder
	b.WriteString("#compdef nucleus\n")
	fmt.Fprintf(&b, completionHeader, "zsh", "zsh")
	b.WriteString("\n_nucleus() {\n    local -a commands\n    commands=(\n")
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "        '%s:%s'\n", c.Name, zshText(c.Summary))
	}
	b.WriteString("    )\n    local curcontext=\"$curcontext\" state line\n    typeset -A opt_args\n\n    _arguments -C \\\n")
	for _, g := range m.Globals {
		fmt.Fprintf(&b, "        %s \\\n", zshFlagSpec(g))
	}
	b.WriteString("        '1:command:->command' \\\n        '*::argument:->arguments'\n\n    case \"$state\" in\n        command)\n            _describe -t commands 'nucleus command' commands\n            ;;\n        arguments)\n            case \"${words[1]}\" in\n")
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "                %s)\n", c.Name)
		if c.CompletesCommands {
			fmt.Fprintf(&b, "                    _values 'command' %s\n                    ;;\n", strings.Join(m.helpTargets(), " "))
			continue
		}
		if len(c.Flags) == 0 && len(c.Subcommands) == 0 {
			b.WriteString("                    _files\n                    ;;\n")
			continue
		}
		b.WriteString("                    _arguments \\\n")
		for _, f := range c.Flags {
			fmt.Fprintf(&b, "                        %s \\\n", zshFlagSpec(f))
		}
		if len(c.Subcommands) > 0 {
			var subs []string
			for _, s := range c.Subcommands {
				subs = append(subs, fmt.Sprintf("%s\\:\"%s\"", s.Name, zshText(s.Help)))
			}
			fmt.Fprintf(&b, "                        '1:subcommand:((%s))' \\\n", strings.Join(subs, " "))
		}
		b.WriteString("                        '*:argument:_files'\n                    ;;\n")
	}
	b.WriteString("            esac\n            ;;\n    esac\n}\n\n_nucleus \"$@\"\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// fishText quotes a help text for a single-quoted fish string.
func fishText(s string) string {
	return strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s)
}

// renderFish writes `complete` lines for fish.
func (m completionModel) renderFish(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, completionHeader, "fish", "fish")
	b.WriteString("complete -c nucleus -f\n\n")
	all := strings.Join(m.commandWords(), " ")
	fmt.Fprintf(&b, "set -l __nucleus_commands %s\n\n", all)
	b.WriteString("# Global options and the command word\n")
	for _, g := range m.Globals {
		fmt.Fprintf(&b, "complete -c nucleus -n 'not __fish_seen_subcommand_from $__nucleus_commands' -l %s -d '%s'", g.Name, fishText(g.Help))
		if len(g.Values) > 0 {
			fmt.Fprintf(&b, " -x -a '%s'", strings.Join(rowWords(g.Values), " "))
		} else if g.TakesValue {
			b.WriteString(" -r")
		}
		b.WriteString("\n")
	}
	for _, c := range m.Commands {
		fmt.Fprintf(&b, "complete -c nucleus -n 'not __fish_seen_subcommand_from $__nucleus_commands' -a %s -d '%s'\n", c.Name, fishText(c.Summary))
	}
	for _, c := range m.Commands {
		if c.CompletesCommands {
			fmt.Fprintf(&b, "\n# %s\ncomplete -c nucleus -n '__fish_seen_subcommand_from %s' -a '%s'\n", c.Name, c.Name, strings.Join(m.helpTargets(), " "))
			continue
		}
		if len(c.Flags) == 0 && len(c.Subcommands) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n# %s\n", c.Name)
		for _, s := range c.Subcommands {
			fmt.Fprintf(&b, "complete -c nucleus -n '__fish_seen_subcommand_from %s' -a %s -d '%s'\n", c.Name, s.Name, fishText(s.Help))
		}
		for _, f := range c.Flags {
			fmt.Fprintf(&b, "complete -c nucleus -n '__fish_seen_subcommand_from %s' -l %s -d '%s'", c.Name, f.Name, fishText(f.Help))
			switch {
			case len(f.Values) > 0:
				fmt.Fprintf(&b, " -x -a '%s'", strings.Join(rowWords(f.Values), " "))
			case f.pathFlag():
				b.WriteString(" -r -F")
			case f.TakesValue:
				b.WriteString(" -r")
			}
			b.WriteString("\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
)

type commandAlias struct {
	command string
	summary string
	rewrite func(args []string) ([]string, error)
}

var commandAliases = map[string]commandAlias{
	"check": {
		command: "health",
		summary: "Alias of health",
	},
	"createsuperuser": {
		command: "createuser",
		summary: "Alias of createuser",
	},
	"dbshell": {
		command: "shell",
		summary: "Alias of shell",
	},
	"makemigrations": {
		command: "migrate",
		summary: "Alias of migrate create <name>",
		rewrite: rewriteMakeMigrationsArgs,
	},
	"runserver": {
		command: "serve",
		summary: "Alias of serve [addr:port]",
		rewrite: rewriteRunserverArgs,
	},
	"showmigrations": {
		command: "migrate",
		summary: "Alias of migrate status",
		rewrite: rewriteShowMigrationsArgs,
	},
	"startproject": {
		command: "new",
		summary: "Alias of new",
	},
}

func sortedAliasNames() []string {
	names := make([]string, 0, len(commandAliases))
	for name := range commandAliases {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ContractAliasCommandNames returns the sorted list of CLI alias names
// (e.g. "runserver", "makemigrations"). Mirrors ContractPrimaryCommandNames
// for callers that need to recognise both primary commands and their
// Django-style aliases — the website CLI overview parity test in
// contracts/ being one such caller.
func ContractAliasCommandNames() []string {
	return sortedAliasNames()
}

func resolveHelpCommand(name string) string {
	alias, ok := commandAliases[name]
	if !ok {
		return name
	}
	return alias.command
}

func canonicalizeCommand(name string, args []string) (string, []string, error) {
	alias, ok := commandAliases[name]
	if !ok {
		return name, args, nil
	}

	if alias.rewrite == nil {
		return alias.command, args, nil
	}

	rewritten, err := alias.rewrite(args)
	if err != nil {
		return "", nil, err
	}
	return alias.command, rewritten, nil
}

func rewriteRunserverArgs(args []string) ([]string, error) {
	fs := flag.NewFlagSet("runserver", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "", "Path to nucleus config file")
	hostFlag := fs.String("host", "", "Override host")
	portFlag := fs.Int("port", 0, "Override port")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return []string{"--help"}, nil
		}
		return nil, err
	}

	rest := fs.Args()
	if len(rest) > 1 {
		return nil, fmt.Errorf("runserver accepts at most one optional address argument")
	}

	host := ""
	port := 0
	if len(rest) == 1 {
		var err error
		host, port, err = parseRunserverAddress(rest[0])
		if err != nil {
			return nil, fmt.Errorf("invalid runserver address %q: %w", rest[0], err)
		}
	}

	if *hostFlag != "" {
		host = *hostFlag
	}
	if *portFlag != 0 {
		if *portFlag < 0 || *portFlag > 65535 {
			return nil, fmt.Errorf("invalid --port %d: expected 1-65535", *portFlag)
		}
		port = *portFlag
	}

	translated := make([]string, 0, 6)
	if *configPath != "" {
		translated = append(translated, "--config", *configPath)
	}
	if host != "" {
		translated = append(translated, "--host", host)
	}
	if port > 0 {
		translated = append(translated, "--port", strconv.Itoa(port))
	}
	return translated, nil
}

func parseRunserverAddress(value string) (string, int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, fmt.Errorf("address cannot be empty")
	}

	if isDigitsOnly(value) {
		port, err := parsePort(value)
		if err != nil {
			return "", 0, err
		}
		return "", port, nil
	}

	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, err
	}
	port, err := parsePort(portText)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("port must be numeric")
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port must be within 1-65535")
	}
	return port, nil
}

func isDigitsOnly(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}

func rewriteMakeMigrationsArgs(args []string) ([]string, error) {
	fs := flag.NewFlagSet("makemigrations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "", "Path to nucleus config file")
	migrationsPath := fs.String("migrations", "migrations", "Migrations directory")
	name := fs.String("name", "", "Migration name")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return []string{"--help"}, nil
		}
		return nil, err
	}

	rest := fs.Args()
	migrationName := strings.TrimSpace(*name)

	if migrationName == "" {
		if len(rest) != 1 {
			return nil, fmt.Errorf("makemigrations requires exactly one migration name (or --name)")
		}
		migrationName = strings.TrimSpace(rest[0])
	} else if len(rest) > 0 {
		return nil, fmt.Errorf("makemigrations accepts either --name or one positional migration name")
	}

	if migrationName == "" {
		return nil, fmt.Errorf("makemigrations requires a non-empty migration name")
	}

	translated := make([]string, 0, 6)
	if *configPath != "" {
		translated = append(translated, "--config", *configPath)
	}
	if *migrationsPath != "migrations" {
		translated = append(translated, "--migrations", *migrationsPath)
	}
	translated = append(translated, "create", migrationName)
	return translated, nil
}

func rewriteShowMigrationsArgs(args []string) ([]string, error) {
	fs := flag.NewFlagSet("showmigrations", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "", "Path to nucleus config file")
	migrationsPath := fs.String("migrations", "migrations", "Migrations directory")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return []string{"--help"}, nil
		}
		return nil, err
	}
	if len(fs.Args()) > 0 {
		return nil, fmt.Errorf("showmigrations does not accept positional arguments")
	}

	translated := make([]string, 0, 5)
	if *configPath != "" {
		translated = append(translated, "--config", *configPath)
	}
	if *migrationsPath != "migrations" {
		translated = append(translated, "--migrations", *migrationsPath)
	}
	translated = append(translated, "status")
	return translated, nil
}

package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type wizardPrompt struct {
	question    string
	defaultVal  string
	validate    func(string) error
	transform   func(string) string
	options     []string
	multiSelect bool
}

func runWizard(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("wizard", flag.ContinueOnError)
	fs.SetOutput(stderr)

	wizardType := fs.String("type", "", "Wizard type: inspectdb, new, startapp")
	configPath := fs.String("config", "", "Path to nucleus config file")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			// --help is a success, same as every other subcommand (NC-07).
			return nil
		}
		return err
	}

	rest := fs.Args()
	if len(rest) > 0 {
		return fmt.Errorf("wizard does not accept positional arguments")
	}

	if *wizardType == "" {
		return fmt.Errorf("wizard type is required (use --type inspectdb|new|startapp)")
	}

	switch *wizardType {
	case "inspectdb":
		return runInspectDBWizard(*configPath, stdin, stdout, stderr)
	case "new":
		return runNewWizard(*configPath, stdin, stdout, stderr)
	case "startapp":
		return runStartAppWizard(*configPath, stdin, stdout, stderr)
	default:
		return fmt.Errorf("unknown wizard type: %s (use inspectdb|new|startapp)", *wizardType)
	}
}

func promptUser(reader io.Reader, writer io.Writer, p wizardPrompt) (string, error) {
	scanner := bufio.NewScanner(reader)

	if len(p.options) > 0 {
		if p.multiSelect {
			return promptMultiSelect(scanner, writer, p)
		}
		return promptSelect(scanner, writer, p)
	}

	return promptUserWithScanner(scanner, writer, p)
}

func promptUserWithScanner(scanner *bufio.Scanner, writer io.Writer, p wizardPrompt) (string, error) {
	defaultStr := ""
	if p.defaultVal != "" {
		defaultStr = fmt.Sprintf(" [%s]", p.defaultVal)
	}

	fmt.Fprintf(writer, "%s%s: ", p.question, defaultStr)

	if !scanner.Scan() {
		return "", scanner.Err()
	}

	input := strings.TrimSpace(scanner.Text())
	if input == "" && p.defaultVal != "" {
		input = p.defaultVal
	}

	if p.transform != nil {
		input = p.transform(input)
	}

	if p.validate != nil {
		if err := p.validate(input); err != nil {
			fmt.Fprintf(writer, "Error: %v\n", err)
			return promptUserWithScanner(scanner, writer, p)
		}
	}

	return input, nil
}

func promptSelect(scanner *bufio.Scanner, writer io.Writer, p wizardPrompt) (string, error) {
	fmt.Fprintf(writer, "%s\n", p.question)
	for i, opt := range p.options {
		fmt.Fprintf(writer, "  [%d] %s\n", i+1, opt)
	}
	defaultStr := ""
	if p.defaultVal != "" {
		defaultStr = fmt.Sprintf(" (default: %s)", p.defaultVal)
	}
	fmt.Fprintf(writer, "Select option%s: ", defaultStr)

	if !scanner.Scan() {
		return "", scanner.Err()
	}

	input := strings.TrimSpace(scanner.Text())
	if input == "" && p.defaultVal != "" {
		input = p.defaultVal
	}

	for i, opt := range p.options {
		if fmt.Sprintf("%d", i+1) == input || strings.EqualFold(opt, input) {
			return opt, nil
		}
	}

	return "", fmt.Errorf("invalid selection: %s", input)
}

func promptMultiSelect(scanner *bufio.Scanner, writer io.Writer, p wizardPrompt) (string, error) {
	fmt.Fprintf(writer, "%s (use space to select, enter to finish)\n", p.question)
	for i, opt := range p.options {
		fmt.Fprintf(writer, "  [%d] %s\n", i+1, opt)
	}
	fmt.Fprintf(writer, "Select options (comma-separated or space-separated numbers): ")

	if !scanner.Scan() {
		return "", scanner.Err()
	}

	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return "", nil
	}

	parts := strings.FieldsFunc(input, func(r rune) bool {
		return r == ',' || r == ' '
	})

	selected := []string{}
	for _, part := range parts {
		for i, opt := range p.options {
			if fmt.Sprintf("%d", i+1) == part || strings.EqualFold(opt, part) {
				selected = append(selected, opt)
				break
			}
		}
	}

	return strings.Join(selected, ","), nil
}

func runInspectDBWizard(configPath string, stdin io.Reader, stdout, stderr io.Writer) error {
	fmt.Fprintf(stdout, "=== Nucleus inspectdb Wizard ===\n\n")

	// Step 1: Database URL
	dbURL, err := promptUser(stdin, stdout, wizardPrompt{
		question:   "Database URL",
		defaultVal: "postgres://localhost:5432/mydb",
		validate: func(s string) error {
			if s == "" {
				return fmt.Errorf("database URL cannot be empty")
			}
			return nil
		},
	})
	if err != nil {
		return err
	}

	// The wizard deliberately does NOT prompt for a table selection: it
	// never connects to the database, and an earlier version printed
	// "Fetching tables from database..." followed by a hard-coded
	// users/posts/comments/tags list — fabricated UI in an anti-hype
	// repo (NC-04). `nucleus inspectdb` itself imports every table; use
	// its own flags to narrow the selection.

	// Step 2: Output package
	outputPackage, err := promptUser(stdin, stdout, wizardPrompt{
		question:   "Output package path",
		defaultVal: "internal/models",
	})
	if err != nil {
		return err
	}

	// Step 3: Naming convention
	namingConvention, err := promptUser(stdin, stdout, wizardPrompt{
		question:   "Model naming convention",
		options:    []string{"PascalCase", "snake_case", "camelCase"},
		defaultVal: "PascalCase",
	})
	if err != nil {
		return err
	}

	// Summary
	fmt.Fprintf(stdout, "\n=== Summary ===\n")
	fmt.Fprintf(stdout, "Database URL: %s\n", dbURL)
	fmt.Fprintf(stdout, "Output package: %s\n", outputPackage)
	fmt.Fprintf(stdout, "Naming convention: %s\n", namingConvention)

	return fmt.Errorf("wizard inspectdb is experimental and did not execute changes; run nucleus inspectdb --config %s --output %s", configPathOrDefault(configPath), outputPackage)
}

func runNewWizard(configPath string, stdin io.Reader, stdout, stderr io.Writer) error {
	fmt.Fprintf(stdout, "=== Nucleus new Wizard ===\n\n")
	return fmt.Errorf("wizard new is experimental and did not execute changes; run nucleus new <name> --module <module>")
}

func runStartAppWizard(configPath string, stdin io.Reader, stdout, stderr io.Writer) error {
	fmt.Fprintf(stdout, "=== Nucleus startapp Wizard ===\n\n")
	return fmt.Errorf("wizard startapp is experimental and did not execute changes; run nucleus startapp <name> --out <path>")
}

func configPathOrDefault(path string) string {
	if strings.TrimSpace(path) == "" {
		return "nucleus.yml"
	}
	return path
}

package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type goTestOptions struct {
	run      string
	count    int
	race     bool
	verbose  bool
	failfast bool
	cover    bool
	timeout  time.Duration
}

func runTest(args []string, _ io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)

	runPattern := fs.String("run", "", "Run only tests matching the regex")
	count := fs.Int("count", 1, "Run each test and benchmark n times")
	race := fs.Bool("race", false, "Enable race detector")
	verbose := fs.Bool("v", false, "Enable verbose output")
	failfast := fs.Bool("failfast", false, "Stop after first test failure")
	cover := fs.Bool("cover", false, "Enable coverage reporting")
	timeout := fs.Duration("timeout", 0, "Set go test timeout (e.g. 60s)")
	dryRun := fs.Bool("dry-run", false, "Print generated go test command without executing it")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	packages := fs.Args()
	if len(packages) == 0 {
		packages = []string{"./..."}
	}

	goArgs, err := buildGoTestArgs(packages, goTestOptions{
		run:      *runPattern,
		count:    *count,
		race:     *race,
		verbose:  *verbose,
		failfast: *failfast,
		cover:    *cover,
		timeout:  *timeout,
	})
	if err != nil {
		return err
	}

	if *dryRun {
		fmt.Fprintf(stdout, "go %s\n", strings.Join(goArgs, " "))
		return nil
	}

	cmd := exec.Command("go", goArgs...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("go test failed")
		}
		return fmt.Errorf("run go test: %w", err)
	}
	return nil
}

func buildGoTestArgs(packages []string, options goTestOptions) ([]string, error) {
	if len(packages) == 0 {
		return nil, fmt.Errorf("at least one package pattern is required")
	}
	if options.count <= 0 {
		return nil, fmt.Errorf("count must be greater than 0")
	}
	if options.timeout < 0 {
		return nil, fmt.Errorf("timeout cannot be negative")
	}

	args := make([]string, 0, 16+len(packages))
	args = append(args, "test")
	if options.run != "" {
		args = append(args, "-run", options.run)
	}
	if options.count != 1 {
		args = append(args, "-count", strconv.Itoa(options.count))
	}
	if options.race {
		args = append(args, "-race")
	}
	if options.verbose {
		args = append(args, "-v")
	}
	if options.failfast {
		args = append(args, "-failfast")
	}
	if options.cover {
		args = append(args, "-cover")
	}
	if options.timeout > 0 {
		args = append(args, "-timeout", options.timeout.String())
	}
	args = append(args, packages...)
	return args, nil
}

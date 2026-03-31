package main

import (
	"io"
	"os"

	"github.com/jcsvwinston/GoFrame/internal/cli"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return cli.Run(args, os.Stdin, stdout, stderr)
}

func runWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return cli.Run(args, stdin, stdout, stderr)
}

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

var version = "dev"

type env struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Getenv       func(string) string
	IsTerminal   func() bool
	ReadPassword func() (string, error)
}

func defaultEnv() env {
	return env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,
		IsTerminal: func() bool {
			return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
		},
		ReadPassword: func() (string, error) {
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			return string(b), err
		},
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], defaultEnv()))
}

const usage = `usage: garmin-mcp <command>

commands:
  version    print the version
`

func run(ctx context.Context, args []string, e env) int {
	if len(args) == 0 {
		fmt.Fprint(e.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(e.Stdout, "garmin-mcp %s\n", version)
		return 0
	default:
		fmt.Fprintf(e.Stderr, "unknown command %q\n", args[0])
		fmt.Fprint(e.Stderr, usage)
		return 2
	}
}

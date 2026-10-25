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
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	return env{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		IsTerminal: func() bool { return term.IsTerminal(in) && term.IsTerminal(out) },
		ReadPassword: func() (string, error) {
			b, err := term.ReadPassword(in)
			return string(b), err
		},
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], defaultEnv())
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, e env) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version":
			fmt.Fprintf(e.Stdout, "garmin-mcp %s\n", version)
			return 0
		}
		fmt.Fprintf(e.Stderr, "unknown command %q\n", args[0])
	}
	fmt.Fprintln(e.Stderr, "usage: garmin-mcp <command>\n\ncommands:\n  version    print version")
	return 2
}

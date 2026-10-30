package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/elgatito/garmin-connect-go-mcp/internal/server"
)

var version = "dev"

type env struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Getenv         func(string) string
	IsTerminal     func() bool
	ReadPassword   func() (string, error)
	Endpoints      func(string) garmin.Endpoints
	Login          *garmin.LoginOptions
}

func defaultEnv() env {
	return env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv, IsTerminal: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) }, ReadPassword: func() (string, error) { b, err := term.ReadPassword(int(os.Stdin.Fd())); return string(b), err }}
}

func run(ctx context.Context, args []string, e env) int {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Fprintln(e.Stdout, "garmin-mcp", version)
		return 0
	}
	if e.Getenv == nil {
		e.Getenv = os.Getenv
	}
	base, err := accounts.DefaultBase(e.Getenv)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	store := accounts.Store{Base: base}
	if len(args) == 0 {
		if e.IsTerminal != nil && e.IsTerminal() {
			names, err := store.List()
			if err != nil {
				fmt.Fprintln(e.Stderr, err)
				return 1
			}
			if len(names) == 0 {
				fmt.Fprintln(e.Stdout, "Welcome to garmin-mcp. Let's connect your first Garmin account.")
				return auth(ctx, e, store, nil)
			}
			showAccounts(e, store)
			fmt.Fprintln(e.Stdout, "Usage: garmin-mcp [serve|auth|accounts|remove|version]")
			return 0
		}
		return serve(ctx, e, store)
	}
	switch args[0] {
	case "serve":
		return serve(ctx, e, store)
	case "auth":
		return auth(ctx, e, store, args[1:])
	case "accounts":
		return showAccounts(e, store)
	case "remove":
		return remove(e, store, args[1:])
	default:
		fmt.Fprintf(e.Stderr, "unknown command %q\n", args[0])
		fmt.Fprintln(e.Stderr, "Usage: garmin-mcp [serve|auth|accounts|remove|version]")
		return 2
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], defaultEnv()))
}

func serve(ctx context.Context, e env, store accounts.Store) int {
	srv, err := server.New(server.Options{Store: store, Endpoints: e.Endpoints, Getenv: e.Getenv, Version: version, Log: e.Stderr})
	if err == nil {
		err = srv.Run(ctx, stdioTransport(ctx, e.Stdin, e.Stdout))
	}
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	return 0
}

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/term"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/elgatito/garmin-connect-go-mcp/internal/server"
)

var version = "dev"

type env struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Getenv       func(string) string
	IsTerminal   func() bool
	ReadPassword func() (string, error)
	Endpoints    func(domain string) garmin.Endpoints // nil: garmin.DefaultEndpoints
	Login        *garmin.LoginOptions                 // nil: every strategy, system roots
}

func defaultEnv() env {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	return env{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Getenv:     os.Getenv,
		Endpoints:  garmin.DefaultEndpoints,
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

const usage = `usage: garmin-mcp <command>

commands:
  auth [--email E] [--cn] [--force]   log in and save a Garmin account
  accounts                            list saved accounts
  remove <email> [--yes]              delete a saved account
  serve                               run the MCP server over stdio
  version                             print version`

func serve(ctx context.Context, e env, st accounts.Store) int {
	srv, err := server.New(server.Options{Store: st, Endpoints: e.Endpoints, Getenv: e.Getenv, Version: version, Log: e.Stderr})
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	// Same as mcp.StdioTransport, but over e's streams so tests can drive it.
	in, ok := e.Stdin.(io.ReadCloser)
	if !ok {
		in = io.NopCloser(e.Stdin)
	}
	if err := srv.Run(ctx, &mcp.IOTransport{Reader: in, Writer: nopWriteCloser{e.Stdout}}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	return 0
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func run(ctx context.Context, args []string, e env) int {
	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "version", "--version":
		fmt.Fprintf(e.Stdout, "garmin-mcp %s\n", version)
		return 0
	case "", "auth", "accounts", "remove", "serve":
	default:
		fmt.Fprintf(e.Stderr, "unknown command %q\n%s\n", cmd, usage)
		return 2
	}
	if e.Endpoints == nil {
		e.Endpoints = garmin.DefaultEndpoints
	}
	base, err := accounts.DefaultBase(e.Getenv)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	st := accounts.Store{Base: base}
	switch cmd {
	case "auth":
		return cmdAuth(ctx, args[1:], e, st)
	case "accounts":
		return cmdAccounts(e, st)
	case "remove":
		return cmdRemove(args[1:], e, st)
	case "serve":
		return serve(ctx, e, st)
	}
	if !e.IsTerminal() {
		return serve(ctx, e, st)
	}
	list, err := st.List()
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(e.Stdout, "Welcome to garmin-mcp. Let's connect your first Garmin account.")
		return cmdAuth(ctx, nil, e, st)
	}
	if code := cmdAccounts(e, st); code != 0 {
		return code
	}
	fmt.Fprintln(e.Stdout, "\n"+usage)
	return 0
}

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/elgatito/garmin-connect-go-mcp/internal/server"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	Endpoints    func(domain string) garmin.Endpoints
	Login        *garmin.LoginOptions
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
		Endpoints: garmin.DefaultEndpoints,
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], defaultEnv()))
}

const usage = `usage: garmin-mcp <command>

commands:
  serve      run the MCP server over stdio
  auth       log in and save a Garmin account
  accounts   list configured accounts
  remove     remove an account
  version    print the version
`

func run(ctx context.Context, args []string, e env) int {
	if len(args) > 0 {
		switch args[0] {
		case "version", "--version":
			fmt.Fprintf(e.Stdout, "garmin-mcp %s\n", version)
			return 0
		case "auth", "accounts", "remove", "serve":
			st, err := storeFor(e)
			if err != nil {
				fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
				return 1
			}
			switch args[0] {
			case "auth":
				return runAuth(ctx, e, st, args[1:])
			case "accounts":
				return runAccounts(e, st)
			case "remove":
				return runRemove(e, st, args[1:])
			default:
				return serve(ctx, e, st)
			}
		default:
			fmt.Fprintf(e.Stderr, "unknown command %q\n", args[0])
			fmt.Fprint(e.Stderr, usage)
			return 2
		}
	}

	st, err := storeFor(e)
	if err != nil {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	if e.IsTerminal == nil || !e.IsTerminal() {
		return serve(ctx, e, st)
	}
	emails, err := st.List()
	if err != nil {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	if len(emails) == 0 {
		fmt.Fprintln(e.Stdout, "Welcome to garmin-mcp. Let's connect your first Garmin account.")
		return runAuth(ctx, e, st, nil)
	}
	printAccounts(e.Stdout, st, emails)
	fmt.Fprint(e.Stdout, usage)
	return 0
}

// serve runs the MCP server over stdio.
func serve(ctx context.Context, e env, st accounts.Store) int {
	srv, err := server.New(server.Options{
		Store:     st,
		Endpoints: e.Endpoints,
		Getenv:    e.Getenv,
		Version:   version,
		Log:       e.Stderr,
	})
	if err != nil {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	// IOTransport rather than StdioTransport so a piped request is handled
	// before the EOF that follows it closes the session.
	transport := &mcp.IOTransport{
		Reader: io.NopCloser(eofGraceReader{e.Stdin}),
		Writer: nopWriteCloser{e.Stdout},
	}
	if err := srv.Run(ctx, transport); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	return 0
}

// eofGraceReader delays EOF briefly so already-buffered requests are handled
// before the session closes.
type eofGraceReader struct{ io.Reader }

func (r eofGraceReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n == 0 && errors.Is(err, io.EOF) {
		time.Sleep(200 * time.Millisecond)
	}
	return n, err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func storeFor(e env) (accounts.Store, error) {
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	base, err := accounts.DefaultBase(getenv)
	if err != nil {
		return accounts.Store{}, err
	}
	return accounts.Store{Base: base}, nil
}

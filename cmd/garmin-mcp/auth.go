package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

type tokenStore struct {
	store accounts.Store
	email string
}

func (t tokenStore) Load() (garmin.Tokens, error) { return t.store.LoadTokens(t.email) }
func (t tokenStore) Save(v garmin.Tokens) error   { return t.store.SaveTokens(t.email, v) }
func endpoint(e env, domain string) garmin.Endpoints {
	if e.Endpoints != nil {
		return e.Endpoints(domain)
	}
	return garmin.DefaultEndpoints(domain)
}
func status(t garmin.Tokens, err error) string {
	if err != nil {
		return "invalid"
	}
	exp, ok := t.Expiry()
	if !ok {
		return "invalid"
	}
	if time.Now().After(exp) {
		return "expired"
	}
	return "ok"
}
func showAccounts(e env, store accounts.Store) int {
	names, err := store.List()
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintln(e.Stdout, `No accounts. Run "garmin-mcp auth" to add one.`)
		return 0
	}
	for _, name := range names {
		st, err := store.LoadSettings(name)
		if err != nil {
			fmt.Fprintln(e.Stderr, err)
			return 1
		}
		tokens, err := store.LoadTokens(name)
		fmt.Fprintf(e.Stdout, "%s  %s  %s\n", name, st.Domain, status(tokens, err))
	}
	return 0
}
func auth(ctx context.Context, e env, store accounts.Store, args []string) int {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	email := fs.String("email", "", "account email")
	cn := fs.Bool("cn", false, "use Garmin China")
	force := fs.Bool("force", false, "replace session")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	reader := bufio.NewReader(e.Stdin)
	read := func(prompt string) (string, error) {
		fmt.Fprint(e.Stdout, prompt)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		if err == io.EOF && line == "" {
			return "", io.EOF
		}
		return strings.TrimSpace(line), nil
	}
	if *email == "" {
		var err error
		*email, err = read("Email: ")
		if err != nil {
			fmt.Fprintln(e.Stderr, err)
			return 1
		}
	}
	name, err := accounts.Normalize(*email)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	old, err := store.LoadTokens(name)
	if err == nil && !old.ExpiresSoon(time.Now()) && !*force {
		fmt.Fprintf(e.Stdout, "Account %s already has a valid session. Use --force to log in again.\n", name)
		return 0
	}
	st, err := store.LoadSettings(name)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	if *cn {
		st.Domain = "garmin.cn"
	}
	if st.Domain == "" {
		st.Domain = "garmin.com"
	}
	fmt.Fprint(e.Stdout, "Password: ")
	password, err := e.ReadPassword()
	fmt.Fprintln(e.Stdout)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	opt := garmin.LoginOptions{Log: e.Stderr}
	if e.Login != nil {
		opt = *e.Login
	}
	tokens, err := garmin.Login(ctx, endpoint(e, st.Domain), name, password, func(ctx context.Context) (string, error) { return read("Enter the MFA code Garmin sent you: ") }, &opt)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Login failed: %v\n", err)
		return 1
	}
	if err = store.SaveTokens(name, tokens); err == nil {
		err = store.SaveSettings(name, st)
	}
	if err != nil {
		fmt.Fprintf(e.Stderr, "Login failed: %v\n", err)
		return 1
	}
	client, err := garmin.NewClient(endpoint(e, st.Domain), tokenStore{store, name}, nil)
	if err == nil {
		var p garmin.Profile
		p, err = client.Profile(ctx)
		if err == nil {
			dir, _ := store.Dir(name)
			fmt.Fprintf(e.Stdout, "Logged in as %s (%s)\nSaved to %s\n", p.FullName, name, dir)
			return 0
		}
	}
	fmt.Fprintf(e.Stderr, "Login failed: %v\n", err)
	return 1
}
func remove(e env, store accounts.Store, args []string) int {
	yes := false
	var name string
	for _, arg := range args {
		if arg == "--yes" {
			yes = true
		} else if name == "" {
			name = arg
		} else {
			fmt.Fprintln(e.Stderr, "usage: garmin-mcp remove <email> [--yes]")
			return 2
		}
	}
	normalized, err := accounts.Normalize(name)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	names, err := store.List()
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	found := false
	for _, n := range names {
		if n == normalized {
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(e.Stderr, "No account %s\n", normalized)
		return 1
	}
	if !yes {
		fmt.Fprintf(e.Stdout, "Remove %s? [y/N] ", normalized)
		answer, _ := bufio.NewReader(e.Stdin).ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			return 0
		}
	}
	if err = store.Remove(normalized); err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	return 0
}

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

func runAuth(ctx context.Context, e env, st accounts.Store, args []string) int {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	emailFlag := fs.String("email", "", "account email")
	cn := fs.Bool("cn", false, "use Garmin Connect China (garmin.cn)")
	force := fs.Bool("force", false, "log in again even with a valid session")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	raw := *emailFlag
	if raw == "" {
		fmt.Fprint(e.Stdout, "Email: ")
		line, err := readLine(e.Stdin)
		if err != nil {
			return loginFailed(e, err)
		}
		raw = line
	}
	email, err := accounts.Normalize(raw)
	if err != nil {
		return loginFailed(e, err)
	}

	if !*force {
		if tok, err := st.LoadTokens(email); err == nil {
			if _, ok := tok.Expiry(); ok && !tok.ExpiresSoon(time.Now()) {
				fmt.Fprintf(e.Stdout, "Account %s already has a valid session. Use --force to log in again.\n", email)
				return 0
			}
		}
	}

	if e.ReadPassword == nil {
		return loginFailed(e, fmt.Errorf("no password prompt available"))
	}
	fmt.Fprint(e.Stdout, "Password: ")
	password, err := e.ReadPassword()
	if err != nil {
		return loginFailed(e, err)
	}
	fmt.Fprintln(e.Stdout)

	domain := "garmin.com"
	if *cn {
		domain = "garmin.cn"
	}
	endpoints := e.Endpoints
	if endpoints == nil {
		endpoints = garmin.DefaultEndpoints
	}
	ep := endpoints(domain)

	mfa := func(context.Context) (string, error) {
		fmt.Fprint(e.Stdout, "Enter the MFA code Garmin sent you: ")
		line, err := readLine(e.Stdin)
		return strings.TrimSpace(line), err
	}
	tok, err := garmin.Login(ctx, ep, email, password, mfa, e.Login)
	if err != nil {
		return loginFailed(e, err)
	}
	if err := st.SaveTokens(email, tok); err != nil {
		return loginFailed(e, err)
	}

	prev, _ := st.LoadSettings(email)
	settings := accounts.Settings{Domain: domain, FitDownloadDir: prev.FitDownloadDir}
	if err := st.SaveSettings(email, settings); err != nil {
		return loginFailed(e, err)
	}

	client, err := garmin.NewClient(ep, accountTokens{st: st, email: email}, nil)
	if err != nil {
		return loginFailed(e, err)
	}
	profile, err := client.Profile(ctx)
	if err != nil {
		return loginFailed(e, err)
	}

	dir, err := st.Dir(email)
	if err != nil {
		return loginFailed(e, err)
	}
	fmt.Fprintf(e.Stdout, "Logged in as %s (%s)\n", profile.FullName, email)
	fmt.Fprintf(e.Stdout, "Saved to %s\n", dir)
	return 0
}

func loginFailed(e env, err error) int {
	fmt.Fprintf(e.Stderr, "Login failed: %v\n", err)
	return 1
}

func runAccounts(e env, st accounts.Store) int {
	emails, err := st.List()
	if err != nil {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	if len(emails) == 0 {
		fmt.Fprintln(e.Stdout, `No accounts. Run "garmin-mcp auth" to add one.`)
		return 0
	}
	printAccounts(e.Stdout, st, emails)
	return 0
}

func printAccounts(w io.Writer, st accounts.Store, emails []string) {
	for _, email := range emails {
		settings, err := st.LoadSettings(email)
		domain := settings.Domain
		if err != nil || domain == "" {
			domain = "garmin.com"
		}
		fmt.Fprintf(w, "%s  %s  %s\n", email, domain, tokenStatus(st, email))
	}
}

func tokenStatus(st accounts.Store, email string) string {
	tok, err := st.LoadTokens(email)
	if err != nil {
		return "invalid"
	}
	if _, ok := tok.Expiry(); !ok {
		return "invalid"
	}
	if tok.ExpiresSoon(time.Now()) {
		return "expired"
	}
	return "ok"
}

func runRemove(e env, st accounts.Store, args []string) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	yes := fs.Bool("yes", false, "skip confirmation")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(e.Stderr, "usage: garmin-mcp remove <email> [--yes]")
		return 2
	}
	raw := fs.Arg(0)
	email, err := accounts.Normalize(raw)
	if err != nil {
		fmt.Fprintf(e.Stderr, "No account %s\n", raw)
		return 1
	}

	emails, err := st.List()
	if err != nil {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	found := false
	for _, known := range emails {
		if known == email {
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(e.Stderr, "No account %s\n", email)
		return 1
	}

	if !*yes {
		fmt.Fprintf(e.Stdout, "Remove %s? [y/N] ", email)
		line, err := readLine(e.Stdin)
		if err != nil {
			fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
			return 1
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
		default:
			return 0
		}
	}
	if err := st.Remove(email); err != nil {
		fmt.Fprintf(e.Stderr, "garmin-mcp: %v\n", err)
		return 1
	}
	return 0
}

// flagsFirst moves flag-like args before positionals so `remove <email> --yes` parses.
func flagsFirst(args []string) []string {
	var flags, pos []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
		} else {
			pos = append(pos, arg)
		}
	}
	return append(flags, pos...)
}

// readLine reads one line one byte at a time so it never buffers the password
// held back for term.ReadPassword.
func readLine(r io.Reader) (string, error) {
	var b strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			switch buf[0] {
			case '\n':
				return b.String(), nil
			case '\r':
			default:
				b.WriteByte(buf[0])
			}
		}
		if err != nil {
			if err == io.EOF {
				return b.String(), nil
			}
			return "", err
		}
	}
}

type accountTokens struct {
	st    accounts.Store
	email string
}

func (a accountTokens) Load() (garmin.Tokens, error) { return a.st.LoadTokens(a.email) }
func (a accountTokens) Save(t garmin.Tokens) error   { return a.st.SaveTokens(a.email, t) }

package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// parseFlags returns (exit code, true) when the caller should stop.
func parseFlags(fs *flag.FlagSet, args []string, e env) (int, bool) {
	fs.SetOutput(e.Stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, true
		}
		return 2, true
	}
	return 0, false
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func cmdAuth(ctx context.Context, args []string, e env, st accounts.Store) int {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	email := fs.String("email", "", "account email")
	cn := fs.Bool("cn", false, "use Garmin Connect China (garmin.cn)")
	force := fs.Bool("force", false, "log in again even if the session is valid")
	if code, stop := parseFlags(fs, args, e); stop {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.Stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	in := bufio.NewReader(e.Stdin) // one reader for the email and MFA lines, or buffered input is lost
	if *email == "" {
		fmt.Fprint(e.Stdout, "Garmin email: ")
		line, err := readLine(in)
		if err != nil {
			fmt.Fprintf(e.Stderr, "Login failed: reading email: %v\n", err)
			return 1
		}
		*email = line
	}
	addr, err := accounts.Normalize(*email)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}

	if tok, err := st.LoadTokens(addr); err == nil && !*force && !tok.ExpiresSoon(time.Now()) {
		fmt.Fprintf(e.Stdout, "Account %s already has a valid session. Use --force to log in again.\n", addr)
		return 0
	}

	fmt.Fprint(e.Stdout, "Garmin password: ")
	pw, err := e.ReadPassword()
	fmt.Fprintln(e.Stdout)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Login failed: reading password: %v\n", err)
		return 1
	}

	// An existing account keeps its stored region; --cn forces China.
	prior, _ := st.LoadSettings(addr)
	domain := prior.Domain
	if *cn {
		domain = "garmin.cn"
	}
	if domain == "garmin.cn" {
		fmt.Fprintln(e.Stdout, "Using Garmin Connect China (garmin.cn)")
	} else {
		fmt.Fprintln(e.Stdout, "Using Garmin Connect (garmin.com)")
	}
	ep := e.Endpoints(domain)
	mfa := func(context.Context) (string, error) {
		fmt.Fprint(e.Stdout, "Enter the MFA code Garmin sent you: ")
		return readLine(in)
	}
	tok, err := garmin.Login(ctx, ep, addr, pw, mfa, e.Login)
	if err != nil {
		fmt.Fprintf(e.Stderr, "Login failed: %v\n", err)
		return 1
	}
	if err := st.SaveTokens(addr, tok); err != nil {
		fmt.Fprintf(e.Stderr, "Login failed: saving tokens: %v\n", err)
		return 1
	}
	settings, err := st.LoadSettings(addr)
	if err != nil {
		settings = accounts.Settings{} // corrupt settings.json: overwrite it
	}
	settings.Domain = domain
	if err := st.SaveSettings(addr, settings); err != nil {
		fmt.Fprintf(e.Stderr, "Login failed: saving settings: %v\n", err)
		return 1
	}

	client, err := garmin.NewClient(ep, st.TokenStore(addr), nil)
	if err == nil {
		var p garmin.Profile
		if p, err = client.Profile(ctx); err == nil {
			dir, _ := st.Dir(addr)
			fmt.Fprintf(e.Stdout, "Logged in as %s (%s)\nSaved to %s\n", p.FullName, addr, dir)
			return 0
		}
	}
	fmt.Fprintf(e.Stderr, "Login failed: profile check: %v\n", err)
	return 1
}

func cmdAccounts(e env, st accounts.Store) int {
	list, err := st.List()
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	if len(list) == 0 {
		fmt.Fprintln(e.Stdout, `No accounts. Run "garmin-mcp auth" to add one.`)
		return 0
	}
	for _, email := range list {
		settings, _ := st.LoadSettings(email) // falls back to garmin.com on error
		fmt.Fprintf(e.Stdout, "%s  %s  %s\n", email, settings.Domain, st.TokenStatus(email))
	}
	return 0
}

func cmdRemove(args []string, e env, st accounts.Store) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if code, stop := parseFlags(fs, args, e); stop {
		return code
	}
	email, rest := fs.Arg(0), []string(nil)
	if fs.NArg() > 1 { // allow flags after the email
		if code, stop := parseFlags(fs, fs.Args()[1:], e); stop {
			return code
		}
		rest = fs.Args()
	}
	if email == "" || len(rest) > 0 {
		fmt.Fprintln(e.Stderr, "usage: garmin-mcp remove <email> [--yes]")
		return 2
	}
	addr, err := accounts.Normalize(email)
	if err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	if _, err := st.LoadTokens(addr); errors.Is(err, accounts.ErrNoAccount) {
		fmt.Fprintf(e.Stderr, "No account %s\n", addr)
		return 1
	}
	if !*yes {
		fmt.Fprintf(e.Stdout, "Remove %s? [y/N] ", addr)
		line, _ := readLine(bufio.NewReader(e.Stdin))
		if a := strings.ToLower(line); a != "y" && a != "yes" {
			fmt.Fprintln(e.Stdout, "Not removed.")
			return 0
		}
	}
	if err := st.Remove(addr); err != nil {
		fmt.Fprintln(e.Stderr, err)
		return 1
	}
	fmt.Fprintf(e.Stdout, "Removed %s\n", addr)
	return 0
}

// Package accounts stores per-email Garmin tokens and settings under a base directory.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

type Settings struct {
	Domain         string `json:"domain"`
	FitDownloadDir string `json:"fit_download_dir"`
}

var ErrNoAccount = errors.New("account not found")

var emailRe = regexp.MustCompile(`^[^@\s/\\:*?"<>|]+@[^@\s/\\:*?"<>|]+$`)

// DefaultBase returns $GARMIN_MCP_HOME, or <user home>/.garmin-mcp.
func DefaultBase(getenv func(string) string) (string, error) {
	if v := getenv("GARMIN_MCP_HOME"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".garmin-mcp"), nil
}

// Normalize trims and lowercases an email and rejects anything that could escape the accounts folder.
func Normalize(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if !emailRe.MatchString(e) || strings.HasPrefix(e, ".") || strings.Contains(e, "..") {
		return "", fmt.Errorf("invalid email %q", email)
	}
	return e, nil
}

type Store struct{ Base string }

func (s Store) Dir(email string) (string, error) {
	e, err := Normalize(email)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Base, "accounts", e), nil
}

func (s Store) LoadTokens(email string) (garmin.Tokens, error) {
	var t garmin.Tokens
	dir, err := s.Dir(email)
	if err != nil {
		return t, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if errors.Is(err, os.ErrNotExist) {
		return t, fmt.Errorf("%w: %s", ErrNoAccount, email)
	}
	if err != nil {
		return t, err
	}
	if err := json.Unmarshal(b, &t); err != nil {
		return garmin.Tokens{}, fmt.Errorf("corrupt tokens.json for %s: %w", email, err)
	}
	if t.DIToken == "" {
		return garmin.Tokens{}, fmt.Errorf("tokens.json for %s has no di_token", email)
	}
	return t, nil
}

func (s Store) SaveTokens(email string, t garmin.Tokens) error {
	return s.save(email, "tokens.json", t)
}

func (s Store) LoadSettings(email string) (Settings, error) {
	st := Settings{Domain: "garmin.com"}
	dir, err := s.Dir(email)
	if err != nil {
		return st, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return Settings{Domain: "garmin.com"}, fmt.Errorf("corrupt settings.json for %s: %w", email, err)
	}
	if st.Domain == "" {
		st.Domain = "garmin.com"
	}
	return st, nil
}

func (s Store) SaveSettings(email string, st Settings) error {
	return s.save(email, "settings.json", st)
}

// TokenStore adapts the store to garmin.TokenStore for one email.
func (s Store) TokenStore(email string) garmin.TokenStore { return emailTokens{s, email} }

type emailTokens struct {
	st    Store
	email string
}

func (t emailTokens) Load() (garmin.Tokens, error) { return t.st.LoadTokens(t.email) }
func (t emailTokens) Save(tok garmin.Tokens) error { return t.st.SaveTokens(t.email, tok) }

// TokenStatus is "ok", "expired" (the access token is past exp; the refresh token may still work) or "invalid".
func (s Store) TokenStatus(email string) string {
	tok, err := s.LoadTokens(email)
	if err != nil {
		return "invalid"
	}
	if exp, ok := tok.Expiry(); ok && time.Now().After(exp) {
		return "expired"
	}
	return "ok"
}

// List returns the sorted emails whose folder holds a tokens.json.
func (s Store) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Base, "accounts"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(s.Base, "accounts", e.Name(), "tokens.json")); err == nil {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s Store) Remove(email string) error {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// save writes v as JSON atomically: temp file in the same dir, then rename over the target.
func (s Store) save(email, name string, v any) (err error) {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}

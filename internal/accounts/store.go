// Package accounts stores per-email Garmin credentials and settings on disk.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// ErrNoAccount reports that no stored tokens exist for an account.
var ErrNoAccount = errors.New("account not found")

// Settings holds the per-account preferences.
type Settings struct {
	Domain         string `json:"domain"`
	FitDownloadDir string `json:"fit_download_dir"`
}

// Store keeps accounts under Base/accounts/<email>.
type Store struct {
	Base string
}

var emailPattern = regexp.MustCompile(`^[^@\s/\\:*?"<>|]+@[^@\s/\\:*?"<>|]+$`)

// DefaultBase returns $GARMIN_MCP_HOME, or ~/.garmin-mcp when it is unset.
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

// Normalize trims, lowercases and validates an email address.
func Normalize(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if strings.HasPrefix(e, ".") || strings.Contains(e, "..") || !emailPattern.MatchString(e) {
		return "", fmt.Errorf("invalid email %q", email)
	}
	return e, nil
}

// Dir returns the account directory. It does not create it.
func (s Store) Dir(email string) (string, error) {
	e, err := Normalize(email)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.Base, "accounts", e), nil
}

// LoadTokens reads the account tokens, wrapping ErrNoAccount when they are missing.
func (s Store) LoadTokens(email string) (garmin.Tokens, error) {
	dir, err := s.Dir(email)
	if err != nil {
		return garmin.Tokens{}, err
	}
	path := filepath.Join(dir, "tokens.json")
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return garmin.Tokens{}, fmt.Errorf("%w: %s", ErrNoAccount, email)
		}
		return garmin.Tokens{}, err
	}
	var t garmin.Tokens
	if err := json.Unmarshal(b, &t); err != nil {
		return garmin.Tokens{}, fmt.Errorf("decode %s: %w", path, err)
	}
	if t.DIToken == "" {
		return garmin.Tokens{}, fmt.Errorf("%s: empty di_token", path)
	}
	return t, nil
}

// SaveTokens writes the account tokens, creating the account directory.
func (s Store) SaveTokens(email string, t garmin.Tokens) error {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "tokens.json"), t)
}

// LoadSettings reads account settings; a missing file yields the defaults.
func (s Store) LoadSettings(email string) (Settings, error) {
	dir, err := s.Dir(email)
	if err != nil {
		return Settings{}, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Settings{Domain: "garmin.com"}, nil
		}
		return Settings{}, err
	}
	var st Settings
	if err := json.Unmarshal(b, &st); err != nil {
		return Settings{}, err
	}
	return st, nil
}

// SaveSettings writes account settings, creating the account directory.
func (s Store) SaveSettings(email string, st Settings) error {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "settings.json"), st)
}

// List returns the sorted emails that have stored tokens.
func (s Store) List() ([]string, error) {
	root := filepath.Join(s.Base, "accounts")
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	emails := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "tokens.json")); err != nil {
			continue
		}
		emails = append(emails, e.Name())
	}
	sort.Strings(emails)
	return emails, nil
}

// Remove deletes the account directory.
func (s Store) Remove(email string) error {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, b)
}

// writeFileAtomic writes data to a temp file and renames it over path.
func writeFileAtomic(path string, data []byte) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// os.Rename cannot replace an existing file on Windows.
	if runtime.GOOS == "windows" {
		os.Remove(path)
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

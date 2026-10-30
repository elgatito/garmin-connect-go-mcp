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
	"unicode"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

type Settings struct {
	Domain         string `json:"domain"`
	FitDownloadDir string `json:"fit_download_dir"`
}

var ErrNoAccount = errors.New("account not found")
var emailPattern = regexp.MustCompile(`^[^@\s/\\:*?"<>|]+@[^@\s/\\:*?"<>|]+$`)

func Normalize(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if !emailPattern.MatchString(normalized) || strings.IndexFunc(normalized, unicode.IsSpace) >= 0 || strings.HasPrefix(normalized, ".") || strings.Contains(normalized, "..") {
		return "", fmt.Errorf("invalid email %q", email)
	}
	return normalized, nil
}
func DefaultBase(getenv func(string) string) (string, error) {
	if p := getenv("GARMIN_MCP_HOME"); p != "" {
		return p, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".garmin-mcp"), nil
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
	if err = json.Unmarshal(b, &t); err != nil {
		return t, err
	}
	if t.DIToken == "" {
		return t, errors.New("empty di_token")
	}
	return t, nil
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
	err = json.Unmarshal(b, &st)
	return st, err
}
func (s Store) write(email, name string, v any) error {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
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
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}
func (s Store) SaveTokens(email string, t garmin.Tokens) error {
	return s.write(email, "tokens.json", t)
}
func (s Store) SaveSettings(email string, st Settings) error {
	return s.write(email, "settings.json", st)
}
func (s Store) List() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(s.Base, "accounts"))
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		normalized, err := Normalize(e.Name())
		if err != nil || normalized != e.Name() {
			continue
		}
		if info, err := os.Stat(filepath.Join(s.Base, "accounts", e.Name(), "tokens.json")); err == nil && !info.IsDir() {
			result = append(result, e.Name())
		}
	}
	sort.Strings(result)
	return result, nil
}
func (s Store) Remove(email string) error {
	dir, err := s.Dir(email)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

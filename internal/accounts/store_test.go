package accounts

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

func newStore(t *testing.T) Store {
	t.Helper()
	return Store{Base: t.TempDir()}
}

func TestNormalize(t *testing.T) {
	ok := map[string]string{" Bob@Example.COM ": "bob@example.com", "a+b@x.io": "a+b@x.io"}
	for in, want := range ok {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "nobody", "../x@y", "a@b/../../etc", `a\b@c`, ".a@b", "a..b@c", "a@b@c", "a b@c", "a@b:c"} {
		if _, err := Normalize(bad); err == nil || err.Error() != fmt.Sprintf("invalid email %q", bad) {
			t.Errorf("%q: err=%v", bad, err)
		}
	}
}

func checkMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s: mode %04o, want %04o", path, got, want)
	}
}

func TestTokensRoundTripAndPerms(t *testing.T) {
	s := newStore(t)
	want := garmin.Tokens{DIToken: "tok", DIRefreshToken: "ref", DIClientID: "cid"}
	if err := s.SaveTokens("Bob@Example.COM", want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.LoadTokens("bob@example.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	dir, err := s.Dir("bob@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		checkMode(t, dir, 0700)
		checkMode(t, filepath.Join(dir, "tokens.json"), 0600)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}

	// A second save replaces the existing file.
	want = garmin.Tokens{DIToken: "tok2", DIRefreshToken: "ref2", DIClientID: "cid2"}
	if err := s.SaveTokens("bob@example.com", want); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, err = s.LoadTokens("bob@example.com")
	if err != nil {
		t.Fatalf("re-load: %v", err)
	}
	if got != want {
		t.Errorf("after overwrite got %+v, want %+v", got, want)
	}
	if leftovers, err := filepath.Glob(filepath.Join(dir, ".tmp-*")); err != nil || len(leftovers) != 0 {
		t.Errorf("temp files left behind after overwrite: %v %v", leftovers, err)
	}
}

func TestLoadTokensMissingIsErrNoAccount(t *testing.T) {
	s := newStore(t)
	if _, err := s.LoadTokens("nobody@example.com"); !errors.Is(err, ErrNoAccount) {
		t.Errorf("err=%v, want ErrNoAccount", err)
	}
}

func TestLoadTokensCorruptIsNotErrNoAccount(t *testing.T) {
	for _, body := range []string{"{", `{"di_token":""}`} {
		s := newStore(t)
		dir, err := s.Dir("bob@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.LoadTokens("bob@example.com"); err == nil || errors.Is(err, ErrNoAccount) {
			t.Errorf("%q: err=%v, want non-nil and not ErrNoAccount", body, err)
		}
	}
}

func TestSettingsDefault(t *testing.T) {
	s := newStore(t)
	st, err := s.LoadSettings("bob@example.com")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if st.Domain != "garmin.com" {
		t.Errorf("domain %q, want garmin.com", st.Domain)
	}

	want := Settings{Domain: "garmin.de", FitDownloadDir: "/tmp/fit"}
	if err := s.SaveSettings("bob@example.com", want); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got, err := s.LoadSettings("bob@example.com"); err != nil || got != want {
		t.Errorf("got %+v %v, want %+v", got, err, want)
	}
}

func TestListAndRemove(t *testing.T) {
	s := newStore(t)
	if got, err := s.List(); err != nil || len(got) != 0 {
		t.Fatalf("empty store: got %v %v, want empty", got, err)
	}

	for _, email := range []string{"B@x.com", "a@x.com"} {
		if err := s.SaveTokens(email, garmin.Tokens{DIToken: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(s.Base, "accounts", "stray@x.com"), 0700); err != nil {
		t.Fatal(err)
	}
	got, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a@x.com", "b@x.com"}; !reflect.DeepEqual(got, want) {
		t.Errorf("list %v, want %v", got, want)
	}

	if err := s.Remove("A@X.com"); err != nil {
		t.Fatal(err)
	}
	got, err = s.List()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"b@x.com"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after remove %v, want %v", got, want)
	}
}

func TestDefaultBase(t *testing.T) {
	got, err := DefaultBase(func(k string) string {
		if k == "GARMIN_MCP_HOME" {
			return "/tmp/z"
		}
		return ""
	})
	if err != nil || got != "/tmp/z" {
		t.Errorf("got %q %v, want /tmp/z", got, err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err = DefaultBase(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".garmin-mcp"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

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

func TestTokensRoundTripAndPerms(t *testing.T) {
	s := Store{Base: t.TempDir()}
	want := garmin.Tokens{DIToken: "a", DIRefreshToken: "b", DIClientID: "c"}
	if err := s.SaveTokens("Bob@x.com", want); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadTokens("bob@x.com")
	if err != nil || got != want {
		t.Fatalf("got %+v %v", got, err)
	}
	dir, _ := s.Dir("bob@x.com")
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
			t.Errorf("dir mode %v", fi.Mode().Perm())
		}
		if fi, _ := os.Stat(filepath.Join(dir, "tokens.json")); fi.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v", fi.Mode().Perm())
		}
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(m) > 0 {
		t.Errorf("temp files left: %v", m)
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Errorf("unexpected dir contents: %v", es)
	}
}

func TestLoadTokensMissingIsErrNoAccount(t *testing.T) {
	s := Store{Base: t.TempDir()}
	if _, err := s.LoadTokens("a@x.com"); !errors.Is(err, ErrNoAccount) {
		t.Errorf("missing dir: %v", err)
	}
	dir, _ := s.Dir("a@x.com")
	os.MkdirAll(dir, 0o700)
	if _, err := s.LoadTokens("a@x.com"); !errors.Is(err, ErrNoAccount) {
		t.Errorf("missing file: %v", err)
	}
}

func TestLoadTokensCorruptIsNotErrNoAccount(t *testing.T) {
	s := Store{Base: t.TempDir()}
	dir, _ := s.Dir("a@x.com")
	os.MkdirAll(dir, 0o700)
	for _, body := range []string{"{", `{"di_token":""}`} {
		os.WriteFile(filepath.Join(dir, "tokens.json"), []byte(body), 0o600)
		if _, err := s.LoadTokens("a@x.com"); err == nil || errors.Is(err, ErrNoAccount) {
			t.Errorf("%q: err=%v", body, err)
		}
	}
}

func TestSettingsDefault(t *testing.T) {
	s := Store{Base: t.TempDir()}
	st, err := s.LoadSettings("a@x.com")
	if err != nil || st.Domain != "garmin.com" {
		t.Fatalf("%+v %v", st, err)
	}
	want := Settings{Domain: "garmin.cn", FitDownloadDir: "/x"}
	if err := s.SaveSettings("a@x.com", want); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadSettings("a@x.com"); err != nil || got != want {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestListAndRemove(t *testing.T) {
	s := Store{Base: t.TempDir()}
	tok := garmin.Tokens{DIToken: "t"}
	for _, e := range []string{"B@x.com", "a@x.com"} {
		if err := s.SaveTokens(e, tok); err != nil {
			t.Fatal(err)
		}
	}
	d, _ := s.Dir("c@x.com")
	os.MkdirAll(d, 0o700)
	if got, err := s.List(); err != nil || !reflect.DeepEqual(got, []string{"a@x.com", "b@x.com"}) {
		t.Fatalf("%v %v", got, err)
	}
	if err := s.Remove("A@X.com"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.List(); !reflect.DeepEqual(got, []string{"b@x.com"}) {
		t.Fatalf("%v", got)
	}
	if got, err := (Store{Base: filepath.Join(s.Base, "nope")}).List(); err != nil || len(got) != 0 {
		t.Fatalf("missing base: %v %v", got, err)
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
		t.Fatalf("%q %v", got, err)
	}
	home, _ := os.UserHomeDir()
	got, err = DefaultBase(func(string) string { return "" })
	if err != nil || got != filepath.Join(home, ".garmin-mcp") {
		t.Fatalf("%q %v", got, err)
	}
}

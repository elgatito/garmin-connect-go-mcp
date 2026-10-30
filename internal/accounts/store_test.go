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
	for in, want := range map[string]string{" Bob@Example.COM ": "bob@example.com", "a+b@x.io": "a+b@x.io"} {
		got, err := Normalize(in)
		if err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "nobody", "../x@y", "a@b/../../etc", `a\b@c`, ".a@b", "a..b@c", "a@b@c", "a b@c", "a\u00a0b@c", "a@b:c"} {
		if _, err := Normalize(bad); err == nil || err.Error() != fmt.Sprintf("invalid email %q", bad) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
func TestStore(t *testing.T) {
	s := Store{Base: t.TempDir()}
	token := garmin.Tokens{DIToken: "token", DIRefreshToken: "refresh", DIClientID: "client"}
	if _, err := s.LoadTokens("a@b"); !errors.Is(err, ErrNoAccount) {
		t.Fatal(err)
	}
	st, err := s.LoadSettings("a@b")
	if err != nil || st.Domain != "garmin.com" {
		t.Fatal(st, err)
	}
	if err := s.SaveTokens("A@B", token); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadTokens("a@b")
	if err != nil || got != token {
		t.Fatal(got, err)
	}
	dir, _ := s.Dir("a@b")
	if runtime.GOOS != "windows" {
		for path, want := range map[string]os.FileMode{dir: 0700, filepath.Join(dir, "tokens.json"): 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != want {
				t.Fatalf("%s: %v %v", path, info, err)
			}
		}
	}
	if err := s.SaveTokens("z@b", token); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || !reflect.DeepEqual(list, []string{"a@b", "z@b"}) {
		t.Fatal(list, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tokens.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadTokens("a@b"); err == nil || errors.Is(err, ErrNoAccount) {
		t.Fatal(err)
	}
	if err := s.Remove("A@B"); err != nil {
		t.Fatal(err)
	}
	list, err = s.List()
	if err != nil || !reflect.DeepEqual(list, []string{"z@b"}) {
		t.Fatal(list, err)
	}
}

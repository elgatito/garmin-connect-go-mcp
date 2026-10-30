package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

func TestAuthAndRemove(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mobile/api/login":
			fmt.Fprint(w, `{"responseStatus":{"type":"SUCCESSFUL"},"serviceTicketId":"ST-1"}`)
		case "/token":
			fmt.Fprint(w, `{"access_token":"token","refresh_token":"refresh"}`)
		case "/userprofile-service/socialProfile":
			fmt.Fprint(w, `{"fullName":"Test User"}`)
		case "/userprofile-service/userprofile/user-settings":
			fmt.Fprint(w, `{}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	var out, errOut bytes.Buffer
	base := t.TempDir()
	e := env{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut, Getenv: func(string) string { return base }, ReadPassword: func() (string, error) { return "pw", nil }, Endpoints: func(string) garmin.Endpoints {
		return garmin.Endpoints{SSO: srv.URL, Connect: srv.URL, ConnectAPI: srv.URL, DIToken: srv.URL + "/token"}
	}, Login: &garmin.LoginOptions{Strategies: []string{"mobile+plain"}}}
	if code := run(context.Background(), []string{"auth", "--email", "Me@X.com"}, e); code != 0 || !strings.Contains(out.String(), "Logged in as Test User (me@x.com)") {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
	store := accounts.Store{Base: base}
	if _, err := store.LoadTokens("me@x.com"); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"remove", "ME@X.com", "--yes"}, e); code != 0 {
		t.Fatal(code, errOut.String())
	}
	if _, err := store.LoadTokens("me@x.com"); err == nil {
		t.Fatal("tokens still exist")
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatal(err)
	}
}

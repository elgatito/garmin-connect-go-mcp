package garmin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memStore struct {
	mu    sync.Mutex
	tok   Tokens
	saved []Tokens
}

func (s *memStore) Load() (Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tok, nil
}

func (s *memStore) Save(t Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok = t
	s.saved = append(s.saved, t)
	return nil
}

func (s *memStore) set(t Tokens) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok = t
}

func (s *memStore) savedTokens() []Tokens {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Tokens(nil), s.saved...)
}

func testEndpoints(base string) Endpoints {
	return Endpoints{
		SSO:        base,
		Connect:    "https://connect.garmin.com",
		ConnectAPI: base,
		DIToken:    base + "/di-oauth2-service/oauth/token",
	}
}

func newTestClient(t *testing.T, base string, tok Tokens) (*Client, *memStore) {
	t.Helper()
	st := &memStore{tok: tok}
	c, err := NewClient(testEndpoints(base), st, http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, st
}

func TestExpiresSoon(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		token string
		want  bool
	}{
		{"expiring", fakeJWT(now.Add(10*time.Minute), "cid"), true},
		{"fresh", fakeJWT(now.Add(20*time.Minute), "cid"), false},
		{"garbage", "garbage", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Tokens{DIToken: tc.token}).ExpiresSoon(now); got != tc.want {
				t.Errorf("ExpiresSoon(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}

func TestGetJSONSendsBearerAndNativeHeaders(t *testing.T) {
	tok := fakeJWT(time.Now().Add(20*time.Minute), "cid")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+tok {
			t.Errorf("Authorization = %q, want Bearer token", got)
		}
		if got := r.Header.Get("X-Garmin-Client-Platform"); got != "Android" {
			t.Errorf("X-Garmin-Client-Platform = %q, want Android", got)
		}
		fmt.Fprint(w, `{"a":1}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: tok, DIRefreshToken: "ref", DIClientID: "cid"})
	got, err := c.GetJSON(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	m, ok := got.(map[string]any)
	if !ok || m["a"] != float64(1) {
		t.Fatalf("got %#v, want map with a=1", got)
	}
}

func TestNoContentIsEmptyObject(t *testing.T) {
	tok := fakeJWT(time.Now().Add(20*time.Minute), "cid")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: tok, DIRefreshToken: "ref", DIClientID: "cid"})
	body, err := c.Do(context.Background(), Req{Method: http.MethodGet, Path: "/empty"})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if string(body) != "{}" {
		t.Fatalf("body = %q, want {}", body)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	tok := fakeJWT(time.Now().Add(20*time.Minute), "cid")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"message":"bad date"}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: tok, DIRefreshToken: "ref", DIClientID: "cid"})
	_, err := c.GetJSON(context.Background(), "/test", nil)
	if err == nil || err.Error() != "API Error 400 - bad date" {
		t.Fatalf("err = %v, want API Error 400 - bad date", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("errors.As(*APIError) = %#v, want status 400", apiErr)
	}

	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"content", `{"content":"nope"}`, "API Error 400 - nope"},
		{"detailed", `{"detailedImportResult":{"failures":[{"messages":["bad fit"]}]}}`, "API Error 400 - bad fit"},
		{"whole JSON", `{"other":"x"}`, `API Error 400 - {"other":"x"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, body)
			}))
			defer srv.Close()
			c, _ := newTestClient(t, srv.URL, Tokens{DIToken: tok, DIRefreshToken: "ref", DIClientID: "cid"})
			if _, err := c.GetJSON(context.Background(), "/test", nil); err == nil || err.Error() != tc.want {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestNonJSONErrorBody(t *testing.T) {
	tok := fakeJWT(time.Now().Add(20*time.Minute), "cid")
	mux := http.NewServeMux()
	mux.HandleFunc("/html", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "<html>busy</html>")
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: tok, DIRefreshToken: "ref", DIClientID: "cid"})
	if _, err := c.GetJSON(context.Background(), "/html", nil); err == nil || err.Error() != "API Error 503 - <html>busy</html>" {
		t.Errorf("html err = %v, want API Error 503 - <html>busy</html>", err)
	}
	if _, err := c.GetJSON(context.Background(), "/empty", nil); err == nil || err.Error() != "API Error 502" {
		t.Errorf("empty err = %v, want API Error 502", err)
	}
}

func TestRefreshOn401(t *testing.T) {
	const oldTok, newTok = "tok-old", "tok-new"
	var diauthCalls, apiCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/di-oauth2-service/oauth/token":
			diauthCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			for k, want := range map[string]string{
				"grant_type":    "refresh_token",
				"client_id":     "cid",
				"refresh_token": "ref-old",
			} {
				if got := r.Form.Get(k); got != want {
					t.Errorf("form %s = %q, want %q", k, got, want)
				}
			}
			wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("cid:"))
			if got := r.Header.Get("Authorization"); got != wantAuth {
				t.Errorf("diauth Authorization = %q, want %q", got, wantAuth)
			}
			fmt.Fprint(w, `{"access_token":"tok-new","refresh_token":"ref-new"}`)
		case "/test":
			apiCalls.Add(1)
			switch got := r.Header.Get("Authorization"); got {
			case "Bearer " + oldTok:
				w.WriteHeader(http.StatusUnauthorized)
				fmt.Fprint(w, `{"message":"unauthorized"}`)
			case "Bearer " + newTok:
				fmt.Fprint(w, `{"ok":true}`)
			default:
				t.Errorf("Authorization = %q", got)
			}
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	c, st := newTestClient(t, srv.URL, Tokens{DIToken: oldTok, DIRefreshToken: "ref-old", DIClientID: "cid"})
	if _, err := c.GetJSON(context.Background(), "/test", nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got := diauthCalls.Load(); got != 1 {
		t.Errorf("diauth calls = %d, want 1", got)
	}
	if got := apiCalls.Load(); got != 2 {
		t.Errorf("api calls = %d, want 2", got)
	}
	want := Tokens{DIToken: newTok, DIRefreshToken: "ref-new", DIClientID: "cid"}
	saved := st.savedTokens()
	if len(saved) != 1 || saved[0] != want {
		t.Errorf("saved = %#v, want %#v", saved, want)
	}
}

func TestRefreshWhenExpiring(t *testing.T) {
	oldTok := fakeJWT(time.Now().Add(time.Minute), "cid")
	const newTok = "tok-new"
	var diauthCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/di-oauth2-service/oauth/token" {
			diauthCalls.Add(1)
			fmt.Fprint(w, `{"access_token":"tok-new"}`)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+newTok {
			t.Errorf("Authorization = %q, want Bearer %s", got, newTok)
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c, st := newTestClient(t, srv.URL, Tokens{DIToken: oldTok, DIRefreshToken: "ref-old", DIClientID: "cid"})
	if _, err := c.GetJSON(context.Background(), "/test", nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got := diauthCalls.Load(); got != 1 {
		t.Errorf("diauth calls = %d, want 1", got)
	}
	// No refresh_token in the response: the old one must be kept.
	want := Tokens{DIToken: newTok, DIRefreshToken: "ref-old", DIClientID: "cid"}
	saved := st.savedTokens()
	if len(saved) != 1 || saved[0] != want {
		t.Errorf("saved = %#v, want %#v", saved, want)
	}
}

func TestRefreshUsesNewerTokensFromStore(t *testing.T) {
	oldTok := fakeJWT(time.Now().Add(time.Minute), "cid")
	storedTok := fakeJWT(time.Now().Add(30*time.Minute), "cid2")
	var diauthCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/di-oauth2-service/oauth/token" {
			diauthCalls.Add(1)
			fmt.Fprint(w, `{"access_token":"unexpected"}`)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+storedTok {
			t.Errorf("Authorization = %q, want stored token", got)
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c, st := newTestClient(t, srv.URL, Tokens{DIToken: oldTok, DIRefreshToken: "ref-old", DIClientID: "cid"})
	st.set(Tokens{DIToken: storedTok, DIRefreshToken: "ref-store", DIClientID: "cid2"})
	if _, err := c.GetJSON(context.Background(), "/test", nil); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got := diauthCalls.Load(); got != 0 {
		t.Errorf("diauth calls = %d, want 0", got)
	}
}

func TestConcurrentCallsRefreshOnce(t *testing.T) {
	oldTok := fakeJWT(time.Now().Add(time.Minute), "cid")
	newTok := fakeJWT(time.Now().Add(time.Hour), "cid")
	var diauthCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/di-oauth2-service/oauth/token" {
			diauthCalls.Add(1)
			fmt.Fprintf(w, `{"access_token":%q}`, newTok)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+newTok {
			t.Errorf("Authorization = %q, want refreshed token", got)
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: oldTok, DIRefreshToken: "ref", DIClientID: "cid"})
	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.GetJSON(context.Background(), "/test", nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("GetJSON: %v", err)
		}
	}
	if got := diauthCalls.Load(); got != 1 {
		t.Fatalf("diauth calls = %d, want exactly 1", got)
	}
}

func TestRefreshRejectedIsSessionExpired(t *testing.T) {
	oldTok := fakeJWT(time.Now().Add(time.Minute), "cid")
	var apiCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/di-oauth2-service/oauth/token" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
			return
		}
		apiCalls.Add(1)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: oldTok, DIRefreshToken: "ref", DIClientID: "cid"})
	_, err := c.GetJSON(context.Background(), "/test", nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("err = %v, want ErrSessionExpired", err)
	}
	if got := apiCalls.Load(); got != 0 {
		t.Errorf("api calls = %d, want 0", got)
	}
}

func TestProfileCached(t *testing.T) {
	tok := fakeJWT(time.Now().Add(20*time.Minute), "cid")
	var socialCalls, settingsCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/userprofile-service/socialProfile", func(w http.ResponseWriter, r *http.Request) {
		socialCalls.Add(1)
		fmt.Fprint(w, `{"displayName":"testuser","fullName":"Test User"}`)
	})
	mux.HandleFunc("/userprofile-service/userprofile/user-settings", func(w http.ResponseWriter, r *http.Request) {
		settingsCalls.Add(1)
		fmt.Fprint(w, `{"userData":{"measurementSystem":"metric"}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL, Tokens{DIToken: tok, DIRefreshToken: "ref", DIClientID: "cid"})
	want := Profile{DisplayName: "testuser", FullName: "Test User", UnitSystem: "metric"}
	for i := range 2 {
		got, err := c.Profile(context.Background())
		if err != nil {
			t.Fatalf("Profile call %d: %v", i, err)
		}
		if got != want {
			t.Errorf("Profile call %d = %#v, want %#v", i, got, want)
		}
	}
	if got := socialCalls.Load(); got != 1 {
		t.Errorf("socialProfile calls = %d, want 1", got)
	}
	if got := settingsCalls.Load(); got != 1 {
		t.Errorf("user-settings calls = %d, want 1", got)
	}
}

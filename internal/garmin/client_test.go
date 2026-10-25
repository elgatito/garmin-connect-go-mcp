package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const diPath = "/di-oauth2-service/oauth/token"

type memStore struct {
	mu    sync.Mutex
	tok   Tokens
	saved []Tokens
}

func (m *memStore) Load() (Tokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tok, nil
}

func (m *memStore) Save(t Tokens) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tok = t
	m.saved = append(m.saved, t)
	return nil
}

func (m *memStore) set(t Tokens) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tok = t
}

func (m *memStore) savedTokens() []Tokens {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Tokens(nil), m.saved...)
}

// fixture starts a server playing both connectapi (api handler) and diauth (di handler).
func fixture(t *testing.T, tok Tokens, api, di http.HandlerFunc) (*Client, *memStore) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", api)
	if di == nil {
		di = func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unexpected diauth call")
			w.WriteHeader(500)
		}
	}
	mux.HandleFunc(diPath, di)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ep := Endpoints{SSO: srv.URL, Connect: srv.URL, ConnectAPI: srv.URL, DIToken: srv.URL + diPath}
	ts := &memStore{tok: tok}
	c, err := NewClient(ep, ts, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c, ts
}

func freshTokens(id string) Tokens {
	return Tokens{DIToken: fakeJWT(time.Now().Add(time.Hour), id), DIRefreshToken: "refresh-" + id, DIClientID: id}
}

// diRefresher answers the refresh grant with newTok and counts calls.
func diRefresher(t *testing.T, newTok string, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.PostForm.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
		}
		if u, p, ok := r.BasicAuth(); !ok || u == "" || p != "" {
			t.Errorf("basic auth = %q %q %v", u, p, ok)
		}
		time.Sleep(20 * time.Millisecond) // widen the race window for concurrent callers
		json.NewEncoder(w).Encode(map[string]string{"access_token": newTok, "refresh_token": "r2"})
	}
}

// bearerOnly accepts only requests carrying tok.
func bearerOnly(t *testing.T, tok string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+tok {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}
}

func TestExpiresSoon(t *testing.T) {
	now := time.Now()
	if !(Tokens{DIToken: fakeJWT(now.Add(10*time.Minute), "c")}).ExpiresSoon(now) {
		t.Error("exp in 10m should be expiring")
	}
	if (Tokens{DIToken: fakeJWT(now.Add(20*time.Minute), "c")}).ExpiresSoon(now) {
		t.Error("exp in 20m should not be expiring")
	}
	if (Tokens{DIToken: "garbage"}).ExpiresSoon(now) {
		t.Error("garbage token should not be expiring")
	}
	exp, ok := Tokens{DIToken: fakeJWT(time.Unix(1700000000, 0), "c")}.Expiry()
	if !ok || exp.Unix() != 1700000000 {
		t.Errorf("Expiry = %v %v", exp, ok)
	}
}

func TestGetJSONSendsBearerAndNativeHeaders(t *testing.T) {
	tok := freshTokens("c1")
	c, _ := fixture(t, tok, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/foo/bar" || r.URL.Query().Get("d") != "1" {
			t.Errorf("url = %s", r.URL)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+tok.DIToken {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-Garmin-Client-Platform"); got != "Android" {
			t.Errorf("X-Garmin-Client-Platform = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		w.Write([]byte(`{"a":1}`))
	}, nil)
	v, err := c.GetJSON(context.Background(), "/foo/bar", map[string][]string{"d": {"1"}})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := v.(map[string]any); !ok || m["a"] != float64(1) {
		t.Errorf("got %#v", v)
	}
}

func TestNoContentIsEmptyObject(t *testing.T) {
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}, nil)
	b, err := c.Do(context.Background(), Req{Method: "DELETE", Path: "x"})
	if err != nil || string(b) != "{}" {
		t.Errorf("got %q %v", b, err)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"message":"bad date"}`))
	}, nil)
	_, err := c.GetJSON(context.Background(), "x", nil)
	if err == nil || err.Error() != "API Error 400 - bad date" {
		t.Fatalf("err = %v", err)
	}
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Errorf("errors.As = %#v", ae)
	}
}

func TestNonJSONErrorBody(t *testing.T) {
	var status atomic.Int32
	status.Store(503)
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		if status.Load() == 503 {
			w.Write([]byte("<html>busy</html>"))
		}
	}, nil)
	_, err := c.GetJSON(context.Background(), "x", nil)
	if err == nil || err.Error() != "API Error 503 - <html>busy</html>" {
		t.Errorf("err = %v", err)
	}
	status.Store(502)
	_, err = c.GetJSON(context.Background(), "x", nil)
	if err == nil || err.Error() != "API Error 502" {
		t.Errorf("err = %v", err)
	}
}

func TestRefreshOn401(t *testing.T) {
	newTok := fakeJWT(time.Now().Add(time.Hour), "c2")
	var calls atomic.Int32
	c, ts := fixture(t, freshTokens("c1"), bearerOnly(t, newTok), diRefresher(t, newTok, &calls))
	if _, err := c.GetJSON(context.Background(), "x", nil); err != nil {
		t.Fatal(err)
	}
	want := Tokens{DIToken: newTok, DIRefreshToken: "r2", DIClientID: "c2"}
	if s := ts.savedTokens(); len(s) != 1 || s[0] != want {
		t.Errorf("saved = %#v", s)
	}
	if calls.Load() != 1 {
		t.Errorf("diauth calls = %d", calls.Load())
	}
}

func TestRefreshWhenExpiring(t *testing.T) {
	old := Tokens{DIToken: fakeJWT(time.Now().Add(time.Minute), "c1"), DIRefreshToken: "r1", DIClientID: "c1"}
	newTok := fakeJWT(time.Now().Add(time.Hour), "c1")
	var calls atomic.Int32
	// The API rejects the old token with 400 so only a pre-request refresh passes.
	c, _ := fixture(t, old, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+newTok {
			w.WriteHeader(400)
			return
		}
		w.Write([]byte(`{}`))
	}, diRefresher(t, newTok, &calls))
	if _, err := c.GetJSON(context.Background(), "x", nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Errorf("diauth calls = %d", calls.Load())
	}
}

func TestRefreshUsesNewerTokensFromStore(t *testing.T) {
	old := Tokens{DIToken: fakeJWT(time.Now().Add(time.Minute), "c1"), DIRefreshToken: "r1", DIClientID: "c1"}
	stored := freshTokens("c9")
	c, ts := fixture(t, old, bearerOnly(t, stored.DIToken), nil)
	ts.set(stored) // another process refreshed after we loaded
	if _, err := c.GetJSON(context.Background(), "x", nil); err != nil {
		t.Fatal(err)
	}
	if s := ts.savedTokens(); len(s) != 0 {
		t.Errorf("unexpected save: %#v", s)
	}
}

func TestConcurrentCallsRefreshOnce(t *testing.T) {
	old := Tokens{DIToken: fakeJWT(time.Now().Add(time.Minute), "c1"), DIRefreshToken: "r1", DIClientID: "c1"}
	newTok := fakeJWT(time.Now().Add(time.Hour), "c1")
	var calls atomic.Int32
	c, _ := fixture(t, old, bearerOnly(t, newTok), diRefresher(t, newTok, &calls))
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := c.GetJSON(context.Background(), "x", nil); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Errorf("diauth calls = %d", calls.Load())
	}
}

func TestRefreshRejectedIsSessionExpired(t *testing.T) {
	old := Tokens{DIToken: fakeJWT(time.Now().Add(time.Minute), "c1"), DIRefreshToken: "r1", DIClientID: "c1"}
	c, _ := fixture(t, old, func(w http.ResponseWriter, r *http.Request) {
		t.Error("API must not be called after a rejected refresh")
	}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	})
	_, err := c.GetJSON(context.Background(), "x", nil)
	if !errors.Is(err, ErrSessionExpired) {
		t.Errorf("err = %v", err)
	}
}

func TestProfileCached(t *testing.T) {
	var social atomic.Int32
	c, _ := fixture(t, freshTokens("c1"), func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/userprofile-service/socialProfile":
			social.Add(1)
			w.Write([]byte(`{"displayName":"dn","fullName":"Full Name"}`))
		case "/userprofile-service/userprofile/user-settings":
			w.Write([]byte(`{"userData":{"measurementSystem":"metric"}}`))
		default:
			w.WriteHeader(404)
		}
	}, nil)
	for range 2 {
		p, err := c.Profile(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if p != (Profile{DisplayName: "dn", FullName: "Full Name", UnitSystem: "metric"}) {
			t.Errorf("profile = %#v", p)
		}
	}
	if social.Load() != 1 {
		t.Errorf("socialProfile calls = %d", social.Load())
	}
}

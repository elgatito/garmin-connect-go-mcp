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
	mu     sync.Mutex
	tokens Tokens
}

func (s *memStore) Load() (Tokens, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.tokens, nil }
func (s *memStore) Save(t Tokens) error   { s.mu.Lock(); defer s.mu.Unlock(); s.tokens = t; return nil }
func jwt(exp time.Time) string {
	return "x." + base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix()))) + ".x"
}
func TestConcurrentRefresh(t *testing.T) {
	var refreshes atomic.Int32
	fresh := jwt(time.Now().Add(time.Hour))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes.Add(1)
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"rotated"}`, fresh)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fresh || r.Header.Get("X-Garmin-Client-Platform") != "Android" {
			t.Errorf("bad headers: %v", r.Header)
		}
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()
	store := &memStore{tokens: Tokens{DIToken: jwt(time.Now().Add(time.Minute)), DIRefreshToken: "old", DIClientID: "id"}}
	c, err := NewClient(Endpoints{ConnectAPI: srv.URL, DIToken: srv.URL + "/token"}, store, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.GetJSON(context.Background(), "test", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes=%d", refreshes.Load())
	}
	tokens, _ := store.Load()
	if tokens.DIRefreshToken != "rotated" {
		t.Fatal(tokens)
	}
}
func TestRefreshUsesNewerTokensFromStore(t *testing.T) {
	old := jwt(time.Now().Add(time.Minute))
	fresh := jwt(time.Now().Add(time.Hour))
	store := &memStore{tokens: Tokens{DIToken: old}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fresh {
			t.Error("wrong token")
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Endpoints{ConnectAPI: srv.URL}, store, srv.Client())
	store.Save(Tokens{DIToken: fresh})
	if _, err := c.GetJSON(context.Background(), "test", nil); err != nil {
		t.Fatal(err)
	}
}
func TestAPIError(t *testing.T) {
	for _, tc := range []struct{ body, want string }{{"<html>busy</html>", "API Error 503 - <html>busy</html>"}, {"", "API Error 503"}, {`{"message":"bad"}`, "API Error 503 - bad"}} {
		if got := apiError(503, []byte(tc.body)).Error(); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestClientRefreshOn401AndNoContent(t *testing.T) {
	fresh := jwt(time.Now().Add(time.Hour))
	old := jwt(time.Now().Add(2 * time.Hour))
	var calls, refreshes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes.Add(1)
			fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"rotated"}`, fresh)
			return
		}
		calls.Add(1)
		if r.Header.Get("Authorization") == "Bearer "+old {
			w.WriteHeader(401)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fresh {
			t.Error("retry did not use refreshed token")
		}
		w.WriteHeader(204)
	}))
	defer srv.Close()
	store := &memStore{tokens: Tokens{DIToken: old, DIRefreshToken: "refresh", DIClientID: "client"}}
	c, e := NewClient(Endpoints{ConnectAPI: srv.URL, DIToken: srv.URL + "/token"}, store, srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	data, e := c.Do(context.Background(), Req{Path: "/test"})
	if e != nil || string(data) != "{}" || calls.Load() != 2 || refreshes.Load() != 1 {
		t.Fatal(string(data), e, calls.Load(), refreshes.Load())
	}
	saved, _ := store.Load()
	if saved.DIToken != fresh || saved.DIRefreshToken != "rotated" {
		t.Fatal(saved)
	}
}
func TestRefreshRejectedIsSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(400) }))
	defer srv.Close()
	store := &memStore{tokens: Tokens{DIToken: jwt(time.Now().Add(time.Minute)), DIRefreshToken: "bad", DIClientID: "client"}}
	c, e := NewClient(Endpoints{ConnectAPI: srv.URL, DIToken: srv.URL + "/token"}, store, srv.Client())
	if e != nil {
		t.Fatal(e)
	}
	_, e = c.GetJSON(context.Background(), "/test", nil)
	if !errors.Is(e, ErrSessionExpired) {
		t.Fatal(e)
	}
}
func TestExpiresSoon(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		token string
		want  bool
	}{{jwt(now.Add(10 * time.Minute)), true}, {jwt(now.Add(20 * time.Minute)), false}, {"garbage", false}} {
		if got := (Tokens{DIToken: tc.token}).ExpiresSoon(now); got != tc.want {
			t.Fatalf("ExpiresSoon=%v want %v", got, tc.want)
		}
	}
}

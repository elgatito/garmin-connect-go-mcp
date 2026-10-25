package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const jwtClientID = "CLIENT_FROM_JWT"

// fakeSSO serves the given routes plus a default diauth handler and counts hits per path.
type fakeSSO struct {
	mu   sync.Mutex
	hits map[string]int
	ep   Endpoints
	opt  *LoginOptions
}

func (f *fakeSSO) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func newFakeSSO(t *testing.T, useTLS bool, strategies []string, routes map[string]http.HandlerFunc) *fakeSSO {
	t.Helper()
	f := &fakeSSO{hits: map[string]int{}}
	if _, ok := routes[diPath]; !ok {
		routes[diPath] = diOK(t)
	}
	mux := http.NewServeMux()
	for p, h := range routes {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			f.hits[p]++
			f.mu.Unlock()
			h(w, r)
		})
	}
	f.opt = &LoginOptions{Strategies: strategies}
	var srv *httptest.Server
	if useTLS {
		srv = httptest.NewTLSServer(mux)
		f.opt.RootCAs = poolFor(srv)
	} else {
		srv = httptest.NewServer(mux)
	}
	t.Cleanup(srv.Close)
	f.ep = Endpoints{SSO: srv.URL, Connect: srv.URL, ConnectAPI: srv.URL, DIToken: srv.URL + diPath}
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func ssoStatus(typ string) map[string]any {
	m := map[string]any{"responseStatus": map[string]any{"type": typ}}
	if typ == "SUCCESSFUL" {
		m["serviceTicketId"] = "ST-1"
	}
	return m
}

func respond(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { writeJSON(w, ssoStatus(typ)) }
}

func status(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

// diOK checks the service-ticket exchange form and returns a token pair.
func diOK(t *testing.T) http.HandlerFunc {
	return diOKFor(t, func(*http.Request) string { return iosServiceURL })
}

// diOKFor is diOK with the expected service URL computed from the request.
func diOKFor(t *testing.T, serviceURL func(*http.Request) string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("service_ticket") != "ST-1" || r.PostForm.Get("grant_type") != diGrantType ||
			r.PostForm.Get("service_url") != serviceURL(r) {
			t.Errorf("diauth form = %v", r.PostForm)
		}
		writeJSON(w, map[string]any{"access_token": fakeJWT(time.Now().Add(time.Hour), jwtClientID), "refresh_token": "R-1"})
	}
}

func checkTokens(t *testing.T, tok Tokens, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if tok.DIToken == "" || tok.DIRefreshToken != "R-1" || tok.DIClientID != jwtClientID {
		t.Fatalf("tokens = %+v", tok)
	}
}

func noPrompt(ctx context.Context) (string, error) {
	return "", errors.New("prompt must not be called")
}

func TestMobileLoginSuccess(t *testing.T) {
	f := newFakeSSO(t, false, []string{"mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != http.MethodPost || r.URL.Query().Get("clientId") != "GCM_IOS_DARK" ||
				body["username"] != "a@b.c" || body["password"] != "pw" {
				t.Errorf("login %s %s body=%v", r.Method, r.URL, body)
			}
			respond("SUCCESSFUL")(w, r)
		},
	})
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
}

func TestMobileLoginInvalidPassword(t *testing.T) {
	f := newFakeSSO(t, true, []string{"mobile+utls", "mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": respond("INVALID_USERNAME_PASSWORD"),
	})
	_, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
	if n := f.count("/mobile/api/login"); n != 1 {
		t.Fatalf("login hits = %d, want 1", n)
	}
}

func TestMobileLoginMFA(t *testing.T) {
	f := newFakeSSO(t, false, []string{"mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"responseStatus": map[string]any{"type": "MFA_REQUIRED"},
				"customerMfaInfo": map[string]any{"mfaLastMethodUsed": "email"}})
		},
		"/mobile/api/mfa/verifyCode": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["mfaVerificationCode"] != "123456" || body["mfaMethod"] != "email" {
				t.Errorf("verifyCode body = %v", body)
			}
			respond("SUCCESSFUL")(w, r)
		},
	})
	calls := 0
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", func(context.Context) (string, error) {
		calls++
		return "123456", nil
	}, f.opt)
	checkTokens(t, tok, err)
	if calls != 1 {
		t.Fatalf("prompt calls = %d", calls)
	}
}

func TestMFAFallsBackToPortalEndpoint(t *testing.T) {
	f := newFakeSSO(t, false, []string{"mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login":          respond("MFA_REQUIRED"),
		"/mobile/api/mfa/verifyCode": status(429),
		"/portal/api/mfa/verifyCode": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("clientId") != portalSSOClientID {
				t.Errorf("portal verify query = %v", r.URL.Query())
			}
			respond("SUCCESSFUL")(w, r)
		},
	})
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", func(context.Context) (string, error) { return "1", nil }, f.opt)
	checkTokens(t, tok, err)
	if f.count("/mobile/api/mfa/verifyCode") != 1 || f.count("/portal/api/mfa/verifyCode") != 1 {
		t.Fatalf("verify hits mobile=%d portal=%d", f.count("/mobile/api/mfa/verifyCode"), f.count("/portal/api/mfa/verifyCode"))
	}
}

func TestDIExchangeTriesClientIDs(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	ok := diOK(t)
	f := newFakeSSO(t, false, []string{"mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": respond("SUCCESSFUL"),
		diPath: func(w http.ResponseWriter, r *http.Request) {
			r.ParseForm()
			mu.Lock()
			seen = append(seen, r.PostForm.Get("client_id"))
			first := len(seen) == 1
			mu.Unlock()
			if first {
				w.WriteHeader(400)
				return
			}
			ok(w, r)
		},
	})
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
	mu.Lock()
	defer mu.Unlock()
	if fmt.Sprint(seen) != fmt.Sprint(diClientIDs[:2]) {
		t.Fatalf("client ids = %v", seen)
	}
}

func TestChainMovesOnAfter429(t *testing.T) {
	var mu sync.Mutex
	n := 0
	f := newFakeSSO(t, true, []string{"mobile+utls", "mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			n++
			first := n == 1
			mu.Unlock()
			if first {
				w.WriteHeader(429)
				return
			}
			respond("SUCCESSFUL")(w, r)
		},
	})
	var log strings.Builder
	f.opt.Log = &log
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
	if strings.Contains(log.String(), "retrying with Go TLS") || strings.Contains(log.String(), "pw") {
		t.Errorf("log = %q", log.String())
	}
	if c := f.count("/mobile/api/login"); c != 2 {
		t.Fatalf("login hits = %d, want 2", c)
	}
}

func TestAllRateLimited(t *testing.T) {
	f := newFakeSSO(t, true, []string{"mobile+utls", "mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": status(429),
	})
	_, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("err = %v", err)
	}
	if c := f.count("/mobile/api/login"); c != 4 { // three utls hellos + plain
		t.Fatalf("login hits = %d, want 4", c)
	}
}

func TestMFANoPrompt(t *testing.T) {
	f := newFakeSSO(t, false, []string{"mobile+plain", "mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": respond("MFA_REQUIRED"),
	})
	_, err := Login(context.Background(), f.ep, "a@b.c", "pw", nil, f.opt)
	if err == nil || err.Error() != "MFA required but no MFA prompt available" {
		t.Fatalf("err = %v", err)
	}
	if c := f.count("/mobile/api/login"); c != 1 {
		t.Fatalf("login hits = %d, want 1", c)
	}
}

func TestMobileSuccessWithUnrelatedErrorField(t *testing.T) {
	f := newFakeSSO(t, false, []string{"mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": func(w http.ResponseWriter, r *http.Request) {
			m := ssoStatus("SUCCESSFUL")
			m["error"] = "some string"
			writeJSON(w, m)
		},
	})
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
}

func Test429InJSONBodyMovesOn(t *testing.T) {
	var mu sync.Mutex
	n := 0
	f := newFakeSSO(t, false, []string{"mobile+plain", "mobile+plain"}, map[string]http.HandlerFunc{
		"/mobile/api/login": func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			n++
			first := n == 1
			mu.Unlock()
			if first {
				writeJSON(w, map[string]any{"error": map[string]any{"status-code": "429"}})
				return
			}
			respond("SUCCESSFUL")(w, r)
		},
	})
	var log strings.Builder
	f.opt.Log = &log
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
	if !strings.Contains(log.String(), "429 in JSON body") {
		t.Errorf("log = %q", log.String())
	}
}

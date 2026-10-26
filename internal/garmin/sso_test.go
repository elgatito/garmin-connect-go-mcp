package garmin

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const diTestPath = "/di-oauth2-service/oauth/token"

func ssoJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func loginSuccess(ticket string) map[string]any {
	return map[string]any{
		"responseStatus":  map[string]any{"type": "SUCCESSFUL"},
		"serviceTicketId": ticket,
	}
}

func mfaRequired(lastMethod string) map[string]any {
	return map[string]any{
		"responseStatus":  map[string]any{"type": "MFA_REQUIRED"},
		"customerMfaInfo": map[string]any{"mfaLastMethodUsed": lastMethod},
	}
}

func diTokenBody(token, refresh string) map[string]any {
	return map[string]any{"access_token": token, "refresh_token": refresh}
}

func testRootPool(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

func TestMobileLoginSuccess(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-FROM-JWT")
	var loginCalls, diCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("login method = %s, want POST", r.Method)
		}
		if got := r.URL.Query().Get("clientId"); got != "GCM_IOS_DARK" {
			t.Errorf("clientId = %q, want GCM_IOS_DARK", got)
		}
		if got := r.URL.Query().Get("locale"); got != "en-US" {
			t.Errorf("locale = %q, want en-US", got)
		}
		if got := r.URL.Query().Get("service"); got != iosServiceURL {
			t.Errorf("service = %q, want %q", got, iosServiceURL)
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode login body: %v", err)
		}
		if body.Username != "user@example.com" || body.Password != "pw" {
			t.Errorf("login body = %+v", body)
		}
		ssoJSON(t, w, loginSuccess("ST-1"))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		diCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse DI form: %v", err)
		}
		if got := r.Form.Get("service_ticket"); got != "ST-1" {
			t.Errorf("service_ticket = %q, want ST-1", got)
		}
		if got := r.Form.Get("grant_type"); got != diGrantType {
			t.Errorf("grant_type = %q, want %q", got, diGrantType)
		}
		if got := r.Form.Get("client_id"); got != diClientIDs[0] {
			t.Errorf("client_id = %q, want %q", got, diClientIDs[0])
		}
		ssoJSON(t, w, diTokenBody(token, "RT-1"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil,
		&LoginOptions{Strategies: []string{"mobile+plain"}})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken == "" || tok.DIRefreshToken != "RT-1" {
		t.Fatalf("tokens = %+v", tok)
	}
	if tok.DIClientID != "CLIENT-FROM-JWT" {
		t.Errorf("DIClientID = %q, want CLIENT-FROM-JWT", tok.DIClientID)
	}
	if loginCalls.Load() != 1 || diCalls.Load() != 1 {
		t.Errorf("calls = login %d, di %d; want 1, 1", loginCalls.Load(), diCalls.Load())
	}
}

func TestMobileLoginInvalidPassword(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		ssoJSON(t, w, map[string]any{
			"responseStatus": map[string]any{"type": "INVALID_USERNAME_PASSWORD"},
		})
	}))
	t.Cleanup(srv.Close)

	_, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "bad", nil, &LoginOptions{
		Strategies: []string{"mobile+plain", "mobile+utls"},
		RootCAs:    testRootPool(srv),
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if err != ErrInvalidCredentials {
		t.Fatalf("err = %v, want the unwrapped sentinel", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("login requests = %d, want 1 (chain must stop)", got)
	}
}

func TestMobileLoginMFA(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-MFA")
	var promptCalls, verifyCalls, diCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		ssoJSON(t, w, mfaRequired("email"))
	})
	mux.HandleFunc("/mobile/api/mfa/verifyCode", func(w http.ResponseWriter, r *http.Request) {
		verifyCalls.Add(1)
		var body struct {
			MFAMethod           string `json:"mfaMethod"`
			MFAVerificationCode string `json:"mfaVerificationCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode MFA body: %v", err)
		}
		if body.MFAMethod != "email" {
			t.Errorf("mfaMethod = %q, want email", body.MFAMethod)
		}
		if body.MFAVerificationCode != "123456" {
			t.Errorf("mfaVerificationCode = %q, want 123456", body.MFAVerificationCode)
		}
		ssoJSON(t, w, loginSuccess("ST-MFA"))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		diCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse DI form: %v", err)
		}
		if got := r.Form.Get("service_ticket"); got != "ST-MFA" {
			t.Errorf("service_ticket = %q, want ST-MFA", got)
		}
		ssoJSON(t, w, diTokenBody(token, "RT-MFA"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mfa := func(context.Context) (string, error) {
		promptCalls.Add(1)
		return "123456", nil
	}
	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", mfa,
		&LoginOptions{Strategies: []string{"mobile+plain"}})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken == "" || tok.DIRefreshToken == "" || tok.DIClientID == "" {
		t.Fatalf("tokens = %+v, want all fields set", tok)
	}
	if got := promptCalls.Load(); got != 1 {
		t.Errorf("prompt calls = %d, want 1", got)
	}
	if got := verifyCalls.Load(); got != 1 {
		t.Errorf("mobile verify calls = %d, want 1", got)
	}
	if got := diCalls.Load(); got != 1 {
		t.Errorf("DI calls = %d, want 1", got)
	}
}

func TestMFAFallsBackToPortalEndpoint(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-PORTAL-MFA")
	var mobileCalls, portalCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		ssoJSON(t, w, mfaRequired("email"))
	})
	mux.HandleFunc("/mobile/api/mfa/verifyCode", func(w http.ResponseWriter, r *http.Request) {
		mobileCalls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	mux.HandleFunc("/portal/api/mfa/verifyCode", func(w http.ResponseWriter, r *http.Request) {
		portalCalls.Add(1)
		if got := r.URL.Query().Get("clientId"); got != portalSSOClientID {
			t.Errorf("clientId = %q, want %q", got, portalSSOClientID)
		}
		ssoJSON(t, w, loginSuccess("ST-PORTAL-MFA"))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		ssoJSON(t, w, diTokenBody(token, "RT-PORTAL-MFA"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mfa := func(context.Context) (string, error) { return "123456", nil }
	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", mfa,
		&LoginOptions{Strategies: []string{"mobile+plain"}})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken == "" || tok.DIRefreshToken == "" || tok.DIClientID == "" {
		t.Fatalf("tokens = %+v, want all fields set", tok)
	}
	if mobileCalls.Load() != 1 || portalCalls.Load() != 1 {
		t.Errorf("verify calls = mobile %d, portal %d; want 1, 1", mobileCalls.Load(), portalCalls.Load())
	}
}

func TestDIExchangeTriesClientIDs(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-DI")

	var mu sync.Mutex
	var seen []string
	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		ssoJSON(t, w, loginSuccess("ST-DI"))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse DI form: %v", err)
		}
		mu.Lock()
		seen = append(seen, r.Form.Get("client_id"))
		n := len(seen)
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			ssoJSON(t, w, map[string]any{"error": "unknown client"})
			return
		}
		ssoJSON(t, w, diTokenBody(token, "RT-DI"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil,
		&LoginOptions{Strategies: []string{"mobile+plain"}})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("DI POSTs = %d, want 2", len(got))
	}
	if got[0] != diClientIDs[0] || got[1] != diClientIDs[1] {
		t.Errorf("client IDs = %v, want first two of %v", got, diClientIDs)
	}
	if tok.DIToken == "" || tok.DIRefreshToken == "" || tok.DIClientID == "" {
		t.Fatalf("tokens = %+v, want all fields set", tok)
	}
}

func TestChainMovesOnAfter429(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-CHAIN")
	var loginCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		if loginCalls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		ssoJSON(t, w, loginSuccess("ST-CHAIN"))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		ssoJSON(t, w, diTokenBody(token, "RT-CHAIN"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil, &LoginOptions{
		Strategies: []string{"mobile+utls", "mobile+plain"},
		RootCAs:    testRootPool(srv),
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken == "" || tok.DIRefreshToken == "" || tok.DIClientID == "" {
		t.Fatalf("tokens = %+v, want all fields set", tok)
	}
	if got := loginCalls.Load(); got != 2 {
		t.Errorf("login requests = %d, want 2", got)
	}
}

func TestAllRateLimited(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	_, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil,
		&LoginOptions{Strategies: []string{"mobile+plain"}})
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("err = %v, want ErrTooManyRequests", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("login requests = %d, want 1", got)
	}
}

func TestMFANoPrompt(t *testing.T) {
	var loginCalls, verifyCalls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/mobile/api/login", func(w http.ResponseWriter, r *http.Request) {
		loginCalls.Add(1)
		ssoJSON(t, w, mfaRequired("email"))
	})
	mux.HandleFunc("/mobile/api/mfa/verifyCode", func(w http.ResponseWriter, r *http.Request) {
		verifyCalls.Add(1)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil,
		&LoginOptions{Strategies: []string{"mobile+plain"}})
	if err == nil || err.Error() != "MFA required but no MFA prompt available" {
		t.Fatalf("err = %v, want exact no-prompt error", err)
	}
	if loginCalls.Load() != 1 || verifyCalls.Load() != 0 {
		t.Errorf("calls = login %d, verify %d; want 1, 0", loginCalls.Load(), verifyCalls.Load())
	}
}

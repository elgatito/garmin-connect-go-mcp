package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	widgetSigninHTML = `<html><head><title>Sign In</title></head><body>` +
		`<input type="hidden" name="_csrf" value="tok"></body></html>`
	widgetMFAHTML = `<html><head><title>MFA</title></head><body>` +
		`<input type="hidden" name="_csrf" value="tok2"></body></html>`
)

func widgetSuccessHTML(ticket string) string {
	return `<html><head><title>Success</title></head><body>` +
		`<a href="https://sso.garmin.com/sso/embed?ticket=` + ticket + `">ok</a></body></html>`
}

// sleepRecorder records requested delays instead of sleeping.
type sleepRecorder struct {
	mu        sync.Mutex
	durations []time.Duration
}

func (s *sleepRecorder) sleep(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durations = append(s.durations, d)
}

func (s *sleepRecorder) all() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.durations...)
}

func assertOneDelay(t *testing.T, rec *sleepRecorder, min, max time.Duration) {
	t.Helper()
	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("recorded sleeps = %v, want exactly one", got)
	}
	if got[0] < min || got[0] > max {
		t.Errorf("recorded sleep = %v, want within [%v, %v]", got[0], min, max)
	}
}

func TestWidgetLoginSuccess(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-WIDGET")
	rec := &sleepRecorder{}
	var embedCalls, signinGETCalls, signinPOSTCalls, diCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/sso/embed", func(w http.ResponseWriter, r *http.Request) {
		embedCalls++
		q := r.URL.Query()
		if got := q.Get("id"); got != "gauth-widget" {
			t.Errorf("embed id = %q, want gauth-widget", got)
		}
		if got := q.Get("embedWidget"); got != "true" {
			t.Errorf("embedWidget = %q, want true", got)
		}
		if got := q.Get("gauthHost"); got != "https://"+r.Host+"/sso" {
			t.Errorf("gauthHost = %q, want %q", got, "https://"+r.Host+"/sso")
		}
		w.Write([]byte(`<html><head><title>Sign In</title></head><body></body></html>`))
	})
	mux.HandleFunc("/sso/signin", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			signinGETCalls++
			q := r.URL.Query()
			embed := "https://" + r.Host + "/sso/embed"
			if got := q.Get("gauthHost"); got != embed {
				t.Errorf("signin gauthHost = %q, want %q", got, embed)
			}
			if got := q.Get("service"); got != embed {
				t.Errorf("signin service = %q, want %q", got, embed)
			}
			if got := q.Get("source"); got != embed {
				t.Errorf("signin source = %q, want %q", got, embed)
			}
			if got := r.Header.Get("Referer"); got != "https://"+r.Host+"/sso/embed" {
				t.Errorf("signin Referer = %q, want embed page", got)
			}
			w.Write([]byte(widgetSigninHTML))
		case http.MethodPost:
			signinPOSTCalls++
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse signin form: %v", err)
			}
			if got := r.PostForm.Get("_csrf"); got != "tok" {
				t.Errorf("_csrf = %q, want tok", got)
			}
			if got := r.PostForm.Get("username"); got != "user@example.com" {
				t.Errorf("username = %q", got)
			}
			if got := r.PostForm.Get("password"); got != "pw" {
				t.Errorf("password = %q", got)
			}
			if got := r.PostForm.Get("embed"); got != "true" {
				t.Errorf("embed = %q, want true", got)
			}
			w.Write([]byte(widgetSuccessHTML("ST-WIDGET")))
		default:
			t.Errorf("unexpected signin method %s", r.Method)
		}
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		diCalls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse DI form: %v", err)
		}
		if got := r.Form.Get("service_ticket"); got != "ST-WIDGET" {
			t.Errorf("service_ticket = %q, want ST-WIDGET", got)
		}
		if got := r.Form.Get("service_url"); got != "https://"+r.Host+"/sso/embed" {
			t.Errorf("service_url = %q", got)
		}
		ssoJSON(t, w, diTokenBody(token, "RT-WIDGET"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil, &LoginOptions{
		Strategies: []string{"widget+utls"},
		RootCAs:    testRootPool(srv),
		Sleep:      rec.sleep,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken != token || tok.DIRefreshToken != "RT-WIDGET" {
		t.Fatalf("tokens = %+v", tok)
	}
	if embedCalls != 1 || signinGETCalls != 1 || signinPOSTCalls != 1 || diCalls != 1 {
		t.Errorf("calls = embed %d, signin GET %d, signin POST %d, DI %d; want 1 each",
			embedCalls, signinGETCalls, signinPOSTCalls, diCalls)
	}
	assertOneDelay(t, rec, 3*time.Second, 8*time.Second)
}

func TestWidgetLoginMFA(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-WIDGET-MFA")
	rec := &sleepRecorder{}
	var promptCalls, verifyCalls, diCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/sso/embed", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html></html>`))
	})
	mux.HandleFunc("/sso/signin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(widgetSigninHTML))
			return
		}
		w.Write([]byte(widgetMFAHTML))
	})
	mux.HandleFunc("/sso/verifyMFA/loginEnterMfaCode", func(w http.ResponseWriter, r *http.Request) {
		verifyCalls++
		if r.Method != http.MethodPost {
			t.Errorf("verify method = %s, want POST", r.Method)
		}
		if got := r.URL.Query().Get("service"); got != "https://"+r.Host+"/sso/embed" {
			t.Errorf("verify service = %q", got)
		}
		if got := r.Header.Get("Referer"); !strings.Contains(got, "/sso/signin") {
			t.Errorf("verify Referer = %q, want the signin response URL", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse verify form: %v", err)
		}
		if got := r.PostForm.Get("mfa-code"); got != "123456" {
			t.Errorf("mfa-code = %q, want 123456", got)
		}
		if got := r.PostForm.Get("_csrf"); got != "tok2" {
			t.Errorf("_csrf = %q, want tok2", got)
		}
		if got := r.PostForm.Get("embed"); got != "true" {
			t.Errorf("embed = %q, want true", got)
		}
		if got := r.PostForm.Get("fromPage"); got != "setupEnterMfaCode" {
			t.Errorf("fromPage = %q, want setupEnterMfaCode", got)
		}
		w.Write([]byte(widgetSuccessHTML("ST-WIDGET-MFA")))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		diCalls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse DI form: %v", err)
		}
		if got := r.Form.Get("service_ticket"); got != "ST-WIDGET-MFA" {
			t.Errorf("service_ticket = %q, want ST-WIDGET-MFA", got)
		}
		ssoJSON(t, w, diTokenBody(token, "RT-WIDGET-MFA"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	mfa := func(context.Context) (string, error) {
		promptCalls++
		return "123456", nil
	}
	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", mfa, &LoginOptions{
		Strategies: []string{"widget+utls"},
		RootCAs:    testRootPool(srv),
		Sleep:      rec.sleep,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken != token || tok.DIRefreshToken != "RT-WIDGET-MFA" {
		t.Fatalf("tokens = %+v", tok)
	}
	if promptCalls != 1 || verifyCalls != 1 || diCalls != 1 {
		t.Errorf("calls = prompt %d, verify %d, DI %d; want 1 each", promptCalls, verifyCalls, diCalls)
	}
	assertOneDelay(t, rec, 3*time.Second, 8*time.Second)
}

func TestPortalLoginSuccess(t *testing.T) {
	token := fakeJWT(time.Now().Add(time.Hour), "CLIENT-PORTAL")
	rec := &sleepRecorder{}
	var getCalls, postCalls, diCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/portal/sso/en-US/sign-in", func(w http.ResponseWriter, r *http.Request) {
		getCalls++
		if r.Method != http.MethodGet {
			t.Errorf("signin method = %s, want GET", r.Method)
		}
		if got := r.URL.Query().Get("clientId"); got != portalSSOClientID {
			t.Errorf("clientId = %q, want %q", got, portalSSOClientID)
		}
		if got := r.URL.Query().Get("service"); got != portalSSOServiceURL {
			t.Errorf("service = %q, want %q", got, portalSSOServiceURL)
		}
		if got := r.Header.Get("User-Agent"); got != desktopUserAgent {
			t.Errorf("User-Agent = %q, want %q", got, desktopUserAgent)
		}
		if got := r.Header.Get("Accept-Language"); got != "en-US,en;q=0.9" {
			t.Errorf("Accept-Language = %q", got)
		}
		w.Write([]byte(`<html><head><title>Sign In</title></head></html>`))
	})
	mux.HandleFunc("/portal/api/login", func(w http.ResponseWriter, r *http.Request) {
		postCalls++
		if r.Method != http.MethodPost {
			t.Errorf("login method = %s, want POST", r.Method)
		}
		if got := r.URL.Query().Get("locale"); got != "en-US" {
			t.Errorf("locale = %q, want en-US", got)
		}
		if got := r.URL.Query().Get("service"); got != portalSSOServiceURL {
			t.Errorf("login service = %q, want %q", got, portalSSOServiceURL)
		}
		if got := r.Header.Get("Origin"); got != "http://"+r.Host {
			t.Errorf("Origin = %q, want %q", got, "http://"+r.Host)
		}
		if got := r.Header.Get("User-Agent"); got != desktopUserAgent {
			t.Errorf("login User-Agent = %q", got)
		}
		var body struct {
			Username     string `json:"username"`
			Password     string `json:"password"`
			RememberMe   bool   `json:"rememberMe"`
			CaptchaToken string `json:"captchaToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode login body: %v", err)
		}
		if body.Username != "user@example.com" || body.Password != "pw" || !body.RememberMe {
			t.Errorf("login body = %+v", body)
		}
		ssoJSON(t, w, loginSuccess("ST-PORTAL"))
	})
	mux.HandleFunc(diTestPath, func(w http.ResponseWriter, r *http.Request) {
		diCalls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse DI form: %v", err)
		}
		if got := r.Form.Get("service_ticket"); got != "ST-PORTAL" {
			t.Errorf("service_ticket = %q, want ST-PORTAL", got)
		}
		if got := r.Form.Get("service_url"); got != portalSSOServiceURL {
			t.Errorf("service_url = %q, want %q", got, portalSSOServiceURL)
		}
		ssoJSON(t, w, diTokenBody(token, "RT-PORTAL"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	tok, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "pw", nil, &LoginOptions{
		Strategies: []string{"portal+plain"},
		Sleep:      rec.sleep,
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.DIToken != token || tok.DIRefreshToken != "RT-PORTAL" {
		t.Fatalf("tokens = %+v", tok)
	}
	if getCalls != 1 || postCalls != 1 || diCalls != 1 {
		t.Errorf("calls = GET %d, POST %d, DI %d; want 1 each", getCalls, postCalls, diCalls)
	}
	assertOneDelay(t, rec, 10*time.Second, 20*time.Second)
}

func TestPortalInvalidPassword(t *testing.T) {
	rec := &sleepRecorder{}
	var getCalls, postCalls int

	mux := http.NewServeMux()
	mux.HandleFunc("/portal/sso/en-US/sign-in", func(w http.ResponseWriter, r *http.Request) {
		getCalls++
		w.Write([]byte(`<html></html>`))
	})
	mux.HandleFunc("/portal/api/login", func(w http.ResponseWriter, r *http.Request) {
		postCalls++
		ssoJSON(t, w, map[string]any{
			"responseStatus": map[string]any{"type": "INVALID_USERNAME_PASSWORD"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, err := Login(context.Background(), testEndpoints(srv.URL), "user@example.com", "bad", nil, &LoginOptions{
		Strategies: []string{"portal+plain", "mobile+plain"},
		Sleep:      rec.sleep,
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if getCalls != 1 || postCalls != 1 {
		t.Errorf("calls = GET %d, POST %d; want 1 each (chain must stop)", getCalls, postCalls)
	}
	assertOneDelay(t, rec, 10*time.Second, 20*time.Second)
}

func TestDefaultChainOrder(t *testing.T) {
	got := make([]string, len(loginStrategies))
	for i, s := range loginStrategies {
		got[i] = s.name
	}
	want := []string{"mobile+utls", "mobile+plain", "widget+utls", "portal+utls", "portal+plain"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("login strategies = %v, want %v", got, want)
	}
}

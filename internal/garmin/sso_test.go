package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMobileLoginMFA(t *testing.T) {
	var mfa atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mobile/api/login":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["username"] != "a@b" || body["password"] != "pw" || r.URL.Query().Get("clientId") != "GCM_IOS_DARK" {
				t.Error("incorrect login request")
			}
			fmt.Fprint(w, `{"responseStatus":{"type":"MFA_REQUIRED"},"customerMfaInfo":{"mfaLastMethodUsed":"email"}}`)
		case "/mobile/api/mfa/verifyCode":
			w.WriteHeader(429)
		case "/portal/api/mfa/verifyCode":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["mfaVerificationCode"] != "123456" {
				t.Error("incorrect MFA request")
			}
			mfa.Add(1)
			fmt.Fprint(w, `{"responseStatus":{"type":"SUCCESSFUL"},"serviceTicketId":"ST-1"}`)
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("service_ticket") != "ST-1" || r.Form.Get("grant_type") != diGrant {
				t.Error("incorrect token request")
			}
			fmt.Fprint(w, `{"access_token":"token","refresh_token":"refresh"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	tokens, err := Login(context.Background(), Endpoints{SSO: srv.URL, Connect: srv.URL, DIToken: srv.URL + "/token"}, "a@b", "pw", func(context.Context) (string, error) { return "123456", nil }, &LoginOptions{Strategies: []string{"mobile+plain"}})
	if err != nil || tokens.DIToken != "token" || tokens.DIRefreshToken != "refresh" || mfa.Load() != 1 {
		t.Fatalf("%+v %v", tokens, err)
	}
}

func TestLoginStopsForInvalidCredentialsAndMissingMFA(t *testing.T) {
	for _, status := range []string{"INVALID_USERNAME_PASSWORD", "MFA_REQUIRED"} {
		t.Run(status, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				fmt.Fprintf(w, `{"responseStatus":{"type":%q}}`, status)
			}))
			defer srv.Close()
			_, err := Login(context.Background(), Endpoints{SSO: srv.URL}, "a@b", "pw", nil, &LoginOptions{Strategies: []string{"mobile+plain", "portal+plain"}})
			if status == "INVALID_USERNAME_PASSWORD" && !errors.Is(err, ErrInvalidCredentials) {
				t.Fatal(err)
			}
			if status == "MFA_REQUIRED" && !errors.Is(err, errMFA) {
				t.Fatal(err)
			}
			if requests.Load() != 1 {
				t.Fatal(requests.Load())
			}
		})
	}
}
func TestDIExchangeTriesClientIDs(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mobile/api/login" {
			fmt.Fprint(w, `{"responseStatus":{"type":"SUCCESSFUL"},"serviceTicketId":"ST-1"}`)
			return
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(400)
			return
		}
		fmt.Fprint(w, `{"access_token":"token","refresh_token":"refresh"}`)
	}))
	defer srv.Close()
	token, err := Login(context.Background(), Endpoints{SSO: srv.URL, DIToken: srv.URL + "/token"}, "a@b", "pw", nil, &LoginOptions{Strategies: []string{"mobile+plain"}})
	if err != nil || token.DIToken != "token" || attempts.Load() != 2 {
		t.Fatal(token, err, attempts.Load())
	}
}
func TestAllRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }))
	defer srv.Close()
	_, err := Login(context.Background(), Endpoints{SSO: srv.URL}, "a@b", "pw", nil, &LoginOptions{Strategies: []string{"mobile+plain", "portal+plain"}})
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatal(err)
	}
}
func TestPortalLoginSuccess(t *testing.T) {
	var slept time.Duration
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/portal/sso/en-US/sign-in":
			fmt.Fprint(w, "signin")
		case "/portal/api/login":
			fmt.Fprint(w, `{"responseStatus":{"type":"SUCCESSFUL"},"serviceTicketId":"ST-1"}`)
		case "/token":
			fmt.Fprint(w, `{"access_token":"token","refresh_token":"refresh"}`)
		default:
			t.Errorf("unexpected %s", r.URL)
		}
	}))
	defer srv.Close()
	tok, err := Login(context.Background(), Endpoints{SSO: srv.URL, Connect: srv.URL, DIToken: srv.URL + "/token"}, "a@b", "pw", nil, &LoginOptions{Strategies: []string{"portal+plain"}, Sleep: func(d time.Duration) { slept = d }})
	if err != nil || tok.DIToken != "token" || slept < 10*time.Second || slept > 20*time.Second {
		t.Fatal(tok, err, slept)
	}
}
func TestWidgetLoginAndMFA(t *testing.T) {
	for _, mfa := range []bool{false, true} {
		t.Run(fmt.Sprint(mfa), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sso/embed":
					fmt.Fprint(w, "embed")
				case "/sso/signin":
					if r.Method == "GET" {
						fmt.Fprint(w, `<input name="_csrf" value="tok">`)
						return
					}
					if err := r.ParseForm(); err != nil || r.Form.Get("_csrf") != "tok" || r.Form.Get("username") != "a@b" {
						t.Error("invalid widget form")
					}
					if mfa {
						fmt.Fprint(w, `<title>MFA</title><input name="_csrf" value="tok">`)
					} else {
						fmt.Fprint(w, `<title>Success</title><a href="embed?ticket=ST-1">ok</a>`)
					}
				case "/sso/verifyMFA/loginEnterMfaCode":
					r.ParseForm()
					if r.Form.Get("mfa-code") != "123456" {
						t.Error("wrong MFA code")
					}
					fmt.Fprint(w, `<title>Success</title><a href="embed?ticket=ST-1">ok</a>`)
				case "/token":
					fmt.Fprint(w, `{"access_token":"token","refresh_token":"refresh"}`)
				default:
					t.Errorf("unexpected %s", r.URL)
				}
			}))
			defer srv.Close()
			s := loginState{ep: Endpoints{SSO: srv.URL, DIToken: srv.URL + "/token"}, email: "a@b", password: "pw", mfa: func(context.Context) (string, error) { return "123456", nil }, opt: LoginOptions{Sleep: func(time.Duration) {}}}
			tok, err := s.widget(context.Background(), s.client(nil))
			if err != nil || tok.DIToken != "token" {
				t.Fatal(tok, err)
			}
		})
	}
}

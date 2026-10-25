package garmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// onServer expects a service URL on the fake server itself (Endpoints.SSO == Endpoints.Connect).
func onServer(path string) func(*http.Request) string {
	return func(r *http.Request) string { return "https://" + r.Host + path }
}

const csrfPage = `<html><head><title>GARMIN Authentication Application</title></head>
<form><input type="hidden" name="_csrf" value="tok"/></form></html>`

const successPage = `<html><head><title>Success</title></head>
<script>var url = "https://sso.example/sso/embed?ticket=ST-1";</script></html>`

func widgetRoutes(t *testing.T, postSignin http.HandlerFunc) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/sso/embed": func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("id") != "gauth-widget" || q.Get("embedWidget") != "true" || q.Get("gauthHost") != "https://"+r.Host+"/sso" {
				t.Errorf("embed query = %v", q)
			}
			http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "s1", Path: "/"})
			fmt.Fprint(w, "<html></html>")
		},
		"/sso/signin": func(w http.ResponseWriter, r *http.Request) {
			embed := "https://" + r.Host + "/sso/embed"
			if q := r.URL.Query(); q.Get("service") != embed || q.Get("gauthHost") != embed || q.Get("source") != embed {
				t.Errorf("signin query = %v", q)
			}
			if r.UserAgent() != desktopUserAgent {
				t.Errorf("User-Agent = %q", r.UserAgent())
			}
			if r.Method == http.MethodGet {
				if r.Referer() != embed {
					t.Errorf("signin GET Referer = %q", r.Referer())
				}
				fmt.Fprint(w, csrfPage)
				return
			}
			r.ParseForm()
			if c, err := r.Cookie("SESSION"); err != nil || c.Value != "s1" {
				t.Errorf("signin POST without embed cookie")
			}
			if r.PostForm.Get("_csrf") != "tok" || r.PostForm.Get("username") != "a@b.c" ||
				r.PostForm.Get("password") != "pw" || r.PostForm.Get("embed") != "true" ||
				!strings.Contains(r.Referer(), "/sso/signin?") {
				t.Errorf("signin POST form = %v Referer = %q", r.PostForm, r.Referer())
			}
			postSignin(w, r)
		},
		diPath: diOKFor(t, onServer("/sso/embed")),
	}
}

// sleeps records the requested delays instead of sleeping.
func sleeps(opt *LoginOptions) *[]time.Duration {
	var d []time.Duration
	opt.Sleep = func(x time.Duration) { d = append(d, x) }
	return &d
}

func checkSleeps(t *testing.T, got []time.Duration, n int, lo, hi time.Duration) {
	t.Helper()
	if len(got) != n {
		t.Fatalf("sleeps = %v, want %d", got, n)
	}
	for _, d := range got {
		if d < lo || d > hi {
			t.Fatalf("sleep %v outside [%v, %v]", d, lo, hi)
		}
	}
}

func TestWidgetLoginSuccess(t *testing.T) {
	f := newFakeSSO(t, true, []string{"widget+utls"}, widgetRoutes(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, successPage)
	}))
	slept := sleeps(f.opt)
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
	checkSleeps(t, *slept, 1, 3*time.Second, 8*time.Second)
	if f.count("/sso/embed") != 1 || f.count("/sso/signin") != 2 {
		t.Fatalf("hits embed=%d signin=%d", f.count("/sso/embed"), f.count("/sso/signin"))
	}
}

func TestWidgetLoginMFA(t *testing.T) {
	routes := widgetRoutes(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<html><head><title>Enter MFA code for login</title></head>
<form><input type="hidden" name="_csrf" value="mfatok"/></form></html>`)
	})
	routes["/sso/verifyMFA/loginEnterMfaCode"] = func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("mfa-code") != "123456" || r.PostForm.Get("_csrf") != "mfatok" ||
			r.PostForm.Get("embed") != "true" || r.PostForm.Get("fromPage") != "setupEnterMfaCode" ||
			r.URL.Query().Get("service") != "https://"+r.Host+"/sso/embed" || !strings.Contains(r.Referer(), "/sso/signin?") {
			t.Errorf("verifyMFA form = %v query = %v Referer = %q", r.PostForm, r.URL.Query(), r.Referer())
		}
		fmt.Fprint(w, successPage)
	}
	f := newFakeSSO(t, true, []string{"widget+utls", "portal+plain"}, routes)
	sleeps(f.opt)
	calls := 0
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", func(context.Context) (string, error) {
		calls++
		return "123456", nil
	}, f.opt)
	checkTokens(t, tok, err)
	if calls != 1 || f.count("/sso/verifyMFA/loginEnterMfaCode") != 1 {
		t.Fatalf("prompt calls = %d, verify hits = %d", calls, f.count("/sso/verifyMFA/loginEnterMfaCode"))
	}
}

func TestWidgetLockedStopsChain(t *testing.T) {
	f := newFakeSSO(t, true, []string{"widget+utls"}, widgetRoutes(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<title>Account Locked</title>`)
	}))
	sleeps(f.opt)
	if _, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func portalRoutes(t *testing.T, login http.HandlerFunc) map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"/portal/sso/en-US/sign-in": func(w http.ResponseWriter, r *http.Request) {
			if q := r.URL.Query(); r.Method != http.MethodGet || q.Get("clientId") != "GarminConnect" ||
				q.Get("service") != onServer("/app")(r) || r.UserAgent() != desktopUserAgent {
				t.Errorf("sign-in %s %v UA=%q", r.Method, q, r.UserAgent())
			}
			http.SetCookie(w, &http.Cookie{Name: "SESSION", Value: "s1", Path: "/"})
			fmt.Fprint(w, "<html></html>")
		},
		"/portal/api/login": func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			q := r.URL.Query()
			wantRef := "https://" + r.Host + "/portal/sso/en-US/sign-in?clientId=GarminConnect&service=" + onServer("/app")(r)
			if r.Method != http.MethodPost || q.Get("clientId") != "GarminConnect" || q.Get("locale") != "en-US" ||
				q.Get("service") != onServer("/app")(r) || r.UserAgent() != desktopUserAgent ||
				r.Header.Get("Origin") != "https://"+r.Host || r.Referer() != wantRef ||
				body["username"] != "a@b.c" || body["password"] != "pw" || body["rememberMe"] != true {
				t.Errorf("login %s %v Origin=%q Referer=%q body=%v", r.Method, q, r.Header.Get("Origin"), r.Referer(), body)
			}
			if c, err := r.Cookie("SESSION"); err != nil || c.Value != "s1" {
				t.Errorf("login POST without sign-in cookie")
			}
			login(w, r)
		},
		diPath: diOKFor(t, onServer("/app")),
	}
}

func TestPortalLoginSuccess(t *testing.T) {
	f := newFakeSSO(t, true, []string{"portal+utls"}, portalRoutes(t, respond("SUCCESSFUL")))
	slept := sleeps(f.opt)
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	checkTokens(t, tok, err)
	checkSleeps(t, *slept, 1, 10*time.Second, 20*time.Second)
}

func TestPortalLoginMFA(t *testing.T) {
	routes := portalRoutes(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"responseStatus": map[string]any{"type": "MFA_REQUIRED"},
			"customerMfaInfo": map[string]any{"mfaLastMethodUsed": "sms"}})
	})
	routes["/portal/api/mfa/verifyCode"] = func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["mfaVerificationCode"] != "123456" || body["mfaMethod"] != "sms" ||
			r.URL.Query().Get("clientId") != "GarminConnect" || r.UserAgent() != desktopUserAgent {
			t.Errorf("portal verify body=%v query=%v UA=%q", body, r.URL.Query(), r.UserAgent())
		}
		respond("SUCCESSFUL")(w, r)
	}
	f := newFakeSSO(t, true, []string{"portal+utls"}, routes)
	sleeps(f.opt)
	tok, err := Login(context.Background(), f.ep, "a@b.c", "pw", func(context.Context) (string, error) { return "123456", nil }, f.opt)
	checkTokens(t, tok, err)
	if f.count("/portal/api/login") != 1 || f.count("/mobile/api/mfa/verifyCode") != 0 {
		t.Fatalf("hits login=%d mobile verify=%d", f.count("/portal/api/login"), f.count("/mobile/api/mfa/verifyCode"))
	}
}

func TestPortalRotatesHellosOn429(t *testing.T) {
	f := newFakeSSO(t, true, []string{"portal+utls"}, portalRoutes(t, status(429)))
	slept := sleeps(f.opt)
	_, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("err = %v", err)
	}
	if n := f.count("/portal/api/login"); n != 5 {
		t.Fatalf("login hits = %d, want 5", n)
	}
	checkSleeps(t, *slept, 5, 10*time.Second, 20*time.Second)
}

func TestPortalInvalidPassword(t *testing.T) {
	f := newFakeSSO(t, true, []string{"portal+utls", "portal+plain"}, portalRoutes(t, respond("INVALID_USERNAME_PASSWORD")))
	sleeps(f.opt)
	_, err := Login(context.Background(), f.ep, "a@b.c", "pw", noPrompt, f.opt)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
	if n := f.count("/portal/api/login"); n != 1 {
		t.Fatalf("login hits = %d, want 1", n)
	}
}

func TestDefaultChainOrder(t *testing.T) {
	var names []string
	for _, s := range strategies {
		names = append(names, s.name)
	}
	if want := []string{"mobile+utls", "mobile+plain", "widget+utls", "portal+utls", "portal+plain"}; !slices.Equal(names, want) {
		t.Fatalf("chain = %v, want %v", names, want)
	}
}

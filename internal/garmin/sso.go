package garmin

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// Copied verbatim from python-garminconnect 0.3.2 client.py.
const (
	iosSSOClientID    = "GCM_IOS_DARK"
	iosServiceURL     = "https://mobile.integration.garmin.com/gcm/ios"
	iosLoginUA        = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	portalSSOClientID = "GarminConnect"
	diGrantType       = "https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket"
	ssoRequestTimeout = 30 * time.Second
)

var diClientIDs = []string{
	"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2",
	"GARMIN_CONNECT_MOBILE_ANDROID_DI_2024Q4",
	"GARMIN_CONNECT_MOBILE_ANDROID_DI",
	"GARMIN_CONNECT_MOBILE_IOS_DI",
}

// Go equivalents of MOBILE_IMPERSONATIONS ("safari_ios", "safari", "chrome120").
var mobileHellos = []utls.ClientHelloID{utls.HelloIOS_Auto, utls.HelloSafari_Auto, utls.HelloChrome_120}

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrTooManyRequests    = errors.New("rate limited by Garmin (429)")
)

// MFAPrompt returns the MFA code the user received.
type MFAPrompt func(ctx context.Context) (string, error)

type LoginOptions struct {
	Strategies []string            // nil: every strategy in chain order
	RootCAs    *x509.CertPool      // nil: system roots
	Sleep      func(time.Duration) // nil: time.Sleep
	Log        io.Writer           // nil: io.Discard; receives strategy names and HTTP statuses only
}

// ssoLogin is the state of one Login call, shared by the strategies.
type ssoLogin struct {
	ep              Endpoints
	email, password string
	mfa             MFAPrompt
	opt             LoginOptions
	mfaRequested    bool // set once Garmin asked for MFA: from then on every error stops the chain
}

type strategy struct {
	name string
	run  func(ctx context.Context, s *ssoLogin) (Tokens, error)
}

// strategies is the login chain in order; the web fallbacks live in sso_web.go.
var strategies = []strategy{
	{"mobile+utls", func(ctx context.Context, s *ssoLogin) (Tokens, error) {
		var err error
		for _, h := range mobileHellos {
			var tok Tokens
			if tok, err = s.mobileLogin(ctx, s.utlsClient(h)); err == nil || s.stops(ctx, err) {
				return tok, err
			}
		}
		return Tokens{}, err
	}},
	{"mobile+plain", func(ctx context.Context, s *ssoLogin) (Tokens, error) {
		return s.mobileLogin(ctx, s.plainClient())
	}},
	{"widget+utls", func(ctx context.Context, s *ssoLogin) (Tokens, error) {
		return s.widgetLogin(ctx, s.utlsClient(utls.HelloChrome_Auto))
	}},
	{"portal+utls", func(ctx context.Context, s *ssoLogin) (Tokens, error) {
		var err error
		for _, h := range portalHellos {
			var tok Tokens
			if tok, err = s.portalLogin(ctx, s.utlsClient(h)); err == nil || s.stops(ctx, err) {
				return tok, err
			}
		}
		return Tokens{}, err
	}},
	{"portal+plain", func(ctx context.Context, s *ssoLogin) (Tokens, error) {
		return s.portalLogin(ctx, s.plainClient())
	}},
}

// Login runs the SSO strategy chain and exchanges the service ticket for DI tokens.
func Login(ctx context.Context, ep Endpoints, email, password string, mfa MFAPrompt, opt *LoginOptions) (Tokens, error) {
	s := &ssoLogin{ep: ep, email: email, password: password, mfa: mfa}
	if opt != nil {
		s.opt = *opt
	}
	if s.opt.Sleep == nil {
		s.opt.Sleep = time.Sleep
	}
	if s.opt.Log == nil {
		s.opt.Log = io.Discard
	}
	chain := strategies
	if s.opt.Strategies != nil {
		chain = nil
		for _, name := range s.opt.Strategies {
			i := 0
			for i < len(strategies) && strategies[i].name != name {
				i++
			}
			if i == len(strategies) {
				return Tokens{}, fmt.Errorf("unknown login strategy %q", name)
			}
			chain = append(chain, strategies[i])
		}
	}

	if len(chain) == 0 {
		return Tokens{}, errors.New("no login strategies selected")
	}
	var lastErr error
	rateLimited := 0
	for _, st := range chain {
		fmt.Fprintf(s.opt.Log, "garmin login: trying %s\n", st.name)
		tok, err := st.run(ctx, s)
		if err == nil {
			return tok, nil
		}
		if s.stops(ctx, err) {
			return Tokens{}, err
		}
		fmt.Fprintf(s.opt.Log, "garmin login: %s failed: %v\n", st.name, err)
		if errors.Is(err, ErrTooManyRequests) {
			rateLimited++
		}
		lastErr = err
	}
	if rateLimited == len(chain) {
		return Tokens{}, fmt.Errorf("all login strategies failed: %w", ErrTooManyRequests)
	}
	return Tokens{}, fmt.Errorf("all login strategies failed: %w", lastErr)
}

// stops reports whether err ends the chain instead of moving on to the next strategy.
func (s *ssoLogin) stops(ctx context.Context, err error) bool {
	return s.mfaRequested || errors.Is(err, ErrInvalidCredentials) || ctx.Err() != nil
}

// promptMFA asks for the MFA code; the chain stops after this whatever the outcome.
func (s *ssoLogin) promptMFA(ctx context.Context) (string, error) {
	s.mfaRequested = true
	if s.mfa == nil {
		return "", errors.New("MFA required but no MFA prompt available")
	}
	return s.mfa(ctx)
}

func (s *ssoLogin) utlsClient(h utls.ClientHelloID) *http.Client {
	jar, _ := cookiejar.New(nil)
	c := newUTLSClient(h, s.opt.RootCAs, jar)
	c.Timeout = ssoRequestTimeout
	return c
}

func (s *ssoLogin) plainClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: ssoRequestTimeout}
	if s.opt.RootCAs != nil {
		c.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: s.opt.RootCAs}}
	}
	return c
}

// ssoResponse is a decoded SSO login/MFA reply, read leniently like python's res.get(...).
type ssoResponse map[string]any

// str returns the string at the nested key path, or "" when missing or not a string.
func (r ssoResponse) str(keys ...string) string {
	var v any = map[string]any(r)
	for _, k := range keys {
		m, _ := v.(map[string]any)
		v = m[k]
	}
	s, _ := v.(string)
	return s
}

// postJSON posts body as JSON with the given headers and decodes the reply.
// It returns the HTTP status; decodeErr is set when the reply is not a JSON object.
func (s *ssoLogin) postJSON(ctx context.Context, hc *http.Client, path string, q url.Values, hdr http.Header, body any) (res ssoResponse, code int, decodeErr, err error) {
	b, err := json.Marshal(body)
	if err != nil {
		return res, 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.ep.SSO+path+"?"+q.Encode(), bytes.NewReader(b))
	if err != nil {
		return res, 0, nil, err
	}
	req.Header = hdr.Clone() // the client writes jar cookies into req.Header; hdr is reused across requests
	resp, err := hc.Do(req)
	if err != nil {
		return res, 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return res, resp.StatusCode, nil, err
	}
	return res, resp.StatusCode, json.Unmarshal(raw, &res), nil
}

func iosLoginParams() url.Values {
	return url.Values{"clientId": {iosSSOClientID}, "locale": {"en-US"}, "service": {iosServiceURL}}
}

func (s *ssoLogin) iosLoginHeaders() http.Header {
	return http.Header{
		"User-Agent":   {iosLoginUA},
		"Accept":       {"application/json, text/plain, */*"},
		"Content-Type": {"application/json"},
		"Origin":       {s.ep.SSO},
	}
}

// mobileLogin ports _do_mobile_login, with the MFA step done inline.
func (s *ssoLogin) mobileLogin(ctx context.Context, hc *http.Client) (Tokens, error) {
	return s.apiLogin(ctx, hc, "mobile", iosLoginParams(), s.iosLoginHeaders())
}

// apiLogin posts the credentials to /<flow>/api/login ("mobile" or "portal") and handles
// the JSON reply the way _do_mobile_login and _do_portal_web_login both do.
func (s *ssoLogin) apiLogin(ctx context.Context, hc *http.Client, flow string, q url.Values, hdr http.Header) (Tokens, error) {
	res, code, decodeErr, err := s.postJSON(ctx, hc, "/"+flow+"/api/login", q, hdr, map[string]any{
		"username": s.email, "password": s.password, "rememberMe": true, "captchaToken": "",
	})
	switch {
	case err != nil:
		return Tokens{}, fmt.Errorf("%s login: %w", flow, err)
	case code == http.StatusTooManyRequests:
		return Tokens{}, fmt.Errorf("%s login: %w", flow, ErrTooManyRequests)
	case decodeErr != nil:
		return Tokens{}, fmt.Errorf("%s login failed (non-JSON): HTTP %d", flow, code)
	}
	switch res.str("responseStatus", "type") {
	case "MFA_REQUIRED":
		method := res.str("customerMfaInfo", "mfaLastMethodUsed")
		if method == "" {
			method = "email"
		}
		mfaCode, err := s.promptMFA(ctx)
		if err != nil {
			return Tokens{}, err
		}
		return s.completeMFA(ctx, hc, flow, hdr, method, mfaCode)
	case "SUCCESSFUL":
		return s.exchangeServiceTicket(ctx, res.str("serviceTicketId"), s.flowServiceURL(flow))
	case "INVALID_USERNAME_PASSWORD":
		return Tokens{}, ErrInvalidCredentials
	}
	if res.str("error", "status-code") == "429" {
		return Tokens{}, fmt.Errorf("%s login: 429 in JSON body: %w", flow, ErrTooManyRequests)
	}
	return Tokens{}, fmt.Errorf("%s login failed: HTTP %d, response type %q", flow, code, res.str("responseStatus", "type"))
}

func (s *ssoLogin) flowServiceURL(flow string) string {
	if flow == "portal" {
		return s.portalServiceURL()
	}
	return iosServiceURL
}

// completeMFA ports the ios/portal branches of _complete_mfa: the flow's verify endpoint,
// then the other one, both with the flow's login headers.
func (s *ssoLogin) completeMFA(ctx context.Context, hc *http.Client, flow string, hdr http.Header, method, mfaCode string) (Tokens, error) {
	body := map[string]any{
		"mfaMethod":           method,
		"mfaVerificationCode": mfaCode,
		"rememberMyBrowser":   true,
		"reconsentList":       []any{},
		"mfaSetup":            false,
	}
	endpoints := []struct {
		path string
		q    url.Values
	}{
		{"/mobile/api/mfa/verifyCode", iosLoginParams()},
		{"/portal/api/mfa/verifyCode", s.portalLoginParams()},
	}
	if flow == "portal" {
		endpoints[0], endpoints[1] = endpoints[1], endpoints[0]
	}
	var failures []string
	rateLimited := 0
	for _, e := range endpoints {
		res, code, decodeErr, err := s.postJSON(ctx, hc, e.path, e.q, hdr, body)
		switch {
		case err != nil:
			failures = append(failures, e.path+": connection error")
		case code == http.StatusTooManyRequests:
			failures = append(failures, e.path+": HTTP 429")
			rateLimited++
		case decodeErr != nil:
			failures = append(failures, fmt.Sprintf("%s: HTTP %d non-JSON", e.path, code))
		case res.str("error", "status-code") == "429":
			failures = append(failures, e.path+": 429 in JSON body")
			rateLimited++
		case res.str("responseStatus", "type") == "SUCCESSFUL":
			return s.exchangeServiceTicket(ctx, res.str("serviceTicketId"), s.flowServiceURL(flow))
		default:
			failures = append(failures, fmt.Sprintf("%s: HTTP %d, response type %q", e.path, code, res.str("responseStatus", "type")))
		}
	}
	if rateLimited == len(endpoints) {
		return Tokens{}, fmt.Errorf("MFA verification rate limited on all endpoints: %w", ErrTooManyRequests)
	}
	return Tokens{}, fmt.Errorf("MFA verification failed: %s", strings.Join(failures, "; "))
}

// exchangeServiceTicket ports _exchange_service_ticket: each DI client ID in order.
func (s *ssoLogin) exchangeServiceTicket(ctx context.Context, ticket, serviceURL string) (Tokens, error) {
	for _, clientID := range diClientIDs {
		form := url.Values{
			"client_id":      {clientID},
			"service_ticket": {ticket},
			"grant_type":     {diGrantType},
			"service_url":    {serviceURL},
		}
		code, raw, err := s.diPost(ctx, clientID, form)
		if err != nil {
			return Tokens{}, fmt.Errorf("DI token exchange: %w", err)
		}
		if code == http.StatusTooManyRequests {
			return Tokens{}, fmt.Errorf("DI token exchange: %w", ErrTooManyRequests)
		}
		if code >= 400 {
			fmt.Fprintf(s.opt.Log, "garmin login: DI exchange HTTP %d for %s\n", code, clientID)
			continue
		}
		var data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		if json.Unmarshal(raw, &data) != nil || data.AccessToken == "" {
			continue
		}
		tok := Tokens{DIToken: data.AccessToken, DIRefreshToken: data.RefreshToken, DIClientID: clientIDFromJWT(data.AccessToken)}
		if tok.DIClientID == "" {
			tok.DIClientID = clientID
		}
		return tok, nil
	}
	return Tokens{}, errors.New("DI token exchange failed for all client IDs")
}

// diPost sends one DI token request: utls Chrome for https (python uses curl_cffi "chrome"),
// falling back to Go's TLS on a transport error; plain HTTP goes straight to Go's client.
func (s *ssoLogin) diPost(ctx context.Context, clientID string, form url.Values) (int, []byte, error) {
	send := func(hc *http.Client) (int, []byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.ep.DIToken, strings.NewReader(form.Encode()))
		if err != nil {
			return 0, nil, err
		}
		req.Header = nativeHeaders()
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":")))
		req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Cache-Control", "no-cache")
		resp, err := hc.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		return resp.StatusCode, b, err
	}
	if strings.HasPrefix(s.ep.DIToken, "https://") {
		hc := newUTLSClient(utls.HelloChrome_Auto, s.opt.RootCAs, nil)
		hc.Timeout = ssoRequestTimeout
		if code, b, err := send(hc); err == nil {
			return code, b, nil
		}
		fmt.Fprintln(s.opt.Log, "garmin login: DI exchange over utls failed, retrying with Go TLS")
	}
	plain := s.plainClient()
	plain.Jar = nil
	return send(plain)
}

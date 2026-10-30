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

const iosService = "https://mobile.integration.garmin.com/gcm/ios"
const iosAgent = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
const desktopAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
const diGrant = "https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket"

var diIDs = []string{"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2", "GARMIN_CONNECT_MOBILE_ANDROID_DI_2024Q4", "GARMIN_CONNECT_MOBILE_ANDROID_DI", "GARMIN_CONNECT_MOBILE_IOS_DI"}
var ErrInvalidCredentials = errors.New("invalid username or password")
var ErrTooManyRequests = errors.New("rate limited by Garmin (429)")

type MFAPrompt func(context.Context) (string, error)
type LoginOptions struct {
	Strategies []string
	RootCAs    *x509.CertPool
	Sleep      func(time.Duration)
	Log        io.Writer
}
type loginState struct {
	ep              Endpoints
	email, password string
	mfa             MFAPrompt
	opt             LoginOptions
}
type loginReply struct {
	ResponseStatus struct {
		Type string `json:"type"`
	} `json:"responseStatus"`
	ServiceTicketID string `json:"serviceTicketId"`
	CustomerMFAInfo struct {
		Method string `json:"mfaLastMethodUsed"`
	} `json:"customerMfaInfo"`
	Error struct {
		Status string `json:"status-code"`
	} `json:"error"`
}

func Login(ctx context.Context, ep Endpoints, email, password string, mfa MFAPrompt, opt *LoginOptions) (Tokens, error) {
	s := loginState{ep: ep, email: email, password: password, mfa: mfa}
	if opt != nil {
		s.opt = *opt
	}
	if s.opt.Sleep == nil {
		s.opt.Sleep = time.Sleep
	}
	if s.opt.Log == nil {
		s.opt.Log = io.Discard
	}
	names := s.opt.Strategies
	if names == nil {
		names = []string{"mobile+utls", "mobile+plain", "widget+utls", "portal+utls", "portal+plain"}
	}
	var last error
	rate := 0
	for _, name := range names {
		fmt.Fprintf(s.opt.Log, "Trying login strategy: %s\n", name)
		var token Tokens
		var err error
		switch name {
		case "mobile+utls":
			token, err = s.rotating(ctx, "mobile", []utls.ClientHelloID{utls.HelloIOS_Auto, utls.HelloSafari_Auto, utls.HelloChrome_120})
		case "mobile+plain":
			token, err = s.jsonLogin(ctx, "mobile", s.client(nil))
		case "widget+utls":
			token, err = s.widget(ctx, s.client(&utls.HelloChrome_Auto))
		case "portal+utls":
			token, err = s.rotating(ctx, "portal", []utls.ClientHelloID{utls.HelloSafari_Auto, utls.HelloIOS_Auto, utls.HelloChrome_120, utls.HelloEdge_Auto, utls.HelloChrome_Auto})
		case "portal+plain":
			token, err = s.jsonLogin(ctx, "portal", s.client(nil))
		default:
			return Tokens{}, fmt.Errorf("unknown login strategy %q", name)
		}
		if err == nil {
			return token, nil
		}
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, errMFA) || ctx.Err() != nil {
			return Tokens{}, err
		}
		if errors.Is(err, ErrTooManyRequests) {
			rate++
		}
		last = err
	}
	if len(names) > 0 && rate == len(names) {
		return Tokens{}, ErrTooManyRequests
	}
	return Tokens{}, fmt.Errorf("all login strategies failed: %w", last)
}

var errMFA = errors.New("MFA required but no MFA prompt available")

func (s *loginState) client(hello *utls.ClientHelloID) *http.Client {
	jar, _ := cookiejar.New(nil)
	if hello != nil {
		c := newUTLSClient(*hello, s.opt.RootCAs, jar)
		c.Timeout = 30 * time.Second
		return c
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if s.opt.RootCAs != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: s.opt.RootCAs, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: transport}
}
func (s *loginState) rotating(ctx context.Context, flow string, hellos []utls.ClientHelloID) (Tokens, error) {
	var err error
	for _, h := range hellos {
		var t Tokens
		t, err = s.jsonLogin(ctx, flow, s.client(&h))
		if err == nil || errors.Is(err, ErrInvalidCredentials) || errors.Is(err, errMFA) || ctx.Err() != nil {
			return t, err
		}
	}
	return Tokens{}, err
}
func (s *loginState) params(flow string) url.Values {
	if flow == "mobile" {
		return url.Values{"clientId": {"GCM_IOS_DARK"}, "locale": {"en-US"}, "service": {iosService}}
	}
	return url.Values{"clientId": {"GarminConnect"}, "locale": {"en-US"}, "service": {s.ep.Connect + "/app"}}
}
func (s *loginState) request(ctx context.Context, c *http.Client, method, path string, params url.Values, body io.Reader, headers http.Header) ([]byte, error) {
	u := strings.TrimRight(s.ep.SSO, "/") + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 429 {
		return nil, ErrTooManyRequests
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("SSO HTTP %d", res.StatusCode)
	}
	return io.ReadAll(res.Body)
}
func (s *loginState) jsonLogin(ctx context.Context, flow string, c *http.Client) (Tokens, error) {
	params := s.params(flow)
	agent := iosAgent
	if flow == "portal" {
		agent = desktopAgent
		if _, err := s.request(ctx, c, "GET", "/portal/sso/en-US/sign-in", params, nil, http.Header{"User-Agent": {agent}, "Accept": {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"}, "Accept-Language": {"en-US,en;q=0.9"}}); err != nil {
			return Tokens{}, err
		}
		s.opt.Sleep(15 * time.Second)
	}
	h := http.Header{"User-Agent": {agent}, "Accept": {"application/json, text/plain, */*"}, "Content-Type": {"application/json"}, "Origin": {s.ep.SSO}}
	if flow == "portal" {
		h.Set("Accept-Language", "en-US,en;q=0.9")
		h.Set("Referer", strings.TrimRight(s.ep.SSO, "/")+"/portal/sso/en-US/sign-in?"+url.Values{"clientId": {"GarminConnect"}, "service": {params.Get("service")}}.Encode())
	}
	body, _ := json.Marshal(map[string]any{"username": s.email, "password": s.password, "rememberMe": true, "captchaToken": ""})
	b, err := s.request(ctx, c, "POST", "/"+flow+"/api/login", params, bytes.NewReader(body), h)
	if err != nil {
		return Tokens{}, err
	}
	var reply loginReply
	if err = json.Unmarshal(b, &reply); err != nil {
		return Tokens{}, errors.New("SSO returned non-JSON response")
	}
	switch reply.ResponseStatus.Type {
	case "INVALID_USERNAME_PASSWORD":
		return Tokens{}, ErrInvalidCredentials
	case "MFA_REQUIRED":
		if s.mfa == nil {
			return Tokens{}, errMFA
		}
		code, err := s.mfa(ctx)
		if err != nil {
			return Tokens{}, fmt.Errorf("%w: %v", errMFA, err)
		}
		method := reply.CustomerMFAInfo.Method
		if method == "" {
			method = "email"
		}
		payload, _ := json.Marshal(map[string]any{"mfaMethod": method, "mfaVerificationCode": code, "rememberMyBrowser": true, "reconsentList": []any{}, "mfaSetup": false})
		alt := "portal"
		if flow == "portal" {
			alt = "mobile"
		}
		success := false
		for _, f := range []string{flow, alt} {
			b, err = s.request(ctx, c, "POST", "/"+f+"/api/mfa/verifyCode", s.params(f), bytes.NewReader(payload), h)
			if err != nil {
				continue
			}
			reply = loginReply{}
			if json.Unmarshal(b, &reply) == nil && reply.ResponseStatus.Type == "SUCCESSFUL" {
				success = true
				break
			}
		}
		if !success {
			return Tokens{}, fmt.Errorf("%w: verification failed", errMFA)
		}
	case "SUCCESSFUL":
	default:
		if reply.Error.Status == "429" {
			return Tokens{}, ErrTooManyRequests
		}
		return Tokens{}, errors.New("SSO login failed")
	}
	if reply.ServiceTicketID == "" {
		return Tokens{}, errors.New("SSO returned no service ticket")
	}
	return s.exchange(ctx, reply.ServiceTicketID, params.Get("service"))
}
func (s *loginState) exchange(ctx context.Context, ticket, service string) (Tokens, error) {
	h := utls.HelloChrome_Auto
	c := s.client(&h)
	for _, id := range diIDs {
		form := url.Values{"client_id": {id}, "service_ticket": {ticket}, "service_url": {service}, "grant_type": {diGrant}}
		req, err := http.NewRequestWithContext(ctx, "POST", s.ep.DIToken, strings.NewReader(form.Encode()))
		if err != nil {
			return Tokens{}, err
		}
		nativeHeaders(req.Header)
		req.SetBasicAuth(id, "")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json,text/html;q=0.9,*/*;q=0.8")
		req.Header.Set("Cache-Control", "no-cache")
		res, err := c.Do(req)
		if err != nil {
			req.Body = io.NopCloser(strings.NewReader(form.Encode()))
			res, err = s.client(nil).Do(req)
		}
		if err != nil {
			return Tokens{}, err
		}
		b, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return Tokens{}, err
		}
		if res.StatusCode == 429 {
			return Tokens{}, ErrTooManyRequests
		}
		if res.StatusCode >= 400 {
			continue
		}
		var v struct {
			Access  string `json:"access_token"`
			Refresh string `json:"refresh_token"`
		}
		if json.Unmarshal(b, &v) != nil || v.Access == "" {
			continue
		}
		parts := strings.Split(v.Access, ".")
		if len(parts) > 1 {
			if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
				var claims struct {
					Client string `json:"client_id"`
				}
				if json.Unmarshal(payload, &claims) == nil && claims.Client != "" {
					id = claims.Client
				}
			}
		}
		return Tokens{DIToken: v.Access, DIRefreshToken: v.Refresh, DIClientID: id}, nil
	}
	return Tokens{}, errors.New("DI token exchange failed")
}

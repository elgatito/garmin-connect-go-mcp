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

// Constants ported verbatim from the pinned Python reference (client.py).
const (
	iosSSOClientID      = "GCM_IOS_DARK"
	iosServiceURL       = "https://mobile.integration.garmin.com/gcm/ios"
	iosLoginUA          = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148"
	portalSSOClientID   = "GarminConnect"
	portalSSOServiceURL = "https://connect.garmin.com/app"
	desktopUserAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	diTokenURL          = "https://diauth.garmin.com/di-oauth2-service/oauth/token"
	diGrantType         = "https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket"
)

var diClientIDs = []string{
	"GARMIN_CONNECT_MOBILE_ANDROID_DI_2025Q2",
	"GARMIN_CONNECT_MOBILE_ANDROID_DI_2024Q4",
	"GARMIN_CONNECT_MOBILE_ANDROID_DI",
	"GARMIN_CONNECT_MOBILE_IOS_DI",
}

var (
	// ErrInvalidCredentials reports a definite username/password rejection.
	ErrInvalidCredentials = errors.New("invalid username or password")
	// ErrTooManyRequests reports Garmin rate limiting every login strategy.
	ErrTooManyRequests = errors.New("rate limited by Garmin (429)")

	errMFARequired = errors.New("garmin: MFA required")
)

// MFAPrompt asks the user for the MFA code Garmin sent them.
type MFAPrompt func(ctx context.Context) (string, error)

// LoginOptions controls the SSO strategy chain.
type LoginOptions struct {
	Strategies []string
	RootCAs    *x509.CertPool
	Sleep      func(time.Duration)
	Log        io.Writer
}

// loginState carries the session from the strategy that asked for MFA to the
// code verification step.
type loginState struct {
	client     *http.Client
	params     url.Values
	headers    http.Header
	mfaMethod  string
	serviceURL string
	flow       string
	page       string // widget signin response holding the MFA CSRF token
}

// loginStrategies is the ordered chain.
var loginStrategies = []struct {
	name string
	run  func(context.Context, Endpoints, string, string, *LoginOptions, *loginState) (Tokens, error)
}{
	{"mobile+utls", mobileUTLSLogin},
	{"mobile+plain", mobilePlainLogin},
	{"widget+utls", widgetUTLSLogin},
	{"portal+utls", portalUTLSLogin},
	{"portal+plain", portalPlainLogin},
}

const ssoRequestTimeout = 30 * time.Second

// Login tries the selected SSO strategies in order. Credential errors and MFA
// challenges stop the chain; every other error moves on to the next strategy.
func Login(ctx context.Context, ep Endpoints, email, password string, mfa MFAPrompt, opt *LoginOptions) (Tokens, error) {
	o := LoginOptions{}
	if opt != nil {
		o = *opt
	}
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.Log == nil {
		o.Log = io.Discard
	}

	names := o.Strategies
	if names == nil {
		names = make([]string, len(loginStrategies))
		for i, s := range loginStrategies {
			names[i] = s.name
		}
	}

	lastErr := errors.New("no login strategies selected")
	rateLimited := 0
	for _, name := range names {
		run := findLoginStrategy(name)
		if run == nil {
			lastErr = fmt.Errorf("unknown login strategy %q", name)
			continue
		}

		st := &loginState{}
		fmt.Fprintf(o.Log, "login: trying %s\n", name)
		tok, err := run(ctx, ep, email, password, &o, st)
		if err == nil {
			return tok, nil
		}

		switch {
		case errors.Is(err, ErrInvalidCredentials):
			return Tokens{}, err
		case errors.Is(err, errMFARequired):
			if mfa == nil {
				return Tokens{}, errors.New("MFA required but no MFA prompt available")
			}
			code, perr := mfa(ctx)
			if perr != nil {
				return Tokens{}, fmt.Errorf("MFA prompt failed: %w", perr)
			}
			return completeMFA(ctx, ep, code, st, &o)
		case errors.Is(err, ErrTooManyRequests):
			rateLimited++
			lastErr = err
		default:
			lastErr = err
		}
		fmt.Fprintf(o.Log, "login: %s failed: %v\n", name, err)
	}

	if len(names) > 0 && rateLimited == len(names) {
		return Tokens{}, ErrTooManyRequests
	}
	return Tokens{}, fmt.Errorf("all login strategies failed: %w", lastErr)
}

func findLoginStrategy(name string) func(context.Context, Endpoints, string, string, *LoginOptions, *loginState) (Tokens, error) {
	for _, s := range loginStrategies {
		if s.name == name {
			return s.run
		}
	}
	return nil
}

// mobileUTLSLogin rotates iOS-app TLS fingerprints. Each fingerprint lands in
// a separate Cloudflare rate-limit bucket.
func mobileUTLSLogin(ctx context.Context, ep Endpoints, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	if !isHTTPS(ep.SSO) {
		return Tokens{}, errors.New("mobile+utls: SSO endpoint is not https")
	}

	var lastErr error
	for _, hello := range []utls.ClientHelloID{utls.HelloIOS_Auto, utls.HelloSafari_Auto, utls.HelloChrome_120} {
		hc := newUTLSClient(hello, opt.RootCAs, newSSOCookieJar())
		hc.Timeout = ssoRequestTimeout
		tok, err := doMobileLogin(ctx, ep, hc, email, password, opt, st)
		if err == nil {
			return tok, nil
		}
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, errMFARequired) {
			return Tokens{}, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("mobile+utls: no TLS fingerprints available")
	}
	return Tokens{}, lastErr
}

func mobilePlainLogin(ctx context.Context, ep Endpoints, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	hc := newSSOPlainClient(opt.RootCAs, newSSOCookieJar())
	return doMobileLogin(ctx, ep, hc, email, password, opt, st)
}

// doMobileLogin mirrors _do_mobile_login: POST /mobile/api/login with the iOS
// app credentials, then exchange a service ticket or record the MFA session.
func doMobileLogin(ctx context.Context, ep Endpoints, hc *http.Client, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	loginParams := url.Values{
		"clientId": {iosSSOClientID},
		"locale":   {"en-US"},
		"service":  {iosServiceURL},
	}
	loginHeaders := http.Header{}
	loginHeaders.Set("User-Agent", iosLoginUA)
	loginHeaders.Set("Accept", "application/json, text/plain, */*")
	loginHeaders.Set("Content-Type", "application/json")
	loginHeaders.Set("Origin", ep.SSO)

	payload, err := json.Marshal(map[string]any{
		"username":     email,
		"password":     password,
		"rememberMe":   true,
		"captchaToken": "",
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("mobile login: encode request: %w", err)
	}

	loginURL := strings.TrimRight(ep.SSO, "/") + "/mobile/api/login?" + loginParams.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL, bytes.NewReader(payload))
	if err != nil {
		return Tokens{}, fmt.Errorf("mobile login: build request: %w", err)
	}
	req.Header = loginHeaders

	resp, err := hc.Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("mobile login: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Tokens{}, fmt.Errorf("mobile login: read response: %w", err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Tokens{}, ErrTooManyRequests
	}

	var res struct {
		ResponseStatus struct {
			Type string `json:"type"`
		} `json:"responseStatus"`
		ServiceTicketID string `json:"serviceTicketId"`
		CustomerMFAInfo struct {
			MFALastMethodUsed string `json:"mfaLastMethodUsed"`
		} `json:"customerMfaInfo"`
		Error struct {
			StatusCode string `json:"status-code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return Tokens{}, fmt.Errorf("mobile login failed (non-JSON): HTTP %d", resp.StatusCode)
	}

	switch res.ResponseStatus.Type {
	case "MFA_REQUIRED":
		method := res.CustomerMFAInfo.MFALastMethodUsed
		if method == "" {
			method = "email"
		}
		st.client = hc
		st.params = loginParams
		st.headers = loginHeaders
		st.mfaMethod = method
		st.serviceURL = iosServiceURL
		st.flow = "ios"
		return Tokens{}, errMFARequired
	case "SUCCESSFUL":
		if res.ServiceTicketID == "" {
			return Tokens{}, errors.New("mobile login: SUCCESSFUL response without service ticket")
		}
		return exchangeServiceTicket(ctx, ep, res.ServiceTicketID, iosServiceURL, opt)
	case "INVALID_USERNAME_PASSWORD":
		return Tokens{}, ErrInvalidCredentials
	}
	if res.Error.StatusCode == "429" {
		return Tokens{}, ErrTooManyRequests
	}
	return Tokens{}, fmt.Errorf("mobile login failed: %s", body)
}

// completeMFA mirrors _complete_mfa: verify at the flow's endpoint, then retry
// the other path because Garmin rate-limits the two buckets separately.
func completeMFA(ctx context.Context, ep Endpoints, code string, st *loginState, opt *LoginOptions) (Tokens, error) {
	if st == nil || st.client == nil {
		return Tokens{}, errors.New("MFA verification failed: missing login session")
	}
	if st.flow == "widget" {
		return completeWidgetMFA(ctx, ep, code, st, opt)
	}

	base := strings.TrimRight(ep.SSO, "/")
	flowPath := st.flow
	if flowPath == "ios" {
		flowPath = "mobile"
	}
	if flowPath == "" {
		flowPath = "portal"
	}

	type endpoint struct {
		url     string
		params  url.Values
		headers http.Header
	}
	endpoints := []endpoint{{
		url:     base + "/" + flowPath + "/api/mfa/verifyCode",
		params:  st.params,
		headers: st.headers,
	}}
	if flowPath == "mobile" {
		endpoints = append(endpoints, endpoint{
			url: base + "/portal/api/mfa/verifyCode",
			params: url.Values{
				"clientId": {portalSSOClientID},
				"locale":   {"en-US"},
				"service":  {portalService(ep)},
			},
			headers: st.headers,
		})
	} else {
		endpoints = append(endpoints, endpoint{
			url: base + "/mobile/api/mfa/verifyCode",
			params: url.Values{
				"clientId": {iosSSOClientID},
				"locale":   {"en-US"},
				"service":  {iosServiceURL},
			},
			headers: st.headers,
		})
	}

	payload, err := json.Marshal(map[string]any{
		"mfaMethod":           st.mfaMethod,
		"mfaVerificationCode": code,
		"rememberMyBrowser":   true,
		"reconsentList":       []any{},
		"mfaSetup":            false,
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("MFA verification: encode request: %w", err)
	}

	var failures []string
	rateLimited := 0
	for _, e := range endpoints {
		mfaURL := e.url
		if len(e.params) > 0 {
			mfaURL += "?" + e.params.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, mfaURL, bytes.NewReader(payload))
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", e.url, err))
			continue
		}
		req.Header = e.headers.Clone()

		resp, err := st.client.Do(req)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: connection error %v", e.url, err))
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			failures = append(failures, fmt.Sprintf("%s: read error %v", e.url, readErr))
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			failures = append(failures, fmt.Sprintf("%s: HTTP 429", e.url))
			rateLimited++
			continue
		}

		var res struct {
			ResponseStatus struct {
				Type string `json:"type"`
			} `json:"responseStatus"`
			ServiceTicketID string `json:"serviceTicketId"`
			Error           struct {
				StatusCode string `json:"status-code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			failures = append(failures, fmt.Sprintf("%s: HTTP %d non-JSON", e.url, resp.StatusCode))
			continue
		}
		if res.Error.StatusCode == "429" {
			failures = append(failures, fmt.Sprintf("%s: 429 in JSON body", e.url))
			rateLimited++
			continue
		}
		if res.ResponseStatus.Type == "SUCCESSFUL" {
			if res.ServiceTicketID == "" {
				failures = append(failures, fmt.Sprintf("%s: SUCCESSFUL without service ticket", e.url))
				continue
			}
			svcURL := st.serviceURL
			if st.flow == "ios" {
				svcURL = iosServiceURL
			}
			if svcURL == "" {
				svcURL = portalSSOServiceURL
			}
			return exchangeServiceTicket(ctx, ep, res.ServiceTicketID, svcURL, opt)
		}
		failures = append(failures, fmt.Sprintf("%s: %s", e.url, body))
	}

	if rateLimited == len(endpoints) {
		return Tokens{}, ErrTooManyRequests
	}
	return Tokens{}, fmt.Errorf("MFA verification failed: %s", strings.Join(failures, "; "))
}

// exchangeServiceTicket mirrors _exchange_service_ticket: try each DI client ID
// in order, preferring the client_id claim carried by the returned JWT.
func exchangeServiceTicket(ctx context.Context, ep Endpoints, ticket, serviceURL string, opt *LoginOptions) (Tokens, error) {
	tokenURL := ep.DIToken
	if tokenURL == "" {
		tokenURL = diTokenURL
	}
	form := url.Values{
		"service_ticket": {ticket},
		"grant_type":     {diGrantType},
		"service_url":    {serviceURL},
	}
	diUsesTLS := isHTTPS(tokenURL)
	plain := newSSOPlainClient(opt.RootCAs, nil)

	var lastErr error
	for _, clientID := range diClientIDs {
		form.Set("client_id", clientID)

		var status int
		var body []byte
		var err error
		if diUsesTLS {
			utlsClient := newUTLSClient(utls.HelloChrome_Auto, opt.RootCAs, nil)
			utlsClient.Timeout = ssoRequestTimeout
			status, body, err = postDIToken(ctx, utlsClient, tokenURL, clientID, form)
		}
		if !diUsesTLS || err != nil {
			// Fall back to standard TLS on a transport error, mirroring Python's
			// plain requests fallback when curl_cffi cannot complete the POST.
			status, body, err = postDIToken(ctx, plain, tokenURL, clientID, form)
		}
		if err != nil {
			lastErr = fmt.Errorf("DI token exchange for %s: %w", clientID, err)
			fmt.Fprintf(opt.Log, "DI exchange: %s transport error: %v\n", clientID, err)
			continue
		}
		if status == http.StatusTooManyRequests {
			return Tokens{}, ErrTooManyRequests
		}
		if status < 200 || status > 299 {
			lastErr = fmt.Errorf("DI token exchange for %s: HTTP %d", clientID, status)
			fmt.Fprintf(opt.Log, "DI exchange: %s HTTP %d\n", clientID, status)
			continue
		}

		var data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(body, &data); err != nil || data.AccessToken == "" {
			lastErr = fmt.Errorf("DI token exchange for %s: invalid token response", clientID)
			fmt.Fprintf(opt.Log, "DI exchange: %s invalid token response\n", clientID)
			continue
		}
		tok := Tokens{
			DIToken:        data.AccessToken,
			DIRefreshToken: data.RefreshToken,
			DIClientID:     clientID,
		}
		if id := tok.clientID(); id != "" {
			tok.DIClientID = id
		}
		return tok, nil
	}

	if lastErr == nil {
		lastErr = errors.New("DI token exchange failed for all client IDs")
	}
	return Tokens{}, lastErr
}

func postDIToken(ctx context.Context, hc *http.Client, tokenURL, clientID string, form url.Values) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, body, nil
}

func newSSOPlainClient(roots *x509.CertPool, jar http.CookieJar) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if roots != nil {
		tr.TLSClientConfig = &tls.Config{RootCAs: roots}
	}
	return &http.Client{Transport: tr, Jar: jar, Timeout: ssoRequestTimeout}
}

func newSSOCookieJar() http.CookieJar {
	jar, _ := cookiejar.New(nil)
	return jar
}

func isHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https"
}

package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

// Delays ported verbatim from the pinned Python reference (client.py).
const (
	loginDelayMin  = 10 * time.Second
	loginDelayMax  = 20 * time.Second
	widgetDelayMin = 3 * time.Second
	widgetDelayMax = 8 * time.Second
)

// Regexes for HTML parsing (widget flow), same patterns as Python.
var (
	csrfRe   = regexp.MustCompile(`name="_csrf"\s+value="(.+?)"`)
	titleRe  = regexp.MustCompile(`<title>(.+?)</title>`)
	ticketRe = regexp.MustCompile(`embed\?ticket=([^"]+)"`)
)

// widgetUTLSLogin mirrors _widget_web_login: SSO embed form flow over a Chrome
// TLS fingerprint.
func widgetUTLSLogin(ctx context.Context, ep Endpoints, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	if !isHTTPS(ep.SSO) {
		return Tokens{}, errors.New("widget+utls: SSO endpoint is not https")
	}
	hc := newUTLSClient(utls.HelloChrome_Auto, opt.RootCAs, newSSOCookieJar())
	hc.Timeout = ssoRequestTimeout
	return doWidgetLogin(ctx, ep, hc, email, password, opt, st)
}

// doWidgetLogin copies the request sequence of _widget_web_login step by step.
func doWidgetLogin(ctx context.Context, ep Endpoints, hc *http.Client, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	base := strings.TrimRight(ep.SSO, "/")
	ssoBase := base + "/sso"
	embed := ssoBase + "/embed"
	embedParams := url.Values{
		"id":          {"gauth-widget"},
		"embedWidget": {"true"},
		"gauthHost":   {ssoBase},
	}
	signinParams := url.Values{
		"id":                              {"gauth-widget"},
		"embedWidget":                     {"true"},
		"gauthHost":                       {embed},
		"service":                         {embed},
		"source":                          {embed},
		"redirectAfterAccountLoginUrl":    {embed},
		"redirectAfterAccountCreationUrl": {embed},
	}

	// Step 1: GET the embed page to establish session cookies.
	embedHeaders := http.Header{"User-Agent": {desktopUserAgent}}
	resp, err := doSSORequest(ctx, hc, http.MethodGet, embed+"?"+embedParams.Encode(), nil, embedHeaders)
	if err != nil {
		return Tokens{}, fmt.Errorf("widget login: embed: %w", err)
	}
	if resp.status == http.StatusTooManyRequests {
		return Tokens{}, ErrTooManyRequests
	}
	if resp.status < 200 || resp.status > 299 {
		return Tokens{}, fmt.Errorf("widget login: embed returned HTTP %d", resp.status)
	}

	// Step 2: GET the signin page for the CSRF token.
	headers := http.Header{
		"User-Agent": {desktopUserAgent},
		"Referer":    {embed},
	}
	resp, err = doSSORequest(ctx, hc, http.MethodGet, ssoBase+"/signin?"+signinParams.Encode(), nil, headers)
	if err != nil {
		return Tokens{}, fmt.Errorf("widget login: signin: %w", err)
	}
	if resp.status == http.StatusTooManyRequests {
		return Tokens{}, ErrTooManyRequests
	}
	csrf := csrfRe.FindStringSubmatch(resp.body)
	if csrf == nil {
		return Tokens{}, errors.New("widget login: missing CSRF token")
	}

	opt.Sleep(antiWAFDelay(widgetDelayMin, widgetDelayMax))

	// Step 3: POST the credentials.
	form := url.Values{
		"username": {email},
		"password": {password},
		"embed":    {"true"},
		"_csrf":    {csrf[1]},
	}
	headers = http.Header{
		"User-Agent": {desktopUserAgent},
		"Referer":    {resp.finalURL},
	}
	resp, err = doSSORequest(ctx, hc, http.MethodPost, ssoBase+"/signin?"+signinParams.Encode(), form, headers)
	if err != nil {
		return Tokens{}, fmt.Errorf("widget login: signin POST: %w", err)
	}
	if resp.status == http.StatusTooManyRequests {
		return Tokens{}, ErrTooManyRequests
	}

	title := ""
	if m := titleRe.FindStringSubmatch(resp.body); m != nil {
		title = m[1]
	}
	lower := strings.ToLower(title)
	for _, hint := range []string{"bad gateway", "service unavailable", "cloudflare", "502", "503"} {
		if strings.Contains(lower, hint) {
			return Tokens{}, fmt.Errorf("widget login: server error %q", title)
		}
	}
	for _, hint := range []string{"locked", "invalid", "incorrect", "account error"} {
		if strings.Contains(lower, hint) {
			return Tokens{}, ErrInvalidCredentials
		}
	}

	if strings.Contains(title, "MFA") {
		st.client = hc
		st.params = signinParams
		st.headers = http.Header{
			"User-Agent": {desktopUserAgent},
			"Referer":    {resp.finalURL},
		}
		st.mfaMethod = "email"
		st.serviceURL = embed
		st.flow = "widget"
		st.page = resp.body
		return Tokens{}, errMFARequired
	}
	if title != "Success" {
		return Tokens{}, fmt.Errorf("widget login: unexpected title %q", title)
	}

	// Step 4: Extract the service ticket.
	m := ticketRe.FindStringSubmatch(resp.body)
	if m == nil {
		return Tokens{}, errors.New("widget login: missing service ticket")
	}
	return exchangeServiceTicket(ctx, ep, m[1], embed, opt)
}

// completeWidgetMFA mirrors _complete_mfa_widget.
func completeWidgetMFA(ctx context.Context, ep Endpoints, code string, st *loginState, opt *LoginOptions) (Tokens, error) {
	csrf := csrfRe.FindStringSubmatch(st.page)
	if csrf == nil {
		return Tokens{}, errors.New("widget MFA: missing CSRF token")
	}

	form := url.Values{
		"mfa-code": {code},
		"embed":    {"true"},
		"_csrf":    {csrf[1]},
		"fromPage": {"setupEnterMfaCode"},
	}
	verifyURL := strings.TrimRight(ep.SSO, "/") + "/sso/verifyMFA/loginEnterMfaCode"
	if len(st.params) > 0 {
		verifyURL += "?" + st.params.Encode()
	}
	resp, err := doSSORequest(ctx, st.client, http.MethodPost, verifyURL, form, st.headers)
	if err != nil {
		return Tokens{}, fmt.Errorf("widget MFA: %w", err)
	}
	if resp.status == http.StatusTooManyRequests {
		return Tokens{}, ErrTooManyRequests
	}

	title := ""
	if m := titleRe.FindStringSubmatch(resp.body); m != nil {
		title = m[1]
	}
	if title != "Success" {
		return Tokens{}, fmt.Errorf("widget MFA failed: %s", title)
	}
	m := ticketRe.FindStringSubmatch(resp.body)
	if m == nil {
		return Tokens{}, errors.New("widget MFA: missing service ticket")
	}
	return exchangeServiceTicket(ctx, ep, m[1], strings.TrimRight(ep.SSO, "/")+"/sso/embed", opt)
}

// portalUTLSLogin mirrors _portal_web_login_cffi: rotate TLS fingerprints,
// because each one lands in a separate Cloudflare rate-limit bucket.
func portalUTLSLogin(ctx context.Context, ep Endpoints, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	if !isHTTPS(ep.SSO) {
		return Tokens{}, errors.New("portal+utls: SSO endpoint is not https")
	}

	var lastErr error
	for _, hello := range []utls.ClientHelloID{
		utls.HelloSafari_Auto,
		utls.HelloIOS_Auto,
		utls.HelloChrome_120,
		utls.HelloEdge_Auto,
		utls.HelloChrome_Auto,
	} {
		hc := newUTLSClient(hello, opt.RootCAs, newSSOCookieJar())
		hc.Timeout = ssoRequestTimeout
		tok, err := doPortalLogin(ctx, ep, hc, email, password, opt, st)
		if err == nil {
			return tok, nil
		}
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, errMFARequired) {
			return Tokens{}, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("portal+utls: no TLS fingerprints available")
	}
	return Tokens{}, lastErr
}

// portalService returns the portal SSO service URL for the account's domain.
func portalService(ep Endpoints) string {
	if ep.Connect == "" {
		return portalSSOServiceURL
	}
	return strings.TrimRight(ep.Connect, "/") + "/app"
}

// portalPlainLogin mirrors _portal_web_login_requests.
func portalPlainLogin(ctx context.Context, ep Endpoints, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	hc := newSSOPlainClient(opt.RootCAs, newSSOCookieJar())
	return doPortalLogin(ctx, ep, hc, email, password, opt, st)
}

// doPortalLogin copies the request sequence of _do_portal_web_login step by step.
func doPortalLogin(ctx context.Context, ep Endpoints, hc *http.Client, email, password string, opt *LoginOptions, st *loginState) (Tokens, error) {
	base := strings.TrimRight(ep.SSO, "/")
	service := portalService(ep)
	signinURL := base + "/portal/sso/en-US/sign-in"

	// Step 1: GET the signin page to grab initial cookies.
	getURL := signinURL + "?" + url.Values{
		"clientId": {portalSSOClientID},
		"service":  {service},
	}.Encode()
	headers := http.Header{
		"User-Agent":      {desktopUserAgent},
		"Accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
		"Accept-Language": {"en-US,en;q=0.9"},
	}
	resp, err := doSSORequest(ctx, hc, http.MethodGet, getURL, nil, headers)
	if err != nil {
		return Tokens{}, fmt.Errorf("portal login: signin GET: %w", err)
	}
	if resp.status == http.StatusTooManyRequests {
		return Tokens{}, ErrTooManyRequests
	}

	opt.Sleep(antiWAFDelay(loginDelayMin, loginDelayMax))

	// Step 2: POST the credentials.
	loginParams := url.Values{
		"clientId": {portalSSOClientID},
		"locale":   {"en-US"},
		"service":  {service},
	}
	postHeaders := http.Header{
		"User-Agent":      {desktopUserAgent},
		"Accept":          {"application/json, text/plain, */*"},
		"Accept-Language": {"en-US,en;q=0.9"},
		"Content-Type":    {"application/json"},
		"Origin":          {base},
		"Referer":         {signinURL + "?clientId=" + portalSSOClientID + "&service=" + service},
	}
	payload, err := json.Marshal(map[string]any{
		"username":     email,
		"password":     password,
		"rememberMe":   true,
		"captchaToken": "",
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("portal login: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/portal/api/login?"+loginParams.Encode(), bytes.NewReader(payload))
	if err != nil {
		return Tokens{}, fmt.Errorf("portal login: build request: %w", err)
	}
	req.Header = postHeaders
	resp2, err := hc.Do(req)
	if err != nil {
		return Tokens{}, fmt.Errorf("portal login: POST: %w", err)
	}
	body, readErr := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if readErr != nil {
		return Tokens{}, fmt.Errorf("portal login: read response: %w", readErr)
	}
	if resp2.StatusCode == http.StatusTooManyRequests {
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
		return Tokens{}, fmt.Errorf("portal login failed (non-JSON): HTTP %d", resp2.StatusCode)
	}

	switch res.ResponseStatus.Type {
	case "MFA_REQUIRED":
		method := res.CustomerMFAInfo.MFALastMethodUsed
		if method == "" {
			method = "email"
		}
		st.client = hc
		st.params = loginParams
		st.headers = postHeaders
		st.mfaMethod = method
		st.serviceURL = service
		st.flow = "portal"
		return Tokens{}, errMFARequired
	case "SUCCESSFUL":
		if res.ServiceTicketID == "" {
			return Tokens{}, errors.New("portal login: SUCCESSFUL response without service ticket")
		}
		return exchangeServiceTicket(ctx, ep, res.ServiceTicketID, service, opt)
	case "INVALID_USERNAME_PASSWORD":
		return Tokens{}, ErrInvalidCredentials
	}
	if res.Error.StatusCode == "429" {
		return Tokens{}, ErrTooManyRequests
	}
	return Tokens{}, fmt.Errorf("portal web login failed: %s", body)
}

// ssoResponse is the readable part of an SSO HTTP response.
type ssoResponse struct {
	status   int
	body     string
	finalURL string
}

// doSSORequest performs one login request; form is sent url-encoded when set.
func doSSORequest(ctx context.Context, hc *http.Client, method, rawURL string, form url.Values, headers http.Header) (ssoResponse, error) {
	var reader io.Reader
	if form != nil {
		reader = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return ssoResponse{}, err
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = http.Header{}
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return ssoResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ssoResponse{}, err
	}
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return ssoResponse{status: resp.StatusCode, body: string(body), finalURL: finalURL}, nil
}

// antiWAFDelay mimics random.uniform(min, max).
func antiWAFDelay(min, max time.Duration) time.Duration {
	return min + time.Duration(rand.Float64()*float64(max-min))
}

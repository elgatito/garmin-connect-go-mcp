package garmin

import (
	"context"
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

// Copied verbatim from python-garminconnect 0.3.2 client.py.
const (
	desktopUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	loginDelayMin    = 10 * time.Second
	loginDelayMax    = 20 * time.Second
	widgetDelayMin   = 3 * time.Second
	widgetDelayMax   = 8 * time.Second
)

var (
	csrfRE   = regexp.MustCompile(`name="_csrf"\s+value="(.+?)"`)
	titleRE  = regexp.MustCompile(`<title>(.+?)</title>`)
	ticketRE = regexp.MustCompile(`embed\?ticket=([^"]+)"`)
)

// Go equivalents of PORTAL_IMPERSONATIONS ("safari", "safari_ios", "chrome120", "edge101", "chrome").
var portalHellos = []utls.ClientHelloID{utls.HelloSafari_Auto, utls.HelloIOS_Auto, utls.HelloChrome_120, utls.HelloEdge_Auto, utls.HelloChrome_Auto}

func uniform(lo, hi time.Duration) time.Duration { return lo + rand.N(hi-lo) }

func (s *ssoLogin) portalServiceURL() string { return s.ep.Connect + "/app" }

func (s *ssoLogin) portalLoginParams() url.Values {
	return url.Values{"clientId": {portalSSOClientID}, "locale": {"en-US"}, "service": {s.portalServiceURL()}}
}

// webDo sends one browser request (desktop User-Agent plus hdr; form, if set, as the
// urlencoded body) and returns the status, the body text and the final URL after redirects.
func webDo(ctx context.Context, hc *http.Client, method, u string, q url.Values, hdr http.Header, form url.Values) (int, string, string, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, u+"?"+q.Encode(), body)
	if err != nil {
		return 0, "", "", err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", desktopUserAgent)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Request.URL.String(), err
}

func pageTitle(html string) string {
	if m := titleRE.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	return ""
}

// widgetLogin ports _widget_web_login: the SSO embed widget HTML form flow.
func (s *ssoLogin) widgetLogin(ctx context.Context, hc *http.Client) (Tokens, error) {
	ssoBase := s.ep.SSO + "/sso"
	ssoEmbed := ssoBase + "/embed"
	embedParams := url.Values{"id": {"gauth-widget"}, "embedWidget": {"true"}, "gauthHost": {ssoBase}}
	signinParams := url.Values{
		"id": {"gauth-widget"}, "embedWidget": {"true"}, "gauthHost": {ssoEmbed},
		"service": {ssoEmbed}, "source": {ssoEmbed},
		"redirectAfterAccountLoginUrl": {ssoEmbed}, "redirectAfterAccountCreationUrl": {ssoEmbed},
	}

	// Step 1: embed page for the session cookies.
	code, _, _, err := webDo(ctx, hc, http.MethodGet, ssoEmbed, embedParams, nil, nil)
	switch {
	case err != nil:
		return Tokens{}, fmt.Errorf("widget embed: %w", err)
	case code == http.StatusTooManyRequests:
		return Tokens{}, fmt.Errorf("widget embed GET: %w", ErrTooManyRequests)
	case code >= 400:
		return Tokens{}, fmt.Errorf("widget embed returned %d", code)
	}

	// Step 2: sign-in page for the CSRF token.
	code, page, pageURL, err := webDo(ctx, hc, http.MethodGet, ssoBase+"/signin", signinParams, http.Header{"Referer": {ssoEmbed}}, nil)
	switch {
	case err != nil:
		return Tokens{}, fmt.Errorf("widget signin: %w", err)
	case code == http.StatusTooManyRequests:
		return Tokens{}, fmt.Errorf("widget signin GET: %w", ErrTooManyRequests)
	}
	csrf := csrfRE.FindStringSubmatch(page)
	if csrf == nil {
		return Tokens{}, errors.New("widget login: missing CSRF token")
	}

	s.opt.Sleep(uniform(widgetDelayMin, widgetDelayMax)) // anti-WAF delay between GET and POST

	// Step 3: post the credentials.
	code, page, pageURL, err = webDo(ctx, hc, http.MethodPost, ssoBase+"/signin", signinParams, http.Header{"Referer": {pageURL}}, url.Values{
		"username": {s.email}, "password": {s.password}, "embed": {"true"}, "_csrf": {csrf[1]},
	})
	switch {
	case err != nil:
		return Tokens{}, fmt.Errorf("widget signin POST: %w", err)
	case code == http.StatusTooManyRequests:
		return Tokens{}, fmt.Errorf("widget signin POST: %w", ErrTooManyRequests)
	}
	title := pageTitle(page)
	lower := strings.ToLower(title)
	for _, hint := range []string{"bad gateway", "service unavailable", "cloudflare", "502", "503"} {
		if strings.Contains(lower, hint) {
			return Tokens{}, fmt.Errorf("widget login: server error %q", title)
		}
	}
	for _, hint := range []string{"locked", "invalid", "incorrect", "account error"} {
		if strings.Contains(lower, hint) {
			return Tokens{}, fmt.Errorf("widget authentication failed (%q): %w", title, ErrInvalidCredentials)
		}
	}
	if strings.Contains(title, "MFA") {
		mfaCode, err := s.promptMFA(ctx)
		if err != nil {
			return Tokens{}, err
		}
		return s.completeMFAWidget(ctx, hc, signinParams, pageURL, page, mfaCode)
	}
	if title != "Success" {
		return Tokens{}, fmt.Errorf("widget login: unexpected title %q", title)
	}

	// Step 4: the service ticket.
	ticket := ticketRE.FindStringSubmatch(page)
	if ticket == nil {
		return Tokens{}, errors.New("widget login: missing service ticket")
	}
	return s.exchangeServiceTicket(ctx, ticket[1], ssoEmbed)
}

// completeMFAWidget ports _complete_mfa_widget; page and referer are those of the MFA page.
func (s *ssoLogin) completeMFAWidget(ctx context.Context, hc *http.Client, signinParams url.Values, referer, page, mfaCode string) (Tokens, error) {
	csrf := csrfRE.FindStringSubmatch(page)
	if csrf == nil {
		return Tokens{}, errors.New("widget MFA: missing CSRF token")
	}
	code, page, _, err := webDo(ctx, hc, http.MethodPost, s.ep.SSO+"/sso/verifyMFA/loginEnterMfaCode", signinParams, http.Header{"Referer": {referer}}, url.Values{
		"mfa-code": {mfaCode}, "embed": {"true"}, "_csrf": {csrf[1]}, "fromPage": {"setupEnterMfaCode"},
	})
	switch {
	case err != nil:
		return Tokens{}, fmt.Errorf("widget MFA verify: %w", err)
	case code == http.StatusTooManyRequests:
		return Tokens{}, fmt.Errorf("widget MFA verify: %w", ErrTooManyRequests)
	}
	if title := pageTitle(page); title != "Success" {
		return Tokens{}, fmt.Errorf("widget MFA failed: %q", title)
	}
	ticket := ticketRE.FindStringSubmatch(page)
	if ticket == nil {
		return Tokens{}, errors.New("widget MFA: missing service ticket")
	}
	return s.exchangeServiceTicket(ctx, ticket[1], s.ep.SSO+"/sso/embed")
}

// portalLogin ports _do_portal_web_login: sign-in page, anti-WAF delay, then /portal/api/login.
func (s *ssoLogin) portalLogin(ctx context.Context, hc *http.Client) (Tokens, error) {
	signinURL := s.ep.SSO + "/portal/sso/en-US/sign-in"
	code, _, _, err := webDo(ctx, hc, http.MethodGet, signinURL,
		url.Values{"clientId": {portalSSOClientID}, "service": {s.portalServiceURL()}},
		http.Header{
			"Accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
			"Accept-Language": {"en-US,en;q=0.9"},
		}, nil)
	switch {
	case err != nil:
		return Tokens{}, fmt.Errorf("portal sign-in page: %w", err)
	case code == http.StatusTooManyRequests:
		return Tokens{}, fmt.Errorf("portal login GET: %w", ErrTooManyRequests)
	}

	s.opt.Sleep(uniform(loginDelayMin, loginDelayMax)) // mimics a browser's "read then type"

	return s.apiLogin(ctx, hc, "portal", s.portalLoginParams(), http.Header{
		"User-Agent":      {desktopUserAgent},
		"Accept":          {"application/json, text/plain, */*"},
		"Accept-Language": {"en-US,en;q=0.9"},
		"Content-Type":    {"application/json"},
		"Origin":          {s.ep.SSO},
		"Referer":         {signinURL + "?clientId=" + portalSSOClientID + "&service=" + s.portalServiceURL()},
	})
}

package garmin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var csrfPattern = regexp.MustCompile(`name="_csrf"\s+value="(.+?)"`)
var titlePattern = regexp.MustCompile(`<title>(.+?)</title>`)
var ticketPattern = regexp.MustCompile(`embed\?ticket=([^"]+)"`)

func match(re *regexp.Regexp, b []byte) string {
	m := re.FindSubmatch(b)
	if len(m) < 2 {
		return ""
	}
	return string(m[1])
}
func (s *loginState) widget(ctx context.Context, c *http.Client) (Tokens, error) {
	base := strings.TrimRight(s.ep.SSO, "/") + "/sso"
	embed := base + "/embed"
	params := url.Values{"id": {"gauth-widget"}, "embedWidget": {"true"}, "gauthHost": {base}}
	h := http.Header{"User-Agent": {desktopAgent}}
	if _, err := s.request(ctx, c, "GET", "/sso/embed", params, nil, h); err != nil {
		return Tokens{}, err
	}
	params.Set("gauthHost", embed)
	for _, k := range []string{"service", "source", "redirectAfterAccountLoginUrl", "redirectAfterAccountCreationUrl"} {
		params.Set(k, embed)
	}
	h.Set("Referer", embed)
	b, err := s.request(ctx, c, "GET", "/sso/signin", params, nil, h)
	if err != nil {
		return Tokens{}, err
	}
	csrf := match(csrfPattern, b)
	if csrf == "" {
		return Tokens{}, errors.New("widget login: missing CSRF token")
	}
	s.opt.Sleep(5 * time.Second)
	h.Set("Content-Type", "application/x-www-form-urlencoded")
	form := url.Values{"username": {s.email}, "password": {s.password}, "embed": {"true"}, "_csrf": {csrf}}
	b, err = s.request(ctx, c, "POST", "/sso/signin", params, strings.NewReader(form.Encode()), h)
	if err != nil {
		return Tokens{}, err
	}
	title := match(titlePattern, b)
	if strings.Contains(title, "MFA") {
		if s.mfa == nil {
			return Tokens{}, errMFA
		}
		code, err := s.mfa(ctx)
		if err != nil {
			return Tokens{}, fmt.Errorf("%w: %v", errMFA, err)
		}
		csrf = match(csrfPattern, b)
		if csrf == "" {
			return Tokens{}, fmt.Errorf("%w: missing CSRF token", errMFA)
		}
		form = url.Values{"mfa-code": {code}, "embed": {"true"}, "_csrf": {csrf}, "fromPage": {"setupEnterMfaCode"}}
		b, err = s.request(ctx, c, "POST", "/sso/verifyMFA/loginEnterMfaCode", params, strings.NewReader(form.Encode()), h)
		if err != nil {
			return Tokens{}, fmt.Errorf("%w: verification failed", errMFA)
		}
		title = match(titlePattern, b)
		if title != "Success" {
			return Tokens{}, fmt.Errorf("%w: verification failed", errMFA)
		}
	}
	lower := strings.ToLower(title)
	for _, v := range []string{"locked", "invalid", "incorrect", "account error"} {
		if strings.Contains(lower, v) {
			return Tokens{}, ErrInvalidCredentials
		}
	}
	if title != "Success" {
		return Tokens{}, errors.New("widget login failed")
	}
	ticket := match(ticketPattern, b)
	if ticket == "" {
		return Tokens{}, errors.New("widget login: missing service ticket")
	}
	return s.exchange(ctx, ticket, embed)
}

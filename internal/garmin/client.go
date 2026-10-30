package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Endpoints struct{ SSO, Connect, ConnectAPI, DIToken string }

func DefaultEndpoints(domain string) Endpoints {
	return Endpoints{"https://sso." + domain, "https://connect." + domain, "https://connectapi." + domain, "https://diauth.garmin.com/di-oauth2-service/oauth/token"}
}

type TokenStore interface {
	Load() (Tokens, error)
	Save(Tokens) error
}
type Client struct {
	ep        Endpoints
	store     TokenStore
	http      *http.Client
	mu        sync.Mutex
	tokens    Tokens
	profileMu sync.Mutex
	profile   *Profile
}

func NewClient(ep Endpoints, ts TokenStore, hc *http.Client) (*Client, error) {
	t, err := ts.Load()
	if err != nil {
		return nil, err
	}
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{ep: ep, store: ts, http: hc, tokens: t}, nil
}

type Req struct {
	Method, Path string
	Query        url.Values
	JSON         any
	Body         []byte
	ContentType  string
	Header       http.Header
	Timeout      time.Duration
}
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("API Error %d", e.Status)
	}
	return fmt.Sprintf("API Error %d - %s", e.Status, e.Message)
}

var ErrSessionExpired = errors.New("garmin session expired")

func nativeHeaders(h http.Header) {
	h.Set("User-Agent", "GCM-Android-5.23")
	h.Set("X-Garmin-User-Agent", "com.garmin.android.apps.connectmobile/5.23; ; Google/sdk_gphone64_arm64/google; Android/33; Dalvik/2.1.0")
	h.Set("X-Garmin-Paired-App-Version", "10861")
	h.Set("X-Garmin-Client-Platform", "Android")
	h.Set("X-App-Ver", "10861")
	h.Set("X-Lang", "en")
	h.Set("X-GCExperience", "GC5")
	h.Set("Accept-Language", "en-US,en;q=0.9")
}
func (c *Client) token() Tokens { c.mu.Lock(); defer c.mu.Unlock(); return c.tokens }
func (c *Client) refresh(ctx context.Context, previous string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tokens.DIToken != previous && !c.tokens.ExpiresSoon(time.Now()) {
		return nil
	}
	stored, err := c.store.Load()
	if err != nil {
		return err
	}
	if stored.DIToken != previous && !stored.ExpiresSoon(time.Now()) {
		c.tokens = stored
		return nil
	}
	if stored.DIRefreshToken == "" || stored.DIClientID == "" {
		return ErrSessionExpired
	}
	form := url.Values{"grant_type": {"refresh_token"}, "client_id": {stored.DIClientID}, "refresh_token": {stored.DIRefreshToken}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ep.DIToken, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	nativeHeaders(req.Header)
	req.SetBasicAuth(stored.DIClientID, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return ErrSessionExpired
	}
	if resp.StatusCode >= 400 {
		return &APIError{Status: resp.StatusCode}
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.AccessToken == "" {
		return errors.New("refresh response has no access token")
	}
	stored.DIToken = result.AccessToken
	if result.RefreshToken != "" {
		stored.DIRefreshToken = result.RefreshToken
	}
	c.tokens = stored
	if err := c.store.Save(stored); err != nil {
		fmt.Fprintf(os.Stderr, "garmin: saving refreshed tokens failed: %v\n", err)
	}
	return nil
}
func (c *Client) Do(ctx context.Context, r Req) ([]byte, error) {
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	body := r.Body
	if r.JSON != nil {
		var err error
		body, err = json.Marshal(r.JSON)
		if err != nil {
			return nil, err
		}
		if r.ContentType == "" {
			r.ContentType = "application/json"
		}
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	u := strings.TrimRight(c.ep.ConnectAPI, "/") + "/" + strings.TrimLeft(r.Path, "/")
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}
	token := c.token()
	if token.ExpiresSoon(time.Now()) {
		if err := c.refresh(ctx, token.DIToken); err != nil {
			return nil, err
		}
		token = c.token()
	}
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		nativeHeaders(req.Header)
		req.Header.Set("Authorization", "Bearer "+token.DIToken)
		req.Header.Set("Accept", "application/json")
		if r.ContentType != "" {
			req.Header.Set("Content-Type", r.ContentType)
		}
		for k, values := range r.Header {
			req.Header[k] = append([]string(nil), values...)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == 401 && attempt == 0 {
			if err = c.refresh(ctx, token.DIToken); err != nil {
				return nil, err
			}
			token = c.token()
			continue
		}
		if resp.StatusCode == 204 {
			return []byte("{}"), nil
		}
		if resp.StatusCode >= 400 {
			return nil, apiError(resp.StatusCode, data)
		}
		return data, nil
	}
	return nil, errors.New("request failed")
}
func apiError(status int, data []byte) error {
	e := &APIError{Status: status}
	var v any
	if json.Unmarshal(data, &v) != nil {
		if len(data) < 500 {
			e.Message = string(data)
		}
		return e
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"message", "content"} {
			if value, ok := m[k]; ok && value != nil && value != "" {
				e.Message = fmt.Sprint(value)
				return e
			}
		}
		if d, ok := m["detailedImportResult"].(map[string]any); ok {
			if f, ok := d["failures"].([]any); ok && len(f) > 0 {
				if first, ok := f[0].(map[string]any); ok {
					if messages, ok := first["messages"].([]any); ok && len(messages) > 0 {
						e.Message = fmt.Sprint(messages[0])
						return e
					}
				}
			}
		}
		e.Message = fmt.Sprint(m)
	}
	return e
}
func (c *Client) DoJSON(ctx context.Context, r Req) (any, error) {
	b, err := c.Do(ctx, r)
	if err != nil || len(b) == 0 {
		return nil, err
	}
	var v any
	err = json.Unmarshal(b, &v)
	return v, err
}
func (c *Client) GetJSON(ctx context.Context, path string, q url.Values) (any, error) {
	return c.DoJSON(ctx, Req{Path: path, Query: q})
}
func (c *Client) Download(ctx context.Context, path string, q url.Values) ([]byte, error) {
	return c.Do(ctx, Req{Path: path, Query: q, Header: http.Header{"Accept": []string{"*/*"}}})
}
func (c *Client) Upload(ctx context.Context, path, field, filename string, data []byte) (any, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	f, err := w.CreateFormFile(field, filename)
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(data); err != nil {
		return nil, err
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: path, Body: b.Bytes(), ContentType: w.FormDataContentType()})
}
func (c *Client) Domain() string {
	u, err := url.Parse(c.ep.Connect)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "connect.")
}

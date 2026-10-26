// Package server registers the Garmin Connect MCP tools and serves them over
// an MCP transport.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultCallTimeout = 90 * time.Second

// Options configures the Garmin MCP server.
type Options struct {
	Store     accounts.Store
	Endpoints func(domain string) garmin.Endpoints
	Getenv    func(string) string
	Version   string
	Log       io.Writer
}

// Account is embedded first by every account-scoped tool input.
type Account struct {
	Email string `json:"email" jsonschema:"Email of the Garmin Connect account to use. Call list_accounts to see configured accounts."`
}

func (a Account) account() string { return a.Email }

// Session is one account's client for the duration of a tool call.
type Session struct {
	Email  string
	Client *garmin.Client
	Store  accounts.Store

	log io.Writer
}

// Settings returns the account's stored settings.
func (s *Session) Settings() (accounts.Settings, error) {
	return s.Store.LoadSettings(s.Email)
}

func (s *Session) logWriter() io.Writer {
	if s.log == nil {
		return io.Discard
	}
	return s.log
}

// New builds the MCP server with the registered tools and resources.
func New(opt Options) (*mcp.Server, error) {
	getenv := opt.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	log := opt.Log
	if log == nil {
		log = os.Stderr
	}
	profile, err := resolveToolProfile(getenv("GARMIN_TOOL_PROFILE"))
	if err != nil {
		return nil, err
	}
	enabled, disabled, err := resolveToolFilters(getenv)
	if err != nil {
		return nil, err
	}
	endpoints := opt.Endpoints
	if endpoints == nil {
		endpoints = garmin.DefaultEndpoints
	}
	mgr := &manager{
		store:     opt.Store,
		endpoints: endpoints,
		clients:   map[string]*garmin.Client{},
	}
	version := opt.Version
	if version == "" {
		version = "dev"
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "garmin-mcp", Version: version}, nil)
	r := &registry{
		srv:         srv,
		mgr:         mgr,
		log:         log,
		callTimeout: resolveCallTimeout(getenv, log),
		enabled:     enabled,
		disabled:    disabled,
		profile:     profile,
		seen:        map[string]bool{},
		unannotated: map[string]bool{},
	}

	registerUserProfileTools(r)
	registerHealthTools(r)
	registerActivityTools(r)
	registerGearTools(r)
	registerTrainingTools(r)
	registerWorkoutTools(r)
	registerCalendarTools(r)
	registerWorkoutBuilderTools(r)
	registerNutritionTools(r)
	registerChallengeTools(r)
	registerDeviceTools(r)
	registerWeightTools(r)
	registerDataManagementTools(r)
	registerWomensHealthTools(r)
	registerCourseTools(r)
	registerActivityAnalysisTools(r)
	addPlainTool(r, "list_accounts", "List the configured Garmin Connect accounts.", r.listAccounts)
	registerWorkoutResources(srv)
	r.logFilterWarnings()
	return srv, nil
}

// addTool registers an account-scoped tool. It normalizes the email, resolves
// the account session, bounds the call, and renders the result.
func addTool[In interface{ account() string }](r *registry, name, description string, h func(context.Context, *Session, In) (any, error)) {
	if !r.register(name) {
		return
	}
	mcp.AddTool(r.srv, &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: annotationsFor(name),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		email, err := accounts.Normalize(in.account())
		if err != nil {
			return nil, nil, err
		}
		s, err := r.mgr.session(email)
		if err != nil {
			return nil, nil, err
		}
		s.log = r.log

		callCtx := ctx
		if r.callTimeout > 0 {
			var cancel context.CancelFunc
			callCtx, cancel = context.WithTimeout(ctx, r.callTimeout)
			defer cancel()
		}
		v, err := h(callCtx, s, in)
		if err != nil {
			// A write failure wins over the bare deadline message: it carries
			// write_committed, which a retry decision needs.
			var wf *writeFailure
			if errors.As(err, &wf) {
				text, rerr := render(wf.payload)
				if rerr != nil {
					return nil, nil, rerr
				}
				return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: text}},
					IsError: true,
				}, nil, nil
			}
		}
		// A handler that produced a result (such as a batch write summary)
		// keeps it even if the call deadline fired while it was finishing;
		// replacing it with the generic timeout text would hide which writes
		// committed.
		if v != nil {
			text, rerr := render(v)
			if rerr != nil {
				return nil, nil, rerr
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
		}
		if callCtx.Err() == context.DeadlineExceeded {
			return nil, nil, errors.New(timeoutMessage(name, r.callTimeout))
		}
		if err != nil {
			if errors.Is(err, garmin.ErrSessionExpired) {
				return nil, nil, expiredSessionError(email)
			}
			return nil, nil, err
		}
		text, err := render(v)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})
}

// addPlainTool registers a tool that is not scoped to one account.
func addPlainTool[In any](r *registry, name, description string, h func(context.Context, In) (any, error)) {
	if !r.register(name) {
		return
	}
	mcp.AddTool(r.srv, &mcp.Tool{
		Name:        name,
		Description: description,
		Annotations: annotationsFor(name),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		v, err := h(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		text, err := render(v)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})
}

// render returns strings as-is and everything else as indented JSON with HTML
// escaping disabled.
func render(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

func resolveCallTimeout(getenv func(string) string, log io.Writer) time.Duration {
	raw := getenv("GARMIN_MCP_CALL_TIMEOUT")
	if strings.TrimSpace(raw) == "" {
		return defaultCallTimeout
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		fmt.Fprintf(log, "Invalid GARMIN_MCP_CALL_TIMEOUT %q; using default %gs.\n", raw, defaultCallTimeout.Seconds())
		return defaultCallTimeout
	}
	if secs <= 0 || math.IsNaN(secs) {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

func timeoutMessage(name string, d time.Duration) string {
	secs := strconv.FormatFloat(d.Seconds(), 'g', -1, 64)
	return fmt.Sprintf(
		"Garmin request '%s' did not return within %ss and was abandoned. This is usually a transient stall on Garmin's side — please try again. (Adjust with GARMIN_MCP_CALL_TIMEOUT, or set it to 0 to disable the limit.)",
		name, secs,
	)
}

func unknownAccountError(email string, emails []string) error {
	configured := "none"
	if len(emails) > 0 {
		configured = strings.Join(emails, ", ")
	}
	return fmt.Errorf(
		`No Garmin account %q is set up. Ask the user to run "garmin-mcp auth --email %s" in a terminal. Configured accounts: %s.`,
		email, email, configured,
	)
}

func expiredSessionError(email string) error {
	return fmt.Errorf(
		`The Garmin session for %q has expired. Ask the user to run "garmin-mcp auth --email %s --force" in a terminal.`,
		email, email,
	)
}

type manager struct {
	store     accounts.Store
	endpoints func(string) garmin.Endpoints

	mu      sync.Mutex
	clients map[string]*garmin.Client
}

// session returns the cached client for email, creating it once under a mutex.
func (m *manager) session(email string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[email]; ok {
		return &Session{Email: email, Client: c, Store: m.store}, nil
	}

	if _, err := m.store.LoadTokens(email); err != nil {
		if errors.Is(err, accounts.ErrNoAccount) {
			emails, _ := m.store.List()
			return nil, unknownAccountError(email, emails)
		}
		return nil, expiredSessionError(email)
	}
	settings, err := m.store.LoadSettings(email)
	if err != nil {
		return nil, err
	}
	domain := settings.Domain
	if domain == "" {
		domain = "garmin.com"
	}
	adapter := &tokenStore{store: m.store, email: email}
	c, err := garmin.NewClient(m.endpoints(domain), adapter, nil)
	if err != nil {
		return nil, expiredSessionError(email)
	}
	m.clients[email] = c
	return &Session{Email: email, Client: c, Store: m.store}, nil
}

func (m *manager) cachedDisplayName(email string) string {
	m.mu.Lock()
	c := m.clients[email]
	m.mu.Unlock()
	if c == nil {
		return ""
	}
	return c.CachedDisplayName()
}

// tokenStore adapts one account's tokens to garmin.TokenStore.
type tokenStore struct {
	store accounts.Store
	email string
}

func (t *tokenStore) Load() (garmin.Tokens, error) { return t.store.LoadTokens(t.email) }

func (t *tokenStore) Save(tok garmin.Tokens) error {
	return t.store.SaveTokens(t.email, tok)
}

type listAccountsInput struct{}

func (r *registry) listAccounts(_ context.Context, _ listAccountsInput) (any, error) {
	emails, err := r.mgr.store.List()
	if err != nil {
		return nil, err
	}
	accountsOut := make([]any, 0, len(emails))
	for _, email := range emails {
		settings, err := r.mgr.store.LoadSettings(email)
		domain := settings.Domain
		if err != nil || domain == "" {
			domain = "garmin.com"
		}
		accountsOut = append(accountsOut, map[string]any{
			"email":        email,
			"domain":       domain,
			"display_name": r.mgr.cachedDisplayName(email),
			"token_status": tokenStatus(r.mgr.store, email),
		})
	}
	return map[string]any{"accounts": accountsOut}, nil
}

func tokenStatus(st accounts.Store, email string) string {
	tok, err := st.LoadTokens(email)
	if err != nil {
		return "invalid"
	}
	if _, ok := tok.Expiry(); !ok {
		return "invalid"
	}
	if tok.ExpiresSoon(time.Now()) {
		return "expired"
	}
	return "ok"
}

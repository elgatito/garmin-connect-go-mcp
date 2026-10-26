// Package server is the multi-account Garmin Connect MCP server.
package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

type Options struct {
	Store     accounts.Store
	Endpoints func(domain string) garmin.Endpoints // nil: garmin.DefaultEndpoints
	Getenv    func(string) string                  // nil: os.Getenv
	Version   string
	Log       io.Writer // nil: os.Stderr
}

// Session is one tool call's view of an account.
type Session struct {
	Email  string
	Client *garmin.Client
	Store  accounts.Store
	Log    io.Writer // for writeTracker
}

// Settings returns the account's settings.json (defaults when missing).
func (s *Session) Settings() (accounts.Settings, error) { return s.Store.LoadSettings(s.Email) }

// New builds the server. It fails on an unknown GARMIN_TOOL_PROFILE or a malformed GARMIN_ENABLED_TOOLS.
func New(opt Options) (*mcp.Server, error) {
	if opt.Endpoints == nil {
		opt.Endpoints = garmin.DefaultEndpoints
	}
	if opt.Getenv == nil {
		opt.Getenv = os.Getenv
	}
	if opt.Log == nil {
		opt.Log = os.Stderr
	}
	filter, err := newToolFilter(opt.Getenv)
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "garmin-mcp", Version: opt.Version}, nil)
	r := &registry{
		srv:     srv,
		mgr:     &manager{store: opt.Store, endpoints: opt.Endpoints, log: opt.Log, clients: map[string]*garmin.Client{}},
		filter:  filter,
		timeout: resolveCallTimeout(opt.Getenv, opt.Log),
		log:     opt.Log,
	}

	srv.AddReceivingMiddleware(r.stringifiedArgs)

	addPlainTool(r, "list_accounts", "List the configured Garmin Connect accounts. Use an email from this list as the email argument of every other tool.", r.mgr.listAccounts)
	registerUserProfileTools(r)
	registerHealthTools(r)
	registerActivityTools(r)
	registerGearTools(r)
	registerTrainingTools(r)
	registerWorkoutTools(r)
	registerCalendarTools(r)
	registerNutritionTools(r)
	registerWorkoutBuilderTools(r)
	addWorkoutResources(srv)

	if unknown := filter.unknownNames(); len(unknown) > 0 {
		fmt.Fprintf(opt.Log, "Tool filter: warning — name(s) not found and ignored: %s\n", strings.Join(unknown, ", "))
	}
	if len(r.unannotated) > 0 {
		fmt.Fprintf(opt.Log, "Tool annotations: warning — unclassified tool(s): %s\n", strings.Join(r.unannotated, ", "))
	}
	return srv, nil
}

// manager caches one garmin.Client per normalized email.
type manager struct {
	store     accounts.Store
	endpoints func(string) garmin.Endpoints
	log       io.Writer
	mu        sync.Mutex
	clients   map[string]*garmin.Client
}

func expiredSession(email string) error {
	return fmt.Errorf(`The Garmin session for "%s" has expired. Ask the user to run "garmin-mcp auth --email %s --force" in a terminal.`, email, email)
}

// session returns the account's Session; email must already be normalized.
func (m *manager) session(email string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.clients[email]
	if c == nil {
		settings, _ := m.store.LoadSettings(email) // falls back to garmin.com
		var err error
		c, err = garmin.NewClient(m.endpoints(settings.Domain), m.store.TokenStore(email), nil)
		if errors.Is(err, accounts.ErrNoAccount) {
			list, _ := m.store.List()
			names := "none"
			if len(list) > 0 {
				names = strings.Join(list, ", ")
			}
			return nil, fmt.Errorf(`No Garmin account "%s" is set up. Ask the user to run "garmin-mcp auth --email %s" in a terminal. Configured accounts: %s.`, email, email, names)
		}
		if err != nil { // corrupt or incomplete tokens.json
			return nil, expiredSession(email)
		}
		m.clients[email] = c
	}
	return &Session{Email: email, Client: c, Store: m.store, Log: m.log}, nil
}

type accountInfo struct {
	Email       string `json:"email"`
	Domain      string `json:"domain"`
	DisplayName string `json:"display_name"`
	TokenStatus string `json:"token_status"`
}

// listAccounts never calls Garmin: display_name comes only from an already cached profile.
func (m *manager) listAccounts(context.Context, struct{}) (any, error) {
	list, err := m.store.List()
	if err != nil {
		return nil, err
	}
	out := []accountInfo{}
	for _, email := range list {
		settings, _ := m.store.LoadSettings(email)
		info := accountInfo{Email: email, Domain: settings.Domain, TokenStatus: m.store.TokenStatus(email)}
		m.mu.Lock()
		c := m.clients[email]
		m.mu.Unlock()
		if c != nil {
			p, _ := c.CachedProfile()
			info.DisplayName = p.DisplayName
		}
		out = append(out, info)
	}
	return map[string]any{"accounts": out}, nil
}

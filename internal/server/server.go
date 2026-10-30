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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Options struct {
	Store     accounts.Store
	Endpoints func(string) garmin.Endpoints
	Getenv    func(string) string
	Version   string
	Log       io.Writer
}
type tokenStore struct {
	store accounts.Store
	email string
}

func (t tokenStore) Load() (garmin.Tokens, error) { return t.store.LoadTokens(t.email) }
func (t tokenStore) Save(v garmin.Tokens) error {
	if err := t.store.SaveTokens(t.email, v); err != nil {
		dir, _ := t.store.Dir(t.email)
		return fmt.Errorf("account %s (%s): %w", t.email, filepath.Join(dir, "tokens.json"), err)
	}
	return nil
}

type manager struct {
	opt     Options
	mu      sync.Mutex
	clients map[string]*garmin.Client
	filter  toolFilter
}
type Session struct {
	Email  string
	Client *garmin.Client
	Store  accounts.Store
}

func (s *Session) Settings() (accounts.Settings, error) { return s.Store.LoadSettings(s.Email) }
func (m *manager) expired(email string) error {
	return fmt.Errorf("The Garmin session for %q has expired. Ask the user to run \"garmin-mcp auth --email <email> --force\" in a terminal.", email)
}
func (m *manager) session(email string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c := m.clients[email]; c != nil {
		return &Session{email, c, m.opt.Store}, nil
	}
	st, err := m.opt.Store.LoadSettings(email)
	if err != nil {
		return nil, m.expired(email)
	}
	c, err := garmin.NewClient(m.opt.Endpoints(st.Domain), tokenStore{m.opt.Store, email}, nil)
	if errors.Is(err, accounts.ErrNoAccount) {
		names, e := m.opt.Store.List()
		if e != nil {
			return nil, e
		}
		list := strings.Join(names, ", ")
		if list == "" {
			list = "none"
		}
		return nil, fmt.Errorf("No Garmin account %q is set up. Ask the user to run \"garmin-mcp auth --email <email>\" in a terminal. Configured accounts: %s.", email, list)
	}
	if err != nil {
		return nil, m.expired(email)
	}
	m.clients[email] = c
	return &Session{email, c, m.opt.Store}, nil
}
func result(v any, err error) *mcp.CallToolResult {
	var out string
	var failure *writeFailure
	if errors.As(err, &failure) {
		r := result(failure.payload, nil)
		r.IsError = true
		return r
	}
	if err != nil {
		out = err.Error()
	} else if s, ok := v.(string); ok {
		out = s
	} else {
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if e := enc.Encode(v); e != nil {
			return result(nil, e)
		}
		out = strings.TrimSuffix(b.String(), "\n")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}, IsError: err != nil}
}
func New(opt Options) (*mcp.Server, error) {
	if opt.Endpoints == nil {
		opt.Endpoints = garmin.DefaultEndpoints
	}
	if opt.Getenv == nil {
		opt.Getenv = os.Getenv
	}
	if opt.Log == nil {
		opt.Log = io.Discard
	}
	filter, err := resolveFilter(opt.Getenv, opt.Log)
	if err != nil {
		return nil, err
	}
	m := &manager{opt: opt, clients: map[string]*garmin.Client{}, filter: filter}
	srv := mcp.NewServer(&mcp.Implementation{Name: "garmin-mcp", Version: opt.Version}, nil)
	if filter.accepts("list_accounts") {
		srv.AddTool(&mcp.Tool{Name: "list_accounts", Description: "List configured Garmin accounts", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}, Annotations: annotation("list_accounts")}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			names, err := opt.Store.List()
			if err != nil {
				return result(nil, err), nil
			}
			list := make([]map[string]any, 0, len(names))
			for _, name := range names {
				st, e := opt.Store.LoadSettings(name)
				if e != nil {
					return result(nil, e), nil
				}
				token, e := opt.Store.LoadTokens(name)
				state := "invalid"
				if e == nil {
					if exp, ok := token.Expiry(); ok {
						state = "ok"
						if time.Now().After(exp) {
							state = "expired"
						}
					}
				}
				display := ""
				m.mu.Lock()
				c := m.clients[name]
				m.mu.Unlock()
				if c != nil {
					if p, ok := c.CachedProfile(); ok {
						display = p.DisplayName
					}
				}
				list = append(list, map[string]any{"email": name, "domain": st.Domain, "display_name": display, "token_status": state})
			}
			return result(map[string]any{"accounts": list}, nil), nil
		})
	}
	registerResources(srv)
	registerProfile(srv, m)
	registerHeartRateWrite(srv, m)
	registerHealth(srv, m)
	registerSleep(srv, m)
	registerHealthSummaries(srv, m)
	registerStatsRange(srv, m)
	registerBody(srv, m)
	registerBodyCompositionWrite(srv, m)
	registerHealthRanges(srv, m)
	registerWeekly(srv, m)
	registerActivities(srv, m)
	registerActivityDetail(srv, m)
	registerActivityWeather(srv, m)
	registerGearActivity(srv, m)
	registerGear(srv, m)
	registerActivityWrites(srv, m)
	registerActivityDelete(srv, m)
	registerManualActivity(srv, m)
	registerExerciseSets(srv, m)
	registerActivityTypeWrites(srv, m)
	registerSplits(srv, m)
	registerActivityLists(srv, m)
	registerWomensHealth(srv, m)
	registerDevices(srv, m)
	registerAlarms(srv, m)
	registerSolar(srv, m)
	registerTraining(srv, m)
	registerTrainingStatus(srv, m)
	registerEndurance(srv, m)
	registerVO2Trend(srv, m)
	registerEnergyBalance(srv, m)
	registerTrainingEffect(srv, m)
	registerFitnessAge(srv, m)
	registerLactate(srv, m)
	registerProgress(srv, m)
	registerHillScore(srv, m)
	registerAcclimation(srv, m)
	registerToleranceTrend(srv, m)
	registerReadiness(srv, m)
	registerRecovery(srv, m)
	registerRecoveryTrends(srv, m)
	registerWorkouts(srv, m)
	registerWorkoutDetail(srv, m)
	registerWorkoutUploads(srv, m)
	registerAerobicBuilders(srv, m)
	registerStrengthBuilder(srv, m)
	registerWorkoutDelete(srv, m)
	registerWorkoutBatch(srv, m)
	registerWorkoutPush(srv, m)
	registerScheduled(srv, m)
	registerCoach(srv, m)
	registerScheduleWrites(srv, m)
	registerScheduleBatch(srv, m)
	registerScheduleWeek(srv, m)
	registerCalendar(srv, m)
	registerFITSettings(srv, m)
	registerFITMessages(srv, m)
	registerFITAnalysis(srv, m)
	registerFITDownload(srv, m)
	registerPowerCurve(srv, m)
	registerCourses(srv, m)
	registerCourseFile(srv, m)
	registerCourseUpload(srv, m)
	registerNutrition(srv, m)
	registerNutritionWrites(srv, m)
	registerNutritionSettings(srv, m)
	registerNutritionDelete(srv, m)
	registerNutritionUpdate(srv, m)
	registerNutritionUpsert(srv, m)
	registerWeight(srv, m)
	registerWeightWrites(srv, m)
	registerWeightDelete(srv, m)
	registerDataWrites(srv, m)
	registerRace(srv, m)
	registerGoals(srv, m)
	registerChallenges(srv, m)
	registerChallengeMore(srv, m)
	registerPersonalRecords(srv, m)
	return srv, nil
}

const emailDescription = "Email of the Garmin Connect account to use. Call list_accounts to see configured accounts."

type orderedProperties map[string]any

func (p orderedProperties) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(p))
	for key := range p {
		if key != "email" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if _, ok := p["email"]; ok {
		keys = append([]string{"email"}, keys...)
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range keys {
		if i > 0 {
			out.WriteByte(',')
		}
		name, _ := json.Marshal(key)
		value, err := json.Marshal(p[key])
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func addTool(srv *mcp.Server, m *manager, name, description string, extra map[string]any, handler func(context.Context, *Session, map[string]any) (any, error), required ...string) {
	if !m.filter.accepts(name) {
		return
	}
	props := orderedProperties{"email": map[string]any{"type": "string", "description": emailDescription}}
	for k, v := range extra {
		props[k] = v
	}
	schema := map[string]any{"type": "object", "properties": props, "required": append([]string{"email"}, required...)}
	annotation := annotation(name)
	srv.AddTool(&mcp.Tool{Name: name, Description: description, InputSchema: schema, Annotations: annotation}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return result(nil, err), nil
		}
		email, ok := args["email"].(string)
		if !ok {
			return result(nil, errors.New("email is required")), nil
		}
		email, err := accounts.Normalize(email)
		if err != nil {
			return result(nil, err), nil
		}
		session, err := m.session(email)
		if err != nil {
			return result(nil, err), nil
		}
		timeout := 90.0
		if raw := strings.TrimSpace(m.opt.Getenv("GARMIN_MCP_CALL_TIMEOUT")); raw != "" {
			n, parseErr := strconv.ParseFloat(raw, 64)
			if parseErr == nil && !math.IsNaN(n) && !math.IsInf(n, 0) && n <= float64(math.MaxInt64)/float64(time.Second) {
				timeout = n
			} else {
				fmt.Fprintf(m.opt.Log, "Invalid GARMIN_MCP_CALL_TIMEOUT %q; using default 90s.\n", raw)
			}
		}
		parent := ctx
		if timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout*float64(time.Second)))
			defer cancel()
		}
		v, err := handler(ctx, session, args)
		var failure *writeFailure
		if err != nil && ctx.Err() == context.DeadlineExceeded && parent.Err() == nil && !errors.As(err, &failure) {
			err = fmt.Errorf("Garmin request '%s' did not return within %gs and was abandoned. This is usually a transient stall on Garmin's side — please try again. (Adjust with GARMIN_MCP_CALL_TIMEOUT, or set it to 0 to disable the limit.)", name, timeout)
		}
		if errors.Is(err, garmin.ErrSessionExpired) {
			err = m.expired(email)
		}
		return result(v, err), nil
	})
}

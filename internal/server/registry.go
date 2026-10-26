package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/elgatito/garmin-connect-go-mcp/internal/accounts"
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Account is embedded as the first field of every tool input struct; the schema flattens it so email comes first.
type Account struct {
	Email string `json:"email" jsonschema:"Email of the Garmin Connect account to use. Call list_accounts to see configured accounts."`
}

func (a Account) account() string { return a.Email }

type registry struct {
	srv         *mcp.Server
	mgr         *manager
	filter      *toolFilter
	timeout     time.Duration // 0: no limit
	log         io.Writer
	unannotated []string
	argKinds    map[string]map[string]byte // tool -> property -> 'o' (object) or 'a' (array)
}

// addTool registers an account tool: it resolves the email to a Session, applies the call timeout,
// and renders the result (see toolResult). Filtered-out names are skipped.
func addTool[In interface{ account() string }](r *registry, name, description string, h func(ctx context.Context, s *Session, in In) (any, error)) {
	addPlainTool(r, name, description, func(ctx context.Context, in In) (any, error) {
		email, err := accounts.Normalize(in.account())
		if err != nil {
			return nil, err
		}
		s, err := r.mgr.session(email)
		if err != nil {
			return nil, err
		}
		v, err := h(ctx, s, in)
		if errors.Is(err, garmin.ErrSessionExpired) {
			return nil, expiredSession(email)
		}
		return v, err
	})
}

// addPlainTool registers a tool without an account (list_accounts).
func addPlainTool[In any](r *registry, name, description string, h func(ctx context.Context, in In) (any, error)) {
	if !r.filter.allow(name) {
		return
	}
	r.noteArgKinds(name, reflect.TypeFor[In]())
	ann := annotationsFor(name)
	if ann == nil {
		r.unannotated = append(r.unannotated, name)
	}
	mcp.AddTool(r.srv, &mcp.Tool{Name: name, Description: description, Annotations: ann},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (res *mcp.CallToolResult, _ any, _ error) {
			// The SDK doesn't recover; a panicking handler would kill the server for every account.
			defer func() {
				if p := recover(); p != nil {
					msg := fmt.Sprintf("internal error in %s: %v", name, p)
					fmt.Fprintf(r.log, "%s\n%s", msg, debug.Stack())
					res = toolResult(nil, errors.New(msg))
				}
			}()
			if r.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, r.timeout)
				defer cancel()
			}
			v, err := h(ctx, in)
			var wf *writeFailure
			if err != nil && !errors.As(err, &wf) && errors.Is(ctx.Err(), context.DeadlineExceeded) {
				err = fmt.Errorf("Garmin request '%s' did not return within %ss and was abandoned. This is usually a "+
					"transient stall on Garmin's side — please try again. (Adjust with GARMIN_MCP_CALL_TIMEOUT, or set it "+
					"to 0 to disable the limit.)", name, strconv.FormatFloat(r.timeout.Seconds(), 'g', -1, 64))
			}
			return toolResult(v, err), nil, nil
		})
}

// toolResult is the one place results are rendered: a string as-is, anything else as indented JSON,
// a *writeFailure as its JSON payload, and any other error as its text; errors set IsError.
func toolResult(v any, err error) *mcp.CallToolResult {
	var wf *writeFailure
	switch {
	case errors.As(err, &wf):
		v = wf.payload
	case err != nil:
		v = err.Error()
	}
	text, ok := v.(string)
	if !ok {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if e := enc.Encode(v); e != nil {
			text, err = fmt.Sprintf("Error encoding result: %v", e), e
		} else {
			text = strings.TrimSuffix(buf.String(), "\n")
		}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: err != nil}
}

// falsy reports Python falsiness of a decoded JSON value (None, "", 0, False, empty list/dict).
func falsy(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	case int:
		return x == 0
	case int64:
		return x == 0
	case uint64:
		return x == 0
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// toolFilter is the port of _ToolFilter: the profile first, then the allowlist adds and the denylist removes.
// Without a profile, a non-empty allowlist wins over the denylist.
type toolFilter struct {
	profile           map[string]bool // nil: every tool
	enabled, disabled map[string]bool
	seen              map[string]bool
}

func newToolFilter(getenv func(string) string) (*toolFilter, error) {
	profile, err := resolveToolProfile(getenv("GARMIN_TOOL_PROFILE"))
	if err != nil {
		return nil, err
	}
	raw := getenv("GARMIN_ENABLED_TOOLS")
	enabled := parseNames(raw)
	if strings.TrimSpace(raw) != "" && len(enabled) == 0 {
		return nil, errors.New("Invalid GARMIN_ENABLED_TOOLS: expected at least one tool name")
	}
	return &toolFilter{profile: profile, enabled: toSet(enabled), disabled: toSet(parseNames(getenv("GARMIN_DISABLED_TOOLS"))), seen: map[string]bool{}}, nil
}

func (f *toolFilter) allow(name string) bool {
	name = strings.ToLower(name)
	f.seen[name] = true
	if f.profile != nil {
		return (f.profile[name] || f.enabled[name]) && !f.disabled[name]
	}
	if len(f.enabled) > 0 {
		return f.enabled[name]
	}
	return !f.disabled[name]
}

// unknownNames lists configured names that matched no tool (likely typos).
func (f *toolFilter) unknownNames() []string {
	configured := f.disabled
	if f.profile != nil {
		configured = map[string]bool{}
		for n := range f.enabled {
			configured[n] = true
		}
		for n := range f.disabled {
			configured[n] = true
		}
	} else if len(f.enabled) > 0 {
		configured = f.enabled
	}
	var out []string
	for n := range configured {
		if !f.seen[n] {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// resolveCallTimeout reads GARMIN_MCP_CALL_TIMEOUT (seconds, default 90, <=0 disables).
func resolveCallTimeout(getenv func(string) string, log io.Writer) time.Duration {
	const def = 90 * time.Second
	raw := getenv("GARMIN_MCP_CALL_TIMEOUT")
	if strings.TrimSpace(raw) == "" {
		return def
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		fmt.Fprintf(log, "Invalid GARMIN_MCP_CALL_TIMEOUT %q; using default 90.0s.\n", raw)
		return def
	}
	if v <= 0 {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

// noteArgKinds records which properties of a tool's input struct are objects (maps) or arrays (slices).
func (r *registry) noteArgKinds(tool string, t reflect.Type) {
	kinds := map[string]byte{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		if t.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.Anonymous {
				walk(f.Type)
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" {
				name = f.Name
			}
			switch f.Type.Kind() {
			case reflect.Map:
				kinds[name] = 'o'
			case reflect.Slice:
				kinds[name] = 'a'
			}
		}
	}
	walk(t)
	if len(kinds) == 0 {
		return
	}
	if r.argKinds == nil {
		r.argKinds = map[string]map[string]byte{}
	}
	r.argKinds[tool] = kinds
}

// stringifiedArgs is receiving middleware that mirrors FastMCP's pre_parse_json: LLM clients often send nested
// objects and arrays as JSON strings, so before the SDK validates a tools/call, an object/array property that
// arrives as a string holding JSON of the right kind is replaced by the parsed value. Anything else is left
// alone, so normal validation reports it.
func (r *registry) stringifiedArgs(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if cr, ok := req.(*mcp.CallToolRequest); ok && cr.Params != nil {
			cr.Params.Arguments = parseStringifiedArgs(r.argKinds[cr.Params.Name], cr.Params.Arguments)
		}
		return next(ctx, method, req)
	}
}

func parseStringifiedArgs(kinds map[string]byte, args json.RawMessage) json.RawMessage {
	if len(kinds) == 0 || len(args) == 0 {
		return args
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(args, &m) != nil {
		return args
	}
	changed := false
	for name, kind := range kinds {
		raw, ok := m[name]
		var str string
		if !ok || json.Unmarshal(raw, &str) != nil {
			continue
		}
		var parsed any
		if json.Unmarshal([]byte(str), &parsed) != nil {
			continue
		}
		_, isObj := parsed.(map[string]any)
		_, isArr := parsed.([]any)
		if (kind == 'o' && isObj) || (kind == 'a' && isArr) {
			m[name] = json.RawMessage(str)
			changed = true
		}
	}
	if !changed {
		return args
	}
	out, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return out
}

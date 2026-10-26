package server

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registry holds the filter and profile state used while tools register.
type registry struct {
	srv         *mcp.Server
	mgr         *manager
	log         io.Writer
	callTimeout time.Duration

	enabled  map[string]bool
	disabled map[string]bool
	profile  map[string]bool // nil means every tool

	seen        map[string]bool // every name encountered, for typo warnings
	unannotated map[string]bool
}

func (r *registry) allowed(name string) bool {
	if r.profile != nil {
		return (r.profile[name] || r.enabled[name]) && !r.disabled[name]
	}
	if len(r.enabled) > 0 {
		return r.enabled[name]
	}
	return !r.disabled[name]
}

// register reports whether a tool passed the filter, recording the name either
// way so unknown filter entries can be reported.
func (r *registry) register(name string) bool {
	name = strings.ToLower(name)
	r.seen[name] = true
	if !r.allowed(name) {
		return false
	}
	if annotationsFor(name) == nil {
		r.unannotated[name] = true
	}
	return true
}

func (r *registry) unknownFilterNames() []string {
	configured := map[string]bool{}
	if r.profile != nil {
		for name := range r.enabled {
			configured[name] = true
		}
		for name := range r.disabled {
			configured[name] = true
		}
	} else if len(r.enabled) > 0 {
		for name := range r.enabled {
			configured[name] = true
		}
	} else {
		for name := range r.disabled {
			configured[name] = true
		}
	}
	var out []string
	for name := range configured {
		if !r.seen[name] {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func (r *registry) logFilterWarnings() {
	if r.log == nil {
		return
	}
	if unknown := r.unknownFilterNames(); len(unknown) > 0 {
		fmt.Fprintf(r.log, "Tool filter: warning — name(s) not found and ignored: %s\n", strings.Join(unknown, ", "))
	}
	if len(r.unannotated) > 0 {
		names := make([]string, 0, len(r.unannotated))
		for name := range r.unannotated {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(r.log, "Tool annotations: warning — unclassified tool(s): %s\n", strings.Join(names, ", "))
	}
}

func resolveToolFilters(getenv func(string) string) (enabled, disabled map[string]bool, err error) {
	raw := getenv("GARMIN_ENABLED_TOOLS")
	enabled = parseToolSet(raw)
	if raw != "" && strings.TrimSpace(raw) != "" && len(enabled) == 0 {
		return nil, nil, fmt.Errorf("Invalid GARMIN_ENABLED_TOOLS: expected at least one tool name")
	}
	return enabled, parseToolSet(getenv("GARMIN_DISABLED_TOOLS")), nil
}

func parseToolSet(value string) map[string]bool {
	out := map[string]bool{}
	for _, name := range strings.Split(value, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			out[name] = true
		}
	}
	return out
}

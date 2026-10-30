package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"io"
	"sort"
	"strings"
)

//go:embed profiles.json tool_names.json tool_descriptions.json tool_annotations.json templates/*.json
var assets embed.FS

type toolFilter struct {
	profile           map[string]bool
	enabled, disabled map[string]bool
}

func parseNames(s string) map[string]bool {
	result := map[string]bool{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			result[v] = true
		}
	}
	return result
}
func resolveFilter(getenv func(string) string, log io.Writer) (toolFilter, error) {
	var profiles map[string][]string
	b, _ := assets.ReadFile("profiles.json")
	if err := json.Unmarshal(b, &profiles); err != nil {
		return toolFilter{}, err
	}
	raw := strings.TrimSpace(getenv("GARMIN_TOOL_PROFILE"))
	filter := toolFilter{enabled: parseNames(getenv("GARMIN_ENABLED_TOOLS")), disabled: parseNames(getenv("GARMIN_DISABLED_TOOLS"))}
	if raw != "" {
		names := parseNames(raw)
		if len(names) == 0 {
			return filter, fmt.Errorf("Invalid GARMIN_TOOL_PROFILE: expected at least one profile name")
		}
		var unknown []string
		for name := range names {
			if _, ok := profiles[name]; !ok {
				unknown = append(unknown, fmt.Sprintf("'%s'", name))
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			return filter, fmt.Errorf("Unknown GARMIN_TOOL_PROFILE %s. Known profiles: activities, all, health, minimal, nutrition, training, workouts", strings.Join(unknown, ", "))
		}
		if !names["all"] {
			filter.profile = map[string]bool{"list_accounts": true}
			for name := range names {
				for _, tool := range profiles[name] {
					filter.profile[tool] = true
				}
			}
		}
	}
	enabled := getenv("GARMIN_ENABLED_TOOLS")
	if strings.TrimSpace(enabled) != "" && len(filter.enabled) == 0 {
		return filter, fmt.Errorf("Invalid GARMIN_ENABLED_TOOLS: expected at least one tool name")
	}
	var names []string
	b, _ = assets.ReadFile("tool_names.json")
	if err := json.Unmarshal(b, &names); err != nil {
		return filter, err
	}
	known := map[string]bool{}
	for _, name := range names {
		known[name] = true
	}
	for _, set := range []map[string]bool{filter.enabled, filter.disabled} {
		for name := range set {
			if !known[name] {
				fmt.Fprintf(log, "Unknown tool name: %s\n", name)
			}
		}
	}
	return filter, nil
}
func (f toolFilter) accepts(name string) bool {
	if f.profile != nil {
		return (f.profile[name] || f.enabled[name]) && !f.disabled[name]
	}
	if len(f.enabled) > 0 {
		return f.enabled[name]
	}
	return !f.disabled[name]
}

func description(name string) string {
	var docs map[string]string
	b, _ := assets.ReadFile("tool_descriptions.json")
	if err := json.Unmarshal(b, &docs); err != nil {
		panic(err)
	}
	return docs[name]
}

func annotation(name string) *mcp.ToolAnnotations {
	var categories map[string][]string
	b, _ := assets.ReadFile("tool_annotations.json")
	if err := json.Unmarshal(b, &categories); err != nil {
		panic(err)
	}
	in := func(category string) bool {
		for _, value := range categories[category] {
			if value == name {
				return true
			}
		}
		return false
	}
	readonly := name == "list_accounts" || in("READ_ONLY_TOOLS")
	destructive := in("DESTRUCTIVE_TOOLS")
	open := name != "list_accounts" && !in("LOCAL_ONLY_TOOLS")
	return &mcp.ToolAnnotations{ReadOnlyHint: readonly, DestructiveHint: &destructive, IdempotentHint: in("IDEMPOTENT_TOOLS"), OpenWorldHint: &open}
}

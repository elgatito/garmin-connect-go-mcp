package server

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fitPage(messages []fitMessage, size int, selected map[string]bool, records bool, offset, limit int) map[string]any {
	counts := map[string]int{}
	for _, msg := range messages {
		counts[msg.Name]++
	}
	omitted := map[string]any{}
	if selected == nil {
		for name, count := range counts {
			if name != "record" && count > 100 {
				omitted[name] = map[string]any{"count": count, "hint": fmt.Sprintf("Request message_types=['%s'] and follow pagination.", name)}
			}
		}
	}
	eligible := func(name string) bool {
		if name == "record" && (!records || (selected != nil && !selected["record"])) {
			return false
		}
		if selected != nil && !selected[name] {
			return false
		}
		_, skip := omitted[name]
		return !skip
	}
	total := 0
	for name, count := range counts {
		if eligible(name) {
			total += count
		}
	}
	returned := map[string]int{}
	seen := map[string]int{}
	out := []any{}
	index := 0
	for i, msg := range messages {
		typeIndex := seen[msg.Name]
		seen[msg.Name]++
		if !eligible(msg.Name) {
			continue
		}
		if index >= offset && index-offset < limit {
			fields := []any{}
			for _, f := range msg.Fields {
				field := map[string]any{"name": f.Name, "value": f.Value, "definition_number": f.Num, "raw_value": f.Raw}
				if f.Units != "" {
					field["units"] = f.Units
				}
				if f.BaseType != "" {
					field["base_type"] = f.BaseType
				}
				if f.ProfileType != "" {
					field["profile_type"] = f.ProfileType
				}
				if f.FieldType != "" {
					field["field_type"] = f.FieldType
				}
				if f.ValueNames != nil {
					if _, ok := f.ValueNames.([]any); ok {
						field["value_names"] = f.ValueNames
					} else {
						field["value_name"] = f.ValueNames
					}
				}
				fields = append(fields, field)
			}
			out = append(out, map[string]any{"message_index": i, "type_index": typeIndex, "type": msg.Name, "fields": fields, "global_message_number": msg.Num})
			returned[msg.Name]++
		}
		index++
		if index-offset >= limit {
			break
		}
	}
	selectedRecords := selected == nil || selected["record"]
	stream := map[string]any{"included": records && selectedRecords, "total_count": counts["record"], "returned_count": returned["record"]}
	if !records && selectedRecords && counts["record"] > 0 {
		stream["hint"] = "Set include_records=true and use message pagination to retrieve records."
	}
	page := map[string]any{"total_eligible_count": total, "returned_count": len(out), "offset": offset, "limit": limit}
	if offset+len(out) < total {
		page["next_offset"] = offset + len(out)
	}
	result := map[string]any{"fit_size_bytes": size, "message_counts": counts, "returned_message_counts": returned, "record_stream": stream, "pagination": page, "messages": out}
	if len(omitted) > 0 {
		result["omitted_high_frequency_message_types"] = omitted
	}
	return result
}
func registerFITMessages(srv *mcp.Server, m *manager) {
	addTool(srv, m, "get_activity_fit_messages", description("get_activity_fit_messages"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "message_types": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "include_records": map[string]any{"type": "boolean", "default": false}, "message_offset": map[string]any{"type": "integer", "default": 0}, "message_limit": map[string]any{"type": "integer", "default": 1000}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		var id int64
		var err error
		switch v := args["activity_id"].(type) {
		case string:
			id, err = strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		case float64:
			id = int64(v)
			if v != float64(id) {
				err = fmt.Errorf("invalid activity ID")
			}
		default:
			err = fmt.Errorf("invalid activity ID")
		}
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("Error downloading FIT data for activity %v: invalid activity ID", args["activity_id"])
		}
		offset, err := intArg(args, "message_offset", 0)
		if err != nil {
			return nil, err
		}
		limit, err := intArg(args, "message_limit", 1000)
		if err != nil {
			return nil, err
		}
		if offset < 0 {
			return nil, fmt.Errorf("message_offset must be at least 0")
		}
		if limit < 1 || limit > 5000 {
			return nil, fmt.Errorf("message_limit must be between 1 and 5000")
		}
		var selected map[string]bool
		if v, ok := args["message_types"].([]any); ok {
			selected = map[string]bool{}
			for _, item := range v {
				if name, ok := item.(string); ok {
					if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
						selected[name] = true
					}
				}
			}
		}
		data, err := s.Client.Download(ctx, fmt.Sprintf("/download-service/files/activity/%d", id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error downloading FIT data for activity %d: %w", id, err)
		}
		if len(data) == 0 {
			return fmt.Sprintf("No FIT data returned for activity %d", id), nil
		}
		fit, err := extractFIT(data)
		if err == nil {
			var messages []fitMessage
			messages, err = decodeFIT(fit)
			if err == nil {
				records, _ := args["include_records"].(bool)
				out := fitPage(messages, len(fit), selected, records, offset, limit)
				out["activity_id"] = id
				out["source"] = "garmin_original_fit"
				return out, nil
			}
		}
		return map[string]any{"error": err.Error(), "activity_id": id, "debug": map[string]any{"download_size_bytes": len(data), "first_16_bytes_hex": hex.EncodeToString(data[:min(len(data), 16)]), "hint": "1f8b = gzip, 504b = ZIP, 0e10/0c10 = raw FIT, 3c or 7b = HTML/JSON error from Garmin"}}, nil
	}, "activity_id")
}

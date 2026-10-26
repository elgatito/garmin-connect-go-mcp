package server

// Port of activity_analysis.py: get_activity_fit_messages.

import (
	"context"
	"encoding/hex"
	"fmt"
)

const fitDebugHint = "1f8b = gzip, 504b = ZIP, 0e10/0c10 = raw FIT, 3c or 7b = HTML/JSON error from Garmin"

const getActivityFitMessagesDesc = `Retrieve and generically parse an activity's original FIT file.

This is the source-of-truth endpoint for original device activity data.
It returns every message and every field emitted by the FIT decoder
without applying sport-specific curation. Messages remain in file order,
and each field includes its decoded value plus units and definition
number when available. Unknown and duplicate fields are retained.

Garmin Connect edits made after upload may be stored only in Garmin's
service and are not necessarily written back into the original FIT file.
This tool intentionally does not merge those service-side edits.

High-frequency ` + "``record``" + ` messages are counted but omitted by default.
Other message types occurring more than 100 times are also inventoried
but omitted from an unfiltered default response. Request those types
explicitly with message_types. All returned messages are bounded by
message_offset/message_limit (maximum 5000 per call); follow
pagination.next_offset until absent to retrieve the full selected stream.

Use message_types to return only particular FIT message types while
message_counts still inventories the complete file. For example, a
strength workout can usually be inspected efficiently with
["session", "lap", "set", "exercise_title"]. Message type names are
case-insensitive and should use FIT/fitparse snake_case names.`

type fitMessagesIn struct {
	Account
	ActivityID     any      `json:"activity_id" jsonschema:"Garmin activity ID."`
	MessageTypes   []string `json:"message_types,omitempty" jsonschema:"Optional list of FIT message types to return. Omit to return low-frequency non-record message types."`
	IncludeRecords bool     `json:"include_records,omitempty" jsonschema:"Include the high-frequency record stream (default false)."`
	MessageOffset  int      `json:"message_offset,omitempty" jsonschema:"Zero-based offset within the selected message stream."`
	MessageLimit   *int     `json:"message_limit,omitempty" jsonschema:"Maximum messages returned per call, 1-5000 (default 1000)."`
}

func registerActivityAnalysisTools(r *registry) {
	addTool(r, "get_activity_fit_messages", getActivityFitMessagesDesc, getActivityFitMessages)
}

func first16Hex(b []byte) string { return hex.EncodeToString(b[:min(16, len(b))]) }

type fitParseFailure struct {
	Error      string `json:"error"`
	ActivityID int64  `json:"activity_id"`
	Debug      struct {
		DownloadSizeBytes int    `json:"download_size_bytes"`
		First16BytesHex   string `json:"first_16_bytes_hex"`
		Hint              string `json:"hint"`
	} `json:"debug"`
}

func getActivityFitMessages(ctx context.Context, s *Session, in fitMessagesIn) (any, error) {
	id, err := parseActivityID(in.ActivityID)
	if err != nil {
		return nil, fmt.Errorf("Error downloading FIT data for activity %s: %w", pyStr(in.ActivityID), err)
	}
	raw, err := s.Client.DownloadActivity(ctx, id, "ORIGINAL")
	if err != nil {
		return nil, fmt.Errorf("Error downloading FIT data for activity %d: %w", id, err)
	}
	if len(raw) == 0 {
		return fmt.Sprintf("No FIT data returned for activity %d", id), nil
	}
	limit := 1000
	if in.MessageLimit != nil {
		limit = *in.MessageLimit
	}
	parsed, err := parseFitMessages(raw, in.MessageTypes, in.IncludeRecords, in.MessageOffset, limit)
	if err != nil {
		out := fitParseFailure{Error: err.Error(), ActivityID: id}
		out.Debug.DownloadSizeBytes, out.Debug.First16BytesHex, out.Debug.Hint = len(raw), first16Hex(raw), fitDebugHint
		return out, nil
	}
	return struct {
		ActivityID int64  `json:"activity_id"`
		Source     string `json:"source"`
		*fitMessagesResult
	}{id, "garmin_original_fit", parsed}, nil
}

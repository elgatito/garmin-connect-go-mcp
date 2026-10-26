package server

// Port of activity_analysis.py: get_activity_fit_messages, download_activity_file, set_fit_download_dir.

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

const downloadActivityFileDesc = `Download an activity and save it to disk as a file.

Saves the activity in the requested format. Defaults to the original .fit
file; Garmin also supports gpx, tcx, and csv.

Directory resolution (first match wins):
  1. output_dir argument (one-off; not persisted)
  2. GARMIN_FIT_DOWNLOAD_DIR environment variable
  3. persisted config (set via set_fit_download_dir)
If none is configured, returns status "needs_setup" with a suggested
default (the server's current working directory). In that case, ask the
user where to save, call set_fit_download_dir(path), then call this tool
again.

Files are named "{activity_id}.{ext}" and overwrite any existing file.`

// Python stores a global ~/.garminconnect_fit_config.json; this server keeps it per account.
const setFitDownloadDirDesc = `Set and persist the default directory for downloaded activity files.

Stores the absolute path in the account's settings.json (fit_download_dir)
so download_activity_file can save files without asking again.`

type fitMessagesIn struct {
	Account
	ActivityID     any      `json:"activity_id" jsonschema:"Garmin activity ID."`
	MessageTypes   []string `json:"message_types,omitempty" jsonschema:"Optional list of FIT message types to return. Omit to return low-frequency non-record message types."`
	IncludeRecords bool     `json:"include_records,omitempty" jsonschema:"Include the high-frequency record stream (default false)."`
	MessageOffset  int      `json:"message_offset,omitempty" jsonschema:"Zero-based offset within the selected message stream."`
	MessageLimit   *int     `json:"message_limit,omitempty" jsonschema:"Maximum messages returned per call, 1-5000 (default 1000)."`
}

type downloadActivityFileIn struct {
	Account
	ActivityID any     `json:"activity_id" jsonschema:"Garmin activity ID"`
	Format     *string `json:"format,omitempty" jsonschema:"One of fit, gpx, tcx, csv (default fit)"`
	OutputDir  *string `json:"output_dir,omitempty" jsonschema:"Optional one-off directory override (not persisted)"`
}

type setFitDownloadDirIn struct {
	Account
	Path string `json:"path" jsonschema:"Directory where activity files (.fit/.gpx/.tcx/.csv) are saved. Pass the current working directory to keep files where the server runs."`
}

func registerActivityAnalysisTools(r *registry) {
	addTool(r, "get_activity_fit_messages", getActivityFitMessagesDesc, getActivityFitMessages)
	addTool(r, "download_activity_file", downloadActivityFileDesc, downloadActivityFile)
	addTool(r, "set_fit_download_dir", setFitDownloadDirDesc, setFitDownloadDir)
	addTool(r, "get_activity_fit_data", getActivityFitDataDesc, getActivityFitData)
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

// fitBytesFailure is the {error, debug} payload for downloaded bytes that are not a readable FIT file.
type fitBytesFailure struct {
	Error string `json:"error"`
	Debug struct {
		TotalBytes      int    `json:"total_bytes"`
		First16BytesHex string `json:"first_16_bytes_hex"`
		Hint            string `json:"hint"`
	} `json:"debug"`
}

func newFitBytesFailure(err error, raw []byte) fitBytesFailure {
	out := fitBytesFailure{Error: err.Error()}
	out.Debug.TotalBytes, out.Debug.First16BytesHex, out.Debug.Hint = len(raw), first16Hex(raw), fitDebugHint
	return out
}

// fitDownloadFormats maps download_activity_file formats to Garmin.ActivityDownloadFormat, in Python's order.
var fitDownloadFormats = [][2]string{{"fit", "ORIGINAL"}, {"gpx", "GPX"}, {"tcx", "TCX"}, {"csv", "CSV"}}

func settingsPath(s *Session) string {
	dir, err := s.Store.Dir(s.Email)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "settings.json")
}

func downloadActivityFile(ctx context.Context, s *Session, in downloadActivityFileIn) (any, error) {
	format := "fit"
	if in.Format != nil {
		format = *in.Format
	}
	name := strings.ToLower(strings.TrimSpace(format))
	dlFormat := ""
	valid := make([]string, len(fitDownloadFormats))
	for i, f := range fitDownloadFormats {
		valid[i] = f[0]
		if f[0] == name {
			dlFormat = f[1]
		}
	}
	if dlFormat == "" {
		return struct {
			Error        string   `json:"error"`
			ValidFormats []string `json:"valid_formats"`
		}{fmt.Sprintf("Invalid format '%s'.", format), valid}, nil
	}

	outputDir := ""
	if in.OutputDir != nil {
		outputDir = *in.OutputDir
	}
	dir := resolveDownloadDir(s, outputDir)
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("Error downloading activity %s: %w", pyStr(in.ActivityID), err)
		}
		return struct {
			Status           string `json:"status"`
			SuggestedDefault string `json:"suggested_default"`
			ConfigPath       string `json:"config_path"`
			Message          string `json:"message"`
		}{"needs_setup", cwd, settingsPath(s), "No download directory configured. Ask the user where to " +
			"save activity files (offer the current working directory " +
			"as the default), then call set_fit_download_dir(path) " +
			"before downloading."}, nil
	}

	id, err := parseActivityID(in.ActivityID)
	if err != nil {
		return nil, fmt.Errorf("Error downloading activity %s: %w", pyStr(in.ActivityID), err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
	}
	raw, err := s.Client.DownloadActivity(ctx, id, dlFormat)
	if err != nil {
		return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
	}
	if len(raw) == 0 {
		return fmt.Sprintf("No %s data returned for activity %d", name, id), nil
	}
	payload := raw
	if name == "fit" {
		if payload, err = extractFitBytes(raw); err != nil {
			return newFitBytesFailure(err, raw), nil
		}
	}
	// Ported as-is from Python: any directory the caller names is written to, and an existing file is replaced.
	path := filepath.Join(dir, fmt.Sprintf("%d.%s", id, name))
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
	}
	return struct {
		ActivityID int64  `json:"activity_id"`
		Format     string `json:"format"`
		FilePath   string `json:"file_path"`
		SizeBytes  int    `json:"size_bytes"`
		Message    string `json:"message"`
	}{id, name, path, len(payload), "Activity file saved."}, nil
}

func setFitDownloadDir(_ context.Context, s *Session, in setFitDownloadDirIn) (any, error) {
	abs, err := filepath.Abs(expandUser(in.Path))
	if err == nil {
		err = os.MkdirAll(abs, 0o755)
	}
	if err == nil {
		st, _ := s.Settings() // an unreadable settings.json yields defaults, like Python's invalid config ({})
		st.FitDownloadDir = abs
		err = s.Store.SaveSettings(s.Email, st)
	}
	if err != nil {
		return nil, fmt.Errorf("Error setting FIT download directory: %w", err)
	}
	return struct {
		DownloadDir string `json:"download_dir"`
		ConfigPath  string `json:"config_path"`
		Message     string `json:"message"`
	}{abs, settingsPath(s), "Default FIT download directory configured."}, nil
}

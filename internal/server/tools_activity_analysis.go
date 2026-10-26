package server

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// --- tool descriptions (ported from the Python docstrings) ------------------

const getActivityFitMessagesDescription = `Retrieve and generically parse an activity's original FIT file.

This is the source-of-truth endpoint for original device activity data.
It returns every message and every field emitted by the FIT decoder
without applying sport-specific curation. Messages remain in file order,
and each field includes its decoded value plus units and definition
number when available. Unknown and duplicate fields are retained.

Garmin Connect edits made after upload may be stored only in Garmin's
service and are not necessarily written back into the original FIT file.
This tool intentionally does not merge those service-side edits.

High-frequency record messages are counted but omitted by default.
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

const getActivityFitDataDescription = `Download and parse FIT file for an activity to expose advanced cycling data.

Returns data not available through the standard REST API, including:
- DI2 / electronic shifting events with cadence at time of shift, grade at shift,
  gear combinations, shift quality classification, and terrain-grouped shift analysis
- Cycling dynamics per session and lap: platform center offset (PCO), left/right power
  balance, torque effectiveness, pedal smoothness
- Variability Index (NP / avg_power) per session and lap
- Climb detection with VAM (vertical ascent rate), avg power/cadence/HR per climb,
  and W/kg per climb (using the latest Garmin weigh-in on or before the ride,
  up to 90 days back; rider_weight_date says which weigh-in was used)
- Grade-correlated stats: avg power, cadence, HR broken down by terrain steepness
- Grade comes from the device's per-record grade field when present; otherwise
  it is derived from altitude and distance (~100 m window). grade_source
  ("device" | "derived") says which
- HR drift / cardiac drift coefficient (aerobic decoupling for rides >=60 min)
- Temperature correlation: avg HR/power in hottest vs. coolest portions of ride
- Power Duration Curve: best mean maximal power at 5s, 30s, 1min, 5min, 10min, 20min, 60min
- Optional full per-second time series when include_records=True

Multisport files (triathlon, duathlon) contain one session per leg. For
these, the response adds multisport=true and a sessions list with every
leg; session and the record-based analyses (power curve, climbs, grade,
HR drift, temperature) describe one leg only — the cycling leg if there
is exactly one, else the longest — given by analyzed_session_index and
explained in analysis_note. Laps and records still cover all legs.

Shift quality:
- proactive: shifted at 70-100 rpm (ideal cadence range)
- reactive: shifted below 70 rpm (already grinding before shifting)
- coasting: shifted at 0 rpm (mid-stop or freewheeling)
- spun_out: shifted above 100 rpm (waited too long in easy gear)

Note: DI2 data requires Shimano Di2 / SRAM eTap. Cycling dynamics require a
compatible power meter (e.g., Garmin Rally, Favero Assioma, PowerTap P1 pedals).`

const getPowerDurationCurveDescription = `Get season-best Power Duration Curve across recent activities.

Downloads FIT files for recent activities of the requested type and computes best mean maximal
power at each standard duration. Returns season bests with which activity and date
each best came from.

Durations: 5s (sprint), 30s, 1min, 5min (VO2 max proxy), 10min, 20min (FTP proxy), 60min

Use the 20-minute best × 0.95 as a strong FTP estimate without a formal test.

Warning: downloads multiple FIT files — may take 30-60 seconds for 20 activities.`

const downloadActivityFileDescription = `Download an activity and save it to disk as a file.

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

const setFitDownloadDirDescription = `Set and persist the default directory for downloaded activity files.

Stores the absolute path in the account's settings.json so
download_activity_file can save files without asking again.`

// --- inputs ----------------------------------------------------------------

type getActivityFitMessagesInput struct {
	Account
	ActivityID     any      `json:"activity_id" jsonschema:"Garmin activity ID."`
	MessageTypes   []string `json:"message_types,omitempty" jsonschema:"Optional list of FIT message types to return. Omit to return low-frequency non-record message types."`
	IncludeRecords bool     `json:"include_records,omitempty" jsonschema:"Include the high-frequency record stream (default false)."`
	MessageOffset  *int     `json:"message_offset,omitempty" jsonschema:"Zero-based offset within the selected message stream."`
	MessageLimit   *int     `json:"message_limit,omitempty" jsonschema:"Maximum messages returned per call, 1-5000 (default 1000)."`
}

type getActivityFitDataInput struct {
	Account
	ActivityID     any  `json:"activity_id" jsonschema:"Garmin activity ID"`
	IncludeRecords bool `json:"include_records,omitempty" jsonschema:"Include full per-second time series (default False). Warning: adds significant data volume for long rides."`
}

type getPowerDurationCurveInput struct {
	Account
	NumActivities *int   `json:"num_activities,omitempty" jsonschema:"Number of recent activities matching activity_type to request (default 20, max 50)"`
	ActivityType  string `json:"activity_type,omitempty" jsonschema:"Activity type to filter (default \"cycling\")"`
}

type downloadActivityFileInput struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"Garmin activity ID"`
	Format     string `json:"format,omitempty" jsonschema:"One of fit, gpx, tcx, csv (default fit)"`
	OutputDir  string `json:"output_dir,omitempty" jsonschema:"Optional one-off directory override (not persisted)"`
}

type setFitDownloadDirInput struct {
	Account
	Path string `json:"path" jsonschema:"Directory where activity files (.fit/.gpx/.tcx/.csv) are saved. Pass the current working directory to keep files where the server runs."`
}

func registerActivityAnalysisTools(r *registry) {
	addTool(r, "get_activity_fit_messages", getActivityFitMessagesDescription, getActivityFitMessages)
	addTool(r, "get_activity_fit_data", getActivityFitDataDescription, getActivityFitData)
	addTool(r, "get_power_duration_curve", getPowerDurationCurveDescription, getPowerDurationCurve)
	addTool(r, "download_activity_file", downloadActivityFileDescription, downloadActivityFile)
	addTool(r, "set_fit_download_dir", setFitDownloadDirDescription, setFitDownloadDir)
}

// --- generic FIT message tool ----------------------------------------------

func getActivityFitMessages(ctx context.Context, s *Session, in getActivityFitMessagesInput) (any, error) {
	id, err := activityIDValue(in.ActivityID)
	if err != nil {
		return fmt.Sprintf("Error downloading FIT data for activity %v: %v", in.ActivityID, err), nil
	}
	offset, limit := 0, 1000
	if in.MessageOffset != nil {
		offset = *in.MessageOffset
	}
	if in.MessageLimit != nil {
		limit = *in.MessageLimit
	}

	raw, err := s.Client.DownloadActivity(ctx, id, "fit")
	if err != nil {
		return fmt.Sprintf("Error downloading FIT data for activity %d: %v", id, err), nil
	}
	if len(raw) == 0 {
		return fmt.Sprintf("No FIT data returned for activity %d", id), nil
	}

	parsed, err := parseFITMessages(raw, fitMessageOptions{
		MessageTypes:   in.MessageTypes,
		IncludeRecords: in.IncludeRecords,
		MessageOffset:  offset,
		MessageLimit:   limit,
	})
	if err != nil {
		return map[string]any{
			"error":       err.Error(),
			"activity_id": id,
			"debug":       fitDownloadDebug(raw),
		}, nil
	}

	result := map[string]any{
		"activity_id": id,
		"source":      "garmin_original_fit",
	}
	for key, value := range parsed {
		result[key] = value
	}
	return result, nil
}

func fitDownloadDebug(raw []byte) map[string]any {
	debug := fitDebug(raw)
	debug["download_size_bytes"] = debug["total_bytes"]
	delete(debug, "total_bytes")
	return debug
}

func fitDebug(raw []byte) map[string]any {
	first := raw
	if len(first) > 16 {
		first = first[:16]
	}
	return map[string]any{
		"total_bytes":        len(raw),
		"first_16_bytes_hex": fmt.Sprintf("%x", first),
		"hint":               "1f8b = gzip, 504b = ZIP, 0e10/0c10 = raw FIT, 3c or 7b = HTML/JSON error from Garmin",
	}
}

type fitMessageOptions struct {
	MessageTypes   []string
	IncludeRecords bool
	MessageOffset  int
	MessageLimit   int
}

// parseFITMessages ports _parse_fit_messages: inventory every message, then
// return only one bounded page of the selected stream.
func parseFITMessages(raw []byte, options fitMessageOptions) (map[string]any, error) {
	if options.MessageOffset < 0 {
		return nil, fmt.Errorf("message_offset must be at least 0")
	}
	if options.MessageLimit < 1 || options.MessageLimit > 5000 {
		return nil, fmt.Errorf("message_limit must be between 1 and 5000")
	}

	var selected map[string]bool
	if options.MessageTypes != nil {
		selected = map[string]bool{}
		for _, item := range options.MessageTypes {
			name := strings.ToLower(strings.TrimSpace(item))
			if name != "" {
				selected[name] = true
			}
		}
	}
	recordsSelected := selected == nil || selected["record"]

	fitBytes, err := extractFITBytes(raw)
	if err != nil {
		return nil, err
	}
	messages, err := decodeFIT(fitBytes)
	if err != nil {
		return nil, err
	}

	messageCounts := map[string]int{}
	for _, message := range messages {
		messageCounts[message.Name]++
	}

	omitted := map[string]int{}
	if selected == nil {
		for name, count := range messageCounts {
			if strings.ToLower(name) != "record" && count > 100 {
				omitted[name] = count
			}
		}
	}

	eligible := func(name string) bool {
		normalized := strings.ToLower(name)
		if normalized == "record" && (!options.IncludeRecords || !recordsSelected) {
			return false
		}
		if selected != nil && !selected[normalized] {
			return false
		}
		_, isOmitted := omitted[name]
		return !isOmitted
	}

	totalEligible := 0
	for name, count := range messageCounts {
		if eligible(name) {
			totalEligible += count
		}
	}

	pageEnd := options.MessageOffset + options.MessageLimit
	page := []any{}
	eligibleIndex := 0
	typeCounts := map[string]int{}
	for messageIndex, message := range messages {
		typeIndex := typeCounts[message.Name]
		typeCounts[message.Name] = typeIndex + 1
		if !eligible(message.Name) {
			continue
		}
		if options.MessageOffset <= eligibleIndex && eligibleIndex < pageEnd {
			fields := make([]any, 0, len(message.Fields))
			for _, field := range message.Fields {
				fields = append(fields, fitFieldMap(field))
			}
			page = append(page, map[string]any{
				"message_index":         messageIndex,
				"type_index":            typeIndex,
				"type":                  message.Name,
				"fields":                fields,
				"global_message_number": message.GlobalMessageNumber,
			})
		}
		eligibleIndex++
		if eligibleIndex >= pageEnd {
			break
		}
	}

	returnedCounts := map[string]int{}
	for _, item := range page {
		returnedCounts[asMap(item)["type"].(string)]++
	}

	recordCount := messageCounts["record"]
	returnedRecordCount := returnedCounts["record"]
	recordStream := map[string]any{
		"included":       options.IncludeRecords && recordsSelected,
		"total_count":    recordCount,
		"returned_count": returnedRecordCount,
	}
	if !options.IncludeRecords && recordsSelected && recordCount > 0 {
		recordStream["hint"] = "Set include_records=true and use message pagination to retrieve records."
	}

	pagination := map[string]any{
		"total_eligible_count": totalEligible,
		"returned_count":       len(page),
		"offset":               options.MessageOffset,
		"limit":                options.MessageLimit,
	}
	if options.MessageOffset+len(page) < totalEligible {
		pagination["next_offset"] = options.MessageOffset + len(page)
	}

	result := map[string]any{
		"fit_size_bytes":          len(fitBytes),
		"message_counts":          messageCounts,
		"returned_message_counts": returnedCounts,
		"record_stream":           recordStream,
		"pagination":              pagination,
		"messages":                page,
	}
	if len(omitted) > 0 {
		highFrequency := map[string]any{}
		for name, count := range omitted {
			highFrequency[name] = map[string]any{
				"count": count,
				"hint":  fmt.Sprintf("Request message_types=['%s'] and follow pagination.", name),
			}
		}
		result["omitted_high_frequency_message_types"] = highFrequency
	}
	return result, nil
}

// --- activity FIT data tool ------------------------------------------------

func getActivityFitData(ctx context.Context, s *Session, in getActivityFitDataInput) (any, error) {
	id, err := activityIDValue(in.ActivityID)
	if err != nil {
		return fmt.Sprintf("Error downloading FIT data for activity %v: %v", in.ActivityID, err), nil
	}
	raw, err := s.Client.DownloadActivity(ctx, id, "fit")
	if err != nil {
		return fmt.Sprintf("Error downloading FIT data for activity %d: %v", id, err), nil
	}
	if len(raw) == 0 {
		return fmt.Sprintf("No FIT data returned for activity %d", id), nil
	}

	parsed, err := parseFIT(raw, in.IncludeRecords)
	if err != nil {
		return map[string]any{
			"error": err.Error(),
			"debug": fitDebug(raw),
		}, nil
	}
	parsed["activity_id"] = id

	session := asMap(parsed["session"])
	startTime, _ := session["start_time"].(string)
	activityDate := ""
	if len(startTime) >= 10 {
		activityDate = startTime[:10]
	}
	if activityDate != "" {
		if weightKg, weighInDate, ok := getRiderWeightKg(ctx, s.Client, activityDate); ok {
			parsed["rider_weight_kg"] = weightKg
			parsed["rider_weight_date"] = weighInDate
			if avgPower, ok := fitNumber(session["avg_power_w"]); ok && avgPower != 0 {
				session["avg_w_per_kg"] = roundTo(avgPower/weightKg, 2)
			}
			if normalized, ok := fitNumber(session["normalized_power_w"]); ok && normalized != 0 {
				session["normalized_w_per_kg"] = roundTo(normalized/weightKg, 2)
			}
			for _, item := range asList(parsed["climbs"]) {
				climb := asMap(item)
				if climbPower, ok := fitNumber(climb["avg_power_w"]); ok && climbPower != 0 {
					climb["avg_w_per_kg"] = roundTo(climbPower/weightKg, 2)
				}
			}
		}
	}
	return parsed, nil
}

func getRiderWeightKg(ctx context.Context, client interface {
	GetBodyComposition(context.Context, string, string) (any, error)
}, activityDate string) (float64, string, bool) {
	day, err := time.Parse("2006-01-02", activityDate)
	if err != nil {
		return 0, "", false
	}
	start := day.AddDate(0, 0, -90)
	data, err := client.GetBodyComposition(ctx, start.Format("2006-01-02"), activityDate)
	if err != nil {
		return 0, "", false
	}
	bestDay := ""
	bestWeight := 0.0
	for _, item := range asList(asMap(data)["dateWeightList"]) {
		entry := asMap(item)
		date, _ := entry["calendarDate"].(string)
		weight, ok := fitNumber(entry["weight"])
		if date == "" || !ok || weight == 0 || date > activityDate {
			continue
		}
		if date > bestDay || (date == bestDay && weight > bestWeight) {
			bestDay, bestWeight = date, weight
		}
	}
	if bestDay == "" {
		return 0, "", false
	}
	return roundTo(bestWeight/1000, 2), bestDay, true
}

// --- power duration curve tool ---------------------------------------------

var pdcDurationsS = []int{5, 30, 60, 300, 600, 1200, 3600}
var pdcLabels = []string{"5s", "30s", "1min", "5min", "10min", "20min", "60min"}

func getPowerDurationCurve(ctx context.Context, s *Session, in getPowerDurationCurveInput) (any, error) {
	numActivities := 20
	if in.NumActivities != nil {
		numActivities = *in.NumActivities
	}
	if numActivities > 50 {
		numActivities = 50
	}
	activityType := in.ActivityType
	if activityType == "" {
		activityType = "cycling"
	}
	requestedType := strings.ToLower(activityType)

	activities, err := s.Client.GetActivitiesByType(ctx, 0, numActivities, requestedType)
	if err != nil {
		return fmt.Sprintf("Error computing power duration curve: %v", err), nil
	}
	if !truthy(activities) {
		return "No activities found.", nil
	}

	var matched []map[string]any
	for _, item := range asList(activities) {
		activity := asMap(item)
		activityTypeMap := asMap(activity["activityType"])
		typeKey := strings.ToLower(fmt.Sprint(activityTypeMap["typeKey"]))
		parentType := fmt.Sprint(activityTypeMap["parentTypeId"])
		if strings.Contains(typeKey, requestedType) ||
			parentType == requestedType ||
			(requestedType == "cycling" && (parentType == "2" || strings.Contains(typeKey, "biking"))) {
			matched = append(matched, activity)
		}
	}
	if len(matched) == 0 {
		return fmt.Sprintf("No %s activities found in the last %d activities.", activityType, numActivities), nil
	}

	seasonBests := map[string]map[string]any{}
	processed, skipped := 0, 0
	for _, activity := range matched {
		actID, ok := fitNumber(activity["activityId"])
		if !ok || actID == 0 {
			continue
		}
		id := int64(actID)
		actDate := ""
		if rawDate, ok := activity["startTimeLocal"].(string); ok && len(rawDate) >= 10 {
			actDate = rawDate[:10]
		}

		raw, err := s.Client.DownloadActivity(ctx, id, "fit")
		if err != nil {
			skipped++
			continue
		}
		if len(raw) == 0 {
			continue
		}
		messages, err := decodeFIT(raw)
		if err != nil {
			skipped++
			continue
		}
		records := []map[string]any{}
		for _, message := range messages {
			if message.Name != "record" {
				continue
			}
			records = append(records, map[string]any{
				"power_w":   fitFieldValue(message, "power"),
				"timestamp": fitFieldValue(message, "timestamp"),
			})
		}
		pdc := computePowerDurationCurve(records)
		for label, watts := range pdc {
			wattsNumber, _ := fitNumber(watts)
			current, exists := seasonBests[label]
			if exists {
				currentWatts, _ := fitNumber(current["watts"])
				if wattsNumber <= currentWatts {
					continue
				}
			}
			seasonBests[label] = map[string]any{
				"watts":       int(math.Round(wattsNumber)),
				"activity_id": id,
				"date":        actDate,
			}
		}
		processed++
	}

	if len(seasonBests) == 0 {
		return fmt.Sprintf("No power data found across %d activities.", processed), nil
	}

	ordered := map[string]any{}
	for _, label := range pdcLabels {
		if best, ok := seasonBests[label]; ok {
			ordered[label] = best
		}
	}
	var ftpEstimate any
	if best, ok := ordered["20min"]; ok {
		watts, _ := fitNumber(asMap(best)["watts"])
		ftpEstimate = int(math.Round(watts * 0.95))
	}
	return map[string]any{
		"activities_analyzed": processed,
		"activities_skipped":  skipped,
		"ftp_estimate_w":      ftpEstimate,
		"ftp_note":            "Estimated as 95% of 20-minute best power",
		"season_bests":        ordered,
	}, nil
}

// --- download activity file / set download dir -----------------------------

func expandHomePath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

func resolveFITDownloadDir(s *Session, outputDir string) (string, bool) {
	if strings.TrimSpace(outputDir) != "" {
		if abs, err := filepath.Abs(expandHomePath(outputDir)); err == nil {
			return abs, true
		}
	}
	if envDir := os.Getenv("GARMIN_FIT_DOWNLOAD_DIR"); envDir != "" {
		if abs, err := filepath.Abs(expandHomePath(envDir)); err == nil {
			return abs, true
		}
	}
	if settings, err := s.Settings(); err == nil && settings.FitDownloadDir != "" {
		if abs, err := filepath.Abs(expandHomePath(settings.FitDownloadDir)); err == nil {
			return abs, true
		}
	}
	return "", false
}

func fitSettingsPath(s *Session) string {
	dir, err := s.Store.Dir(s.Email)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "settings.json")
}

func downloadActivityFile(ctx context.Context, s *Session, in downloadActivityFileInput) (any, error) {
	id, err := activityIDValue(in.ActivityID)
	if err != nil {
		return fmt.Sprintf("Error downloading activity %v: %v", in.ActivityID, err), nil
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "fit"
	}
	validFormats := []string{"fit", "gpx", "tcx", "csv"}
	if format != "fit" && format != "gpx" && format != "tcx" && format != "csv" {
		return map[string]any{
			"error":         fmt.Sprintf("Invalid format '%s'.", in.Format),
			"valid_formats": validFormats,
		}, nil
	}

	downloadDir, ok := resolveFITDownloadDir(s, in.OutputDir)
	if !ok {
		cwd, _ := os.Getwd()
		return map[string]any{
			"status":            "needs_setup",
			"suggested_default": cwd,
			"config_path":       fitSettingsPath(s),
			"message": "No download directory configured. Ask the user where to " +
				"save activity files (offer the current working directory " +
				"as the default), then call set_fit_download_dir(path) " +
				"before downloading.",
		}, nil
	}
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return fmt.Sprintf("Error downloading activity %d: %v", id, err), nil
	}

	data, err := s.Client.DownloadActivity(ctx, id, format)
	if err != nil {
		return fmt.Sprintf("Error downloading activity %d: %v", id, err), nil
	}
	if len(data) == 0 {
		return fmt.Sprintf("No %s data returned for activity %d", format, id), nil
	}

	payload := data
	if format == "fit" {
		payload, err = extractFITBytes(data)
		if err != nil {
			return map[string]any{
				"error": err.Error(),
				"debug": fitDebug(data),
			}, nil
		}
	}

	filePath := filepath.Join(downloadDir, fmt.Sprintf("%d.%s", id, format))
	if err := os.WriteFile(filePath, payload, 0644); err != nil {
		return fmt.Sprintf("Error downloading activity %d: %v", id, err), nil
	}
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}
	return map[string]any{
		"activity_id": id,
		"format":      format,
		"file_path":   absPath,
		"size_bytes":  len(payload),
		"message":     "Activity file saved.",
	}, nil
}

func setFitDownloadDir(_ context.Context, s *Session, in setFitDownloadDirInput) (any, error) {
	absPath, err := filepath.Abs(expandHomePath(in.Path))
	if err != nil {
		return fmt.Sprintf("Error setting FIT download directory: %v", err), nil
	}
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return fmt.Sprintf("Error setting FIT download directory: %v", err), nil
	}
	settings, err := s.Settings()
	if err != nil {
		return fmt.Sprintf("Error setting FIT download directory: %v", err), nil
	}
	settings.FitDownloadDir = absPath
	if err := s.Store.SaveSettings(s.Email, settings); err != nil {
		return fmt.Sprintf("Error setting FIT download directory: %v", err), nil
	}
	return map[string]any{
		"download_dir": absPath,
		"config_path":  fitSettingsPath(s),
		"message":      "Default FIT download directory configured.",
	}, nil
}

// --- FIT decoding helpers ---------------------------------------------------

func fitFieldValue(message fitMessage, names ...string) any {
	for _, name := range names {
		for _, field := range message.Fields {
			if field.Name == name && field.Value != nil {
				return field.Value
			}
		}
	}
	return nil
}

func decodeGearChange(data uint32) map[string]any {
	rearGearNum := data & 0xFF
	rearTeeth := (data >> 8) & 0xFF
	frontGearNum := (data >> 16) & 0xFF
	frontTeeth := (data >> 24) & 0xFF
	out := map[string]any{
		"rear_gear_num":  rearGearNum,
		"front_gear_num": frontGearNum,
		"rear_teeth":     nil,
		"front_teeth":    nil,
	}
	if rearTeeth > 0 {
		out["rear_teeth"] = rearTeeth
	}
	if frontTeeth > 0 {
		out["front_teeth"] = frontTeeth
	}
	return out
}

func decodeLeftRightBalance(value any, perRecord bool) *float64 {
	if value == nil {
		return nil
	}
	number, ok := fitNumber(value)
	if !ok {
		return nil
	}
	intValue := int64(number)
	var rightDominant bool
	var pct float64
	if perRecord {
		rightDominant = intValue&0x80 != 0
		pct = float64(intValue & 0x7F)
	} else {
		rightDominant = intValue&0x8000 != 0
		pct = float64(intValue&0x3FFF) / 100.0
	}
	if pct > 100 {
		return nil
	}
	if rightDominant {
		result := roundTo(100.0-pct, 1)
		return &result
	}
	result := roundTo(pct, 1)
	return &result
}

func semicirclesToDegrees(value any) any {
	number, ok := fitNumber(value)
	if !ok {
		return nil
	}
	return roundTo(number*(180.0/math.Pow(2, 31)), 6)
}

var fitTimestampLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05",
}

func parseFITTimestamp(value any) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		return v.UTC(), true
	case string:
		text := strings.Replace(v, "Z", "+00:00", 1)
		if text == "" {
			return time.Time{}, false
		}
		for _, layout := range fitTimestampLayouts {
			parsed, err := time.Parse(layout, text)
			if err == nil {
				return parsed.UTC(), true
			}
		}
	}
	return time.Time{}, false
}

// pythonTimeString renders a FIT timestamp the way str(datetime) does.
func pythonTimeString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		if parsed, ok := parseFITTimestamp(text); ok {
			return parsed.Format("2006-01-02 15:04:05")
		}
		return text
	}
	if parsed, ok := parseFITTimestamp(value); ok {
		return parsed.Format("2006-01-02 15:04:05")
	}
	return fmt.Sprint(value)
}

func fitSliceNumbers(value any) []float64 {
	if !isFitSlice(value) {
		if number, ok := fitNumber(value); ok {
			return []float64{number}
		}
		return nil
	}
	items := fitSliceToAny(value)
	out := make([]float64, 0, len(items))
	for _, item := range items {
		if number, ok := fitNumber(item); ok {
			out = append(out, number)
		}
	}
	return out
}

// --- power duration curve ---------------------------------------------------

func computePowerDurationCurve(records []map[string]any) map[string]any {
	powers := make([]any, len(records))
	hasPower := false
	for i, record := range records {
		powers[i] = record["power_w"]
		if record["power_w"] != nil {
			hasPower = true
		}
	}
	if !hasPower {
		return nil
	}

	gaps := recordGapsS(records)
	filled := []float64{}
	for i, power := range powers {
		held := heldSeconds(gaps[i])
		value := 0.0
		if number, ok := fitNumber(power); ok {
			value = number
		}
		for j := 0; j < held; j++ {
			filled = append(filled, value)
		}
		padding := gaps[i] - held
		if padding > pdcDurationsS[len(pdcDurationsS)-1] {
			padding = pdcDurationsS[len(pdcDurationsS)-1]
		}
		for j := 0; j < padding; j++ {
			filled = append(filled, 0)
		}
	}
	n := len(filled)
	if n == 0 {
		return nil
	}

	prefix := make([]float64, n+1)
	for i, power := range filled {
		prefix[i+1] = prefix[i] + power
	}

	curve := map[string]any{}
	for i, duration := range pdcDurationsS {
		if n < duration {
			continue
		}
		best := 0.0
		for start := 0; start+duration <= n; start++ {
			window := prefix[start+duration] - prefix[start]
			if window > best {
				best = window
			}
		}
		bestAvg := best / float64(duration)
		if bestAvg > 0 {
			curve[pdcLabels[i]] = int(math.Round(bestAvg))
		}
	}
	if len(curve) == 0 {
		return nil
	}
	return curve
}

const maxRecordGapS = 10

func recordGapsS(records []map[string]any) []int {
	if len(records) == 0 {
		return []int{}
	}
	times := make([]time.Time, len(records))
	for i, record := range records {
		parsed, ok := parseFITTimestamp(record["timestamp"])
		if !ok {
			return ones(len(records))
		}
		times[i] = parsed
	}
	gaps := make([]int, 0, len(records))
	for i := 0; i+1 < len(times); i++ {
		gap := int(times[i+1].Sub(times[i]).Seconds())
		if gap < 0 {
			return ones(len(records))
		}
		gaps = append(gaps, gap)
	}
	if len(gaps) == 0 {
		return []int{1}
	}
	return append(gaps, heldSeconds(gaps[len(gaps)-1]))
}

func ones(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = 1
	}
	return out
}

func heldSeconds(gap int) int {
	if gap <= maxRecordGapS {
		return gap
	}
	return 1
}

func recordSeconds(records []map[string]any) []int {
	gaps := recordGapsS(records)
	out := make([]int, len(gaps))
	for i, gap := range gaps {
		out[i] = heldSeconds(gap)
	}
	return out
}

// --- grade derivation -------------------------------------------------------

const (
	gradeWindowM       = 100.0
	gradeMinSpanM      = 20.0
	gradeClampPct      = 40.0
	minDeviceGradeRate = 0.05
)

func deriveGrades(records []map[string]any) (string, bool) {
	deviceCount := 0
	for _, record := range records {
		if record["grade_pct"] != nil {
			deviceCount++
		}
	}
	if len(records) > 0 && float64(deviceCount) >= minDeviceGradeRate*float64(len(records)) {
		return "device", true
	}

	type point struct {
		index    int
		distance float64
		altitude float64
	}
	points := []point{}
	for i, record := range records {
		distance, okDistance := fitNumber(record["distance_m"])
		altitude, okAltitude := fitNumber(record["altitude_m"])
		if okDistance && okAltitude {
			points = append(points, point{index: i, distance: distance, altitude: altitude})
		}
	}

	half := gradeWindowM / 2
	derived := 0
	lo, hi := 0, 0
	for _, current := range points {
		for lo < len(points)-1 && points[lo].distance < current.distance-half {
			lo++
		}
		if hi < lo {
			hi = lo
		}
		for hi+1 < len(points) && points[hi+1].distance <= current.distance+half {
			hi++
		}
		span := points[hi].distance - points[lo].distance
		if span < gradeMinSpanM || records[current.index]["grade_pct"] != nil {
			continue
		}
		grade := (points[hi].altitude - points[lo].altitude) / span * 100
		grade = math.Max(-gradeClampPct, math.Min(gradeClampPct, grade))
		records[current.index]["grade_pct"] = roundTo(grade, 2)
		derived++
	}
	if derived > 0 {
		return "derived", true
	}
	if deviceCount > 0 {
		return "device", true
	}
	return "", false
}

func fillShiftGrades(shifts []map[string]any, shiftRecordCounts []int, records []map[string]any) {
	lastGrades := make([]any, len(records))
	var last any
	for i, record := range records {
		if record["grade_pct"] != nil {
			last = record["grade_pct"]
		}
		lastGrades[i] = last
	}
	for i, shift := range shifts {
		if _, exists := shift["grade_at_shift_pct"]; exists {
			continue
		}
		count := shiftRecordCounts[i]
		if count == 0 {
			continue
		}
		grade := lastGrades[count-1]
		if grade != nil {
			if number, ok := fitNumber(grade); ok {
				shift["grade_at_shift_pct"] = roundTo(number, 1)
			}
		}
	}
}

// --- climb detection --------------------------------------------------------

func detectClimbs(records []map[string]any, minElevationGain, minAvgGrade float64, minDurationS int) []map[string]any {
	if len(records) == 0 {
		return []map[string]any{}
	}
	const flatToleranceS = 30

	climbs := []map[string]any{}
	inClimb := false
	climbStart := 0
	flatCount, flatS := 0, 0
	heldS := recordSeconds(records)

	for i, record := range records {
		gradeValue := record["grade_pct"]
		if gradeValue == nil {
			continue
		}
		grade, _ := fitNumber(gradeValue)
		if !inClimb {
			if grade >= minAvgGrade {
				inClimb = true
				climbStart = i
				flatCount, flatS = 0, 0
			}
			continue
		}
		if grade < minAvgGrade {
			flatCount++
			flatS += heldS[i]
			if flatS > flatToleranceS {
				endIndex := i - flatCount
				climb := summarizeClimb(records, climbStart, endIndex, heldS)
				if climbGain, _ := fitNumber(climb["elevation_gain_m"]); climbGain >= minElevationGain {
					if climbDuration, _ := fitNumber(climb["duration_s"]); climbDuration >= float64(minDurationS) {
						climbs = append(climbs, climb)
					}
				}
				inClimb = false
				flatCount, flatS = 0, 0
			}
		} else {
			flatCount, flatS = 0, 0
		}
	}

	if inClimb {
		climb := summarizeClimb(records, climbStart, len(records)-1, heldS)
		if climbGain, _ := fitNumber(climb["elevation_gain_m"]); climbGain >= minElevationGain {
			if climbDuration, _ := fitNumber(climb["duration_s"]); climbDuration >= float64(minDurationS) {
				climbs = append(climbs, climb)
			}
		}
	}
	return climbs
}

func summarizeClimb(records []map[string]any, startIndex, endIndex int, heldS []int) map[string]any {
	if startIndex > endIndex || endIndex >= len(records) {
		return map[string]any{}
	}
	segment := records[startIndex : endIndex+1]
	segmentHeld := heldS[startIndex : endIndex+1]
	duration := 0
	for _, held := range segmentHeld {
		duration += held
	}

	result := map[string]any{
		"start_time": segment[0]["timestamp"],
		"end_time":   segment[len(segment)-1]["timestamp"],
		"duration_s": duration,
	}
	if result["start_time"] == nil {
		result["start_time"] = ""
	}
	if result["end_time"] == nil {
		result["end_time"] = ""
	}

	startAltitude, okStartAltitude := fitNumber(segment[0]["altitude_m"])
	endAltitude, okEndAltitude := fitNumber(segment[len(segment)-1]["altitude_m"])
	if okStartAltitude && okEndAltitude {
		elevationGain := endAltitude - startAltitude
		result["elevation_gain_m"] = roundTo(elevationGain, 1)
		if duration > 0 {
			result["vam_m_per_hr"] = int(math.Round(elevationGain / float64(duration) * 3600))
		}
	}

	startDistance, okStartDistance := fitNumber(segment[0]["distance_m"])
	endDistance, okEndDistance := fitNumber(segment[len(segment)-1]["distance_m"])
	if okStartDistance && okEndDistance {
		result["distance_m"] = int(math.Round(endDistance - startDistance))
	} else {
		totalDistance := 0.0
		hasSpeed := false
		for i, record := range segment {
			speed, ok := fitNumber(record["speed_mps"])
			if !ok {
				continue
			}
			totalDistance += speed * float64(segmentHeld[i])
			hasSpeed = true
		}
		if hasSpeed {
			result["distance_m"] = int(math.Round(totalDistance))
		}
	}

	powers := []any{}
	cadences := []any{}
	heartRates := []any{}
	grades := []any{}
	for _, record := range segment {
		if record["power_w"] != nil {
			powers = append(powers, record["power_w"])
		}
		if record["cadence_rpm"] != nil {
			cadences = append(cadences, record["cadence_rpm"])
		}
		if record["heart_rate_bpm"] != nil {
			heartRates = append(heartRates, record["heart_rate_bpm"])
		}
		if record["grade_pct"] != nil {
			grades = append(grades, record["grade_pct"])
		}
	}
	if len(grades) > 0 {
		if avg, ok := safeAvg(grades); ok {
			result["avg_grade_pct"] = roundTo(avg, 1)
		}
		if maxGrade, ok := maxNumber(grades); ok {
			result["max_grade_pct"] = roundTo(maxGrade, 1)
		}
	}
	if len(powers) > 0 {
		if avg, ok := safeAvg(powers); ok {
			result["avg_power_w"] = int(math.Round(avg))
		}
	}
	if len(cadences) > 0 {
		if avg, ok := safeAvg(cadences); ok {
			result["avg_cadence_rpm"] = int(math.Round(avg))
		}
	}
	if len(heartRates) > 0 {
		if avg, ok := safeAvg(heartRates); ok {
			result["avg_hr_bpm"] = int(math.Round(avg))
		}
	}
	return result
}

// --- grade analysis ---------------------------------------------------------

func gradeAnalysis(records []map[string]any) map[string]any {
	type bin struct {
		label   string
		records []map[string]any
		timeS   int
	}
	bins := map[string]*bin{
		"descending": {"descending (<-3%)", nil, 0},
		"flat":       {"flat (-3% to 3%)", nil, 0},
		"gentle":     {"gentle (3-6%)", nil, 0},
		"moderate":   {"moderate (6-9%)", nil, 0},
		"steep":      {"steep (>9%)", nil, 0},
	}
	heldSecondsList := recordSeconds(records)
	hasGrade := false
	for i, record := range records {
		gradeValue := record["grade_pct"]
		if gradeValue == nil {
			continue
		}
		hasGrade = true
		grade, _ := fitNumber(gradeValue)
		key := "steep"
		switch {
		case grade < -3:
			key = "descending"
		case grade < 3:
			key = "flat"
		case grade < 6:
			key = "gentle"
		case grade < 9:
			key = "moderate"
		}
		bins[key].records = append(bins[key].records, record)
		bins[key].timeS += heldSecondsList[i]
	}
	if !hasGrade {
		return nil
	}

	result := map[string]any{}
	for _, key := range []string{"descending", "flat", "gentle", "moderate", "steep"} {
		data := bins[key]
		if len(data.records) == 0 {
			continue
		}
		entry := map[string]any{"time_s": data.timeS}
		powers, cadences, heartRates := []any{}, []any{}, []any{}
		for _, record := range data.records {
			if record["power_w"] != nil {
				powers = append(powers, record["power_w"])
			}
			if record["cadence_rpm"] != nil {
				cadences = append(cadences, record["cadence_rpm"])
			}
			if record["heart_rate_bpm"] != nil {
				heartRates = append(heartRates, record["heart_rate_bpm"])
			}
		}
		if avg, ok := safeAvg(powers); ok {
			entry["avg_power_w"] = int(math.Round(avg))
		}
		if avg, ok := safeAvg(cadences); ok {
			entry["avg_cadence_rpm"] = int(math.Round(avg))
		}
		if avg, ok := safeAvg(heartRates); ok {
			entry["avg_hr_bpm"] = int(math.Round(avg))
		}
		result[key] = entry
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// --- HR drift ---------------------------------------------------------------

func computeHRDrift(records []map[string]any) map[string]any {
	const minDurationS = 3600

	filtered := []map[string]any{}
	filteredS := 0
	heldSecondsList := recordSeconds(records)
	for i, record := range records {
		_, okPower := fitNumber(record["power_w"])
		heartRate, okHeartRate := fitNumber(record["heart_rate_bpm"])
		if okPower && okHeartRate && heartRate > 0 {
			filtered = append(filtered, record)
			filteredS += heldSecondsList[i]
		}
	}
	if filteredS < minDurationS {
		return nil
	}

	mid := len(filtered) / 2
	ratio := func(recs []map[string]any) (float64, bool) {
		powers, hearts := []any{}, []any{}
		for _, record := range recs {
			powers = append(powers, record["power_w"])
			hearts = append(hearts, record["heart_rate_bpm"])
		}
		avgPower, okPower := safeAvg(powers)
		avgHeart, okHeart := safeAvg(hearts)
		if !okPower || !okHeart || avgHeart == 0 {
			return 0, false
		}
		return avgPower / avgHeart, true
	}
	first, okFirst := ratio(filtered[:mid])
	second, okSecond := ratio(filtered[mid:])
	if !okFirst || !okSecond || first == 0 {
		return nil
	}

	drift := (second - first) / first * 100
	interpretation := "well_coupled"
	switch {
	case math.Abs(drift) < 5:
		interpretation = "well_coupled"
	case drift > 0:
		interpretation = "power_increased_vs_hr"
	case drift > -10:
		interpretation = "moderate_drift"
	default:
		interpretation = "significant_decoupling"
	}
	return map[string]any{
		"hr_drift_pct":               roundTo(drift, 1),
		"first_half_power_hr_ratio":  roundTo(first, 3),
		"second_half_power_hr_ratio": roundTo(second, 3),
		"interpretation":             interpretation,
		"note": "Negative drift = HR increased vs power (decoupling); below -10% suggests " +
			"aerobic base is limiting factor. Positive drift = power increased vs HR " +
			"(not decoupling).",
	}
}

// --- temperature ------------------------------------------------------------

func computeTemperatureStats(records []map[string]any) map[string]any {
	temps := []float64{}
	withTemp := []map[string]any{}
	for _, record := range records {
		temperature, ok := fitNumber(record["temperature_c"])
		if !ok {
			continue
		}
		temps = append(temps, temperature)
		withTemp = append(withTemp, record)
	}
	if len(temps) == 0 {
		return nil
	}
	avgTemp := 0.0
	for _, temperature := range temps {
		avgTemp += temperature
	}
	avgTemp /= float64(len(temps))
	minTemp, maxTemp := temps[0], temps[0]
	for _, temperature := range temps {
		if temperature < minTemp {
			minTemp = temperature
		}
		if temperature > maxTemp {
			maxTemp = temperature
		}
	}

	sort.SliceStable(withTemp, func(i, j int) bool {
		left, _ := fitNumber(withTemp[i]["temperature_c"])
		right, _ := fitNumber(withTemp[j]["temperature_c"])
		return left < right
	})
	third := len(withTemp) / 3
	if third < 1 {
		third = 1
	}
	coolest := withTemp[:third]
	hottest := withTemp[len(withTemp)-third:]

	avgHeart := func(recs []map[string]any) any {
		values := []any{}
		for _, record := range recs {
			if record["heart_rate_bpm"] != nil && truthyNumber(record["heart_rate_bpm"]) {
				values = append(values, record["heart_rate_bpm"])
			}
		}
		if avg, ok := safeAvg(values); ok {
			return roundTo(avg, 1)
		}
		return nil
	}
	avgPower := func(recs []map[string]any) any {
		values := []any{}
		for _, record := range recs {
			if record["power_w"] != nil {
				values = append(values, record["power_w"])
			}
		}
		if avg, ok := safeAvg(values); ok {
			return roundTo(avg, 1)
		}
		return nil
	}

	result := map[string]any{
		"avg_temp_c":   roundTo(avgTemp, 1),
		"min_temp_c":   minTemp,
		"max_temp_c":   maxTemp,
		"temp_range_c": roundTo(maxTemp-minTemp, 1),
	}
	if value := avgHeart(coolest); value != nil {
		result["avg_hr_coolest_third_bpm"] = value
	}
	if value := avgHeart(hottest); value != nil {
		result["avg_hr_hottest_third_bpm"] = value
	}
	if value := avgPower(coolest); value != nil {
		result["avg_power_coolest_third_w"] = value
	}
	if value := avgPower(hottest); value != nil {
		result["avg_power_hottest_third_w"] = value
	}
	return result
}

func truthyNumber(value any) bool {
	number, ok := fitNumber(value)
	return ok && number != 0
}

// --- shift summary ----------------------------------------------------------

func computeShiftSummary(shifts []map[string]any) map[string]any {
	if len(shifts) == 0 {
		return map[string]any{"total_shifts": 0, "note": "No DI2 shift events found in FIT file"}
	}

	qualityCounts := map[string]int{"proactive": 0, "reactive": 0, "coasting": 0, "spun_out": 0, "unknown": 0}
	terrainQuality := map[string]map[string]int{
		"climbing":   {"proactive": 0, "reactive": 0, "coasting": 0, "spun_out": 0, "unknown": 0},
		"flat":       {"proactive": 0, "reactive": 0, "coasting": 0, "spun_out": 0, "unknown": 0},
		"descending": {"proactive": 0, "reactive": 0, "coasting": 0, "spun_out": 0, "unknown": 0},
	}
	frontShifts, rearShifts := 0, 0
	gearUsage := map[string]int{}
	gearOrder := []string{}
	cadencesAtShift := []float64{}

	shiftTimes := []time.Time{}
	for _, shift := range shifts {
		if parsed, ok := parseFITTimestamp(shift["timestamp"]); ok {
			shiftTimes = append(shiftTimes, parsed)
		}
	}
	sort.Slice(shiftTimes, func(i, j int) bool { return shiftTimes[i].Before(shiftTimes[j]) })
	panicBursts := 0
	for i := 0; i < len(shiftTimes); {
		end := i
		for end+1 < len(shiftTimes) && shiftTimes[end+1].Sub(shiftTimes[i]).Seconds() <= 10 {
			end++
		}
		if end-i+1 >= 3 {
			panicBursts++
			i = end + 1
		} else {
			i++
		}
	}

	for _, shift := range shifts {
		quality := "unknown"
		if value, ok := shift["quality"].(string); ok && value != "" {
			quality = value
		}
		qualityCounts[quality]++

		gradeValue := shift["grade_at_shift_pct"]
		terrain := "flat"
		if gradeValue != nil {
			grade, _ := fitNumber(gradeValue)
			if grade >= 3 {
				terrain = "climbing"
			} else if grade <= -3 {
				terrain = "descending"
			}
		}
		terrainQuality[terrain][quality]++

		if event, _ := shift["event"].(string); strings.Contains(event, "front") {
			frontShifts++
		} else {
			rearShifts++
		}

		if combo, ok := shift["gear_combo"].(string); ok && combo != "" {
			if _, exists := gearUsage[combo]; !exists {
				gearOrder = append(gearOrder, combo)
			}
			gearUsage[combo]++
		}
		if cadence, ok := fitNumber(shift["cadence_at_shift_rpm"]); ok {
			cadencesAtShift = append(cadencesAtShift, cadence)
		}
	}

	total := len(shifts)
	proactive := qualityCounts["proactive"]
	reactive := qualityCounts["reactive"]
	summary := map[string]any{
		"total_shifts":         total,
		"front_shifts":         frontShifts,
		"rear_shifts":          rearShifts,
		"proactive_shifts":     proactive,
		"reactive_shifts":      reactive,
		"coasting_shifts":      qualityCounts["coasting"],
		"spun_out_shifts":      qualityCounts["spun_out"],
		"proactive_pct":        percentOf(proactive, total),
		"reactive_pct":         percentOf(reactive, total),
		"panic_burst_episodes": panicBursts,
		"gear_usage":           orderedGearUsage(gearOrder, gearUsage),
	}

	hasTerrainData := false
	for _, shift := range shifts {
		if shift["grade_at_shift_pct"] != nil {
			hasTerrainData = true
			break
		}
	}
	if hasTerrainData {
		terrainSummary := map[string]any{}
		for _, terrain := range []string{"climbing", "flat", "descending"} {
			counts := terrainQuality[terrain]
			terrainTotal := 0
			for _, count := range counts {
				terrainTotal += count
			}
			if terrainTotal == 0 {
				continue
			}
			terrainSummary[terrain] = map[string]any{
				"total":        terrainTotal,
				"proactive":    counts["proactive"],
				"reactive":     counts["reactive"],
				"reactive_pct": percentOf(counts["reactive"], terrainTotal),
			}
		}
		if len(terrainSummary) > 0 {
			summary["by_terrain"] = terrainSummary
		}
	}

	if len(cadencesAtShift) > 0 {
		sum := 0.0
		minCadence, maxCadence := cadencesAtShift[0], cadencesAtShift[0]
		for _, cadence := range cadencesAtShift {
			sum += cadence
			if cadence < minCadence {
				minCadence = cadence
			}
			if cadence > maxCadence {
				maxCadence = cadence
			}
		}
		summary["avg_cadence_at_shift_rpm"] = roundTo(sum/float64(len(cadencesAtShift)), 1)
		summary["min_cadence_at_shift_rpm"] = minCadence
		summary["max_cadence_at_shift_rpm"] = maxCadence
	}
	return summary
}

func percentOf(part, total int) any {
	if total == 0 {
		return 0
	}
	return roundTo(float64(part)/float64(total)*100, 1)
}

func orderedGearUsage(order []string, counts map[string]int) map[string]any {
	sort.SliceStable(order, func(i, j int) bool { return counts[order[i]] > counts[order[j]] })
	out := map[string]any{}
	for _, key := range order {
		out[key] = counts[key]
	}
	return out
}

// --- HRV --------------------------------------------------------------------

func computeHRVMetrics(rrIntervalsS []float64) map[string]any {
	if len(rrIntervalsS) < 10 {
		return nil
	}
	rrMS := make([]float64, len(rrIntervalsS))
	for i, rr := range rrIntervalsS {
		rrMS[i] = rr * 1000
	}
	diffs := make([]float64, len(rrMS)-1)
	for i := 0; i+1 < len(rrMS); i++ {
		diffs[i] = rrMS[i+1] - rrMS[i]
	}
	squaredSum := 0.0
	for _, diff := range diffs {
		squaredSum += diff * diff
	}
	rmssd := 0.0
	if len(diffs) > 0 {
		rmssd = math.Sqrt(squaredSum / float64(len(diffs)))
	}
	meanRR := 0.0
	for _, rr := range rrMS {
		meanRR += rr
	}
	meanRR /= float64(len(rrMS))
	sdnn := 0.0
	if len(rrMS) > 1 {
		variance := 0.0
		for _, rr := range rrMS {
			variance += (rr - meanRR) * (rr - meanRR)
		}
		sdnn = math.Sqrt(variance / float64(len(rrMS)-1))
	}
	nn50 := 0
	for _, diff := range diffs {
		if math.Abs(diff) > 50 {
			nn50++
		}
	}
	pnn50 := 0.0
	if len(diffs) > 0 {
		pnn50 = 100 * float64(nn50) / float64(len(diffs))
	}
	var meanHR any
	if meanRR > 0 {
		meanHR = roundTo(60000.0/meanRR, 1)
	}
	return map[string]any{
		"rmssd_ms":    roundTo(rmssd, 1),
		"sdnn_ms":     roundTo(sdnn, 1),
		"pnn50_pct":   roundTo(pnn50, 2),
		"mean_rr_ms":  roundTo(meanRR, 1),
		"mean_hr_bpm": meanHR,
		"rr_count":    len(rrMS),
	}
}

// --- session / record curation ---------------------------------------------

func curateSession(message fitMessage) map[string]any {
	session := map[string]any{
		"sport":                 fitFieldValue(message, "sport"),
		"sub_sport":             fitFieldValue(message, "sub_sport"),
		"start_time":            pythonTimeString(fitFieldValue(message, "start_time")),
		"total_elapsed_time_s":  fitFieldValue(message, "total_elapsed_time"),
		"total_timer_time_s":    fitFieldValue(message, "total_timer_time"),
		"total_distance_m":      fitFieldValue(message, "total_distance"),
		"total_calories":        fitFieldValue(message, "total_calories"),
		"avg_speed_mps":         fitFieldValue(message, "enhanced_avg_speed", "avg_speed"),
		"max_speed_mps":         fitFieldValue(message, "enhanced_max_speed", "max_speed"),
		"avg_power_w":           fitFieldValue(message, "avg_power"),
		"max_power_w":           fitFieldValue(message, "max_power"),
		"normalized_power_w":    fitFieldValue(message, "normalized_power"),
		"avg_cadence_rpm":       fitFieldValue(message, "avg_cadence"),
		"max_cadence_rpm":       fitFieldValue(message, "max_cadence"),
		"avg_heart_rate_bpm":    fitFieldValue(message, "avg_heart_rate"),
		"max_heart_rate_bpm":    fitFieldValue(message, "max_heart_rate"),
		"total_ascent_m":        fitFieldValue(message, "total_ascent"),
		"total_descent_m":       fitFieldValue(message, "total_descent"),
		"total_training_effect": fitFieldValue(message, "total_training_effect"),

		"avg_left_pco_mm":                    fitFieldValue(message, "avg_left_pco"),
		"avg_right_pco_mm":                   fitFieldValue(message, "avg_right_pco"),
		"avg_left_torque_effectiveness_pct":  fitFieldValue(message, "avg_left_torque_effectiveness"),
		"avg_right_torque_effectiveness_pct": fitFieldValue(message, "avg_right_torque_effectiveness"),
		"avg_left_pedal_smoothness_pct":      fitFieldValue(message, "avg_left_pedal_smoothness"),
		"avg_right_pedal_smoothness_pct":     fitFieldValue(message, "avg_right_pedal_smoothness"),
	}
	for _, side := range []string{"left", "right"} {
		phase := fitFieldValue(message, "avg_"+side+"_power_phase")
		if phase == nil {
			continue
		}
		if isFitSlice(phase) {
			values := fitSliceNumbers(phase)
			if len(values) > 0 {
				session["avg_"+side+"_power_phase_start_deg"] = values[0]
			}
			if len(values) > 1 {
				session["avg_"+side+"_power_phase_end_deg"] = values[1]
			}
		} else {
			session["avg_"+side+"_power_phase_start_deg"] = phase
		}
	}
	if left := decodeLeftRightBalance(fitFieldValue(message, "left_right_balance"), false); left != nil {
		session["avg_left_power_pct"] = *left
		session["avg_right_power_pct"] = roundTo(100-*left, 1)
	}
	pruneNil(session)

	if normalized, ok := fitNumber(session["normalized_power_w"]); ok && normalized != 0 {
		if average, ok := fitNumber(session["avg_power_w"]); ok && average > 0 {
			session["variability_index"] = roundTo(normalized/average, 3)
		}
	}
	return session
}

func curateLap(message fitMessage, lapNumber int) map[string]any {
	lap := map[string]any{
		"lap_number":           lapNumber,
		"start_time":           pythonTimeString(fitFieldValue(message, "start_time")),
		"total_elapsed_time_s": fitFieldValue(message, "total_elapsed_time"),
		"total_distance_m":     fitFieldValue(message, "total_distance"),
		"avg_speed_mps":        fitFieldValue(message, "enhanced_avg_speed", "avg_speed"),
		"avg_power_w":          fitFieldValue(message, "avg_power"),
		"normalized_power_w":   fitFieldValue(message, "normalized_power"),
		"avg_cadence_rpm":      fitFieldValue(message, "avg_cadence"),
		"avg_heart_rate_bpm":   fitFieldValue(message, "avg_heart_rate"),

		"avg_left_pco_mm":                    fitFieldValue(message, "avg_left_pco"),
		"avg_right_pco_mm":                   fitFieldValue(message, "avg_right_pco"),
		"avg_left_torque_effectiveness_pct":  fitFieldValue(message, "avg_left_torque_effectiveness"),
		"avg_right_torque_effectiveness_pct": fitFieldValue(message, "avg_right_torque_effectiveness"),
		"avg_left_pedal_smoothness_pct":      fitFieldValue(message, "avg_left_pedal_smoothness"),
		"avg_right_pedal_smoothness_pct":     fitFieldValue(message, "avg_right_pedal_smoothness"),
		"total_ascent_m":                     fitFieldValue(message, "total_ascent"),
		"total_descent_m":                    fitFieldValue(message, "total_descent"),
	}
	if distance := lap["total_distance_m"]; distance != nil && wrappedLapDistance(distance) {
		lap["total_distance_m"] = nil
		lap["distance_invalid"] = true
	}
	if left := decodeLeftRightBalance(fitFieldValue(message, "left_right_balance"), false); left != nil {
		lap["avg_left_power_pct"] = *left
		lap["avg_right_power_pct"] = roundTo(100-*left, 1)
	}
	pruneNil(lap)

	if normalized, ok := fitNumber(lap["normalized_power_w"]); ok && normalized != 0 {
		if average, ok := fitNumber(lap["avg_power_w"]); ok && average > 0 {
			lap["variability_index"] = roundTo(normalized/average, 3)
		}
	}
	return lap
}

func curateRecord(message fitMessage) map[string]any {
	cadence := fitFieldValue(message, "cadence")
	grade := fitFieldValue(message, "grade")
	record := map[string]any{
		"timestamp":      pythonTimeString(fitFieldValue(message, "timestamp")),
		"power_w":        fitFieldValue(message, "power"),
		"cadence_rpm":    cadence,
		"heart_rate_bpm": fitFieldValue(message, "heart_rate"),
		"speed_mps":      fitFieldValue(message, "enhanced_speed", "speed"),
		"altitude_m":     fitFieldValue(message, "enhanced_altitude", "altitude"),
		"distance_m":     fitFieldValue(message, "distance"),
		"grade_pct":      grade,
		"temperature_c":  fitFieldValue(message, "temperature"),
		"lat_deg":        semicirclesToDegrees(fitFieldValue(message, "position_lat")),
		"lon_deg":        semicirclesToDegrees(fitFieldValue(message, "position_long")),

		"left_pco_mm":                    fitFieldValue(message, "left_pco"),
		"right_pco_mm":                   fitFieldValue(message, "right_pco"),
		"left_torque_effectiveness_pct":  fitFieldValue(message, "left_torque_effectiveness"),
		"right_torque_effectiveness_pct": fitFieldValue(message, "right_torque_effectiveness"),
		"left_pedal_smoothness_pct":      fitFieldValue(message, "left_pedal_smoothness"),
		"right_pedal_smoothness_pct":     fitFieldValue(message, "right_pedal_smoothness"),
	}
	if left := decodeLeftRightBalance(fitFieldValue(message, "left_right_balance"), true); left != nil {
		record["left_power_pct"] = *left
		record["right_power_pct"] = roundTo(100-*left, 1)
	}
	for _, side := range []string{"left", "right"} {
		phase := fitFieldValue(message, side+"_power_phase")
		values := fitSliceNumbers(phase)
		if len(values) > 1 {
			record[side+"_power_phase_start_deg"] = values[0]
			record[side+"_power_phase_end_deg"] = values[1]
		}
	}
	pruneNil(record)
	return record
}

// --- main FIT parsing -------------------------------------------------------

func primarySessionIndex(sessions []map[string]any) int {
	cycling := []int{}
	for i, session := range sessions {
		if session["sport"] == "cycling" {
			cycling = append(cycling, i)
		}
	}
	if len(cycling) == 1 {
		return cycling[0]
	}
	best := 0
	bestTime := -1.0
	for i, session := range sessions {
		elapsed, _ := fitNumber(session["total_elapsed_time_s"])
		if elapsed > bestTime {
			best, bestTime = i, elapsed
		}
	}
	return best
}

func recordsInSession(records []map[string]any, session map[string]any) []map[string]any {
	start, ok := parseFITTimestamp(session["start_time"])
	elapsed, okElapsed := fitNumber(session["total_elapsed_time_s"])
	if !ok || !okElapsed || elapsed == 0 {
		return nil
	}
	end := start.Add(time.Duration(elapsed * float64(time.Second)))
	times := make([]time.Time, len(records))
	for i, record := range records {
		parsed, ok := parseFITTimestamp(record["timestamp"])
		if !ok {
			return nil
		}
		times[i] = parsed
	}
	out := []map[string]any{}
	for i, record := range records {
		if !times[i].Before(start) && times[i].Before(end) {
			out = append(out, record)
		}
	}
	return out
}

func parseFIT(raw []byte, includeRecords bool) (map[string]any, error) {
	messages, err := decodeFIT(raw)
	if err != nil {
		return nil, err
	}

	sessions := []map[string]any{}
	laps := []map[string]any{}
	shifts := []map[string]any{}
	shiftRecordCounts := []int{}
	records := []map[string]any{}
	type rrPair struct {
		timestamp any
		rr        float64
	}
	rrPairs := []rrPair{}
	var lastRecordTimestamp any
	var lastCadence any
	var lastGrade any

	for _, message := range messages {
		switch message.Name {
		case "session":
			sessions = append(sessions, curateSession(message))
		case "lap":
			laps = append(laps, curateLap(message, len(laps)+1))
		case "event":
			eventType, _ := fitFieldValue(message, "event").(string)
			if eventType != "rear_gear_change" && eventType != "front_gear_change" && eventType != "gear_change" {
				continue
			}
			gearRaw := fitFieldValue(message, "gear_change_data", "data")
			timestamp := fitFieldValue(message, "timestamp")
			shift := map[string]any{
				"timestamp":            pythonTimeString(timestamp),
				"event":                eventType,
				"cadence_at_shift_rpm": lastCadence,
			}
			if lastGrade != nil {
				if grade, ok := fitNumber(lastGrade); ok {
					shift["grade_at_shift_pct"] = roundTo(grade, 1)
				}
			}
			if gearRaw != nil {
				if gearNumber, ok := fitNumber(gearRaw); ok {
					decoded := decodeGearChange(uint32(int64(gearNumber)))
					for key, value := range decoded {
						shift[key] = value
					}
					frontTeeth, _ := fitNumber(decoded["front_teeth"])
					rearTeeth, _ := fitNumber(decoded["rear_teeth"])
					if frontTeeth != 0 && rearTeeth != 0 {
						shift["gear_combo"] = fmt.Sprintf("%d/%dt", int(frontTeeth), int(rearTeeth))
					}
				} else {
					shift["gear_change_data_raw"] = fmt.Sprint(gearRaw)
				}
			}
			if lastCadence != nil {
				if cadence, ok := fitNumber(lastCadence); ok {
					switch {
					case cadence == 0:
						shift["quality"] = "coasting"
					case cadence < 70:
						shift["quality"] = "reactive"
					case cadence > 100:
						shift["quality"] = "spun_out"
					default:
						shift["quality"] = "proactive"
					}
				} else {
					shift["quality"] = "unknown"
				}
			} else {
				shift["quality"] = "unknown"
			}
			pruneNil(shift)
			shifts = append(shifts, shift)
			shiftRecordCounts = append(shiftRecordCounts, len(records))
		case "record":
			if cadence := fitFieldValue(message, "cadence"); cadence != nil {
				lastCadence = cadence
			}
			if grade := fitFieldValue(message, "grade"); grade != nil {
				lastGrade = grade
			}
			if timestamp := fitFieldValue(message, "timestamp"); timestamp != nil {
				lastRecordTimestamp = timestamp
			}
			records = append(records, curateRecord(message))
		case "hrv":
			for _, rr := range fitSliceNumbers(fitFieldValue(message, "time")) {
				if rr > 0.2 && rr < 3.0 {
					rrPairs = append(rrPairs, rrPair{timestamp: lastRecordTimestamp, rr: rr})
				}
			}
		}
	}

	multisport := len(sessions) > 1
	analysisRecords := records
	session := map[string]any{}
	analyzedIndex := 0
	analysisNote := ""
	if multisport {
		analyzedIndex = primarySessionIndex(sessions)
		session = map[string]any{}
		for key, value := range sessions[analyzedIndex] {
			session[key] = value
		}
		sport, _ := session["sport"].(string)
		if sport == "" {
			sport = "unknown"
		}
		leg := fmt.Sprintf("leg %d (%s)", analyzedIndex, sport)
		if filtered := recordsInSession(records, session); filtered != nil {
			analysisRecords = filtered
			analysisNote = fmt.Sprintf("Multisport file: session and record-based analyses (power curve, climbs, grade, HR drift, temperature) cover %s only. See sessions for every leg.", leg)
		} else {
			analysisRecords = records
			analysisNote = fmt.Sprintf("Multisport file: session summarises %s, but record timestamps or the session window are missing, so record-based analyses cover all legs.", leg)
		}
	} else if len(sessions) > 0 {
		session = sessions[0]
	}

	gradeSource := ""
	if len(records) > 0 {
		gradeSource, _ = deriveGrades(records)
	}
	if gradeSource == "derived" {
		fillShiftGrades(shifts, shiftRecordCounts, records)
	}

	shiftSummary := computeShiftSummary(shifts)

	var gradeStats map[string]any
	var hrDrift map[string]any
	var tempStats map[string]any
	climbs := []map[string]any{}
	var pdc map[string]any
	if len(analysisRecords) > 0 {
		gradeStats = gradeAnalysis(analysisRecords)
		hrDrift = computeHRDrift(analysisRecords)
		tempStats = computeTemperatureStats(analysisRecords)
		climbs = detectClimbs(analysisRecords, 50, 3, 120)
		pdc = computePowerDurationCurve(analysisRecords)
	}

	if gradeStats != nil {
		session["grade_analysis"] = gradeStats
	}
	if hrDrift != nil {
		session["hr_drift"] = hrDrift
	}
	if tempStats != nil {
		session["temperature_stats"] = tempStats
	}

	result := map[string]any{
		"session":       session,
		"laps":          laps,
		"shift_summary": shiftSummary,
		"shifts":        shifts,
	}
	if multisport {
		result["multisport"] = true
		result["sessions"] = sessions
		result["analyzed_session_index"] = analyzedIndex
		result["analysis_note"] = analysisNote
	}
	if len(climbs) > 0 {
		result["climbs"] = climbs
	}
	if pdc != nil {
		result["power_duration_curve"] = pdc
	}
	if gradeSource != "" {
		result["grade_source"] = gradeSource
	}

	fillMissingLapPower(records, laps)

	if len(rrPairs) > 0 {
		allRR := make([]float64, len(rrPairs))
		for i, pair := range rrPairs {
			allRR[i] = pair.rr
		}
		if hrv := computeHRVMetrics(allRR); hrv != nil {
			result["hrv"] = hrv
		}

		for _, lap := range laps {
			start, ok := parseFITTimestamp(lap["start_time"])
			elapsed, okElapsed := fitNumber(lap["total_elapsed_time_s"])
			if !ok || !okElapsed || elapsed == 0 {
				continue
			}
			end := start.Add(time.Duration(elapsed * float64(time.Second)))
			lapRR := []float64{}
			for _, pair := range rrPairs {
				timestamp, ok := parseFITTimestamp(pair.timestamp)
				if !ok {
					continue
				}
				if !timestamp.Before(start) && timestamp.Before(end) {
					lapRR = append(lapRR, pair.rr)
				}
			}
			if lapHRV := computeHRVMetrics(lapRR); lapHRV != nil {
				lap["hrv"] = lapHRV
			}
		}

		if includeRecords {
			intervals := make([]any, 0, len(rrPairs))
			for _, pair := range rrPairs {
				var timestamp any
				if text := pythonTimeString(pair.timestamp); text != "" {
					timestamp = text
				}
				intervals = append(intervals, map[string]any{
					"timestamp":  timestamp,
					"rr_seconds": pair.rr,
				})
			}
			result["rr_intervals_seconds"] = intervals
		}
	}

	if includeRecords {
		result["records"] = records
	}
	return result, nil
}

func fillMissingLapPower(records []map[string]any, laps []map[string]any) {
	if len(records) == 0 {
		return
	}
	missing := false
	for _, lap := range laps {
		if _, ok := lap["avg_power_w"]; !ok {
			missing = true
			break
		}
	}
	if !missing {
		return
	}
	type recordPower struct {
		timestamp time.Time
		power     float64
	}
	powers := []recordPower{}
	for _, record := range records {
		power, ok := fitNumber(record["power_w"])
		if !ok {
			continue
		}
		timestamp, ok := parseFITTimestamp(record["timestamp"])
		if !ok {
			continue
		}
		powers = append(powers, recordPower{timestamp: timestamp, power: power})
	}
	if len(powers) == 0 {
		return
	}
	for _, lap := range laps {
		if _, ok := lap["avg_power_w"]; ok {
			continue
		}
		start, ok := parseFITTimestamp(lap["start_time"])
		elapsed, okElapsed := fitNumber(lap["total_elapsed_time_s"])
		if !ok || !okElapsed || elapsed == 0 {
			continue
		}
		end := start.Add(time.Duration(elapsed * float64(time.Second)))
		values := []any{}
		maxPower := 0.0
		for _, power := range powers {
			if !power.timestamp.Before(start) && power.timestamp.Before(end) {
				values = append(values, power.power)
				if power.power > maxPower {
					maxPower = power.power
				}
			}
		}
		if len(values) > 0 {
			if avg, ok := safeAvg(values); ok {
				lap["avg_power_w"] = int(math.Round(avg))
			}
			lap["max_power_w"] = int(math.Round(maxPower))
		}
	}
}

func safeAvg(values []any) (float64, bool) {
	sum := 0.0
	count := 0
	for _, value := range values {
		if value == nil {
			continue
		}
		number, ok := fitNumber(value)
		if !ok {
			continue
		}
		sum += number
		count++
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}

func maxNumber(values []any) (float64, bool) {
	best := 0.0
	found := false
	for _, value := range values {
		number, ok := fitNumber(value)
		if !ok {
			continue
		}
		if !found || number > best {
			best, found = number, true
		}
	}
	return best, found
}

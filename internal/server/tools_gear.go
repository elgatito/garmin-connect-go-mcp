package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const getGearDescription = `Get all gear registered with the user account

Returns complete gear inventory including free-text notes, usage statistics,
and default activity associations. The notes field is null when unavailable.
defaults maps each activity type (Garmin typeKey, e.g. "running") to the
names of its default gear.
A gear item's stats_error reports a failed usage-statistics request;
a null total_distance_km means Garmin did not provide a distance.
No parameters required - user profile is fetched automatically.`

const addGearToActivityDescription = `Associate gear with an activity

Links a specific piece of gear (like shoes, bike, etc.) to an activity.`

const removeGearFromActivityDescription = `Remove gear association from an activity

Unlinks a specific piece of gear from an activity.`

// activityTypeMapping maps Garmin activity typeId to typeKey for the types gear
// is usually a default for, as returned by get_activity_types.
var activityTypeMapping = map[int64]string{
	1:   "running",
	2:   "cycling",
	3:   "hiking",
	4:   "other",
	5:   "mountain_biking",
	6:   "trail_running",
	7:   "street_running",
	8:   "track_running",
	9:   "walking",
	10:  "road_biking",
	13:  "strength_training",
	15:  "casual_walking",
	16:  "speed_walking",
	18:  "treadmill_running",
	19:  "cyclocross",
	20:  "downhill_biking",
	21:  "track_cycling",
	22:  "recumbent_cycling",
	25:  "indoor_cycling",
	26:  "swimming",
	27:  "lap_swimming",
	28:  "open_water_swimming",
	143: "gravel_cycling",
	152: "virtual_ride",
	153: "virtual_run",
	154: "obstacle_run",
	156: "indoor_running",
	175: "e_bike_mountain",
	176: "e_bike_fitness",
	181: "ultra_run",
}

type getGearInput struct {
	Account
	IncludeStats *bool `json:"include_stats,omitempty" jsonschema:"Include usage statistics for each gear item (default True). Set to False for faster response with large gear collections."`
}

type addGearInput struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"ID of the activity"`
	GearUUID   string `json:"gear_uuid" jsonschema:"UUID of the gear to add (get from get_gear)"`
}

type removeGearInput struct {
	Account
	ActivityID any    `json:"activity_id" jsonschema:"ID of the activity"`
	GearUUID   string `json:"gear_uuid" jsonschema:"UUID of the gear to remove"`
}

func registerGearTools(r *registry) {
	addTool(r, "get_gear", getGearDescription, getGear)
	addTool(r, "add_gear_to_activity", addGearToActivityDescription, addGearToActivity)
	addTool(r, "remove_gear_from_activity", removeGearFromActivityDescription, removeGearFromActivity)
}

// --- get_gear ---

func getGear(ctx context.Context, s *Session, in getGearInput) (any, error) {
	includeStats := true
	if in.IncludeStats != nil {
		includeStats = *in.IncludeStats
	}

	device, err := s.Client.GetDeviceLastUsed(ctx)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving gear: %w", err)
	}
	if !truthy(device) {
		return "Could not retrieve user profile. Please ensure you have a synced device.", nil
	}
	profileID := asMap(device)["userProfileNumber"]

	gearList, err := s.Client.GetGear(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving gear: %w", err)
	}
	if !truthy(gearList) {
		return "No gear found.", nil
	}

	// Notes are only exposed by Garmin's v2 gear endpoint. Its UUIDs are
	// hyphenated, unlike the legacy endpoint used for the existing fields.
	notesByUUID := map[string]any{}
	v2List, err := s.Client.GetGearV2List(ctx)
	if err != nil {
		fmt.Fprintf(s.logWriter(), "Garmin v2 gear list unavailable: %v\n", err)
	} else if v2, ok := v2List.([]any); !ok {
		fmt.Fprintf(s.logWriter(), "Unexpected Garmin v2 gear response type: %T\n", v2List)
	} else {
		for _, item := range v2 {
			gearV2 := asMap(item)
			if gearV2 == nil {
				continue
			}
			if normalized := normalizeGearUUID(gearV2["uuid"]); normalized != "" {
				notesByUUID[normalized] = gearV2["notes"]
			}
		}
	}

	defaultsList, err := s.Client.GetGearDefaults(ctx, profileID)
	if err != nil {
		return nil, fmt.Errorf("Error retrieving gear: %w", err)
	}
	defaultsByUUID := map[string][]string{}
	defaultsOrder := []string{}
	for _, item := range asList(defaultsList) {
		d := asMap(item)
		uuid, _ := d["uuid"].(string)
		if uuid == "" {
			continue
		}
		activityType := gearActivityType(d["activityTypePk"])
		if _, exists := defaultsByUUID[uuid]; !exists {
			defaultsOrder = append(defaultsOrder, uuid)
		}
		defaultsByUUID[uuid] = append(defaultsByUUID[uuid], activityType)
	}

	curatedGear := []any{}
	activeCount, retiredCount := 0, 0
	for _, item := range asList(gearList) {
		g := asMap(item)
		uuid, _ := g["uuid"].(string)
		status := strings.ToLower(stringValue(g["gearStatusName"]))

		if status == "active" {
			activeCount++
		} else if status == "retired" {
			retiredCount++
		}

		notes := g["notes"]
		if _, exists := notesByUUID[normalizeGearUUID(g["uuid"])]; exists && uuid != "" {
			notes = notesByUUID[normalizeGearUUID(g["uuid"])]
		}
		gearItem := map[string]any{
			"uuid":       uuid,
			"name":       g["displayName"],
			"full_name":  g["customMakeModel"],
			"type":       g["gearTypeName"],
			"status":     status,
			"date_begin": parseISODate(g["dateBegin"]),
			"date_end":   parseISODate(g["dateEnd"]),
			"notes":      notes,
		}

		if maxMeters, ok := numberOf(g["maximumMeters"]); ok && maxMeters > 0 {
			gearItem["max_distance_km"] = roundTo(maxMeters/1000, 1)
		}

		if activities, ok := defaultsByUUID[uuid]; ok {
			gearItem["is_default_for"] = activities
		}

		if includeStats {
			stats, err := s.Client.GetGearStats(ctx, uuid)
			if err != nil {
				gearItem["stats_error"] = err.Error()
			} else if truthy(stats) {
				statsMap := asMap(stats)
				var distance any
				if d, ok := numberOf(statsMap["totalDistance"]); ok {
					distance = roundTo(d/1000, 1)
				}
				gearItem["stats"] = map[string]any{
					"total_activities":  statsMap["totalActivities"],
					"total_distance_km": distance,
				}
			}
		}

		curatedGear = append(curatedGear, gearItem)
	}

	sort.SliceStable(curatedGear, func(i, j int) bool {
		a := asMap(curatedGear[i])
		b := asMap(curatedGear[j])
		aActive := a["status"] == "active"
		bActive := b["status"] == "active"
		if aActive != bActive {
			return aActive
		}
		return stringValue(a["date_begin"]) > stringValue(b["date_begin"])
	})

	gearByUUID := map[string]map[string]any{}
	for _, item := range curatedGear {
		g := asMap(item)
		gearByUUID[stringValue(g["uuid"])] = g
	}
	defaultsSummary := map[string]any{}
	for _, uuid := range defaultsOrder {
		gear := gearByUUID[uuid]
		name := stringValue(gear["name"])
		if name == "" {
			name = stringValue(gear["full_name"])
		}
		if name == "" {
			name = uuid
		}
		for _, activity := range defaultsByUUID[uuid] {
			list, _ := defaultsSummary[activity].([]string)
			defaultsSummary[activity] = append(list, name)
		}
	}

	return map[string]any{
		"gear_count":    len(curatedGear),
		"active_count":  activeCount,
		"retired_count": retiredCount,
		"defaults":      defaultsSummary,
		"gear":          curatedGear,
	}, nil
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func parseISODate(v any) any {
	s := stringValue(v)
	if s == "" {
		return nil
	}
	if i := strings.Index(s, "T"); i >= 0 {
		return s[:i]
	}
	return s
}

func normalizeGearUUID(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}

func gearActivityType(v any) string {
	if f, ok := numberOf(v); ok && f == float64(int64(f)) {
		if name, ok := activityTypeMapping[int64(f)]; ok {
			return name
		}
		return fmt.Sprintf("activity_%d", int64(f))
	}
	return fmt.Sprintf("activity_%v", v)
}

// --- add_gear_to_activity / remove_gear_from_activity ---

func isMissingGear(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "gear") &&
		(strings.Contains(msg, "doesn't exist") || strings.Contains(msg, "not found") || strings.Contains(msg, "404"))
}

func missingGearMessage(gearUUID string, id int64) string {
	return fmt.Sprintf("No gear association changed; gear %s was not found for activity %d.", gearUUID, id)
}

func addGearToActivity(ctx context.Context, s *Session, in addGearInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error adding gear to activity")
	if err != nil {
		return nil, err
	}
	tracker := newWriteTracker("add_gear_to_activity", s.logWriter()).
		withPrefix("Error adding gear to activity")
	tracker.Sent()
	if _, err := s.Client.AddGearToActivity(ctx, in.GearUUID, id); err != nil {
		if isMissingGear(err) {
			return missingGearMessage(in.GearUUID, id), nil
		}
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"gear_uuid":   in.GearUUID,
		"message":     "Gear successfully added to activity",
	}, nil
}

func removeGearFromActivity(ctx context.Context, s *Session, in removeGearInput) (any, error) {
	id, err := activityIDOrErr(in.ActivityID, "Error removing gear from activity")
	if err != nil {
		return nil, err
	}
	tracker := newWriteTracker("remove_gear_from_activity", s.logWriter()).
		withPrefix("Error removing gear from activity")
	tracker.Sent()
	if _, err := s.Client.RemoveGearFromActivity(ctx, in.GearUUID, id); err != nil {
		if isMissingGear(err) {
			return missingGearMessage(in.GearUUID, id), nil
		}
		return nil, tracker.Fail(err)
	}
	tracker.Committed()
	return map[string]any{
		"success":     true,
		"activity_id": id,
		"gear_uuid":   in.GearUUID,
		"message":     "Gear successfully removed from activity",
	}, nil
}

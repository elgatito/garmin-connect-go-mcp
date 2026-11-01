package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/gear_management.py.

// activityTypeMapping maps Garmin activity typeIds to typeKeys for the types gear is usually a default for.
// Any other typeId is reported as activity_<typeId>.
var activityTypeMapping = map[int]string{
	1: "running", 2: "cycling", 3: "hiking", 4: "other", 5: "mountain_biking", 6: "trail_running", 7: "street_running",
	8: "track_running", 9: "walking", 10: "road_biking", 13: "strength_training", 15: "casual_walking", 16: "speed_walking",
	18: "treadmill_running", 19: "cyclocross", 20: "downhill_biking", 21: "track_cycling", 22: "recumbent_cycling",
	25: "indoor_cycling", 26: "swimming", 27: "lap_swimming", 28: "open_water_swimming", 143: "gravel_cycling",
	152: "virtual_ride", 153: "virtual_run", 154: "obstacle_run", 156: "indoor_running", 175: "e_bike_mountain",
	176: "e_bike_fitness", 181: "ultra_run",
}

const getGearDesc = `Get all gear registered with the user account

Returns complete gear inventory including free-text notes, usage statistics,
and default activity associations. The notes field is null when unavailable.
defaults maps each activity type (Garmin typeKey, e.g. "running") to the
names of its default gear.
A gear item's stats_error reports a failed usage-statistics request;
a null total_distance_km means Garmin did not provide a distance.
No parameters required - user profile is fetched automatically.`

type getGearIn struct {
	Account
	IncludeStats *bool `json:"include_stats,omitempty" jsonschema:"Include usage statistics for each gear item (default True). Set to False for faster response with large gear collections."`
}

type gearLinkIn struct {
	Account
	ActivityID int64  `json:"activity_id" jsonschema:"ID of the activity"`
	GearUUID   string `json:"gear_uuid" jsonschema:"UUID of the gear to add (get from get_gear)"`
}

type gearUnlinkIn struct {
	Account
	ActivityID int64  `json:"activity_id" jsonschema:"ID of the activity"`
	GearUUID   string `json:"gear_uuid" jsonschema:"UUID of the gear to remove"`
}

// parseISODateOnly is _parse_iso_date: the date part of an optional ISO datetime.
func parseISODateOnly(v any) any {
	if falsy(v) {
		return nil
	}
	s := pyStr(v)
	if i := strings.Index(s, "T"); i >= 0 {
		return s[:i]
	}
	return s
}

// normalizeGearUUID makes the hyphenated (v2) and plain (legacy) forms comparable; "" means none.
func normalizeGearUUID(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}

// isMissingGear is _is_missing_gear.
func isMissingGear(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "gear") && (strings.Contains(m, "doesn't exist") || strings.Contains(m, "not found") || strings.Contains(m, "404"))
}

func registerGearTools(r *registry) {
	addTool(r, "get_gear", getGearDesc, getGear)
	addTool(r, "add_gear_to_activity", "Associate gear with an activity\n\nLinks a specific piece of gear (like shoes, bike, etc.) to an activity.",
		func(ctx context.Context, s *Session, in gearLinkIn) (any, error) {
			return linkGear(ctx, s, in.ActivityID, in.GearUUID, s.Client.AddGearToActivity, "added to", "adding gear to")
		})
	addTool(r, "remove_gear_from_activity", "Remove gear association from an activity\n\nUnlinks a specific piece of gear from an activity.",
		func(ctx context.Context, s *Session, in gearUnlinkIn) (any, error) {
			return linkGear(ctx, s, in.ActivityID, in.GearUUID, s.Client.RemoveGearFromActivity, "removed from", "removing gear from")
		})
}

func linkGear(ctx context.Context, s *Session, activityID int64, gearUUID string,
	call func(ctx context.Context, gearUUID string, id int64) (any, error), done, doing string) (any, error) {
	if _, err := call(ctx, gearUUID, activityID); err != nil {
		if isMissingGear(err) {
			return fmt.Sprintf("No gear association changed; gear %s was not found for activity %d.", gearUUID, activityID), nil
		}
		return nil, fmt.Errorf("Error %s activity: %w", doing, err)
	}
	return map[string]any{
		"success": true, "activity_id": activityID, "gear_uuid": gearUUID, "message": "Gear successfully " + done + " activity",
	}, nil
}

type gearEntry struct {
	item   map[string]any
	active bool
	begin  string
}

func getGear(ctx context.Context, s *Session, in getGearIn) (any, error) {
	includeStats := in.IncludeStats == nil || *in.IncludeStats
	fail := func(err error) (any, error) { return nil, fmt.Errorf("Error retrieving gear: %w", err) }

	// 1. user_profile_id from the last used device
	device, err := s.Client.GetDeviceLastUsed(ctx)
	if err != nil {
		return fail(err)
	}
	if falsy(device) {
		return "Could not retrieve user profile. Please ensure you have a synced device.", nil
	}
	profileID := idString(asMap(device)["userProfileNumber"])

	// 2. all gear
	gearData, err := s.Client.GetGear(ctx, profileID)
	if err != nil {
		return fail(err)
	}
	if falsy(gearData) {
		return "No gear found.", nil
	}

	// Notes are only exposed by Garmin's v2 gear endpoint. Its UUIDs are hyphenated, unlike the legacy endpoint.
	notesByUUID := map[string]any{}
	if v2, err := s.Client.GetGearV2List(ctx); err != nil {
		fmt.Fprintf(s.Log, "gear_management: Garmin v2 gear list unavailable: %v\n", err)
	} else {
		for _, g := range asList(v2) {
			if m := asMap(g); m != nil {
				if u := normalizeGearUUID(m["uuid"]); u != "" {
					notesByUUID[u] = m["notes"]
				}
			}
		}
	}

	// 3. defaults map gear -> activity types
	defaultsData, err := s.Client.GetGearDefaults(ctx, profileID)
	if err != nil {
		return fail(err)
	}
	defaultsByUUID := map[string][]string{}
	var defaultsOrder []string
	for _, d := range asList(defaultsData) {
		dm := asMap(d)
		uuid, _ := dm["uuid"].(string)
		activityType := "activity_" + idString(dm["activityTypePk"])
		if pk, ok := asNumber(dm["activityTypePk"]); ok {
			if name, found := activityTypeMapping[int(pk)]; found && pk == float64(int(pk)) {
				activityType = name
			}
		}
		if _, seen := defaultsByUUID[uuid]; !seen {
			defaultsOrder = append(defaultsOrder, uuid)
		}
		defaultsByUUID[uuid] = append(defaultsByUUID[uuid], activityType)
	}

	// 4. curated gear list
	var entries []gearEntry
	activeCount, retiredCount := 0, 0
	for _, gd := range asList(gearData) {
		g := asMap(gd)
		uuid, _ := g["uuid"].(string)
		status, _ := getDefault(g, "gearStatusName", "").(string)
		status = strings.ToLower(status)
		switch status {
		case "active":
			activeCount++
		case "retired":
			retiredCount++
		}

		notes := g["notes"]
		if key := normalizeGearUUID(g["uuid"]); key != "" {
			if n, ok := notesByUUID[key]; ok {
				notes = n
			}
		}
		item := map[string]any{
			"uuid": g["uuid"], "name": g["displayName"], "full_name": g["customMakeModel"], "type": g["gearTypeName"],
			"status": status, "date_begin": parseISODateOnly(g["dateBegin"]), "date_end": parseISODateOnly(g["dateEnd"]), "notes": notes,
		}
		if maxMeters, ok := asNumber(g["maximumMeters"]); ok && maxMeters > 0 {
			item["max_distance_km"] = round(maxMeters/1000, 1)
		}
		if is, ok := defaultsByUUID[uuid]; ok {
			item["is_default_for"] = is
		}

		// 5. stats if requested
		if includeStats {
			stats, err := s.Client.GetGearStats(ctx, gearUUIDArg(g["uuid"]))
			switch {
			case errors.Is(err, garmin.ErrSessionExpired):
				return nil, err
			case err != nil:
				item["stats_error"] = err.Error()
			case !falsy(stats):
				sm := asMap(stats)
				var km any
				if d, ok := asNumber(sm["totalDistance"]); ok {
					km = round(d/1000, 1)
				}
				item["stats"] = map[string]any{"total_activities": sm["totalActivities"], "total_distance_km": km}
			}
		}
		begin, _ := item["date_begin"].(string)
		entries = append(entries, gearEntry{item, status == "active", begin})
	}

	// Active gear first, then by date_begin descending.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].active != entries[j].active {
			return entries[i].active
		}
		return entries[i].begin > entries[j].begin
	})
	curated := make([]any, len(entries))
	for i, e := range entries {
		curated[i] = e.item
	}

	// activity -> gear names (an activity type can have several default gear items, and displayName is null for
	// gear without a nickname)
	defaultsSummary := map[string][]string{}
	for _, uuid := range defaultsOrder {
		var gear map[string]any
		for _, e := range entries {
			if u, _ := e.item["uuid"].(string); u == uuid {
				gear = e.item
				break
			}
		}
		name := pyStr(firstTruthy(firstTruthy(gear["name"], gear["full_name"]), uuid))
		for _, activity := range defaultsByUUID[uuid] {
			defaultsSummary[activity] = append(defaultsSummary[activity], name)
		}
	}

	return map[string]any{
		"gear_count": len(curated), "active_count": activeCount, "retired_count": retiredCount,
		"defaults": defaultsSummary, "gear": curated,
	}, nil
}

// gearUUIDArg is str(uuid) for the URL of a gear request.
func gearUUIDArg(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "None"
}

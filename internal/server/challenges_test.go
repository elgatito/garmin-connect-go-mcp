package server

import "testing"

func TestBadgeChallengeProgress(t *testing.T) {
	out := badgeChallenge(map[string]any{"challengeCategoryId": float64(1), "badgeChallengeStatusId": float64(2), "badgeUnitId": float64(1), "badgeProgressValue": float64(2000), "badgeTargetValue": float64(5000)})
	if out["category"] != "Running" || out["progress"] != "2.00 km" || out["progress_percent"] != "40.0%" {
		t.Fatal(out)
	}
}
func TestEarnedBadgesOrdering(t *testing.T) {
	got := earnedBadges([]any{map[string]any{"badgeName": "old", "badgeEarnedDate": "2020-01-01T01:00"}, map[string]any{"badgeName": "new", "badgeEarnedDate": "2021-01-01T01:00"}})
	rows := got["badges"].([]map[string]any)
	if rows[0]["name"] != "new" {
		t.Fatal(got)
	}
}

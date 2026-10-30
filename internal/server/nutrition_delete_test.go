package server

import "testing"

func TestFindFoodLogEntry(t *testing.T) {
	entry := map[string]any{"logId": "abc"}
	v := map[string]any{"meals": []any{map[string]any{"foods": []any{entry}}}}
	if got := findLogEntry(v, "abc"); got["logId"] != "abc" {
		t.Fatal(got)
	}
	if findLogEntry(v, "missing") != nil {
		t.Fatal("unexpected match")
	}
}

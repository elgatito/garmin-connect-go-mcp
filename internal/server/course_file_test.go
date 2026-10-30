package server

import (
	"strings"
	"testing"
)

func TestCourseGPXEscapesText(t *testing.T) {
	b, _, _, _ := buildGPX(1, map[string]any{"courseName": "A & B", "geoPoints": []any{map[string]any{"latitude": float64(0.000001), "longitude": float64(12)}}})
	if !strings.Contains(string(b), "A &amp; B") || !strings.Contains(string(b), "lat='0.000001'") {
		t.Fatal(string(b))
	}
}

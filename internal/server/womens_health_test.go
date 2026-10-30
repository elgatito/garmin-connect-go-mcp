package server

import (
	"reflect"
	"testing"
)

func TestStitchMenstrual(t *testing.T) {
	tests := []struct {
		chunks []any
		want   any
	}{{[]any{[]any{float64(1), float64(2)}, []any{float64(2), float64(3)}}, []any{float64(1), float64(2), float64(3)}}, {[]any{map[string]any{"data": []any{float64(1)}}, map[string]any{"data": []any{float64(1), float64(2)}}}, map[string][]any{"data": {float64(1), float64(2)}}}}
	for _, tc := range tests {
		if got := stitchMenstrual(tc.chunks); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("got %#v want %#v", got, tc.want)
		}
	}
}

package server

import (
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"testing"
)

func TestCustomFoodUnsupportedRequiresMatching403(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{{&garmin.APIError{Status: 403, Message: "Custom foods not supported in this region"}, true}, {&garmin.APIError{Status: 403, Message: "Access denied"}, false}, {&garmin.APIError{Status: 500, Message: "custom food unsupported"}, false}} {
		if got := customFoodUnsupported(tc.err); got != tc.want {
			t.Fatalf("%v: got %v", tc.err, got)
		}
	}
}

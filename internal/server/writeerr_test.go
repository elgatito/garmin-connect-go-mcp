package server

import (
	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
	"io"
	"testing"
)

func TestWriteOutcome(t *testing.T) {
	for _, tc := range []struct {
		status int
		sent   bool
		want   any
	}{{400, true, false}, {503, true, "unknown"}, {503, false, false}} {
		tr := newWriteTracker("test", "failed", io.Discard)
		if tc.sent {
			tr.Sent()
		}
		failure := tr.Fail(&garmin.APIError{Status: tc.status}).(*writeFailure)
		if failure.payload["write_committed"] != tc.want || failure.payload["upstream_status"] != tc.status {
			t.Fatal(failure.payload)
		}
	}
}

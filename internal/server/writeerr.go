package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

type writeFailure struct{ payload map[string]any }

func (e *writeFailure) Error() string { return fmt.Sprint(e.payload["message"]) }

type writeTracker struct {
	tool, prefix    string
	log             io.Writer
	sent, committed bool
}

func newWriteTracker(tool, prefix string, log io.Writer) *writeTracker {
	return &writeTracker{tool: tool, prefix: prefix, log: log}
}
func (t *writeTracker) Sent()      { t.sent = true }
func (t *writeTracker) Committed() { t.committed = true }
func (t *writeTracker) Fail(err error) error {
	var raw [6]byte
	if _, e := rand.Read(raw[:]); e != nil {
		return e
	}
	id := hex.EncodeToString(raw[:])
	stage := "internal"
	var committed any = false
	var status any
	var body any
	var api *garmin.APIError
	var ne net.Error
	switch {
	case t.committed:
		stage = "response_parse"
		committed = true
	case errors.Is(err, garmin.ErrSessionExpired):
		stage = "auth_refresh"
	case errors.As(err, &api):
		stage = "garmin_request"
		status = api.Status
		if api.Message != "" {
			body = string([]rune(api.Message)[:min(500, len([]rune(api.Message)))])
		}
		if t.sent && api.Status >= 500 {
			committed = "unknown"
		}
	case errors.As(err, &ne), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		stage = "garmin_request"
		if t.sent {
			committed = "unknown"
		}
	default:
		if t.sent {
			committed = "unknown"
		}
	}
	payload := map[string]any{"status": "error", "message": t.prefix + ": " + err.Error(), "tool": t.tool, "stage": stage, "error_type": fmt.Sprintf("%T", err), "upstream_status": status, "upstream_body": body, "write_committed": committed, "correlation_id": id}
	if committed == "unknown" {
		payload["hint"] = "The request may have reached Garmin before failing. Check whether the change was applied before retrying, to avoid a duplicate."
	} else if committed == false {
		payload["hint"] = "Nothing was written, so a retry cannot create a duplicate."
	}
	fmt.Fprintf(t.log, "%s failed [correlation_id=%s stage=%s write_committed=%v]: %s\n", t.tool, id, stage, committed, strings.ReplaceAll(err.Error(), "\n", " "))
	return &writeFailure{payload}
}

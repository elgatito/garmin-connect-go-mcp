package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

const maxUpstreamBodyChars = 500

// writeFailure is a structured failed-write result rendered as a tool error.
type writeFailure struct {
	payload map[string]any
}

func (e *writeFailure) Error() string {
	msg, _ := e.payload["message"].(string)
	return msg
}

// writeTracker records whether a write request was sent or acknowledged so a
// failure can say whether it committed.
type writeTracker struct {
	tool   string
	prefix string
	log    io.Writer
	sent   bool
	done   bool
}

func newWriteTracker(tool string, log io.Writer) *writeTracker {
	return &writeTracker{tool: tool, prefix: "Error", log: log}
}

func (t *writeTracker) withPrefix(prefix string) *writeTracker {
	t.prefix = prefix
	return t
}

// Sent marks that the write request is about to go out.
func (t *writeTracker) Sent() { t.sent = true }

// Committed marks that Garmin accepted the write.
func (t *writeTracker) Committed() { t.done = true }

// Fail logs err with a correlation ID and returns a structured write failure.
func (t *writeTracker) Fail(err error) error {
	cid := correlationID()
	stage, committed := t.classify(err)
	status, body := upstreamError(err)
	logw := t.log
	if logw == nil {
		logw = io.Discard
	}
	fmt.Fprintf(logw,
		"%s failed [correlation_id=%s stage=%s error_type=%s upstream_status=%v write_committed=%v]: %v\n",
		t.tool, cid, stage, errorTypeName(err), status, committed, err)

	payload := map[string]any{
		"status":          "error",
		"message":         fmt.Sprintf("%s: %v", t.prefix, err),
		"tool":            t.tool,
		"stage":           stage,
		"error_type":      errorTypeName(err),
		"upstream_status": status,
		"upstream_body":   body,
		"write_committed": committed,
		"correlation_id":  cid,
	}
	switch committed {
	case "unknown":
		payload["hint"] = "The request may have reached Garmin before failing. Check whether the change was applied before retrying, to avoid a duplicate."
	case false:
		payload["hint"] = "Nothing was written, so a retry cannot create a duplicate."
	}
	return &writeFailure{payload: payload}
}

func (t *writeTracker) classify(err error) (string, any) {
	if t.done {
		return "response_parse", true
	}
	var api *garmin.APIError
	if errors.As(err, &api) {
		if !t.sent && api.Status == 401 {
			return "auth_refresh", false
		}
		if t.sent && api.Status >= 500 {
			return "garmin_request", "unknown"
		}
		return "garmin_request", false
	}
	if errors.Is(err, garmin.ErrSessionExpired) {
		return "auth_refresh", false
	}
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		return "response_parse", t.sent
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "garmin_request", false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "garmin_request", false
	}
	var netErr net.Error
	if errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "garmin_request", unknownIfSent(t.sent)
	}
	return "internal", unknownIfSent(t.sent)
}

func unknownIfSent(sent bool) any {
	if sent {
		return "unknown"
	}
	return false
}

// upstreamError extracts Garmin's HTTP status and message.
func upstreamError(err error) (any, any) {
	var api *garmin.APIError
	if !errors.As(err, &api) {
		return nil, nil
	}
	body := strings.TrimSpace(api.Message)
	if runes := []rune(body); len(runes) > maxUpstreamBodyChars {
		body = string(runes[:maxUpstreamBodyChars])
	}
	if body == "" {
		return api.Status, nil
	}
	return api.Status, body
}

func errorTypeName(err error) string {
	switch {
	case errors.Is(err, garmin.ErrSessionExpired):
		return "SessionExpired"
	case errors.Is(err, context.DeadlineExceeded):
		return "DeadlineExceeded"
	case errors.Is(err, context.Canceled):
		return "Canceled"
	}
	t := reflect.TypeOf(err)
	if t == nil {
		return ""
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() != "" {
		return t.Name()
	}
	return t.String()
}

func correlationID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "000000000000"
	}
	return hex.EncodeToString(b[:])
}

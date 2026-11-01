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
	"net/url"
	"strings"

	"github.com/elgatito/garmin-connect-go-mcp/internal/garmin"
)

// Port of garmin_mcp/write_errors.py: a failed write tells the caller whether it may have landed.

const maxBodyChars = 500

// writeFailure is a structured write-tool error; the registry renders payload as JSON with IsError.
// It must not unwrap to the cause: addTool would turn an ErrSessionExpired cause into the plain expired-session copy.
type writeFailure struct{ payload map[string]any }

func (w *writeFailure) Error() string { return fmt.Sprint(w.payload["message"]) }

type writeTracker struct {
	tool, prefix string
	log          io.Writer
	sent, done   bool
}

// newWriteTracker starts tracking one write call. prefix is the Python message prefix, e.g. "Error logging food".
func newWriteTracker(tool, prefix string, log io.Writer) *writeTracker {
	return &writeTracker{tool: tool, prefix: prefix, log: log}
}

// Sent marks that the write request is about to go out (Python: sending()).
func (t *writeTracker) Sent() { t.sent = true }

// Committed marks that Garmin accepted the write.
func (t *writeTracker) Committed() { t.done = true }

// upstream returns Garmin's status and trimmed message (nil when unknown or empty).
func upstream(err error) (status, body any) {
	var apiErr *garmin.APIError
	if !errors.As(err, &apiErr) {
		return nil, nil
	}
	if b := []rune(strings.TrimSpace(apiErr.Message)); len(b) > 0 {
		body = string(b[:min(len(b), maxBodyChars)])
	}
	return apiErr.Status, body
}

// classify returns (stage, write_committed, error_type); error_type uses the Python exception names.
func (t *writeTracker) classify(err error) (string, any, string) {
	var (
		apiErr *garmin.APIError
		urlErr *url.Error
		synErr *json.SyntaxError
		typErr *json.UnmarshalTypeError
	)
	unknownIfSent := func() any {
		if t.sent {
			return "unknown"
		}
		return false
	}
	errType := strings.TrimPrefix(fmt.Sprintf("%T", err), "*")
	stage, committed := "internal", unknownIfSent()
	switch {
	case errors.Is(err, garmin.ErrSessionExpired):
		stage, committed, errType = "auth_refresh", false, "GarminConnectAuthenticationError" // refresh runs before the request
	case errors.As(err, &apiErr):
		stage, committed, errType = "garmin_request", false, "GarminConnectConnectionError"
		if apiErr.Status == 429 {
			errType = "GarminConnectTooManyRequestsError"
		}
		if t.sent && apiErr.Status >= 500 {
			committed = "unknown" // a 5xx can come back after the backend applied the write
		}
	case errors.As(err, &synErr), errors.As(err, &typErr):
		stage, committed, errType = "response_parse", t.sent, "JSONDecodeError"
	case errors.As(err, &urlErr):
		var opErr *net.OpError
		var dnsErr *net.DNSError
		neverOpened := errors.As(err, &dnsErr) || (errors.As(err, &opErr) && opErr.Op == "dial")
		stage, errType = "garmin_request", "ConnectionError"
		switch {
		case neverOpened && urlErr.Timeout():
			errType = "ConnectTimeout"
		case urlErr.Timeout():
			errType = "ReadTimeout"
		}
		if neverOpened {
			committed = false
		}
	case errors.Is(err, context.DeadlineExceeded):
		stage, errType = "garmin_request", "TimeoutError"
	}
	if t.done {
		stage, committed = "response_parse", true // Garmin accepted the write; only handling its response failed
	}
	return stage, committed, errType
}

// Fail logs err with a correlation ID and returns it as a *writeFailure.
func (t *writeTracker) Fail(err error) error {
	b := make([]byte, 6)
	rand.Read(b)
	cid := hex.EncodeToString(b)
	stage, committed, errType := t.classify(err)
	status, body := upstream(err)
	logStatus, logCommitted := "None", fmt.Sprint(committed)
	if status != nil {
		logStatus = fmt.Sprint(status)
	}
	if c, ok := committed.(bool); ok {
		logCommitted = map[bool]string{true: "True", false: "False"}[c]
	}
	fmt.Fprintf(t.log, "%s failed [correlation_id=%s stage=%s error_type=%s upstream_status=%s write_committed=%s]: %v\n",
		t.tool, cid, stage, errType, logStatus, logCommitted, err)
	p := map[string]any{
		"status":          "error",
		"message":         fmt.Sprintf("%s: %v", t.prefix, err),
		"tool":            t.tool,
		"stage":           stage,
		"error_type":      errType,
		"upstream_status": status,
		"upstream_body":   body,
		"write_committed": committed,
		"correlation_id":  cid,
	}
	switch committed {
	case "unknown":
		p["hint"] = "The request may have reached Garmin before failing. Check whether the change was applied before retrying, to avoid a duplicate."
	case false:
		p["hint"] = "Nothing was written, so a retry cannot create a duplicate."
	}
	return &writeFailure{p}
}

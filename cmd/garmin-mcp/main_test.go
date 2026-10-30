package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var out bytes.Buffer
		if code := run(context.Background(), []string{arg}, env{Stdout: &out, Stderr: io.Discard}); code != 0 || strings.TrimSpace(out.String()) != "garmin-mcp dev" {
			t.Fatalf("%s: code=%d out=%q", arg, code, out.String())
		}
	}
}
func TestUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), []string{"bogus"}, env{Stdout: io.Discard, Stderr: &out}); code != 2 || !strings.Contains(out.String(), `unknown command "bogus"`) {
		t.Fatalf("code=%d error=%q", code, out.String())
	}
}

func TestServeRespondsBeforeInputEOF(t *testing.T) {
	var out, stderr bytes.Buffer
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	e := env{Stdin: input, Stdout: &out, Stderr: &stderr, Getenv: func(k string) string {
		if k == "GARMIN_MCP_HOME" {
			return t.TempDir()
		}
		return ""
	}, IsTerminal: func() bool { return false }}
	if code := run(context.Background(), []string{"serve"}, e); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var response struct {
		ID     int `json:"id"`
		Result struct {
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.ID != 1 || response.Result.ServerInfo.Name != "garmin-mcp" {
		t.Fatalf("response %s: %v", out.String(), err)
	}
}

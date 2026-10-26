package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var out bytes.Buffer
		code := run(context.Background(), []string{arg}, env{Stdout: &out, Stderr: io.Discard})
		if code != 0 || strings.TrimSpace(out.String()) != "garmin-mcp dev" {
			t.Fatalf("%s: code=%d out=%q", arg, code, out.String())
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"bogus"}, env{Stdout: io.Discard, Stderr: &errOut}); code != 2 {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(errOut.String(), "unknown command \"bogus\"") {
		t.Fatal(errOut.String())
	}
}

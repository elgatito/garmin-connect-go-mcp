package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// makeArchive builds a release archive holding bin as garmin-mcp (garmin-mcp.exe in a .zip) plus a README.
func makeArchive(t *testing.T, name string, bin []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if strings.HasSuffix(name, ".zip") {
		zw := zip.NewWriter(&buf)
		for n, b := range map[string][]byte{"README.md": []byte("readme"), "garmin-mcp.exe": bin} {
			w, _ := zw.Create(n)
			w.Write(b)
		}
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755})
	for _, f := range []struct {
		n string
		b []byte
	}{{"./README.md", []byte("readme")}, {"./garmin-mcp", bin}} {
		tw.WriteHeader(&tar.Header{Name: f.n, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(f.b))})
		tw.Write(f.b)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

type updateRig struct {
	exe       string
	out, errb bytes.Buffer
	env       env
	mu        sync.Mutex
	downloads int
}

// newUpdateRig serves a latest release tagged tag with this platform's archive holding "new-binary";
// badSum publishes a wrong checksum. The running binary is a temp file holding "old-binary".
func newUpdateRig(t *testing.T, tag string, badSum bool) *updateRig {
	t.Helper()
	name := "garmin-mcp_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(name, ".tar.gz") + ".zip"
	}
	archive := makeArchive(t, name, []byte("new-binary"))
	sum := sha256.Sum256(archive)
	if badSum {
		sum[0]++
	}
	r := &updateRig{exe: filepath.Join(t.TempDir(), "garmin-mcp")}
	os.WriteFile(r.exe, []byte("old-binary"), 0o755)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/latest":
			json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": []map[string]string{
				{"name": "garmin-mcp_other_os.tar.gz", "browser_download_url": srv.URL + "/nope"},
				{"name": name, "browser_download_url": srv.URL + "/archive"},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/sums"},
			}})
		case "/archive":
			r.mu.Lock()
			r.downloads++
			r.mu.Unlock()
			w.Write(archive)
		case "/sums":
			w.Write([]byte("0000  garmin-mcp_other_os.tar.gz\n" + hex.EncodeToString(sum[:]) + "  " + name + "\n"))
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	r.env = env{Stdout: &r.out, Stderr: &r.errb, ReleaseAPI: srv.URL + "/latest", Executable: func() (string, error) { return r.exe, nil }}
	return r
}

func (r *updateRig) run(args ...string) int {
	return run(context.Background(), append([]string{"update"}, args...), r.env)
}

func (r *updateRig) exeContent(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(r.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func withVersion(t *testing.T, v string) {
	old := version
	version = v
	t.Cleanup(func() { version = old })
}

func TestUpdateReplacesBinary(t *testing.T) {
	withVersion(t, "v0.1.0")
	r := newUpdateRig(t, "v0.2.0", false)
	if code := r.run(); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	if got := r.exeContent(t); got != "new-binary" {
		t.Fatalf("binary = %q", got)
	}
	if fi, _ := os.Stat(r.exe); runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("not executable: %v", fi.Mode())
	}
	if left, _ := filepath.Glob(r.exe + ".*"); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
	if !strings.Contains(r.out.String(), "Updated "+r.exe+" to v0.2.0.") {
		t.Errorf("stdout = %q", r.out.String())
	}
}

func TestUpdateDevBuildInstallsRelease(t *testing.T) {
	withVersion(t, "dev")
	r := newUpdateRig(t, "v0.1.0", false)
	if code := r.run(); code != 0 || r.exeContent(t) != "new-binary" {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
}

func TestUpdateUpToDate(t *testing.T) {
	withVersion(t, "v0.2.0")
	r := newUpdateRig(t, "v0.2.0", false)
	if code := r.run(); code != 0 || !strings.Contains(r.out.String(), "garmin-mcp v0.2.0 is up to date") {
		t.Fatalf("code=%d out=%q stderr=%q", code, r.out.String(), r.errb.String())
	}
	if r.downloads != 0 || r.exeContent(t) != "old-binary" {
		t.Fatalf("downloads=%d", r.downloads)
	}
}

func TestUpdateCheckOnlyDoesNotInstall(t *testing.T) {
	withVersion(t, "v0.1.0")
	r := newUpdateRig(t, "v0.2.0", false)
	if code := r.run("--check"); code != 0 || !strings.Contains(r.out.String(), "A newer version is available: v0.2.0") {
		t.Fatalf("code=%d out=%q", code, r.out.String())
	}
	if r.downloads != 0 || r.exeContent(t) != "old-binary" {
		t.Fatalf("downloads=%d", r.downloads)
	}
}

func TestUpdateChecksumMismatchKeepsBinary(t *testing.T) {
	withVersion(t, "v0.1.0")
	r := newUpdateRig(t, "v0.2.0", true)
	if code := r.run(); code != 1 || !strings.Contains(r.errb.String(), "checksum mismatch") {
		t.Fatalf("code=%d stderr=%q", code, r.errb.String())
	}
	if got := r.exeContent(t); got != "old-binary" {
		t.Fatalf("binary = %q", got)
	}
	if left, _ := filepath.Glob(r.exe + ".*"); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		tag, current string
		want         bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.10.0", "v0.9.9", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"v0.1.0", "dev", true},
		{"v0.2.0", "v0.2.0-rc1", true},
		{"v0.3.0-rc1", "v0.2.0", false},
		{"", "v0.1.0", false},
	} {
		if got := newer(c.tag, c.current); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.tag, c.current, got)
		}
	}
}

func TestExtractBinaryBothFormats(t *testing.T) {
	for _, name := range []string{"garmin-mcp_windows_amd64.zip", "garmin-mcp_darwin_arm64.tar.gz"} {
		got, err := extractBinary(makeArchive(t, name, []byte("bin")), name)
		if err != nil || string(got) != "bin" {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
}

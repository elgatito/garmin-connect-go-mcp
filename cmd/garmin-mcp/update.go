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
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

const releaseAPI = "https://api.github.com/repos/elgatito/garmin-connect-go-mcp/releases/latest"

type release struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func cmdUpdate(ctx context.Context, args []string, e env) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only report whether a newer version is available")
	if code, stop := parseFlags(fs, args, e); stop {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.Stderr, "unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if err := update(ctx, e, *check); err != nil {
		fmt.Fprintf(e.Stderr, "Update failed: %v\n", err)
		return 1
	}
	return 0
}

// update checks the latest GitHub release and, unless checkOnly, replaces the running binary with that
// release's build for this platform after verifying it against the release's checksums.txt.
func update(ctx context.Context, e env, checkOnly bool) error {
	api, executable := e.ReleaseAPI, e.Executable
	if api == "" {
		api = releaseAPI
	}
	if executable == nil {
		executable = os.Executable
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	body, err := fetch(ctx, client, api)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	if !newer(rel.Tag, version) {
		fmt.Fprintf(e.Stdout, "garmin-mcp %s is up to date (latest release: %s).\n", version, rel.Tag)
		return nil
	}
	fmt.Fprintf(e.Stdout, "A newer version is available: %s (this is %s).\n", rel.Tag, version)
	if checkOnly {
		fmt.Fprintln(e.Stdout, `Run "garmin-mcp update" to install it.`)
		return nil
	}

	name := "garmin-mcp_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if runtime.GOOS == "windows" {
		name = "garmin-mcp_" + runtime.GOOS + "_" + runtime.GOARCH + ".zip"
	}
	var archiveURL, sumsURL string
	for _, a := range rel.Assets {
		switch a.Name {
		case name:
			archiveURL = a.URL
		case "checksums.txt":
			sumsURL = a.URL
		}
	}
	if archiveURL == "" {
		return fmt.Errorf("release %s has no build for %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	if sumsURL == "" {
		return fmt.Errorf("release %s has no checksums.txt; not installing an unverified download", rel.Tag)
	}
	fmt.Fprintf(e.Stdout, "Downloading %s...\n", name)
	archive, err := fetch(ctx, client, archiveURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", name, err)
	}
	sums, err := fetch(ctx, client, sumsURL)
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := verifyChecksum(sums, name, archive); err != nil {
		return err
	}
	bin, err := extractBinary(archive, name)
	if err != nil {
		return err
	}
	exe, err := executable()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if err := replaceExecutable(exe, bin); err != nil {
		return fmt.Errorf("replacing %s: %w", exe, err)
	}
	fmt.Fprintf(e.Stdout, "Updated %s to %s. Restart your MCP clients so they start the new version.\n", exe, rel.Tag)
	return nil
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "garmin-mcp/"+version)
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 200<<20))
}

// newer reports whether tag (vX.Y.Z) is a later release than current. A current version that isn't a release
// (dev or a pre-release build) is always older; a tag that isn't a plain release is never newer.
func newer(tag, current string) bool {
	t, ok := parseVersion(tag)
	if !ok {
		return false
	}
	c, ok := parseVersion(current)
	return !ok || slices.Compare(t[:], c[:]) > 0
}

func parseVersion(s string) (v [3]int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// verifyChecksum checks data against name's line in a sha256sum-format checksums.txt.
func verifyChecksum(sums []byte, name string, data []byte) error {
	sum := sha256.Sum256(data)
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if !strings.EqualFold(f[0], hex.EncodeToString(sum[:])) {
				return fmt.Errorf("checksum mismatch for %s; not installing it", name)
			}
			return nil
		}
	}
	return fmt.Errorf("checksums.txt has no entry for %s; not installing it", name)
}

// extractBinary returns garmin-mcp (garmin-mcp.exe from a .zip) out of a release archive.
func extractBinary(archive []byte, name string) ([]byte, error) {
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == "garmin-mcp.exe" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("garmin-mcp.exe not found in %s", name)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("garmin-mcp not found in %s", name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == "garmin-mcp" {
			return io.ReadAll(tr)
		}
	}
}

// replaceExecutable puts bin in place of exe. Windows can't overwrite a running .exe but can rename it, so the
// old binary is moved to <exe>.old first and deleted; on Windows that delete fails until the old process exits,
// and the leftover is removed by the next update.
func replaceExecutable(exe string, bin []byte) error {
	tmp, old := exe+".new", exe+".old"
	os.Remove(old)
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(exe, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Rename(old, exe)
		os.Remove(tmp)
		return err
	}
	os.Remove(old)
	return nil
}

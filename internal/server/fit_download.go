package server

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"path/filepath"
	"strings"
)

func absoluteDir(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}
func saveDownloadedFile(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".garmin-download-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func registerFITDownload(srv *mcp.Server, m *manager) {
	addTool(srv, m, "download_activity_file", description("download_activity_file"), map[string]any{"activity_id": map[string]any{"anyOf": []any{map[string]any{"type": "integer"}, map[string]any{"type": "string"}}}, "format": map[string]any{"type": "string", "default": "fit"}, "output_dir": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		format := "fit"
		if v, ok := args["format"].(string); ok {
			format = strings.ToLower(strings.TrimSpace(v))
		}
		paths := map[string]string{"fit": "/download-service/files/activity/", "gpx": "/download-service/export/gpx/activity/", "tcx": "/download-service/export/tcx/activity/", "csv": "/download-service/export/csv/activity/"}
		endpoint, ok := paths[format]
		if !ok {
			return map[string]any{"error": fmt.Sprintf("Invalid format '%v'.", args["format"]), "valid_formats": []string{"fit", "gpx", "tcx", "csv"}}, nil
		}
		dir, _ := args["output_dir"].(string)
		if dir == "" {
			dir = m.opt.Getenv("GARMIN_FIT_DOWNLOAD_DIR")
		}
		if dir == "" {
			settings, err := s.Settings()
			if err != nil {
				return nil, fmt.Errorf("Error downloading activity %v: %w", args["activity_id"], err)
			}
			dir = settings.FitDownloadDir
		}
		if dir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return nil, err
			}
			accountDir, err := s.Store.Dir(s.Email)
			if err != nil {
				return nil, err
			}
			return map[string]any{"status": "needs_setup", "suggested_default": cwd, "config_path": filepath.Join(accountDir, "settings.json"), "message": "No download directory configured. Ask the user where to save activity files (offer the current working directory as the default), then call set_fit_download_dir(path) before downloading."}, nil
		}
		id, err := activityID(args["activity_id"])
		if err != nil {
			return nil, fmt.Errorf("Error downloading activity %v: %w", args["activity_id"], err)
		}
		dir, err = absoluteDir(dir)
		if err != nil {
			return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
		}
		data, err := s.Client.Download(ctx, fmt.Sprintf("%s%d", endpoint, id), nil)
		if err != nil {
			return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
		}
		if len(data) == 0 {
			return fmt.Sprintf("No %s data returned for activity %d", format, id), nil
		}
		payload := data
		if format == "fit" {
			payload, err = extractFIT(data)
			if err != nil {
				return map[string]any{"error": err.Error(), "debug": map[string]any{"total_bytes": len(data), "first_16_bytes_hex": hex.EncodeToString(data[:min(16, len(data))]), "hint": "1f8b = gzip, 504b = ZIP, 0e10/0c10 = raw FIT, 3c or 7b = HTML/JSON error from Garmin"}}, nil
			}
		}
		if err = os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
		}
		file := filepath.Join(dir, fmt.Sprintf("%d.%s", id, format))
		if err = saveDownloadedFile(file, payload); err != nil {
			return nil, fmt.Errorf("Error downloading activity %d: %w", id, err)
		}
		return map[string]any{"activity_id": id, "format": format, "file_path": file, "size_bytes": len(payload), "message": "Activity file saved."}, nil
	}, "activity_id")
}

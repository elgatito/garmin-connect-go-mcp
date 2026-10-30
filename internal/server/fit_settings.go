package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerFITSettings(srv *mcp.Server, m *manager) {
	addTool(srv, m, "set_fit_download_dir", description("set_fit_download_dir"), map[string]any{"path": map[string]any{"type": "string"}}, func(ctx context.Context, s *Session, args map[string]any) (any, error) {
		path, ok := args["path"].(string)
		if !ok {
			return nil, fmt.Errorf("Error setting FIT download directory: path is required")
		}
		if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("Error setting FIT download directory: %w", err)
			}
			path = filepath.Join(home, path[1:])
		}
		path, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("Error setting FIT download directory: %w", err)
		}
		if err = os.MkdirAll(path, 0755); err != nil {
			return nil, fmt.Errorf("Error setting FIT download directory: %w", err)
		}
		st, err := s.Settings()
		if err != nil {
			return nil, fmt.Errorf("Error setting FIT download directory: %w", err)
		}
		st.FitDownloadDir = path
		if err = s.Store.SaveSettings(s.Email, st); err != nil {
			return nil, fmt.Errorf("Error setting FIT download directory: %w", err)
		}
		dir, err := s.Store.Dir(s.Email)
		if err != nil {
			return nil, err
		}
		return map[string]any{"download_dir": path, "config_path": filepath.Join(dir, "settings.json"), "message": "Default FIT download directory configured."}, nil
	}, "path")
}

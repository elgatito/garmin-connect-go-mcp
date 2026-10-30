package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerResources(s *mcp.Server) {
	for _, item := range []struct{ uri, name, file string }{
		{"workout://templates/simple-run", "get_simple_run_template", "simple-run"},
		{"workout://templates/interval-running", "get_interval_template", "interval-running"},
		{"workout://templates/tempo-run", "get_tempo_template", "tempo-run"},
		{"workout://templates/strength-circuit", "get_strength_template", "strength-circuit"},
		{"workout://reference/structure", "get_structure_reference", "structure"},
	} {
		item := item
		body, err := assets.ReadFile("templates/" + item.file + ".json")
		if err != nil {
			panic(err)
		}
		s.AddResource(&mcp.Resource{URI: item.uri, Name: item.name, MIMEType: "application/json"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: item.uri, MIMEType: "application/json", Text: string(body)}}}, nil
		})
	}
}

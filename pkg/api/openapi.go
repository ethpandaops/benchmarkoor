package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var openapiYAML []byte

// openapiJSON holds the pre-converted JSON representation of the OpenAPI spec.
var openapiJSON []byte

func init() {
	var raw any
	if err := yaml.Unmarshal(openapiYAML, &raw); err != nil {
		panic(fmt.Sprintf("openapi: failed to parse YAML: %v", err))
	}

	converted := convertMapKeys(raw)

	data, err := json.Marshal(converted)
	if err != nil {
		panic(fmt.Sprintf("openapi: failed to marshal JSON: %v", err))
	}

	openapiJSON = data
}

// handleOpenAPISpec serves the OpenAPI spec as JSON.
func (s *server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(openapiJSON) //nolint:errcheck // best-effort write
}

// convertMapKeys recursively converts map[string]any (from YAML unmarshal)
// into a structure that json.Marshal can handle. YAML v3 unmarshals maps as
// map[string]any by default, but nested slices and maps still need traversal.
func convertMapKeys(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[k] = convertMapKeys(v)
		}

		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = convertMapKeys(v)
		}

		return out
	default:
		return v
	}
}

package modes

import (
	"encoding/json"
	"errors"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/papers"
)

// errorDetails keeps the same machine-readable error on MCP and CLI adapters.
func errorDetails(err error) map[string]any {
	var paperError *papers.Error
	if errors.As(err, &paperError) {
		data, _ := json.Marshal(paperError)
		var result map[string]any
		if json.Unmarshal(data, &result) == nil {
			return result
		}
	}
	return map[string]any{
		"code": anna.CodeOf(err), "message": applicationError(err).Error(), "retryable": false,
	}
}

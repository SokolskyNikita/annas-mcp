package modes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/papers"
)

func TestArticleToolsExposeReplayableControlsWithoutChangingBookInputs(t *testing.T) {
	t.Parallel()
	session := connectTestServer(t, &fakeArchive{})
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	type schemaCase struct {
		Tool   string
		Fields []string
	}
	cases := fixtureJSON[[]schemaCase](t, "article-schema.json")
	fieldsByTool := make(map[string]map[string]any)
	for _, tool := range listed.Tools {
		encoded, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(encoded, &schema); err != nil {
			t.Fatal(err)
		}
		fieldsByTool[tool.Name] = schema.Properties
	}
	for _, tc := range cases {
		for _, field := range tc.Fields {
			if fieldsByTool[tc.Tool][field] == nil {
				t.Errorf("%s does not advertise %s", tc.Tool, field)
			}
		}
	}
	if fieldsByTool["book_search"]["work_type"] != nil || fieldsByTool["book_search"]["provider"] != nil {
		t.Fatal("scholarly controls leaked into book search")
	}
}

func TestArticleFailureRetainsRecoveryEvidenceAndLegacyCategory(t *testing.T) {
	t.Parallel()
	failure := fixtureJSON[papers.Error](t, "article-error.json")
	result, output, err := paperToolFailure(&failure)
	if err != nil || output != nil || !result.IsError {
		t.Fatalf("wrong tool failure: %+v %v", result, err)
	}
	if !strings.HasPrefix(toolText(t, result), "[NOT_FOUND]") {
		t.Fatal("missing legacy category")
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var recovered papers.Error
	if err := json.Unmarshal(encoded, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.LandingPage != failure.LandingPage || len(recovered.NextActions) != 1 || recovered.Retryable {
		t.Fatalf("lost terminal recovery evidence: %+v", recovered)
	}
}

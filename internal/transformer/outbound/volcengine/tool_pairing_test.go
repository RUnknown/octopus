package volcengine

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// Volcengine reuses openai.ConvertToResponsesRequest directly instead of going
// through the OpenAI outbound, so it needs its own tool-call repair. Without it
// a failover from an OpenAI channel to a Volcengine channel would change whether
// the same conversation is accepted.
func TestResponseOutboundPairsOrphanedToolCalls(t *testing.T) {
	prompt := "call the tool"
	req := &model.InternalLLMRequest{
		Model: "doubao-seed-1-6",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &prompt}},
			{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID:       "call_orphan",
					Type:     "function",
					Function: model.FunctionCall{Name: "lookup", Arguments: `{}`},
				}},
			},
		},
	}

	httpReq, err := (&ResponseOutbound{}).TransformRequest(context.Background(), req, "https://ark.cn-beijing.volces.com/api/v3", "test-key")
	if err != nil {
		t.Fatalf("outbound transformation failed: %v", err)
	}
	defer httpReq.Body.Close()

	raw, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("failed to read outbound body: %v", err)
	}
	if !strings.Contains(string(raw), "function_call_output") {
		t.Fatalf("orphaned tool call was not answered on the volcengine outbound: %s", raw)
	}
}

package gemini

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// Gemini rejects a history whose functionCall has no matching functionResponse.
// The repair used to live only on the Anthropic outbound, so a failover between
// providers could change whether the same conversation was accepted.
func TestMessagesOutboundPairsOrphanedToolCalls(t *testing.T) {
	prompt := "call the tool"
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
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

	httpReq, err := (&MessagesOutbound{}).TransformRequest(context.Background(), req, "https://generativelanguage.googleapis.com/v1beta", "test-key")
	if err != nil {
		t.Fatalf("outbound transformation failed: %v", err)
	}
	defer httpReq.Body.Close()

	raw, err := io.ReadAll(httpReq.Body)
	if err != nil {
		t.Fatalf("failed to read outbound body: %v", err)
	}

	var payload struct {
		Contents []struct {
			Parts []struct {
				FunctionCall     *struct{} `json:"functionCall"`
				FunctionResponse *struct {
					Name string `json:"name"`
				} `json:"functionResponse"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("failed to parse outbound body: %v", err)
	}

	calls, responses := 0, 0
	for _, content := range payload.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				calls++
			}
			if part.FunctionResponse != nil {
				responses++
			}
		}
	}
	if calls == 0 {
		t.Fatalf("expected a functionCall in the outbound body: %s", raw)
	}
	if responses != calls {
		t.Fatalf("expected every functionCall to be answered, got %d calls and %d responses: %s", calls, responses, raw)
	}
}

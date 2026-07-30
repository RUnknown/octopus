package openai

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// An assistant tool call with no matching tool result makes Chat Completions and
// the Responses API reject the whole request. The repair used to run only on the
// Anthropic outbound, so the same conversation was accepted or rejected
// depending on which channel a failover picked. These tests pin the repair to
// the OpenAI paths.
func orphanedToolCallRequest() *model.InternalLLMRequest {
	prompt := "call the tool"
	return &model.InternalLLMRequest{
		Model: "gpt-5",
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
}

func readOutboundBody(t *testing.T, body io.ReadCloser) []byte {
	t.Helper()
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read outbound body: %v", err)
	}
	return raw
}

func TestChatOutboundPairsOrphanedToolCalls(t *testing.T) {
	httpReq, err := (&ChatOutbound{}).TransformRequest(context.Background(), orphanedToolCallRequest(), "https://api.openai.com/v1", "test-key")
	if err != nil {
		t.Fatalf("outbound transformation failed: %v", err)
	}

	var payload struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	raw := readOutboundBody(t, httpReq.Body)
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("failed to parse outbound body: %v", err)
	}

	found := false
	for _, msg := range payload.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "call_orphan" {
			found = true
		}
	}
	if !found {
		t.Fatalf("orphaned tool call was not answered on the chat outbound: %s", raw)
	}
}

func TestResponseOutboundPairsOrphanedToolCalls(t *testing.T) {
	httpReq, err := (&ResponseOutbound{}).TransformRequest(context.Background(), orphanedToolCallRequest(), "https://api.openai.com/v1", "test-key")
	if err != nil {
		t.Fatalf("outbound transformation failed: %v", err)
	}

	raw := readOutboundBody(t, httpReq.Body)
	if !strings.Contains(string(raw), "function_call_output") {
		t.Fatalf("orphaned tool call was not answered on the responses outbound: %s", raw)
	}
}

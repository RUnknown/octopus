package compat

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestFixOrphanedToolCallsInsertsMissingResults(t *testing.T) {
	followup := "next"
	messages := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}},
				{ID: "call_b", Function: model.FunctionCall{Name: "search"}},
			},
		},
		{
			Role:       "tool",
			ToolCallID: stringPtr("call_a"),
			Content:    model.MessageContent{Content: stringPtr("ok")},
		},
		{Role: "user", Content: model.MessageContent{Content: &followup}},
	}

	got := FixOrphanedToolCalls(messages)
	if len(got) != 4 {
		t.Fatalf("expected one synthetic tool result, got %d messages: %+v", len(got), got)
	}
	if got[1].Role != "tool" || got[1].ToolCallID == nil || *got[1].ToolCallID != "call_b" {
		t.Fatalf("unexpected synthetic tool result: %+v", got[1])
	}
	if got[1].Content.Content == nil || *got[1].Content.Content != "" {
		t.Fatalf("expected empty synthetic content, got %+v", got[1].Content)
	}
	if got[2].Role != "tool" || got[2].ToolCallID == nil || *got[2].ToolCallID != "call_a" {
		t.Fatalf("existing tool result was not preserved after synthetic result: %+v", got[2])
	}
}

func TestFixOrphanedToolCallsStopsAtNextAssistant(t *testing.T) {
	messages := []model.Message{
		{
			Role:      "assistant",
			ToolCalls: []model.ToolCall{{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}}},
		},
		{
			Role:      "assistant",
			ToolCalls: []model.ToolCall{{ID: "call_b", Function: model.FunctionCall{Name: "search"}}},
		},
		{
			Role:       "tool",
			ToolCallID: stringPtr("call_a"),
		},
	}

	got := FixOrphanedToolCalls(messages)
	if len(got) != 5 {
		t.Fatalf("expected both assistant turns to be patched independently, got %+v", got)
	}
	if got[1].ToolCallID == nil || *got[1].ToolCallID != "call_a" {
		t.Fatalf("first assistant was not patched before next assistant: %+v", got)
	}
	if got[3].ToolCallID == nil || *got[3].ToolCallID != "call_b" {
		t.Fatalf("second assistant was not patched: %+v", got)
	}
}

func stringPtr(v string) *string {
	return &v
}

func TestPairToolCallsRepairsRequestInPlace(t *testing.T) {
	prompt := "hi"
	req := &model.InternalLLMRequest{
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: &prompt}},
			{
				Role:      "assistant",
				ToolCalls: []model.ToolCall{{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}}},
			},
		},
	}

	PairToolCalls(req)

	if len(req.Messages) != 3 {
		t.Fatalf("expected a synthetic tool result appended, got %+v", req.Messages)
	}
	last := req.Messages[2]
	if last.Role != "tool" || last.ToolCallID == nil || *last.ToolCallID != "call_a" {
		t.Fatalf("unexpected repair result: %+v", last)
	}
}

func TestPairToolCallsIgnoresEmptyRequests(t *testing.T) {
	PairToolCalls(nil)

	req := &model.InternalLLMRequest{}
	PairToolCalls(req)
	if len(req.Messages) != 0 {
		t.Fatalf("expected no messages to be synthesized, got %+v", req.Messages)
	}
}

func TestPairToolCallsLeavesAnsweredCallsUntouched(t *testing.T) {
	req := &model.InternalLLMRequest{
		Messages: []model.Message{
			{
				Role:      "assistant",
				ToolCalls: []model.ToolCall{{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}}},
			},
			{
				Role:       "tool",
				ToolCallID: stringPtr("call_a"),
				Content:    model.MessageContent{Content: stringPtr("done")},
			},
		},
	}

	PairToolCalls(req)

	if len(req.Messages) != 2 {
		t.Fatalf("answered tool call must not be patched, got %+v", req.Messages)
	}
}

func TestDeduplicateToolResultsKeepsLastContentAtFirstPosition(t *testing.T) {
	callID := "call_a"
	first := "stale"
	last := "corrected"
	messages := []model.Message{
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: callID, Function: model.FunctionCall{Name: "lookup"}}}},
		{Role: "tool", ToolCallID: &callID, Content: model.MessageContent{Content: &first}},
		{Role: "user", Content: model.MessageContent{Content: stringPtr("continue")}},
		{Role: "tool", ToolCallID: &callID, Content: model.MessageContent{Content: &last}},
	}

	got := DeduplicateToolResults(messages)
	if len(got) != 3 {
		t.Fatalf("message count = %d, want 3: %+v", len(got), got)
	}
	if got[1].Role != "tool" || got[1].Content.Content == nil || *got[1].Content.Content != last {
		t.Fatalf("deduplicated tool result = %+v, want last content", got[1])
	}
	if got[2].Role != "user" {
		t.Fatalf("non-tool message order changed: %+v", got)
	}
}

func TestDeduplicateToolResultsPreservesEmptyIDs(t *testing.T) {
	empty := ""
	messages := []model.Message{
		{Role: "tool", ToolCallID: &empty},
		{Role: "tool", ToolCallID: &empty},
	}
	if got := DeduplicateToolResults(messages); len(got) != len(messages) {
		t.Fatalf("empty tool IDs were deduplicated: %+v", got)
	}
}

// PatchAnthropicRequest is kept as the Anthropic entrypoint but must stay
// equivalent to the shared repair so behaviour cannot drift per protocol.
func TestPatchAnthropicRequestMatchesPairToolCalls(t *testing.T) {
	build := func() *model.InternalLLMRequest {
		return &model.InternalLLMRequest{
			Messages: []model.Message{
				{
					Role:      "assistant",
					ToolCalls: []model.ToolCall{{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}}},
				},
			},
		}
	}

	viaAnthropic := build()
	PatchAnthropicRequest(viaAnthropic)
	viaShared := build()
	PairToolCalls(viaShared)

	if len(viaAnthropic.Messages) != len(viaShared.Messages) {
		t.Fatalf("anthropic patch produced %d messages, shared repair produced %d", len(viaAnthropic.Messages), len(viaShared.Messages))
	}
	for i := range viaShared.Messages {
		if viaAnthropic.Messages[i].Role != viaShared.Messages[i].Role {
			t.Fatalf("message %d role mismatch: %q vs %q", i, viaAnthropic.Messages[i].Role, viaShared.Messages[i].Role)
		}
	}
}

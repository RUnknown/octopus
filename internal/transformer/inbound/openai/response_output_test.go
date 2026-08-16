package openai

import (
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestStreamCompletedEventHasNonEmptyOutputWithMessage(t *testing.T) {
	text := "hello"
	stop := "stop"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_01",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role:    "assistant",
					Content: model.MessageContent{Content: &text},
				},
			}},
		},
		{
			ID:     "resp_01",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant"},
				FinishReason: &stop,
			}},
		},
		{
			ID:     "resp_01",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Usage: &model.Usage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		},
	}
	events := feedStream(t, chunks)

	var completed *ResponsesStreamEvent
	for i := range events {
		if events[i].Type == "response.completed" {
			completed = &events[i]
			break
		}
	}
	if completed == nil || completed.Response == nil {
		t.Fatalf("expected response.completed event, got events: %+v", eventTypes(events))
	}
	if len(completed.Response.Output) == 0 {
		t.Fatalf("response.completed.output must be non-empty (O-H3)")
	}
	first := completed.Response.Output[0]
	if first.Type != "message" {
		t.Fatalf("first output type = %q, want message", first.Type)
	}
}

func TestStreamCompletedSynthesizesShellWhenEmpty(t *testing.T) {
	stop := "stop"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_02",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant"},
				FinishReason: &stop,
			}},
		},
		{
			ID:    "resp_02",
			Model: "gpt-4o",
			Usage: &model.Usage{
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			},
		},
	}
	events := feedStream(t, chunks)

	var completed *ResponsesStreamEvent
	for i := range events {
		if events[i].Type == "response.completed" {
			completed = &events[i]
		}
	}
	if completed == nil || completed.Response == nil {
		t.Fatalf("expected response.completed event")
	}
	if len(completed.Response.Output) == 0 {
		t.Fatalf("output must be non-empty even when no items were emitted")
	}
	first := completed.Response.Output[0]
	if first.Type != "message" {
		t.Fatalf("synthetic output type = %q, want message", first.Type)
	}
	if first.Status == nil || *first.Status != "completed" {
		t.Fatalf("synthetic status = %v, want completed", first.Status)
	}
	_ = lo.ToPtr("ignore")
}

func TestStreamIncompleteMarksActiveToolAndResponseIncomplete(t *testing.T) {
	arguments := `{"path":"unfinished`
	length := "length"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_incomplete_tool",
			Model:  "gpt-5",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role: "assistant",
					ToolCalls: []model.ToolCall{{
						Index: 0,
						ID:    "call_patch",
						Type:  "function",
						Function: model.FunctionCall{
							Name:      "apply_patch",
							Arguments: arguments,
						},
					}},
				},
			}},
		},
		{
			ID:     "resp_incomplete_tool",
			Model:  "gpt-5",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant"},
				FinishReason: &length,
			}},
		},
		{
			ID:    "resp_incomplete_tool",
			Model: "gpt-5",
			Usage: &model.Usage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		},
	}

	events := feedStream(t, chunks)
	var terminal *ResponsesStreamEvent
	var toolDone *ResponsesStreamEvent
	for idx := range events {
		switch events[idx].Type {
		case "response.incomplete":
			terminal = &events[idx]
		case "response.output_item.done":
			if events[idx].Item != nil && events[idx].Item.Type == "function_call" {
				toolDone = &events[idx]
			}
		case "response.completed":
			t.Fatalf("truncated tool stream emitted response.completed")
		}
	}
	if toolDone == nil || toolDone.Item == nil || toolDone.Item.Status == nil || *toolDone.Item.Status != "incomplete" {
		t.Fatalf("tool item was not marked incomplete: %+v", toolDone)
	}
	if terminal == nil || terminal.Response == nil {
		t.Fatalf("missing response.incomplete event: %+v", eventTypes(events))
	}
	if terminal.Response.IncompleteDetails == nil || terminal.Response.IncompleteDetails.Reason != "max_output_tokens" {
		t.Fatalf("unexpected incomplete details: %+v", terminal.Response.IncompleteDetails)
	}
	if len(terminal.Response.Output) != 1 || terminal.Response.Output[0].Status == nil || *terminal.Response.Output[0].Status != "incomplete" {
		t.Fatalf("terminal output was not marked incomplete: %+v", terminal.Response.Output)
	}
}

func TestStreamExplicitFinishWithoutUsageFinalizesOnDone(t *testing.T) {
	stop := "stop"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_no_usage",
			Model:  "gpt-5",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant"},
				FinishReason: &stop,
			}},
		},
		{Object: "[DONE]"},
	}

	events := feedStream(t, chunks)
	completed := 0
	for _, event := range events {
		if event.Type == "response.completed" {
			completed++
		}
	}
	if completed != 1 {
		t.Fatalf("response.completed count = %d, want 1; events=%v", completed, eventTypes(events))
	}
}

func TestStreamDoneWithoutFinishDoesNotClaimCompletion(t *testing.T) {
	reasoning := "still thinking"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_interrupted",
			Model:  "gpt-5",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role:             "assistant",
					ReasoningContent: &reasoning,
				},
			}},
		},
		{Object: "[DONE]"},
	}

	for _, event := range feedStream(t, chunks) {
		if event.Type == "response.completed" || event.Type == "response.incomplete" {
			t.Fatalf("interrupted stream emitted terminal event %q", event.Type)
		}
	}
}

func TestNonStreamContentFilterMarksOutputIncomplete(t *testing.T) {
	finish := "content_filter"
	content := "partial"
	out := convertToResponsesAPIResponse(&model.InternalLLMResponse{
		ID:    "resp_filtered",
		Model: "gpt-5",
		Choices: []model.Choice{{
			Message:      &model.Message{Role: "assistant", Content: model.MessageContent{Content: &content}},
			FinishReason: &finish,
		}},
	})

	if out.Status == nil || *out.Status != "incomplete" {
		t.Fatalf("status = %v, want incomplete", out.Status)
	}
	if out.IncompleteDetails == nil || out.IncompleteDetails.Reason != "content_filter" {
		t.Fatalf("unexpected incomplete details: %+v", out.IncompleteDetails)
	}
	if len(out.Output) != 1 || out.Output[0].Status == nil || *out.Output[0].Status != "incomplete" {
		t.Fatalf("output was not marked incomplete: %+v", out.Output)
	}
}

func TestConvertToResponsesAPIResponsePreservesRefusalContent(t *testing.T) {
	stop := "refusal"
	resp := &model.InternalLLMResponse{
		ID:      "resp_refusal",
		Model:   "gpt-4o",
		Created: 123,
		Choices: []model.Choice{{
			Message: &model.Message{
				Role:    "assistant",
				Refusal: "I cannot help with that.",
			},
			FinishReason: &stop,
		}},
	}

	out := convertToResponsesAPIResponse(resp)
	if len(out.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(out.Output))
	}
	msg := out.Output[0]
	if msg.Type != "message" || msg.Content == nil || len(msg.Content.Items) != 1 {
		t.Fatalf("unexpected message shape: %+v", msg)
	}
	part := msg.Content.Items[0]
	if part.Type != "refusal" || part.Refusal == nil || *part.Refusal != "I cannot help with that." {
		t.Fatalf("expected refusal content item, got %+v", part)
	}
	if out.Status == nil || *out.Status != "failed" {
		t.Fatalf("expected failed status for refusal stop, got %v", out.Status)
	}
}

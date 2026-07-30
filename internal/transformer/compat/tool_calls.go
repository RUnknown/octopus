package compat

import (
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// PatchAnthropicRequest applies small protocol repairs before Anthropic wire
// conversion. It is kept as the Anthropic-specific entrypoint so future
// Anthropic-only fixes have a home, but the tool-call repair it performs is
// shared with every other outbound protocol.
func PatchAnthropicRequest(req *model.InternalLLMRequest) {
	PairToolCalls(req)
}

// PairToolCalls repairs unanswered assistant tool calls on any outbound
// protocol. Chat Completions, Gemini and Anthropic all reject histories where
// an assistant tool call has no matching tool result, so the repair belongs on
// every outbound path rather than only the Anthropic one. Operates on the
// protocol-neutral IR, so behaviour stays identical across channels and a
// failover between providers cannot change whether a request is accepted.
func PairToolCalls(req *model.InternalLLMRequest) {
	if req == nil || len(req.Messages) == 0 {
		return
	}
	req.Messages = FixOrphanedToolCalls(req.Messages)
}

// FixOrphanedToolCalls inserts empty tool_result messages for assistant
// tool_use blocks that are not answered before the next assistant turn.
func FixOrphanedToolCalls(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]model.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		out = append(out, msg)
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		answered := answeredToolCallIDsBeforeNextAssistant(messages, i+1)
		for _, toolCall := range msg.ToolCalls {
			id := strings.TrimSpace(toolCall.ID)
			if id == "" {
				continue
			}
			if _, ok := answered[id]; ok {
				continue
			}
			out = append(out, emptyToolResult(toolCall))
		}
	}
	return out
}

func answeredToolCallIDsBeforeNextAssistant(messages []model.Message, start int) map[string]struct{} {
	answered := make(map[string]struct{})
	for i := start; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == "assistant" {
			break
		}
		if msg.Role != "tool" || msg.ToolCallID == nil {
			continue
		}
		id := strings.TrimSpace(*msg.ToolCallID)
		if id != "" {
			answered[id] = struct{}{}
		}
	}
	return answered
}

func emptyToolResult(toolCall model.ToolCall) model.Message {
	id := toolCall.ID
	name := toolCall.Function.Name
	content := ""
	return model.Message{
		Role:         "tool",
		Content:      model.MessageContent{Content: &content},
		ToolCallID:   &id,
		ToolCallName: &name,
	}
}

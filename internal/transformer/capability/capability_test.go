package capability

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestEvaluateDocumentsCurrentProtocolGaps(t *testing.T) {
	geminiInbound := Evaluate(ProtocolGemini, ProtocolOpenAIChat)
	if geminiInbound.Supported {
		t.Fatalf("expected Gemini inbound to be unsupported")
	}
	if geminiInbound.MissingReason != "client protocol has no inbound adapter" {
		t.Fatalf("unexpected Gemini missing reason: %q", geminiInbound.MissingReason)
	}

	embeddingToChat := Evaluate(ProtocolOpenAIEmbedding, ProtocolOpenAIChat)
	if embeddingToChat.Supported {
		t.Fatalf("expected embedding request to chat upstream to be unsupported")
	}
	if embeddingToChat.Mode != ModeMissing {
		t.Fatalf("expected missing mode, got %s", embeddingToChat.Mode)
	}
}

func TestEvaluateOutboundCodexUsesResponsesProfile(t *testing.T) {
	cap := EvaluateOutbound(ProtocolCodexResponses, outbound.OutboundTypeOpenAIResponse)
	if !cap.Supported {
		t.Fatalf("expected Codex Responses profile to route to OpenAI Responses upstream: %s", cap.MissingReason)
	}
	if cap.Mode != ModeProfile {
		t.Fatalf("expected profile mode, got %s", cap.Mode)
	}
	if !cap.NativePassthrough {
		t.Fatalf("expected Responses-profile route to preserve native passthrough capability")
	}
}

func TestEvaluateOutboundCrossProtocolIsLossyIR(t *testing.T) {
	cap := EvaluateOutbound(ProtocolAnthropic, outbound.OutboundTypeGemini)
	if !cap.Supported {
		t.Fatalf("expected Anthropic to Gemini to be supported through IR: %s", cap.MissingReason)
	}
	if cap.Mode != ModeIR {
		t.Fatalf("expected IR mode, got %s", cap.Mode)
	}
	if len(cap.LossyReasons) == 0 {
		t.Fatalf("expected cross-protocol lossy reasons")
	}
}

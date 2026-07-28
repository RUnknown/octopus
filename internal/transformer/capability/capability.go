package capability

import (
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

type Protocol string

type RequestFamily string

type SupportLevel string

type TransformMode string

const (
	ProtocolOpenAIChat      Protocol = "openai_chat"
	ProtocolOpenAIResponses Protocol = "openai_responses"
	ProtocolAnthropic       Protocol = "anthropic"
	ProtocolGemini          Protocol = "gemini"
	ProtocolCodexResponses  Protocol = "codex_responses"
	ProtocolOpenAIEmbedding Protocol = "openai_embeddings"
	ProtocolVolcengine      Protocol = "volcengine_responses"

	FamilyChat      RequestFamily = "chat"
	FamilyEmbedding RequestFamily = "embedding"

	SupportFull    SupportLevel = "full"
	SupportPartial SupportLevel = "partial"
	SupportMissing SupportLevel = "missing"

	ModeNative      TransformMode = "native"
	ModeIR          TransformMode = "ir"
	ModePassthrough TransformMode = "passthrough"
	ModeProfile     TransformMode = "profile"
	ModeMissing     TransformMode = "missing"
)

type ProtocolInfo struct {
	Protocol      Protocol      `json:"protocol"`
	Name          string        `json:"name"`
	Endpoint      string        `json:"endpoint"`
	RequestFamily RequestFamily `json:"request_family"`
	Inbound       bool          `json:"inbound"`
	Outbound      bool          `json:"outbound"`
	Note          string        `json:"note,omitempty"`
}

type FeatureSupport struct {
	Stream       SupportLevel `json:"stream"`
	Tools        SupportLevel `json:"tools"`
	Thinking     SupportLevel `json:"thinking"`
	Images       SupportLevel `json:"images"`
	Files        SupportLevel `json:"files"`
	CacheControl SupportLevel `json:"cache_control"`
}

type TransformCapability struct {
	ClientProtocol    Protocol       `json:"client_protocol"`
	UpstreamProtocol  Protocol       `json:"upstream_protocol"`
	RequestFamily     RequestFamily  `json:"request_family"`
	Supported         bool           `json:"supported"`
	Support           SupportLevel   `json:"support"`
	Mode              TransformMode  `json:"mode"`
	NativePassthrough bool           `json:"native_passthrough"`
	Features          FeatureSupport `json:"features"`
	LossyReasons      []string       `json:"lossy_reasons,omitempty"`
	MissingReason     string         `json:"missing_reason,omitempty"`
}

type Matrix struct {
	Protocols []ProtocolInfo        `json:"protocols"`
	Matrix    []TransformCapability `json:"matrix"`
	Notes     []string              `json:"notes,omitempty"`
}

var knownProtocols = []ProtocolInfo{
	{Protocol: ProtocolOpenAIChat, Name: "OpenAI Chat Completions", Endpoint: "POST /v1/chat/completions", RequestFamily: FamilyChat, Inbound: true, Outbound: true},
	{Protocol: ProtocolOpenAIResponses, Name: "OpenAI Responses", Endpoint: "POST /v1/responses", RequestFamily: FamilyChat, Inbound: true, Outbound: true},
	{Protocol: ProtocolAnthropic, Name: "Anthropic Messages", Endpoint: "POST /v1/messages", RequestFamily: FamilyChat, Inbound: true, Outbound: true},
	{Protocol: ProtocolGemini, Name: "Gemini GenerateContent", Endpoint: "POST /v1beta/models/{model}:generateContent", RequestFamily: FamilyChat, Inbound: false, Outbound: true, Note: "当前只有 outbound adapter，没有 Gemini 客户端入口。"},
	{Protocol: ProtocolCodexResponses, Name: "Codex Responses Profile", Endpoint: "POST /v1/responses", RequestFamily: FamilyChat, Inbound: true, Outbound: true, Note: "当前不是独立协议类型，复用 OpenAI Responses 入口和上游。"},
	{Protocol: ProtocolOpenAIEmbedding, Name: "OpenAI Embeddings", Endpoint: "POST /v1/embeddings", RequestFamily: FamilyEmbedding, Inbound: true, Outbound: true},
	{Protocol: ProtocolVolcengine, Name: "Volcengine Responses", Endpoint: "POST /api/v3/chat/completions", RequestFamily: FamilyChat, Inbound: false, Outbound: true, Note: "当前作为上游类型存在，没有独立客户端入口。"},
}

func Protocols() []ProtocolInfo {
	out := make([]ProtocolInfo, len(knownProtocols))
	copy(out, knownProtocols)
	return out
}

func ClientProtocols() []Protocol {
	protocols := make([]Protocol, 0, len(knownProtocols))
	for _, info := range knownProtocols {
		if info.Inbound {
			protocols = append(protocols, info.Protocol)
		}
	}
	return protocols
}

func ParseProtocol(value string) (Protocol, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, "/", "_")
	switch normalized {
	case "openai_chat", "chat", "chat_completions", "openai_chat_completions":
		return ProtocolOpenAIChat, true
	case "openai_responses", "responses", "openai_response":
		return ProtocolOpenAIResponses, true
	case "anthropic", "anthropic_messages", "messages":
		return ProtocolAnthropic, true
	case "gemini", "gemini_contents", "generate_content", "generatecontent":
		return ProtocolGemini, true
	case "codex", "codex_responses", "codex_response":
		return ProtocolCodexResponses, true
	case "openai_embeddings", "openai_embedding", "embeddings", "embedding":
		return ProtocolOpenAIEmbedding, true
	case "volcengine", "volcengine_responses":
		return ProtocolVolcengine, true
	default:
		return "", false
	}
}

func OutboundProtocol(t outbound.OutboundType) Protocol {
	switch t {
	case outbound.OutboundTypeOpenAIChat:
		return ProtocolOpenAIChat
	case outbound.OutboundTypeOpenAIResponse:
		return ProtocolOpenAIResponses
	case outbound.OutboundTypeAnthropic:
		return ProtocolAnthropic
	case outbound.OutboundTypeGemini:
		return ProtocolGemini
	case outbound.OutboundTypeVolcengine:
		return ProtocolVolcengine
	case outbound.OutboundTypeOpenAIEmbedding:
		return ProtocolOpenAIEmbedding
	default:
		return ""
	}
}

func OutboundProtocolName(t outbound.OutboundType) string {
	protocol := OutboundProtocol(t)
	if protocol == "" {
		return fmt.Sprintf("unknown_outbound_%d", t)
	}
	return string(protocol)
}

func MatrixSnapshot() Matrix {
	clients := []Protocol{ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropic, ProtocolGemini, ProtocolCodexResponses, ProtocolOpenAIEmbedding}
	upstreams := []Protocol{ProtocolOpenAIChat, ProtocolOpenAIResponses, ProtocolAnthropic, ProtocolGemini, ProtocolCodexResponses, ProtocolOpenAIEmbedding, ProtocolVolcengine}
	items := make([]TransformCapability, 0, len(clients)*len(upstreams))
	for _, client := range clients {
		for _, upstream := range upstreams {
			items = append(items, Evaluate(client, upstream))
		}
	}
	return Matrix{
		Protocols: Protocols(),
		Matrix:    items,
		Notes: []string{
			"当前实现采用 Inbound -> InternalLLMRequest/InternalLLMResponse -> Outbound 的中心 IR 架构，不是 ccLoad 那种显式 pair converter registry。",
			"Gemini 当前只有上游转换，没有客户端 Gemini GenerateContent 入口。",
			"Codex 当前按 OpenAI Responses profile 处理，不是独立协议枚举。",
		},
	}
}

func EvaluateOutbound(client Protocol, upstream outbound.OutboundType) TransformCapability {
	return Evaluate(client, OutboundProtocol(upstream))
}

func Evaluate(client Protocol, upstream Protocol) TransformCapability {
	client = normalizeKnown(client)
	upstream = normalizeKnown(upstream)
	cap := TransformCapability{
		ClientProtocol:   client,
		UpstreamProtocol: upstream,
		RequestFamily:    requestFamily(client),
		Supported:        true,
		Support:          SupportPartial,
		Mode:             ModeIR,
		Features:         partialChatFeatures(),
	}

	if client == "" || upstream == "" {
		cap.Supported = false
		cap.Support = SupportMissing
		cap.Mode = ModeMissing
		cap.MissingReason = "unknown protocol"
		cap.Features = missingFeatures()
		return cap
	}

	if !hasInbound(client) {
		cap.Supported = false
		cap.Support = SupportMissing
		cap.Mode = ModeMissing
		cap.MissingReason = "client protocol has no inbound adapter"
		cap.Features = missingFeatures()
		return cap
	}
	if !hasOutbound(upstream) {
		cap.Supported = false
		cap.Support = SupportMissing
		cap.Mode = ModeMissing
		cap.MissingReason = "upstream protocol has no outbound adapter"
		cap.Features = missingFeatures()
		return cap
	}

	clientFamily := requestFamily(client)
	upstreamFamily := requestFamily(upstream)
	cap.RequestFamily = clientFamily
	if clientFamily != upstreamFamily {
		cap.Supported = false
		cap.Support = SupportMissing
		cap.Mode = ModeMissing
		cap.MissingReason = fmt.Sprintf("request family mismatch: %s cannot route to %s", clientFamily, upstreamFamily)
		cap.Features = missingFeatures()
		return cap
	}

	if clientFamily == FamilyEmbedding {
		if client == ProtocolOpenAIEmbedding && upstream == ProtocolOpenAIEmbedding {
			cap.Support = SupportFull
			cap.Mode = ModeNative
			cap.NativePassthrough = true
			cap.Features = FeatureSupport{Stream: SupportMissing, Tools: SupportMissing, Thinking: SupportMissing, Images: SupportMissing, Files: SupportMissing, CacheControl: SupportMissing}
			return cap
		}
		cap.Supported = false
		cap.Support = SupportMissing
		cap.Mode = ModeMissing
		cap.MissingReason = "embedding requests currently only route to OpenAI embedding upstreams"
		cap.Features = missingFeatures()
		return cap
	}

	if client == ProtocolCodexResponses {
		cap.Mode = ModeProfile
		cap.Features = partialChatFeatures()
		cap.LossyReasons = append(cap.LossyReasons, "Codex is represented as an OpenAI Responses profile, so unsupported Codex-specific fields depend on Responses passthrough support")
		if upstream == ProtocolOpenAIResponses || upstream == ProtocolCodexResponses {
			cap.Support = SupportFull
			cap.NativePassthrough = true
			cap.Features = nativeChatFeatures(ProtocolOpenAIResponses)
			return cap
		}
	}

	if client == upstream {
		cap.Support = SupportFull
		cap.Mode = ModeNative
		cap.NativePassthrough = true
		cap.Features = nativeChatFeatures(client)
		return cap
	}

	if upstream == ProtocolCodexResponses {
		upstream = ProtocolOpenAIResponses
		cap.UpstreamProtocol = ProtocolCodexResponses
		cap.Mode = ModeProfile
	}

	cap.LossyReasons = lossyReasons(client, upstream)
	return cap
}

func normalizeKnown(protocol Protocol) Protocol {
	if parsed, ok := ParseProtocol(string(protocol)); ok {
		return parsed
	}
	return ""
}

func hasInbound(protocol Protocol) bool {
	for _, info := range knownProtocols {
		if info.Protocol == protocol {
			return info.Inbound
		}
	}
	return false
}

func hasOutbound(protocol Protocol) bool {
	for _, info := range knownProtocols {
		if info.Protocol == protocol {
			return info.Outbound
		}
	}
	return false
}

func requestFamily(protocol Protocol) RequestFamily {
	for _, info := range knownProtocols {
		if info.Protocol == protocol {
			return info.RequestFamily
		}
	}
	return "unknown"
}

func nativeChatFeatures(protocol Protocol) FeatureSupport {
	features := FeatureSupport{Stream: SupportFull, Tools: SupportFull, Thinking: SupportPartial, Images: SupportPartial, Files: SupportPartial, CacheControl: SupportPartial}
	switch protocol {
	case ProtocolOpenAIResponses, ProtocolCodexResponses:
		features.Thinking = SupportFull
		features.Images = SupportFull
		features.Files = SupportFull
		features.CacheControl = SupportFull
	case ProtocolAnthropic:
		features.Thinking = SupportFull
		features.CacheControl = SupportFull
	case ProtocolGemini:
		features.Thinking = SupportPartial
		features.Images = SupportFull
		features.Files = SupportPartial
	case ProtocolOpenAIChat, ProtocolVolcengine:
		features.Thinking = SupportPartial
	}
	return features
}

func partialChatFeatures() FeatureSupport {
	return FeatureSupport{Stream: SupportFull, Tools: SupportPartial, Thinking: SupportPartial, Images: SupportPartial, Files: SupportPartial, CacheControl: SupportPartial}
}

func missingFeatures() FeatureSupport {
	return FeatureSupport{Stream: SupportMissing, Tools: SupportMissing, Thinking: SupportMissing, Images: SupportMissing, Files: SupportMissing, CacheControl: SupportMissing}
}

func lossyReasons(client Protocol, upstream Protocol) []string {
	reasons := []string{"cross-protocol routing uses the internal request/response model, so provider-specific raw fields may be dropped"}
	if client == ProtocolOpenAIResponses || upstream == ProtocolOpenAIResponses {
		reasons = append(reasons, "OpenAI Responses item-level fields are only lossless on same-protocol passthrough paths")
	}
	if client == ProtocolAnthropic || upstream == ProtocolAnthropic {
		reasons = append(reasons, "Anthropic beta fields, cache-control details, and thinking signatures are only lossless on same-protocol passthrough paths")
	}
	if client == ProtocolGemini || upstream == ProtocolGemini {
		reasons = append(reasons, "Gemini contents, safety settings, and media/file metadata are normalized before routing")
	}
	if client == ProtocolOpenAIChat || upstream == ProtocolOpenAIChat || upstream == ProtocolVolcengine {
		reasons = append(reasons, "Chat-completions style upstreams do not preserve all Responses or Anthropic structured output semantics")
	}
	return reasons
}

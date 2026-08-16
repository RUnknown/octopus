package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/relay"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/v1").
		Use(middleware.APIKeyAuth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/chat/completions", http.MethodPost).
				Handle(chat),
		).
		AddRoute(
			router.NewRoute("/responses", http.MethodPost).
				Handle(response),
		).
		AddRoute(
			router.NewRoute("/codex/responses", http.MethodPost).
				Handle(response),
		).
		AddRoute(
			router.NewRoute("/responses/compact", http.MethodPost).
				Handle(responseCompact),
		).
		AddRoute(
			router.NewRoute("/messages", http.MethodPost).
				Handle(message),
		).
		AddRoute(
			router.NewRoute("/embeddings", http.MethodPost).
				Handle(embedding),
		)

	// WebSocket route for /v1/responses (no RequireJSON middleware)
	router.NewGroupRouter("/v1").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/responses", http.MethodGet).
				Handle(wsResponse),
		).
		AddRoute(
			router.NewRoute("/codex/responses", http.MethodGet).
				Handle(wsResponse),
		)

	// Compatibility alias used by clients that point chatgpt_base_url at a
	// ChatGPT-style backend. Both ingress paths still normalize to the regular
	// OpenAI Responses transformer and /v1/responses upstream path.
	router.NewGroupRouter("/backend-api/codex").
		Use(middleware.APIKeyAuth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/responses", http.MethodPost).
				Handle(response),
		)
	router.NewGroupRouter("/backend-api/codex").
		Use(middleware.APIKeyAuth()).
		AddRoute(
			router.NewRoute("/responses", http.MethodGet).
				Handle(wsResponse),
		)
}

func chat(c *gin.Context) {
	relay.Handler(inbound.InboundTypeOpenAIChat, c)
}
func response(c *gin.Context) {
	relay.Handler(inbound.InboundTypeOpenAIResponse, c)
}
func responseCompact(c *gin.Context) {
	relay.HandleResponsesCompact(c)
}
func message(c *gin.Context) {
	relay.Handler(inbound.InboundTypeAnthropic, c)
}
func embedding(c *gin.Context) {
	relay.Handler(inbound.InboundTypeOpenAIEmbedding, c)
}
func wsResponse(c *gin.Context) {
	relay.HandleWSResponse(c)
}

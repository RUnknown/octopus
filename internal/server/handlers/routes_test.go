package handlers

import (
	"net/http"
	"testing"

	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func TestRegisterHandlerRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := router.RegisterAll(engine); err != nil {
		t.Fatalf("register handler routes: %v", err)
	}

	expected := map[string]bool{
		http.MethodDelete + " /api/v1/runtime/clear":              false,
		http.MethodPost + " /api/v1/channel/test-image":           false,
		http.MethodPost + " /api/v1/channel/test-sub2api-balance": false,
		http.MethodPost + " /v1/codex/responses":                  false,
		http.MethodGet + " /v1/codex/responses":                   false,
		http.MethodPost + " /backend-api/codex/responses":         false,
		http.MethodGet + " /backend-api/codex/responses":          false,
	}
	for _, route := range engine.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := expected[key]; ok {
			expected[key] = true
		}
	}
	for route, found := range expected {
		if !found {
			t.Fatalf("expected %s to be registered", route)
		}
	}
}

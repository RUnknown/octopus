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

	foundRuntimeClear := false
	for _, route := range engine.Routes() {
		if route.Method == http.MethodDelete && route.Path == "/api/v1/runtime/clear" {
			foundRuntimeClear = true
			break
		}
	}
	if !foundRuntimeClear {
		t.Fatal("expected DELETE /api/v1/runtime/clear to be registered")
	}
}

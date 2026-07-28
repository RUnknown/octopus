package handlers

import (
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
}

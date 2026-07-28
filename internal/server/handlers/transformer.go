package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/transformer/capability"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/transformer").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/capabilities", http.MethodGet).
				Handle(getTransformerCapabilities),
		)
}

func getTransformerCapabilities(c *gin.Context) {
	resp.Success(c, capability.MatrixSnapshot())
}

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCreateChannelRejectsDuplicatedURLScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/channel/create", strings.NewReader(`{
		"name":"invalid",
		"type":0,
		"base_urls":[{"url":"https://https://example.com","delay":0}],
		"keys":[{"enabled":true,"channel_key":"secret"}],
		"model":"gpt-4o"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	createChannel(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "duplicated URL scheme") {
		t.Fatalf("unexpected response body: %s", recorder.Body.String())
	}
}

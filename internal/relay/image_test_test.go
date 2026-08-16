package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestRunImageGenerationTest(t *testing.T) {
	var observed map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %q, want /v1/images/generations", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer image-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := request.Header.Get("X-Test-Header"); got != "present" {
			t.Errorf("custom header = %q", got)
		}
		if err := json.NewDecoder(request.Body).Decode(&observed); err != nil {
			t.Errorf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"created":1,"data":[{"b64_json":"aW1hZ2U=","revised_prompt":"revised"}]}`))
	}))
	defer upstream.Close()

	channel := &dbmodel.Channel{
		ID:           9,
		Type:         outbound.OutboundTypeOpenAIChat,
		BaseUrls:     []dbmodel.BaseUrl{{URL: upstream.URL + "/v1"}},
		Keys:         []dbmodel.ChannelKey{{ID: 12, Enabled: true, ChannelKey: "image-key"}},
		CustomHeader: []dbmodel.CustomHeader{{HeaderKey: "X-Test-Header", HeaderValue: "present"}},
	}
	result, key, err := RunImageGenerationTest(context.Background(), channel, ImageGenerationTestRequest{
		ChannelID:    9,
		Model:        "gpt-image-1",
		Prompt:       "draw an octopus",
		Size:         "1024x1024",
		OutputFormat: "png",
	})
	if err != nil {
		t.Fatalf("RunImageGenerationTest() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || result.KeyID != 12 || key.ID != 12 {
		t.Fatalf("unexpected result=%+v key=%+v", result, key)
	}
	if observed["model"] != "gpt-image-1" || observed["prompt"] != "draw an octopus" ||
		observed["size"] != "1024x1024" || observed["output_format"] != "png" {
		t.Fatalf("unexpected request payload: %+v", observed)
	}
	if !json.Valid(result.Body) {
		t.Fatalf("response body is not valid JSON: %s", result.Body)
	}
}

func TestRunImageGenerationTestReturnsUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer upstream.Close()

	channel := &dbmodel.Channel{
		ID:       3,
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []dbmodel.BaseUrl{{URL: upstream.URL}},
		Keys:     []dbmodel.ChannelKey{{ID: 4, Enabled: true, ChannelKey: "key"}},
	}
	result, key, err := RunImageGenerationTest(context.Background(), channel, ImageGenerationTestRequest{
		ChannelID: 3,
		Model:     "image-model",
		Prompt:    "test",
	})
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if result == nil || result.StatusCode != http.StatusTooManyRequests || key.ID != 4 {
		t.Fatalf("unexpected result=%+v key=%+v error=%v", result, key, err)
	}
}

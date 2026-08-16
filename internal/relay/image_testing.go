package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
)

type ImageGenerationTestRequest struct {
	ChannelID    int    `json:"channel_id"`
	KeyID        int    `json:"key_id,omitempty"`
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	Size         string `json:"size,omitempty"`
	Quality      string `json:"quality,omitempty"`
	Background   string `json:"background,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
}

type ImageGenerationTestResult struct {
	StatusCode  int             `json:"status_code"`
	DurationMS  int64           `json:"duration_ms"`
	ContentType string          `json:"content_type,omitempty"`
	KeyID       int             `json:"key_id"`
	Body        json.RawMessage `json:"body"`
}

func (request *ImageGenerationTestRequest) Validate() error {
	if request == nil {
		return fmt.Errorf("image test request is required")
	}
	if request.ChannelID <= 0 {
		return fmt.Errorf("channel id is required")
	}
	request.Model = strings.TrimSpace(request.Model)
	request.Prompt = strings.TrimSpace(request.Prompt)
	request.Size = strings.TrimSpace(request.Size)
	request.Quality = strings.TrimSpace(request.Quality)
	request.Background = strings.TrimSpace(request.Background)
	request.OutputFormat = strings.TrimSpace(request.OutputFormat)
	if request.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(request.Model) > 256 {
		return fmt.Errorf("model is too long")
	}
	if request.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if len(request.Prompt) > 20000 {
		return fmt.Errorf("prompt is too long")
	}
	return nil
}

func RunImageGenerationTest(ctx context.Context, channel *dbmodel.Channel, request ImageGenerationTestRequest) (*ImageGenerationTestResult, dbmodel.ChannelKey, error) {
	if err := request.Validate(); err != nil {
		return nil, dbmodel.ChannelKey{}, err
	}
	if channel == nil || channel.ID != request.ChannelID {
		return nil, dbmodel.ChannelKey{}, fmt.Errorf("channel not found")
	}
	if channel.Type != outbound.OutboundTypeOpenAIChat && channel.Type != outbound.OutboundTypeOpenAIResponse {
		return nil, dbmodel.ChannelKey{}, fmt.Errorf("channel type does not support the OpenAI Images API")
	}
	if err := dbmodel.ValidateChannelBaseURLs(channel.BaseUrls); err != nil {
		return nil, dbmodel.ChannelKey{}, err
	}

	usedKey := selectImageTestKey(channel, request.KeyID)
	if usedKey.ChannelKey == "" {
		if request.KeyID > 0 {
			return nil, dbmodel.ChannelKey{}, fmt.Errorf("selected channel key is unavailable")
		}
		return nil, dbmodel.ChannelKey{}, fmt.Errorf("channel has no available key")
	}

	payload := map[string]any{
		"model":  request.Model,
		"prompt": request.Prompt,
		"n":      1,
	}
	for key, value := range map[string]string{
		"size":          request.Size,
		"quality":       request.Quality,
		"background":    request.Background,
		"output_format": request.OutputFormat,
	} {
		if value != "" {
			payload[key] = value
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, usedKey, fmt.Errorf("encode image test request: %w", err)
	}

	parsedURL, err := url.Parse(strings.TrimSuffix(channel.GetBaseUrl(), "/"))
	if err != nil {
		return nil, usedKey, fmt.Errorf("parse channel base URL: %w", err)
	}
	parsedURL.Path = strings.TrimSuffix(parsedURL.Path, "/") + "/images/generations"
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, usedKey, fmt.Errorf("create image test request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+usedKey.ChannelKey)
	httpRequest.Header.Set("User-Agent", "")
	for _, header := range channel.CustomHeader {
		if strings.TrimSpace(header.HeaderKey) != "" {
			httpRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}

	client, err := helper.ChannelHTTPClientWithContext(ctx, channel)
	if err != nil {
		return nil, usedKey, err
	}
	startedAt := time.Now()
	response, err := client.Do(httpRequest)
	if err != nil {
		return nil, usedKey, fmt.Errorf("send image test request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := iolimit.ReadAll(response.Body, iolimit.UpstreamResponseMaxBytes())
	if err != nil {
		return nil, usedKey, fmt.Errorf("read image test response: %w", err)
	}
	result := &ImageGenerationTestResult{
		StatusCode:  response.StatusCode,
		DurationMS:  time.Since(startedAt).Milliseconds(),
		ContentType: response.Header.Get("Content-Type"),
		KeyID:       usedKey.ID,
		Body:        append(json.RawMessage(nil), responseBody...),
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result, usedKey, fmt.Errorf("upstream image test failed with status %d: %s", response.StatusCode, truncateImageTestError(responseBody))
	}
	if len(responseBody) == 0 || !json.Valid(responseBody) {
		return result, usedKey, fmt.Errorf("upstream image test returned invalid JSON")
	}
	return result, usedKey, nil
}

func selectImageTestKey(channel *dbmodel.Channel, keyID int) dbmodel.ChannelKey {
	if channel == nil {
		return dbmodel.ChannelKey{}
	}
	if keyID <= 0 {
		return channel.GetChannelKey()
	}
	for _, key := range channel.Keys {
		if key.ID == keyID && key.Enabled && strings.TrimSpace(key.ChannelKey) != "" {
			return key
		}
	}
	return dbmodel.ChannelKey{}
}

func truncateImageTestError(body []byte) string {
	const maxErrorBytes = 16 * 1024
	if len(body) <= maxErrorBytes {
		return strings.TrimSpace(string(body))
	}
	return strings.TrimSpace(string(body[:maxErrorBytes])) + "...(truncated)"
}

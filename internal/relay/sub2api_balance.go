package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
)

const sub2APIBalanceTimeout = 20 * time.Second

type Sub2APIBalanceTestRequest struct {
	ChannelID int `json:"channel_id"`
	KeyID     int `json:"key_id,omitempty"`
}

type Sub2APIBalanceTestResult struct {
	StatusCode int      `json:"status_code"`
	DurationMS int64    `json:"duration_ms"`
	KeyID      int      `json:"key_id"`
	Mode       string   `json:"mode,omitempty"`
	PlanName   string   `json:"plan_name,omitempty"`
	Remaining  float64  `json:"remaining"`
	Balance    *float64 `json:"balance,omitempty"`
	Limit      *float64 `json:"limit,omitempty"`
	Used       *float64 `json:"used,omitempty"`
	Unit       string   `json:"unit"`
	IsValid    *bool    `json:"is_valid,omitempty"`
}

func (request *Sub2APIBalanceTestRequest) Validate() error {
	if request == nil {
		return fmt.Errorf("balance test request is required")
	}
	if request.ChannelID <= 0 {
		return fmt.Errorf("channel id is required")
	}
	if request.KeyID < 0 {
		return fmt.Errorf("key id must be non-negative")
	}
	return nil
}

func RunSub2APIBalanceTest(ctx context.Context, channel *dbmodel.Channel, request Sub2APIBalanceTestRequest) (*Sub2APIBalanceTestResult, dbmodel.ChannelKey, error) {
	if err := request.Validate(); err != nil {
		return nil, dbmodel.ChannelKey{}, err
	}
	if channel == nil || channel.ID != request.ChannelID {
		return nil, dbmodel.ChannelKey{}, fmt.Errorf("channel not found")
	}
	if err := dbmodel.ValidateChannelBaseURLs(channel.BaseUrls); err != nil {
		return nil, dbmodel.ChannelKey{}, err
	}

	usedKey := selectChannelTestKey(channel, request.KeyID)
	if usedKey.ChannelKey == "" {
		if request.KeyID > 0 {
			return nil, dbmodel.ChannelKey{}, fmt.Errorf("selected channel key is unavailable")
		}
		return nil, dbmodel.ChannelKey{}, fmt.Errorf("channel has no available key")
	}

	usageURL, err := buildSub2APIUsageURL(channel.GetBaseUrl())
	if err != nil {
		return nil, usedKey, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, sub2APIBalanceTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(requestCtx, http.MethodGet, usageURL, nil)
	if err != nil {
		return nil, usedKey, fmt.Errorf("create Sub2API balance request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	for _, header := range channel.CustomHeader {
		if strings.TrimSpace(header.HeaderKey) != "" {
			httpRequest.Header.Set(header.HeaderKey, header.HeaderValue)
		}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+strings.TrimSpace(usedKey.ChannelKey))

	httpClient, err := helper.ChannelHTTPClientWithContext(requestCtx, channel)
	if err != nil {
		return nil, usedKey, err
	}
	startedAt := time.Now()
	response, err := httpClient.Do(httpRequest)
	if err != nil {
		return nil, usedKey, fmt.Errorf("send Sub2API balance request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := iolimit.ReadAll(response.Body, iolimit.DefaultErrorBodyMaxBytes)
	if err != nil {
		return nil, usedKey, fmt.Errorf("read Sub2API balance response: %w", err)
	}
	result := &Sub2APIBalanceTestResult{
		StatusCode: response.StatusCode,
		DurationMS: time.Since(startedAt).Milliseconds(),
		KeyID:      usedKey.ID,
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result, usedKey, fmt.Errorf("Sub2API balance request failed with status %d: %s", response.StatusCode, truncateChannelTestError(responseBody))
	}
	if err := parseSub2APIBalanceResponse(responseBody, result); err != nil {
		return result, usedKey, err
	}
	return result, usedKey, nil
}

func buildSub2APIUsageURL(baseURL string) (string, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", fmt.Errorf("parse channel base URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("channel base URL must use http or https")
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("channel base URL must have a host")
	}
	path := strings.TrimRight(parsedURL.Path, "/")
	if strings.EqualFold(pathSegment(path), "v1") {
		parsedURL.Path = path + "/usage"
	} else {
		parsedURL.Path = path + "/v1/usage"
	}
	parsedURL.RawPath = ""
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	return parsedURL.String(), nil
}

func pathSegment(path string) string {
	if index := strings.LastIndexByte(path, '/'); index >= 0 {
		return path[index+1:]
	}
	return path
}

func parseSub2APIBalanceResponse(body []byte, result *Sub2APIBalanceTestResult) error {
	if result == nil {
		return fmt.Errorf("Sub2API balance result is required")
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("Sub2API balance response is invalid JSON: %w", err)
	}
	remaining, ok := firstNumber(payload,
		[]string{"remaining"},
		[]string{"quota", "remaining"},
		[]string{"usage", "remaining"},
		[]string{"balance"},
		[]string{"total_available"},
	)
	if !ok {
		return fmt.Errorf("Sub2API balance response is missing remaining balance")
	}
	result.Remaining = remaining
	result.Mode, _ = firstString(payload, []string{"mode"})
	result.PlanName, _ = firstString(payload, []string{"planName"}, []string{"plan_name"})
	result.Unit, _ = firstString(payload, []string{"unit"}, []string{"quota", "unit"}, []string{"usage", "unit"})
	if result.Unit == "" {
		result.Unit = "USD"
	}
	if value, found := firstNumber(payload, []string{"balance"}); found {
		result.Balance = &value
	}
	if value, found := firstNumber(payload, []string{"quota", "limit"}, []string{"limit"}, []string{"total_granted"}); found {
		result.Limit = &value
	}
	if value, found := firstNumber(payload, []string{"quota", "used"}, []string{"used"}); found {
		result.Used = &value
	}
	if value, found := firstBool(payload, []string{"isValid"}, []string{"is_valid"}, []string{"is_active"}); found {
		result.IsValid = &value
	}
	return nil
}

func firstNumber(payload map[string]any, paths ...[]string) (float64, bool) {
	for _, path := range paths {
		value, ok := nestedValue(payload, path)
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case json.Number:
			number, err := typed.Float64()
			if err == nil {
				return number, true
			}
		case float64:
			return typed, true
		case string:
			number, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
			if err == nil {
				return number, true
			}
		}
	}
	return 0, false
}

func firstString(payload map[string]any, paths ...[]string) (string, bool) {
	for _, path := range paths {
		value, ok := nestedValue(payload, path)
		if !ok {
			continue
		}
		if typed, ok := value.(string); ok && strings.TrimSpace(typed) != "" {
			return strings.TrimSpace(typed), true
		}
	}
	return "", false
}

func firstBool(payload map[string]any, paths ...[]string) (bool, bool) {
	for _, path := range paths {
		value, ok := nestedValue(payload, path)
		if !ok {
			continue
		}
		if typed, ok := value.(bool); ok {
			return typed, true
		}
	}
	return false, false
}

func nestedValue(payload map[string]any, path []string) (any, bool) {
	var value any = payload
	for _, segment := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

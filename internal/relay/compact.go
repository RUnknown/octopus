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
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/outlierwindow"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	openaiOutbound "github.com/bestruirui/octopus/internal/transformer/outbound/openai"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

type responsesCompactRequest struct {
	Model              string          `json:"model"`
	Input              json.RawMessage `json:"input,omitempty"`
	PreviousResponseID *string         `json:"previous_response_id,omitempty"`
}

type responsesCompactResponse struct {
	ID        string                         `json:"id"`
	Object    string                         `json:"object"`
	CreatedAt int64                          `json:"created_at"`
	Output    []openaiOutbound.ResponsesItem `json:"output"`
	Usage     *openaiOutbound.ResponsesUsage `json:"usage,omitempty"`
	Error     *transformerModel.ErrorDetail  `json:"error,omitempty"`
}

// HandleResponsesCompact proxies OpenAI-compatible /responses/compact requests upstream.
func HandleResponsesCompact(c *gin.Context) {
	body, err := iolimit.ReadRequestBody(c.Writer, c.Request, iolimit.RequestBodyMaxBytes())
	if err != nil {
		if iolimit.IsTooLarge(err) {
			resp.Error(c, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			resp.Error(c, http.StatusBadRequest, err.Error())
		}
		return
	}

	var compactReq responsesCompactRequest
	if err := json.Unmarshal(body, &compactReq); err != nil {
		resp.Error(c, http.StatusBadRequest, fmt.Sprintf("failed to decode responses compact request: %v", err))
		return
	}
	if strings.TrimSpace(compactReq.Model) == "" {
		resp.Error(c, http.StatusBadRequest, "model is required")
		return
	}
	if len(compactReq.Input) == 0 && compactReq.PreviousResponseID == nil {
		resp.Error(c, http.StatusBadRequest, "either input or previous_response_id is required")
		return
	}

	if !apiKeyAllowsModel(c.GetString("supported_models"), c.GetString("model_list_mode"), compactReq.Model) {
		resp.ErrorWithCode(c, http.StatusBadRequest, CodeRelayModelNotSupported, "model not supported")
		return
	}

	requestModel := compactReq.Model
	apiKeyID := c.GetInt("api_key_id")

	group, err := op.GroupGetEnabledMap(requestModel, c.Request.Context())
	if err != nil {
		resp.ErrorWithCode(c, http.StatusNotFound, CodeRelayModelNotFound, "model not found")
		return
	}

	iter := balancer.NewIterator(group, apiKeyID, requestModel)
	if iter.Len() == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}

	metricsReq := &transformerModel.InternalLLMRequest{Model: requestModel, RawRequest: body}
	metrics := NewRelayMetrics(apiKeyID, requestModel, body, metricsReq)
	activeRequestID := beginActiveRequest(apiKeyID, requestModel, false)
	defer finishActiveRequest(activeRequestID)

	var lastErr error
	var lastStatusCode int
	var lastRetryAfter time.Duration

	maxSameChannelRetries := 1
	if group.RetryEnabled {
		maxSameChannelRetries = group.MaxRetries
		if maxSameChannelRetries <= 0 {
			maxSameChannelRetries = 3
		}
	}

	for iter.Next() {
		updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
			view.Stage = "select_candidate"
			view.Attempt = iter.Index() + 1
			view.GroupID = group.ID
		})
		select {
		case <-c.Request.Context().Done():
			log.Infof("compact request context canceled, stopping retry")
			metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
			return
		default:
		}

		item := iter.Item()
		updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
			view.ChannelID = item.ChannelID
			view.ActualModel = item.ModelName
			view.LastMessage = "compact candidate selected"
		})
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			iter.Skip(item.ChannelID, 0, fmt.Sprintf("channel_%d", item.ChannelID), fmt.Sprintf("channel not found: %v", err))
			lastErr = err
			continue
		}
		if !channel.Enabled {
			iter.Skip(channel.ID, 0, channel.Name, "channel disabled")
			continue
		}
		if !supportsResponsesCompact(channel.Type) {
			iter.Skip(channel.ID, 0, channel.Name, "channel type not compatible with responses compact")
			continue
		}

		selectOpts := dbmodel.ChannelKeySelectOptions{
			ExcludeKeyIDs:  make(map[int]struct{}),
			PreferredKeyID: iter.StickyKeyID(),
		}
		var usedKey dbmodel.ChannelKey
		for {
			usedKey = channel.GetChannelKey(selectOpts)
			if usedKey.ChannelKey == "" {
				break
			}
			if !iter.SkipCircuitBreak(channel.ID, usedKey.ID, channel.Name) {
				break
			}
			selectOpts.ExcludeKeyIDs[usedKey.ID] = struct{}{}
			usedKey = dbmodel.ChannelKey{}
		}
		if usedKey.ChannelKey == "" {
			if len(selectOpts.ExcludeKeyIDs) == 0 {
				iter.Skip(channel.ID, 0, channel.Name, "no available key")
			}
			continue
		}
		updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
			view.Stage = "forwarding"
			view.ChannelID = channel.ID
			view.ChannelKeyID = usedKey.ID
			view.ChannelName = channel.Name
			view.ActualModel = item.ModelName
			view.LastMessage = "upstream compact request in progress"
		})

		var attemptErr error
		var statusCode int
		var retryAfter time.Duration
		var success bool

		for retryNum := 0; retryNum < maxSameChannelRetries; retryNum++ {
			if retryNum > 0 {
				updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
					view.Stage = "retry_backoff"
					view.Attempt = retryNum + 1
					view.LastMessage = "waiting before compact request retry"
				})
				delay := computeBackoff(retryNum, retryAfter)
				select {
				case <-c.Request.Context().Done():
					metrics.SaveWithChannelStats(c.Request.Context(), false, context.Canceled, iter.Attempts(), false)
					return
				case <-time.After(delay):
				}
			}
			updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
				view.Stage = "forwarding"
				view.Attempt = retryNum + 1
				view.LastMessage = "upstream compact request in progress"
			})

			statusCode, retryAfter, attemptErr = forwardResponsesCompact(c, metrics, iter, channel, usedKey, body)
			if attemptErr == nil {
				success = true
				break
			}
			if !isRetryableStatus(statusCode) {
				break
			}
		}

		usedKey.StatusCode = statusCode
		usedKey.LastUseTimeStamp = time.Now().Unix()
		op.ChannelKeyUpdate(usedKey)

		if success {
			updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
				view.Stage = "collecting_response"
				view.LastMessage = "upstream compact request succeeded"
			})
			op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestSuccess: 1})
			balancer.RecordSuccess(channel.ID, usedKey.ID, requestModel)
			// Auto策略：记录成功与延迟（毫秒），与 relay 主链路一致
			balancer.RecordAutoSuccess(channel.ID, requestModel)
			balancer.RecordAutoLatency(channel.ID, requestModel, span.Duration().Milliseconds())
			balancer.SetSticky(apiKeyID, requestModel, channel.ID, usedKey.ID)
			outlierwindow.Report(channel.ID, true, statusCode, time.Now())
			metrics.SaveWithChannelStats(c.Request.Context(), true, nil, iter.Attempts(), false)
			return
		}

		op.StatsChannelUpdate(channel.ID, dbmodel.StatsMetrics{RequestFailed: 1})
		updateActiveRequest(activeRequestID, func(view *ActiveRequestView) {
			view.Stage = "failed_attempt"
			if attemptErr != nil {
				view.LastMessage = attemptErr.Error()
			}
		})
		failureKind := circuitFailureKind(group.RetryEnabled, statusCode)
		balancer.RecordFailure(channel.ID, usedKey.ID, requestModel, failureKind)
		// Auto策略：记录失败，与 relay 主链路一致
		balancer.RecordAutoFailure(channel.ID, requestModel)
		outlierwindow.Report(channel.ID, false, statusCode, time.Now())
		lastErr = attemptErr
		lastStatusCode = statusCode
		lastRetryAfter = retryAfter
	}

	metrics.SaveWithChannelStats(c.Request.Context(), false, lastErr, iter.Attempts(), false)
	if lastErr == nil && lastStatusCode == 0 {
		resp.ErrorWithCode(c, http.StatusServiceUnavailable, CodeRelayNoAvailableChannel, "no available channel")
		return
	}
	if isPassthroughStatus(lastStatusCode) {
		if lastRetryAfter > 0 {
			c.Header("Retry-After", fmt.Sprintf("%d", int(lastRetryAfter.Seconds())))
		}
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	if lastStatusCode > 0 {
		resp.Error(c, lastStatusCode, "channel failed")
		return
	}
	resp.Error(c, http.StatusBadGateway, "channel failed")
}

func supportsResponsesCompact(channelType outbound.OutboundType) bool {
	switch channelType {
	case outbound.OutboundTypeOpenAIResponse:
		return true
	default:
		return false
	}
}

func forwardResponsesCompact(c *gin.Context, metrics *RelayMetrics, iter *balancer.Iterator, channel *dbmodel.Channel, usedKey dbmodel.ChannelKey, requestBody []byte) (int, time.Duration, error) {
	span := iter.StartAttempt(channel.ID, usedKey.ID, channel.Name)
	request, err := buildResponsesCompactRequest(c.Request.Context(), channel, usedKey.ChannelKey, requestBody)
	if err != nil {
		span.SetRetryable(true)
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to create compact request: %w", err)
	}
	metrics.SetTransportRequestPayload(requestBody, metrics.RequestModel)
	copyProxyHeaders(c.Request.Header, channel, request.Header)

	response, err := sendCompactRequest(channel, request)
	if err != nil {
		span.SetRetryable(true)
		span.End(dbmodel.AttemptFailed, 0, err.Error())
		return 0, 0, fmt.Errorf("failed to send compact request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, truncated, readErr := iolimit.ReadAtMost(response.Body, iolimit.DefaultErrorBodyMaxBytes)
		if readErr != nil {
			span.SetRetryable(isRetryableStatus(response.StatusCode))
			span.End(dbmodel.AttemptFailed, response.StatusCode, readErr.Error())
			return response.StatusCode, 0, fmt.Errorf("failed to read compact response body: %w", readErr)
		}
		if truncated {
			body = append(body, []byte("\n[upstream error body truncated]")...)
		}
		retryAfter := parseRetryAfter(response.Header.Get("Retry-After"))
		statusCode := normalizeUpstreamStatusCode(response.StatusCode, string(body))
		span.SetRetryable(isRetryableStatus(statusCode))
		span.End(dbmodel.AttemptFailed, statusCode, string(body))
		return statusCode, retryAfter, fmt.Errorf("upstream error: %d: %s", response.StatusCode, string(body))
	}

	body, readErr := iolimit.ReadAll(response.Body, iolimit.UpstreamResponseMaxBytes())
	if readErr != nil {
		span.SetRetryable(isRetryableStatus(response.StatusCode))
		span.End(dbmodel.AttemptFailed, response.StatusCode, readErr.Error())
		return response.StatusCode, 0, fmt.Errorf("failed to read compact response body: %w", readErr)
	}

	copyProxyResponseHeaders(c.Writer.Header(), response.Header)
	contentType := response.Header.Get("Content-Type")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	c.Data(response.StatusCode, contentType, body)

	var compactResp responsesCompactResponse
	if err := json.Unmarshal(body, &compactResp); err == nil {
		metrics.SetInternalResponse(compactResponseToInternalResponse(&compactResp), metrics.RequestModel)
	}

	span.End(dbmodel.AttemptSuccess, response.StatusCode, "")
	return response.StatusCode, 0, nil
}

func buildResponsesCompactRequest(ctx context.Context, channel *dbmodel.Channel, key string, requestBody []byte) (*http.Request, error) {
	parsedURL, err := url.Parse(strings.TrimSuffix(channel.GetBaseUrl(), "/"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse base url: %w", err)
	}
	parsedURL.Path = parsedURL.Path + "/responses/compact"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, parsedURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	return req, nil
}

func copyProxyHeaders(src http.Header, channel *dbmodel.Channel, dst http.Header) {
	for key, values := range src {
		lowerKey := strings.ToLower(key)
		if hopByHopHeaders[lowerKey] || lowerKey == "content-type" {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	for _, header := range channel.CustomHeader {
		if strings.EqualFold(header.HeaderKey, "Content-Type") {
			continue
		}
		dst.Set(header.HeaderKey, header.HeaderValue)
	}
	// 防止 Go 默认 User-Agent 泄露到上游
	if dst.Get("User-Agent") == "" {
		dst.Set("User-Agent", "")
	}
}

func copyProxyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		dst.Del(key)
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func sendCompactRequest(channel *dbmodel.Channel, req *http.Request) (*http.Response, error) {
	httpClient, err := helper.ChannelHTTPClientWithContext(req.Context(), channel)
	if err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func compactResponseToInternalResponse(resp *responsesCompactResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}
	return &transformerModel.InternalLLMResponse{
		ID:      resp.ID,
		Object:  resp.Object,
		Created: resp.CreatedAt,
		Usage:   convertCompactUsage(resp.Usage),
	}
}

func convertCompactUsage(usage *openaiOutbound.ResponsesUsage) *transformerModel.Usage {
	if usage == nil {
		return nil
	}
	result := &transformerModel.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.TotalTokens,
	}
	if usage.InputTokenDetails.CachedTokens > 0 {
		result.PromptTokensDetails = &transformerModel.PromptTokensDetails{
			CachedTokens: usage.InputTokenDetails.CachedTokens,
		}
	}
	if usage.OutputTokenDetails.ReasoningTokens > 0 {
		result.CompletionTokensDetails = &transformerModel.CompletionTokensDetails{
			ReasoningTokens: usage.OutputTokenDetails.ReasoningTokens,
		}
	}
	return result
}

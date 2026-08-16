package relay

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/tokenizer"
)

// maxAttemptsLogged 是单条 relay_log 中最多持久化的 attempts 决策记录条数。
// 防止极端场景（如所有渠道/Key 均不可用时的连续跳过/熔断）attempts 无限膨胀
// 撑爆数据库（对齐上游 issue #192 修复思路）。
const maxAttemptsLogged = 256

// capAttemptsForLog 将 attempts 决策记录截断到 maxAttemptsLogged 条为止，
// 并返回截断后的切片与原始总数（TotalAttempts 用总数，避免信息丢失）。
func capAttemptsForLog(attempts []model.ChannelAttempt) ([]model.ChannelAttempt, int) {
	total := len(attempts)
	if total > maxAttemptsLogged {
		attempts = attempts[:maxAttemptsLogged]
		log.Warnf("truncating relay_log attempts from %d to %d entries", total, maxAttemptsLogged)
	}
	return attempts, total
}

// capRelayLogContent 将单条日志的请求/响应正文按合计上限截断。
// maxContentSizeMB < 0 表示不限制；超限时优先截断响应正文（保留请求可审计内容）。
func capRelayLogContent(reqContent, respContent string, maxContentSizeMB int) (string, string) {
	if maxContentSizeMB < 0 {
		return reqContent, respContent
	}
	const bytesPerMiB int64 = 1024 * 1024
	maxSize := int64(maxContentSizeMB) * bytesPerMiB
	total := int64(len(reqContent)) + int64(len(respContent))
	if total <= maxSize {
		return reqContent, respContent
	}
	log.Warnf("relay log content size=%d bytes exceeds limit=%d MiB, truncating", total, maxContentSizeMB)
	// 优先截断响应正文
	if respBudget := maxSize - int64(len(reqContent)); respBudget > 0 {
		respContent = respContent[:respBudget]
	} else {
		// 响应预算为负，说明请求已超限：按比例截断请求，响应留空
		reqContent = reqContent[:maxSize]
		respContent = ""
	}
	return reqContent, respContent
}

// RelayMetrics 负责最终的日志收集与持久化
type RelayMetrics struct {
	APIKeyID     int
	RequestModel string
	StartTime    time.Time
	LogID        int64

	// 首 Token 时间
	FirstTokenTime time.Time

	// 请求和响应内容
	RawRequest       []byte
	InternalRequest  *transformerModel.InternalLLMRequest
	InternalResponse *transformerModel.InternalLLMResponse

	// 统计指标
	ActualModel string
	Stats       model.StatsMetrics
	UsedWS      bool
	WSMode      *model.RelayLogWSMode
	WSExecMode  *model.RelayLogWSExecMode
	WSRecovery  *model.RelayLogWSRecovery

	TransportInputTokens *int
	BillInputTokens      *int
	CacheReadTokens      *int
	CacheWriteTokens     *int
}

func (m *RelayMetrics) StartLog() {
	if m.LogID != 0 {
		return
	}
	m.LogID = op.RelayLogStart(model.RelayLog{
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ActualModelName:  m.RequestModel,
	})
}

func NewRelayMetrics(apiKeyID int, requestModel string, rawBody []byte, req *transformerModel.InternalLLMRequest) *RelayMetrics {
	return &RelayMetrics{
		APIKeyID:        apiKeyID,
		RequestModel:    requestModel,
		StartTime:       time.Now(),
		RawRequest:      rawBody,
		InternalRequest: req,
	}
}

func (m *RelayMetrics) SetFirstTokenTime(t time.Time) {
	m.FirstTokenTime = t
}

func (m *RelayMetrics) SetTransportRequestPayload(payload []byte, modelName string) {
	if len(payload) == 0 {
		return
	}
	count := tokenizer.CountTokens(string(payload), modelName)
	m.TransportInputTokens = intPtr(count)
}

func (m *RelayMetrics) SetWSMode(mode model.RelayLogWSMode) {
	if mode == "" {
		return
	}
	m.WSMode = wsModePtr(mode)
}

func (m *RelayMetrics) SetWSExecMode(mode model.RelayLogWSExecMode) {
	if mode == "" {
		return
	}
	m.WSExecMode = wsExecModePtr(mode)
}

func (m *RelayMetrics) SetWSRecovery(recovery model.RelayLogWSRecovery) {
	if recovery == "" {
		return
	}
	m.WSRecovery = wsRecoveryPtr(recovery)
}

func (m *RelayMetrics) SetInternalResponse(resp *transformerModel.InternalLLMResponse, actualModel string) {
	m.InternalResponse = resp
	m.ActualModel = actualModel

	if resp == nil {
		return
	}

	inputReported := false
	if usage := resp.Usage; usage != nil {
		nonCachedInput := usage.BillableNonCachedInput()
		cacheReadTokens := usage.BillableCacheReadInput()
		cacheWriteTokens := usage.BillableCacheWriteInput()

		m.BillInputTokens = intPtr(int(nonCachedInput))
		m.CacheReadTokens = intPtr(int(cacheReadTokens))
		m.CacheWriteTokens = intPtr(int(cacheWriteTokens))
		// Normalize input totals across protocols. OpenAI/Gemini already include
		// cached reads in PromptTokens, while Anthropic reports them separately.
		m.Stats.InputToken = usage.EffectiveInputTokens()
		m.Stats.OutputToken = usage.CompletionTokens
		m.Stats.CacheReadToken = cacheReadTokens
		m.Stats.CacheWriteToken = cacheWriteTokens
		inputReported = usage.EffectiveInputTokens() > 0

		if modelPrice := resolveModelPrice(actualModel); modelPrice != nil {
			m.Stats.InputCost = (float64(cacheReadTokens)*modelPrice.CacheRead +
				float64(cacheWriteTokens)*modelPrice.CacheWrite +
				float64(nonCachedInput)*modelPrice.Input) * 1e-6
			m.Stats.OutputCost = float64(usage.CompletionTokens) * modelPrice.Output * 1e-6
		}
	}

	// 降级：上游未上报 input（usage 缺失，或 usage 中输入侧全为 0）时，用请求侧
	// 估算的 TransportInputTokens 兜底，使 input token/费用不为 0；output 无法从
	// 请求侧估算，保持 0。tiktoken 统一用 o200k_base，对 Claude/Gemini 为近似值。
	if !inputReported && m.TransportInputTokens != nil && *m.TransportInputTokens > 0 {
		estimated := int64(*m.TransportInputTokens)
		m.Stats.InputToken = estimated
		m.BillInputTokens = intPtr(int(estimated))
		if modelPrice := resolveModelPrice(actualModel); modelPrice != nil {
			m.Stats.InputCost = float64(estimated) * modelPrice.Input * 1e-6
		}
	}
}

func (m *RelayMetrics) Save(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt) {
	m.SaveWithChannelStats(ctx, success, err, attempts, true)
}

func (m *RelayMetrics) SaveWithChannelStats(ctx context.Context, success bool, err error, attempts []model.ChannelAttempt, updateChannelStats bool) {
	duration := time.Since(m.StartTime)

	globalStats := model.StatsMetrics{
		WaitTime:        duration.Milliseconds(),
		InputToken:      m.Stats.InputToken,
		OutputToken:     m.Stats.OutputToken,
		CacheReadToken:  m.Stats.CacheReadToken,
		CacheWriteToken: m.Stats.CacheWriteToken,
		InputCost:       m.Stats.InputCost,
		OutputCost:      m.Stats.OutputCost,
	}
	if success {
		globalStats.RequestSuccess = 1
	} else {
		globalStats.RequestFailed = 1
	}

	channelID, channelName := finalChannel(attempts)
	op.StatsTotalUpdate(globalStats)
	op.StatsHourlyUpdate(globalStats)
	op.StatsDailyUpdate(context.Background(), globalStats)
	op.StatsAPIKeyUpdate(m.APIKeyID, globalStats)
	if updateChannelStats {
		op.StatsChannelUpdate(channelID, globalStats)
	} else {
		updateFinalChannelUsageStats(channelID, globalStats)
	}
	modelName := strings.TrimSpace(m.ActualModel)
	if modelName == "" {
		modelName = strings.TrimSpace(m.RequestModel)
	}
	if modelName != "" {
		_ = op.StatsModelNameUpdate(modelName, channelID, globalStats)
	}
	op.StatsSiteModelHourlyRecordAttempts(attempts, m.ActualModel)

	// 上游未上报 usage（或输入侧全为 0）时打告警，便于定位是哪个通道缺失 usage。
	if success && (m.InternalResponse == nil || m.InternalResponse.Usage == nil ||
		m.InternalResponse.Usage.EffectiveInputTokens() == 0) {
		fallbackInput := 0
		if m.TransportInputTokens != nil {
			fallbackInput = *m.TransportInputTokens
		}
		log.Debugw("relay.usage_missing",
			"actual_model", m.ActualModel,
			"channel_id", channelID,
			"channel", channelName,
			"had_usage", m.InternalResponse != nil && m.InternalResponse.Usage != nil,
			"fallback_input_tokens", fallbackInput,
		)
	}

	if conf.AppConfig.Log.Relay.Summary || !success {
		fields := []interface{}{
			"model", m.RequestModel,
			"actual_model", m.ActualModel,
			"channel_id", channelID,
			"channel", channelName,
			"success", success,
			"duration_ms", duration.Milliseconds(),
			"input_token", m.Stats.InputToken,
			"output_token", m.Stats.OutputToken,
			"input_cost", m.Stats.InputCost,
			"output_cost", m.Stats.OutputCost,
			"total_cost", m.Stats.InputCost + m.Stats.OutputCost,
			"attempts", len(attempts),
			"ws", m.UsedWS,
		}
		if success {
			log.Infow("relay.complete", fields...)
		} else {
			log.Warnw("relay.complete", fields...)
		}
	}

	m.saveLog(ctx, success, err, duration, attempts, channelID, channelName)
}

func finalChannel(attempts []model.ChannelAttempt) (int, string) {
	var lastID int
	var lastName string
	for i := len(attempts) - 1; i >= 0; i-- {
		a := attempts[i]
		if a.Status == model.AttemptSuccess {
			return a.ChannelID, a.ChannelName
		}
		if a.Status == model.AttemptFailed && lastID == 0 {
			lastID = a.ChannelID
			lastName = a.ChannelName
		}
	}
	return lastID, lastName
}

func (m *RelayMetrics) saveLog(ctx context.Context, success bool, err error, duration time.Duration, attempts []model.ChannelAttempt, channelID int, channelName string) {
	actualModel := m.ActualModel
	if actualModel == "" {
		actualModel = m.RequestModel
	}

	// 截断 attempts 决策记录条数（防爆库兜底）：TotalAttempts 保留原始总数
	attempts, totalAttempts := capAttemptsForLog(attempts)

	relayLog := model.RelayLog{
		ID:               m.LogID,
		Time:             m.StartTime.Unix(),
		RequestModelName: m.RequestModel,
		ChannelName:      channelName,
		ChannelId:        channelID,
		ActualModelName:  actualModel,
		UseTime:          int(duration.Milliseconds()),
		Attempts:         attempts,
		TotalAttempts:    totalAttempts,
		UsedWS:           m.UsedWS,
	}

	if apiKey, getErr := op.APIKeyGet(m.APIKeyID, ctx); getErr == nil {
		relayLog.RequestAPIKeyName = apiKey.Name
	}

	// 首字时间
	if !m.FirstTokenTime.IsZero() {
		relayLog.Ftut = int(m.FirstTokenTime.Sub(m.StartTime).Milliseconds())
	}

	// Relay logs preserve the upstream protocol's reported input semantics for
	// their existing per-request cache breakdown. Aggregated stats use the
	// normalized total input above.
	relayLog.InputTokens = int(m.Stats.InputToken)
	if m.InternalResponse != nil && m.InternalResponse.Usage != nil && m.InternalResponse.Usage.EffectiveInputTokens() > 0 {
		relayLog.InputTokens = int(m.InternalResponse.Usage.PromptTokens)
	}
	relayLog.OutputTokens = int(m.Stats.OutputToken)
	relayLog.Cost = m.Stats.InputCost + m.Stats.OutputCost
	relayLog.TransportInputTokens = m.TransportInputTokens
	relayLog.BillInputTokens = m.BillInputTokens
	relayLog.CacheReadTokens = m.CacheReadTokens
	relayLog.CacheWriteTokens = m.CacheWriteTokens
	relayLog.WSMode = m.WSMode
	relayLog.WSExecMode = m.WSExecMode
	relayLog.WSRecovery = m.WSRecovery

	// 请求内容：优先原始请求体，保留 provider 专有字段（如 Anthropic cache_control）
	if len(m.RawRequest) > 0 {
		relayLog.RequestContent = string(m.RawRequest)
	} else if m.InternalRequest != nil {
		if reqJSON, jsonErr := json.Marshal(m.InternalRequest); jsonErr == nil {
			relayLog.RequestContent = string(reqJSON)
		}
	}

	// 响应内容
	if m.InternalResponse != nil {
		respForLog := m.filterResponseForLog(m.InternalResponse)
		if respJSON, jsonErr := json.Marshal(respForLog); jsonErr == nil {
			relayLog.ResponseContent = string(respJSON)
		}
	}

	// 单条日志正文上限：防止超大请求/响应正文撑爆数据库或拖垮日志队列
	maxContentSizeMB, settingErr := op.SettingGetInt(model.SettingKeyRelayLogMaxContentSizeMB)
	if settingErr != nil || maxContentSizeMB < -1 {
		maxContentSizeMB = model.DefaultRelayLogMaxContentSizeMB
	}
	relayLog.RequestContent, relayLog.ResponseContent = capRelayLogContent(
		relayLog.RequestContent, relayLog.ResponseContent, maxContentSizeMB)

	// 错误信息
	if err != nil {
		relayLog.Error = err.Error()
	}
	relayLog.Success = success

	var logErr error
	if m.LogID != 0 {
		logErr = op.RelayLogUpdate(ctx, relayLog)
	} else {
		logErr = op.RelayLogAdd(ctx, relayLog)
	}
	if logErr != nil {
		log.Warnf("failed to save relay log: %v", logErr)
	}
}

func updateFinalChannelUsageStats(channelID int, metrics model.StatsMetrics) {
	if channelID == 0 {
		return
	}
	usageStats := model.StatsMetrics{
		InputToken:      metrics.InputToken,
		OutputToken:     metrics.OutputToken,
		CacheReadToken:  metrics.CacheReadToken,
		CacheWriteToken: metrics.CacheWriteToken,
		InputCost:       metrics.InputCost,
		OutputCost:      metrics.OutputCost,
	}
	if usageStats.InputToken == 0 && usageStats.OutputToken == 0 && usageStats.CacheReadToken == 0 && usageStats.CacheWriteToken == 0 && usageStats.InputCost == 0 && usageStats.OutputCost == 0 {
		return
	}
	op.StatsChannelUpdate(channelID, usageStats)
}

func intPtr(value int) *int {
	return &value
}

// resolveModelPrice returns the global price configured for the actual model.
func resolveModelPrice(actualModel string) *model.LLMPrice {
	return price.GetLLMPrice(actualModel)
}

func wsModePtr(value model.RelayLogWSMode) *model.RelayLogWSMode {
	return &value
}

func wsExecModePtr(value model.RelayLogWSExecMode) *model.RelayLogWSExecMode {
	return &value
}

func wsRecoveryPtr(value model.RelayLogWSRecovery) *model.RelayLogWSRecovery {
	return &value
}

// filterResponseForLog 创建响应的浅拷贝，过滤掉 images、MultipleContent 中的图片数据和 Audio.Data 以减少存储压力
func (m *RelayMetrics) filterResponseForLog(resp *transformerModel.InternalLLMResponse) *transformerModel.InternalLLMResponse {
	if resp == nil {
		return nil
	}

	filterMsg := func(msg *transformerModel.Message) *transformerModel.Message {
		if msg == nil {
			return nil
		}
		c := *msg
		c.Images = nil
		if len(c.Content.MultipleContent) > 0 {
			parts := make([]transformerModel.MessageContentPart, 0, len(c.Content.MultipleContent))
			for _, p := range c.Content.MultipleContent {
				if p.Type == "image_url" && p.ImageURL != nil {
					parts = append(parts, transformerModel.MessageContentPart{
						Type:     "image_url",
						ImageURL: &transformerModel.ImageURL{URL: "[image data omitted for storage]"},
					})
				} else {
					parts = append(parts, p)
				}
			}
			c.Content = transformerModel.MessageContent{Content: c.Content.Content, MultipleContent: parts}
		}
		if c.Audio != nil && c.Audio.Data != "" {
			a := *c.Audio
			a.Data = "[audio data omitted for storage]"
			c.Audio = &a
		}
		return &c
	}

	filtered := *resp
	filtered.Choices = make([]transformerModel.Choice, len(resp.Choices))
	for i, choice := range resp.Choices {
		filtered.Choices[i] = choice
		filtered.Choices[i].Message = filterMsg(choice.Message)
		filtered.Choices[i].Delta = filterMsg(choice.Delta)
	}
	return &filtered
}

package price

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/iolimit"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const defaultLLMPriceURL = "https://models.dev/api.json"

var Provider = []string{
	"openai",     // GPT 系列
	"anthropic",  // Claude 系列
	"google",     // Gemini 系列
	"deepseek",   // DeepSeek 系列
	"xai",        // Grok 系列
	"alibaba",    // Qwen 系列
	"zhipuai",    // GLM 系列
	"minimax",    // MiniMax 系列
	"moonshotai", // Kimi/Moonshot
	"v0",         // v0 系列
}

var lastUpdateTime time.Time
var lastUpdateTimeLock sync.RWMutex

func UpdateLLMPrice(ctx context.Context) error {
	log.Debugf("update LLM price task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("update LLM price task finished, update time: %s", time.Since(startTime))
	}()
	client, err := client.GetHTTPClientSystemProxy(false)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, configuredLLMPriceURL(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch LLM info: %s", resp.Status)
	}
	var rawPrice map[string]struct {
		Models map[string]struct {
			ID   string         `json:"id"`
			Cost model.LLMPrice `json:"cost"`
		} `json:"models"`
	}
	body, err := iolimit.ReadAll(resp.Body, iolimit.MetadataResponseMaxBytes())
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if err := json.Unmarshal(body, &rawPrice); err != nil {
		return fmt.Errorf("failed to parse LLM info: %w", err)
	}
	llmPriceLock.Lock()
	for _, provider := range Provider {
		for _, model := range rawPrice[provider].Models {
			model.ID = strings.ToLower(model.ID)
			llmPrice[model.ID] = model.Cost
		}
	}
	llmPriceLock.Unlock()
	lastUpdateTimeLock.Lock()
	lastUpdateTime = time.Now()
	lastUpdateTimeLock.Unlock()
	return nil
}

func configuredLLMPriceURL() string {
	value, err := op.SettingGetString(model.SettingKeyModelPriceURL)
	if err != nil {
		return defaultLLMPriceURL
	}
	return selectLLMPriceURL(value)
}

func selectLLMPriceURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultLLMPriceURL
	}
	return value
}

func GetLastUpdateTime() time.Time {
	lastUpdateTimeLock.RLock()
	value := lastUpdateTime
	lastUpdateTimeLock.RUnlock()
	return value
}

func GetLLMPrice(modelName string) *model.LLMPrice {
	modelName = strings.ToLower(modelName)
	price, err := op.LLMGet(modelName)
	if err == nil {
		return &price
	}
	llmPriceLock.RLock()
	defer llmPriceLock.RUnlock()
	price, ok := llmPrice[modelName]
	if !ok {
		return nil
	}
	return &price
}

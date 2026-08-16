package balancer

import (
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

// RequestRecord represents a single request outcome within the sliding window.
type RequestRecord struct {
	Timestamp time.Time
	Success   bool
}

// ChannelStats tracks request statistics for a single channel+model combination.
// It uses a ring buffer for the sliding window implementation.
type ChannelStats struct {
	mu sync.RWMutex

	// Ring buffer for sliding window
	window []RequestRecord
	head   int // Next write position
	count  int // Number of records written

	// Cached values to avoid frequent recomputation
	cachedSuccessRate  float64
	cachedTotalSamples int
	lastCacheUpdate    time.Time
	cacheValidDuration time.Duration

	// EMA-smoothed latency in milliseconds
	avgLatencyMs float64
}

// Global storage for channel statistics.
// Key format: "channelID:modelName"
var globalAutoStats sync.Map // string -> *ChannelStats

// statsKey generates the key for channel stats storage.
func statsKey(channelID int, modelName string) string {
	return buildKey2(channelID, normalizeAutoStatsModelName(modelName))
}

func normalizeAutoStatsModelName(modelName string) string {
	return strings.ToLower(strings.TrimSpace(modelName))
}

// getOrCreateStats retrieves or creates a ChannelStats entry.
func getOrCreateStats(channelID int, modelName string) *ChannelStats {
	key := statsKey(channelID, modelName)
	if v, ok := globalAutoStats.Load(key); ok {
		if s, ok := v.(*ChannelStats); ok {
			return s
		}
	}
	threshold := getSampleThreshold()
	entry := &ChannelStats{
		window:             make([]RequestRecord, threshold),
		cacheValidDuration: 5 * time.Second,
	}
	actual, _ := globalAutoStats.LoadOrStore(key, entry)
	if s, ok := actual.(*ChannelStats); ok {
		return s
	}
	return entry
}

// getMinSamples returns the minimum samples threshold before using success rate.
func getMinSamples() int {
	v, err := op.SettingGetInt(model.SettingKeyAutoStrategyMinSamples)
	if err != nil || v <= 0 {
		return 10
	}
	return v
}

// getTimeWindow returns the time window duration in seconds.
func getTimeWindow() time.Duration {
	v, err := op.SettingGetInt(model.SettingKeyAutoStrategyTimeWindow)
	if err != nil || v <= 0 {
		return 300 * time.Second
	}
	return time.Duration(v) * time.Second
}

// getSampleThreshold returns the sliding window size.
func getSampleThreshold() int {
	v, err := op.SettingGetInt(model.SettingKeyAutoStrategySampleThreshold)
	if err != nil || v <= 0 {
		return 100
	}
	return v
}

// Record records a request outcome to the sliding window.
func (cs *ChannelStats) Record(success bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Write to ring buffer
	cs.window[cs.head] = RequestRecord{
		Timestamp: time.Now(),
		Success:   success,
	}
	cs.head = (cs.head + 1) % len(cs.window)
	if cs.count < len(cs.window) {
		cs.count++
	}

	// Invalidate cache
	cs.lastCacheUpdate = time.Time{}
}

// GetStats returns the success rate and total samples within the time window.
func (cs *ChannelStats) GetStats(windowDuration time.Duration) (successRate float64, totalSamples int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now()

	// Return cached value if still valid
	if now.Sub(cs.lastCacheUpdate) < cs.cacheValidDuration && cs.cachedTotalSamples > 0 {
		return cs.cachedSuccessRate, cs.cachedTotalSamples
	}

	// Recompute stats
	cutoff := now.Add(-windowDuration)
	successCount := 0
	validCount := 0

	for i := 0; i < cs.count; i++ {
		record := cs.window[i]
		if record.Timestamp.After(cutoff) || record.Timestamp.Equal(cutoff) {
			validCount++
			if record.Success {
				successCount++
			}
		}
	}

	// Update cache
	if validCount > 0 {
		cs.cachedSuccessRate = float64(successCount) / float64(validCount)
	} else {
		cs.cachedSuccessRate = 0
	}
	cs.cachedTotalSamples = validCount
	cs.lastCacheUpdate = now

	return cs.cachedSuccessRate, cs.cachedTotalSamples
}

// RecordAutoSuccess records a successful request for the Auto strategy.
func RecordAutoSuccess(channelID int, modelName string) {
	stats := getOrCreateStats(channelID, modelName)
	stats.Record(true)
}

// RecordAutoFailure records a failed request for the Auto strategy.
func RecordAutoFailure(channelID int, modelName string) {
	stats := getOrCreateStats(channelID, modelName)
	stats.Record(false)
}

// RecordAutoLatency records the observed latency (in milliseconds) for the Auto strategy.
// Latency is smoothed via EMA with alpha=0.3 to dampen short-term spikes.
func RecordAutoLatency(channelID int, modelName string, latencyMs int64) {
	stats := getOrCreateStats(channelID, modelName)
	stats.recordLatency(float64(latencyMs))
}

func (cs *ChannelStats) recordLatency(latencyMs float64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	const alpha = 0.3
	if cs.avgLatencyMs == 0 {
		cs.avgLatencyMs = latencyMs
	} else {
		cs.avgLatencyMs = alpha*latencyMs + (1-alpha)*cs.avgLatencyMs
	}
}

// GetLatency returns the EMA-smoothed latency in milliseconds.
// Returns 0 when no latency data has been recorded yet.
func (cs *ChannelStats) GetLatency() float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.avgLatencyMs
}

// GetAutoStats returns the success rate and total samples for a channel+model.
// This is primarily for debugging/testing purposes.
func GetAutoStats(channelID int, modelName string) (successRate float64, totalSamples int) {
	key := statsKey(channelID, modelName)
	v, ok := globalAutoStats.Load(key)
	if !ok {
		return 0, 0
	}
	stats, ok := v.(*ChannelStats)
	if !ok {
		return 0, 0
	}
	return stats.GetStats(getTimeWindow())
}

// RemoveChannelStats deletes all auto-strategy statistics entries for the given channel.
// Called when a channel is deleted to prevent globalAutoStats from growing unbounded.
func RemoveChannelStats(channelID int) {
	prefix := buildKeyPrefix(channelID)
	globalAutoStats.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) > 0 && strings.HasPrefix(k, prefix) {
			globalAutoStats.Delete(key)
		}
		return true
	})
}

// lastActivity 返回滑动窗口中最近一条记录的时间。空窗口返回零值。
func (cs *ChannelStats) lastActivity() time.Time {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	if cs.count == 0 {
		return time.Time{}
	}
	// head 指向下一个写入位置，最近写入的记录在 head-1。
	last := (cs.head - 1 + len(cs.window)) % len(cs.window)
	return cs.window[last].Timestamp
}

// PurgeIdleStats 删除空闲时长超过 idleFor 的统计条目。globalAutoStats 的 key 含
// 客户端请求携带的 modelName（基数不受控），若有刷量/扫描类请求携带任意 model 名，
// map 会持续膨胀且每条还分配 window slice。之前在渠道/Key 删除时清理，缺少按空闲
// 时长的周期回收（issue #46）。返回删除的条目数。
func PurgeIdleStats(idleFor time.Duration) int {
	if idleFor <= 0 {
		return 0
	}
	now := time.Now()
	removed := 0
	globalAutoStats.Range(func(key, value any) bool {
		stats, ok := value.(*ChannelStats)
		if !ok {
			globalAutoStats.Delete(key)
			removed++
			return true
		}
		last := stats.lastActivity()
		// 零值（从未记录）或超过空闲阈值均视为可回收。
		if last.IsZero() || now.Sub(last) >= idleFor {
			globalAutoStats.Delete(key)
			removed++
		}
		return true
	})
	return removed
}

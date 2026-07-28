package balancer

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// CircuitState 熔断器状态
type CircuitState int

type FailureKind int

const (
	StateClosed   CircuitState = iota // 正常通行
	StateOpen                         // 熔断中，拒绝所有请求
	StateHalfOpen                     // 半开，仅允许单个试探请求

	FailureHard FailureKind = iota
	FailureSoftRateLimit
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

// circuitEntry 单个熔断器条目
type circuitEntry struct {
	State               CircuitState
	ConsecutiveFailures int64
	LastFailureTime     time.Time
	TripCount           int // 累计熔断触发次数（用于指数退避）
	HalfOpenSince       time.Time
	LastActivityTime    time.Time
	mu                  sync.Mutex
}

// 全局熔断器存储
var globalBreaker sync.Map // key: string -> value: *circuitEntry
var circuitBreakerStoreMu sync.Mutex
var circuitBreakerEntryCount int

const (
	maxCircuitBreakerEntries = 10000
	circuitBreakerIdleTTL    = time.Hour
)

func init() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for now := range ticker.C {
			pruneCircuitBreakers(now)
		}
	}()
}

// circuitKey 生成熔断器键：channelID:channelKeyID:modelName
func circuitKey(channelID, keyID int, modelName string) string {
	return fmt.Sprintf("%d:%d:%s", channelID, keyID, modelName)
}

func resetCircuitBreakerByChannel(channelID int) {
	prefix := fmt.Sprintf("%d:", channelID)
	globalBreaker.Range(func(key, value any) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			deleteCircuitBreakerEntry(k, value)
		}
		return true
	})
}

// getOrCreateEntry 获取或创建熔断器条目
func getOrCreateEntry(key string) *circuitEntry {
	if v, ok := globalBreaker.Load(key); ok {
		return v.(*circuitEntry)
	}
	circuitBreakerStoreMu.Lock()
	defer circuitBreakerStoreMu.Unlock()
	if v, ok := globalBreaker.Load(key); ok {
		return v.(*circuitEntry)
	}
	if circuitBreakerEntryCount >= maxCircuitBreakerEntries {
		return nil
	}
	entry := &circuitEntry{State: StateClosed, LastActivityTime: time.Now()}
	globalBreaker.Store(key, entry)
	circuitBreakerEntryCount++
	return entry
}

func deleteCircuitBreakerEntry(key, expected any) bool {
	circuitBreakerStoreMu.Lock()
	defer circuitBreakerStoreMu.Unlock()
	if expected != nil {
		current, ok := globalBreaker.Load(key)
		if !ok || current != expected {
			return false
		}
	}
	if _, loaded := globalBreaker.LoadAndDelete(key); !loaded {
		return false
	}
	if circuitBreakerEntryCount > 0 {
		circuitBreakerEntryCount--
	}
	return true
}

func resetCircuitBreakers() {
	circuitBreakerStoreMu.Lock()
	defer circuitBreakerStoreMu.Unlock()
	globalBreaker.Range(func(key, _ any) bool {
		globalBreaker.Delete(key)
		return true
	})
	circuitBreakerEntryCount = 0
}

func circuitBreakerCount() int {
	circuitBreakerStoreMu.Lock()
	defer circuitBreakerStoreMu.Unlock()
	return circuitBreakerEntryCount
}

// getThreshold 获取熔断阈值配置
func getThreshold() int64 {
	v, err := op.SettingGetInt(model.SettingKeyCircuitBreakerThreshold)
	if err != nil || v <= 0 {
		return 5
	}
	return int64(v)
}

// GetCooldown 获取当前冷却时间（带指数退避）
func GetCooldown(tripCount int) time.Duration {
	base, err := op.SettingGetInt(model.SettingKeyCircuitBreakerCooldown)
	if err != nil || base <= 0 {
		base = 60
	}
	maxCooldown, err := op.SettingGetInt(model.SettingKeyCircuitBreakerMaxCooldown)
	if err != nil || maxCooldown <= 0 {
		maxCooldown = 600
	}

	// 指数退避：baseCooldown * 2^(tripCount-1)
	cooldown := base
	if tripCount > 1 {
		shift := tripCount - 1
		if shift > 20 { // 防止溢出
			shift = 20
		}
		cooldown = base << shift
	}
	if cooldown > maxCooldown {
		cooldown = maxCooldown
	}

	return time.Duration(cooldown) * time.Second
}

// IsTripped 检查通道是否处于熔断状态
// 返回 tripped=true 表示该通道应被跳过，remaining 为剩余冷却时间
func IsTripped(channelID, keyID int, modelName string) (tripped bool, remaining time.Duration) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return false, 0 // 无记录，视为 Closed
	}
	entry := v.(*circuitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	switch entry.State {
	case StateClosed:
		return false, 0

	case StateOpen:
		cooldown := GetCooldown(entry.TripCount)
		elapsed := time.Since(entry.LastFailureTime)
		if elapsed >= cooldown {
			now := time.Now()
			entry.State = StateHalfOpen
			entry.HalfOpenSince = now
			log.Infof("circuit breaker [%s] Open -> HalfOpen (cooldown %v elapsed)", key, cooldown)
			return false, 0
		}
		// 仍在冷却中
		return true, cooldown - elapsed

	case StateHalfOpen:
		cooldown := GetCooldown(entry.TripCount)
		if entry.HalfOpenSince.IsZero() {
			entry.HalfOpenSince = time.Now()
		}
		if time.Since(entry.HalfOpenSince) >= cooldown {
			entry.State = StateOpen
			entry.LastFailureTime = time.Now()
			entry.HalfOpenSince = time.Time{}
			log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe timed out, cooldown=%v)", key, cooldown)
			return true, cooldown
		}
		// 已有试探请求在进行中，拒绝其他请求
		return true, 0

	default:
		return false, 0
	}
}

type CircuitSnapshot struct {
	ChannelID           int    `json:"channel_id"`
	ChannelKeyID        int    `json:"channel_key_id"`
	ModelName           string `json:"model_name"`
	State               string `json:"state"`
	StateName           string `json:"state_name,omitempty"`
	Tripped             bool   `json:"tripped"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
	TripCount           int    `json:"trip_count"`
	RemainingCooldownMS int64  `json:"remaining_cooldown_ms"`
	LastFailureUnix     int64  `json:"last_failure_unix,omitempty"`
	CooldownSeconds     int    `json:"cooldown_seconds,omitempty"`
	RecoverAtUnix       int64  `json:"recover_at_unix,omitempty"`
	HalfOpenSinceUnix   int64  `json:"half_open_since_unix,omitempty"`
}

// InspectCircuit returns a read-only circuit breaker snapshot without moving Open to HalfOpen.
func InspectCircuit(channelID, keyID int, modelName string) CircuitSnapshot {
	key := circuitKey(channelID, keyID, modelName)
	value, ok := globalBreaker.Load(key)
	if !ok {
		return CircuitSnapshot{ChannelID: channelID, ChannelKeyID: keyID, ModelName: modelName, State: StateClosed.String(), StateName: StateClosed.String()}
	}
	entry, ok := value.(*circuitEntry)
	if !ok || entry == nil {
		return CircuitSnapshot{ChannelID: channelID, ChannelKeyID: keyID, ModelName: modelName, State: StateClosed.String(), StateName: StateClosed.String()}
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return circuitSnapshotLocked(channelID, keyID, modelName, entry, time.Now())
}

func circuitSnapshotLocked(channelID, keyID int, modelName string, entry *circuitEntry, now time.Time) CircuitSnapshot {
	stateName := entry.State.String()
	snapshot := CircuitSnapshot{
		ChannelID:           channelID,
		ChannelKeyID:        keyID,
		ModelName:           modelName,
		State:               stateName,
		StateName:           stateName,
		ConsecutiveFailures: entry.ConsecutiveFailures,
		TripCount:           entry.TripCount,
	}
	if !entry.LastFailureTime.IsZero() {
		snapshot.LastFailureUnix = entry.LastFailureTime.Unix()
	}
	if !entry.HalfOpenSince.IsZero() {
		snapshot.HalfOpenSinceUnix = entry.HalfOpenSince.Unix()
	}
	switch entry.State {
	case StateOpen:
		remaining := GetCooldown(entry.TripCount) - now.Sub(entry.LastFailureTime)
		if remaining < 0 {
			remaining = 0
		}
		snapshot.Tripped = remaining > 0
		snapshot.RemainingCooldownMS = remaining.Milliseconds()
		snapshot.CooldownSeconds = int(remaining.Seconds())
		if remaining > 0 {
			snapshot.RecoverAtUnix = now.Add(remaining).Unix()
		}
	case StateHalfOpen:
		snapshot.Tripped = true
	}
	return snapshot
}

func ListCircuitSnapshots() []CircuitSnapshot {
	result := make([]CircuitSnapshot, 0)
	globalBreaker.Range(func(key, value any) bool {
		keyString, ok := key.(string)
		if !ok {
			return true
		}
		entry, ok := value.(*circuitEntry)
		if !ok || entry == nil {
			return true
		}
		parts := strings.SplitN(keyString, ":", 3)
		if len(parts) != 3 {
			return true
		}
		var channelID, keyID int
		_, _ = fmt.Sscanf(parts[0], "%d", &channelID)
		_, _ = fmt.Sscanf(parts[1], "%d", &keyID)

		entry.mu.Lock()
		state := entry.State
		failures := entry.ConsecutiveFailures
		tripCount := entry.TripCount
		lastFailure := entry.LastFailureTime
		entry.mu.Unlock()
		if state == StateClosed && failures == 0 && tripCount == 0 {
			return true
		}

		snapshot := CircuitSnapshot{
			ChannelID:           channelID,
			ChannelKeyID:        keyID,
			ModelName:           parts[2],
			ConsecutiveFailures: failures,
			TripCount:           tripCount,
		}
		switch state {
		case StateOpen:
			snapshot.State = "open"
			remaining := GetCooldown(tripCount) - time.Since(lastFailure)
			if remaining < 0 {
				remaining = 0
			}
			snapshot.RemainingCooldownMS = remaining.Milliseconds()
		case StateHalfOpen:
			snapshot.State = "half_open"
		default:
			snapshot.State = "closed"
		}
		result = append(result, snapshot)
		return true
	})
	return result
}

// RecordSuccess 记录成功，重置熔断器状态
func RecordSuccess(channelID, keyID int, modelName string) {
	key := circuitKey(channelID, keyID, modelName)
	v, ok := globalBreaker.Load(key)
	if !ok {
		return
	}
	entry := v.(*circuitEntry)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == StateHalfOpen {
		log.Infof("circuit breaker [%s] HalfOpen -> Closed (probe succeeded)", key)
	}

	// 重置全部状态
	entry.State = StateClosed
	entry.ConsecutiveFailures = 0
	entry.TripCount = 0
	entry.HalfOpenSince = time.Time{}
	entry.LastActivityTime = time.Now()
}

// RecordFailure 记录失败，可能触发熔断。
// FailureSoftRateLimit 用于 429/503 这类软失败：Closed 状态下不累计阈值，
// HalfOpen 状态下重新进入 Open，但不放大 TripCount。
func RecordFailure(channelID, keyID int, modelName string, kind FailureKind) {
	key := circuitKey(channelID, keyID, modelName)
	var entry *circuitEntry
	if kind == FailureSoftRateLimit {
		value, ok := globalBreaker.Load(key)
		if !ok {
			return
		}
		entry, ok = value.(*circuitEntry)
		if !ok || entry == nil {
			return
		}
	} else {
		entry = getOrCreateEntry(key)
		if entry == nil {
			return
		}
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.LastFailureTime = time.Now()
	entry.LastActivityTime = entry.LastFailureTime
	entry.HalfOpenSince = time.Time{}

	switch entry.State {
	case StateClosed:
		if kind == FailureSoftRateLimit {
			return
		}
		entry.ConsecutiveFailures++
		threshold := getThreshold()
		if entry.ConsecutiveFailures >= threshold {
			entry.State = StateOpen
			entry.TripCount++
			log.Warnf("circuit breaker [%s] Closed -> Open (failures=%d >= threshold=%d, tripCount=%d, cooldown=%v)",
				key, entry.ConsecutiveFailures, threshold, entry.TripCount, GetCooldown(entry.TripCount))
		}

	case StateHalfOpen:
		if kind == FailureSoftRateLimit {
			entry.State = StateOpen
			log.Warnf("circuit breaker [%s] HalfOpen -> Open (soft rate limit, tripCount=%d, cooldown=%v)",
				key, entry.TripCount, GetCooldown(entry.TripCount))
			return
		}
		// 试探失败，重新进入 Open 状态，TripCount 递增（冷却时间翻倍）
		entry.State = StateOpen
		entry.TripCount++
		entry.ConsecutiveFailures = 0 // 重新开始计数
		log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe failed, tripCount=%d, cooldown=%v)",
			key, entry.TripCount, GetCooldown(entry.TripCount))

	case StateOpen:
		// 理论上不应该在 Open 状态下接收到失败记录（请求应被拒绝），
		// 但为安全起见仍更新失败时间
	}
}

func pruneCircuitBreakers(now time.Time) {
	type candidate struct {
		key          any
		entry        *circuitEntry
		lastActivity time.Time
		closed       bool
	}
	entries := make([]candidate, 0)
	globalBreaker.Range(func(key, value any) bool {
		entry, ok := value.(*circuitEntry)
		if !ok || entry == nil {
			deleteCircuitBreakerEntry(key, value)
			return true
		}
		entry.mu.Lock()
		lastActivity := entry.LastActivityTime
		if lastActivity.IsZero() {
			lastActivity = entry.LastFailureTime
		}
		closed := entry.State == StateClosed
		entry.mu.Unlock()
		if closed && !lastActivity.IsZero() && now.Sub(lastActivity) > circuitBreakerIdleTTL {
			deleteCircuitBreakerEntry(key, entry)
			return true
		}
		entries = append(entries, candidate{key: key, entry: entry, lastActivity: lastActivity, closed: closed})
		return true
	})
	if len(entries) <= maxCircuitBreakerEntries {
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].closed != entries[j].closed {
			return entries[i].closed
		}
		return entries[i].lastActivity.Before(entries[j].lastActivity)
	})
	for _, item := range entries[:len(entries)-maxCircuitBreakerEntries] {
		deleteCircuitBreakerEntry(item.key, item.entry)
	}
}

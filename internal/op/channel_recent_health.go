package op

import (
	"sync"
	"time"
)

const recentHealthCap = 256

type recentHealthSample struct {
	at time.Time
	ok bool
}

type recentHealthRing struct {
	mu   sync.Mutex
	buf  []recentHealthSample
	head int
	size int
}

func (r *recentHealthRing) add(ok bool, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.buf == nil {
		r.buf = make([]recentHealthSample, recentHealthCap)
	}
	r.buf[r.head] = recentHealthSample{at: at, ok: ok}
	r.head = (r.head + 1) % recentHealthCap
	if r.size < recentHealthCap {
		r.size++
	}
}

func (r *recentHealthRing) countSince(since time.Time) (success, failed int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.head - r.size
	if start < 0 {
		start += recentHealthCap
	}
	for index := 0; index < r.size; index++ {
		sample := r.buf[(start+index)%recentHealthCap]
		if sample.at.Before(since) {
			continue
		}
		if sample.ok {
			success++
		} else {
			failed++
		}
	}
	return success, failed
}

var recentChannelHealth sync.Map

func StatsChannelRecentClear() {
	recentChannelHealth.Clear()
}

func StatsChannelRecentRecord(channelID int, success bool) {
	if channelID <= 0 {
		return
	}
	value, _ := recentChannelHealth.LoadOrStore(channelID, &recentHealthRing{})
	value.(*recentHealthRing).add(success, time.Now())
}

type ChannelRecentHealth struct {
	ChannelID      int
	RequestSuccess int64
	RequestFailed  int64
	TotalRequests  int64
	FailRate       float64
}

func StatsChannelRecentSnapshot(window time.Duration) []ChannelRecentHealth {
	if window <= 0 {
		window = time.Hour
	}
	since := time.Now().Add(-window)
	result := make([]ChannelRecentHealth, 0)
	recentChannelHealth.Range(func(key, value any) bool {
		channelID, ok := key.(int)
		if !ok {
			return true
		}
		ring, ok := value.(*recentHealthRing)
		if !ok || ring == nil {
			return true
		}
		success, failed := ring.countSince(since)
		total := success + failed
		if total == 0 {
			return true
		}
		result = append(result, ChannelRecentHealth{
			ChannelID:      channelID,
			RequestSuccess: success,
			RequestFailed:  failed,
			TotalRequests:  total,
			FailRate:       float64(failed) * 100 / float64(total),
		})
		return true
	})
	return result
}

package op

import (
	"testing"
	"time"
)

func TestStatsChannelRecentSnapshotWindow(t *testing.T) {
	const channelID = 910001
	recentChannelHealth.Delete(channelID)
	StatsChannelRecentRecord(channelID, true)
	StatsChannelRecentRecord(channelID, true)
	StatsChannelRecentRecord(channelID, false)

	value, _ := recentChannelHealth.Load(channelID)
	ring := value.(*recentHealthRing)
	ring.add(false, time.Now().Add(-2*time.Hour))

	var found *ChannelRecentHealth
	for _, item := range StatsChannelRecentSnapshot(time.Hour) {
		if item.ChannelID == channelID {
			copy := item
			found = &copy
			break
		}
	}
	if found == nil {
		t.Fatalf("expected channel %d in snapshot", channelID)
	}
	if found.RequestSuccess != 2 || found.RequestFailed != 1 || found.TotalRequests != 3 {
		t.Fatalf("unexpected snapshot: %+v", found)
	}
}

func TestStatsChannelRecentClear(t *testing.T) {
	StatsChannelRecentClear()
	t.Cleanup(StatsChannelRecentClear)
	StatsChannelRecentRecord(910001, true)
	StatsChannelRecentRecord(910002, false)

	StatsChannelRecentClear()

	if snapshot := StatsChannelRecentSnapshot(time.Hour); len(snapshot) != 0 {
		t.Fatalf("expected recent channel health to be cleared, got %#v", snapshot)
	}
}

package relay

import (
	"sync"
	"testing"
	"time"
)

func TestListActiveRequestsUsesPreciseStartTime(t *testing.T) {
	id := beginActiveRequest(7, "test-model", true)
	defer finishActiveRequest(id)

	startedAt := time.Now().Add(-1250 * time.Millisecond)
	activeRequests.mu.Lock()
	state := activeRequests.requests[id]
	state.startedAt = startedAt
	state.StartedAt = startedAt.Unix()
	activeRequests.mu.Unlock()

	var found *ActiveRequestView
	for _, view := range ListActiveRequests() {
		if view.ID == id {
			copy := view
			found = &copy
			break
		}
	}
	if found == nil {
		t.Fatal("active request was not listed")
	}
	if found.ElapsedMS < 1200 || found.ElapsedMS > 1450 {
		t.Fatalf("expected precise elapsed time around 1250ms, got %dms", found.ElapsedMS)
	}
}

func TestActiveRequestLifecycleIsConcurrentSafe(t *testing.T) {
	const workers = 32
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(worker int) {
			defer wg.Done()
			id := beginActiveRequest(worker+1, "concurrent-model", worker%2 == 0)
			updateActiveRequest(id, func(view *ActiveRequestView) {
				view.Stage = "forwarding"
				view.Attempt = 1
			})
			_ = ListActiveRequests()
			finishActiveRequest(id)
		}(i)
	}
	wg.Wait()
}

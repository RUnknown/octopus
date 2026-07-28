package relay

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type ActiveRequestView struct {
	ID             int64  `json:"id"`
	APIKeyID       int    `json:"api_key_id"`
	RequestModel   string `json:"request_model"`
	ActualModel    string `json:"actual_model,omitempty"`
	GroupID        int    `json:"group_id,omitempty"`
	ChannelID      int    `json:"channel_id,omitempty"`
	ChannelKeyID   int    `json:"channel_key_id,omitempty"`
	ChannelName    string `json:"channel_name,omitempty"`
	Stage          string `json:"stage"`
	Attempt        int    `json:"attempt,omitempty"`
	StartedAt      int64  `json:"started_at"`
	UpdatedAt      int64  `json:"updated_at"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	StageElapsedMS int64  `json:"stage_elapsed_ms"`
	Streaming      bool   `json:"streaming"`
	UsedWS         bool   `json:"used_ws,omitempty"`
	LastMessage    string `json:"last_message,omitempty"`
}

type activeRequestState struct {
	ActiveRequestView
	stageStartedAt time.Time
}

type activeRequestManager struct {
	mu       sync.RWMutex
	nextID   atomic.Int64
	requests map[int64]*activeRequestState
}

var activeRequests = &activeRequestManager{requests: make(map[int64]*activeRequestState)}

func beginActiveRequest(apiKeyID int, requestModel string, stream bool) int64 {
	id := activeRequests.nextID.Add(1)
	now := time.Now()
	activeRequests.mu.Lock()
	activeRequests.requests[id] = &activeRequestState{
		ActiveRequestView: ActiveRequestView{
			ID:           id,
			APIKeyID:     apiKeyID,
			RequestModel: requestModel,
			Stage:        "routing",
			StartedAt:    now.Unix(),
			UpdatedAt:    now.Unix(),
			Streaming:    stream,
		},
		stageStartedAt: now,
	}
	activeRequests.mu.Unlock()
	return id
}

func finishActiveRequest(id int64) {
	if id == 0 {
		return
	}
	activeRequests.mu.Lock()
	delete(activeRequests.requests, id)
	activeRequests.mu.Unlock()
}

func updateActiveRequest(id int64, fn func(*ActiveRequestView)) {
	if id == 0 || fn == nil {
		return
	}
	now := time.Now()
	activeRequests.mu.Lock()
	state := activeRequests.requests[id]
	if state != nil {
		oldStage := state.Stage
		fn(&state.ActiveRequestView)
		if state.Stage != oldStage {
			state.stageStartedAt = now
		}
		state.UpdatedAt = now.Unix()
	}
	activeRequests.mu.Unlock()
}

func ListActiveRequests() []ActiveRequestView {
	now := time.Now()
	activeRequests.mu.RLock()
	views := make([]ActiveRequestView, 0, len(activeRequests.requests))
	for _, state := range activeRequests.requests {
		view := state.ActiveRequestView
		view.ElapsedMS = now.Sub(time.Unix(view.StartedAt, 0)).Milliseconds()
		view.StageElapsedMS = now.Sub(state.stageStartedAt).Milliseconds()
		views = append(views, view)
	}
	activeRequests.mu.RUnlock()
	sort.SliceStable(views, func(i, j int) bool {
		return views[i].ID > views[j].ID
	})
	return views
}

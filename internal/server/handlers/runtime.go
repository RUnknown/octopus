package handlers

import (
	"net/http"
	"sort"
	"time"

	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/runtime").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/overview", http.MethodGet).Handle(getRuntimeOverview)).
		AddRoute(router.NewRoute("/clear", http.MethodDelete).Handle(clearRuntimeOverview))
}

type runtimeCircuitView struct {
	ChannelID           int    `json:"channel_id"`
	ChannelName         string `json:"channel_name"`
	ChannelKeyID        int    `json:"channel_key_id"`
	ModelName           string `json:"model_name"`
	State               string `json:"state"`
	ConsecutiveFailures int64  `json:"consecutive_failures"`
	TripCount           int    `json:"trip_count"`
	RemainingCooldownMS int64  `json:"remaining_cooldown_ms"`
}

type runtimeChannelHealth struct {
	ChannelID      int     `json:"channel_id"`
	ChannelName    string  `json:"channel_name"`
	RequestSuccess int64   `json:"request_success"`
	RequestFailed  int64   `json:"request_failed"`
	TotalRequests  int64   `json:"total_requests"`
	FailRate       float64 `json:"fail_rate"`
	Enabled        bool    `json:"enabled"`
	Window         string  `json:"window"`
}

type runtimeOverview struct {
	OpenCircuits     int                    `json:"open_circuits"`
	HalfOpenCircuits int                    `json:"half_open_circuits"`
	Circuits         []runtimeCircuitView   `json:"circuits"`
	ChannelHealth    []runtimeChannelHealth `json:"channel_health"`
	UnhealthyCount   int                    `json:"unhealthy_count"`
	HealthWindow     string                 `json:"health_window"`
}

func getRuntimeOverview(c *gin.Context) {
	context := c.Request.Context()
	snapshots := balancer.ListCircuitSnapshots()
	circuits := make([]runtimeCircuitView, 0, len(snapshots))
	openCount, halfOpenCount := 0, 0
	for _, snapshot := range snapshots {
		channelName := ""
		if channel, err := op.ChannelGet(snapshot.ChannelID, context); err == nil && channel != nil {
			channelName = channel.Name
		}
		circuits = append(circuits, runtimeCircuitView{
			ChannelID:           snapshot.ChannelID,
			ChannelName:         channelName,
			ChannelKeyID:        snapshot.ChannelKeyID,
			ModelName:           snapshot.ModelName,
			State:               snapshot.State,
			ConsecutiveFailures: snapshot.ConsecutiveFailures,
			TripCount:           snapshot.TripCount,
			RemainingCooldownMS: snapshot.RemainingCooldownMS,
		})
		if snapshot.State == "open" {
			openCount++
		} else if snapshot.State == "half_open" {
			halfOpenCount++
		}
	}
	sort.Slice(circuits, func(i, j int) bool {
		if circuits[i].State != circuits[j].State {
			return circuitStateRank(circuits[i].State) < circuitStateRank(circuits[j].State)
		}
		if circuits[i].ChannelName != circuits[j].ChannelName {
			return circuits[i].ChannelName < circuits[j].ChannelName
		}
		return circuits[i].ModelName < circuits[j].ModelName
	})

	const windowLabel = "1h"
	recent := op.StatsChannelRecentSnapshot(time.Hour)
	health := make([]runtimeChannelHealth, 0, len(recent))
	unhealthyCount := 0
	for _, item := range recent {
		channelName := ""
		enabled := true
		if channel, err := op.ChannelGet(item.ChannelID, context); err == nil && channel != nil {
			channelName = channel.Name
			enabled = channel.Enabled
		}
		view := runtimeChannelHealth{
			ChannelID:      item.ChannelID,
			ChannelName:    channelName,
			RequestSuccess: item.RequestSuccess,
			RequestFailed:  item.RequestFailed,
			TotalRequests:  item.TotalRequests,
			FailRate:       item.FailRate,
			Enabled:        enabled,
			Window:         windowLabel,
		}
		if item.RequestFailed > 0 || item.FailRate >= 10 {
			health = append(health, view)
		}
		if item.FailRate >= 20 || item.RequestFailed >= 3 {
			unhealthyCount++
		}
	}
	sort.Slice(health, func(i, j int) bool {
		if health[i].FailRate != health[j].FailRate {
			return health[i].FailRate > health[j].FailRate
		}
		if health[i].RequestFailed != health[j].RequestFailed {
			return health[i].RequestFailed > health[j].RequestFailed
		}
		return health[i].ChannelName < health[j].ChannelName
	})
	if len(health) > 20 {
		health = health[:20]
	}

	resp.Success(c, runtimeOverview{
		OpenCircuits:     openCount,
		HalfOpenCircuits: halfOpenCount,
		Circuits:         circuits,
		ChannelHealth:    health,
		UnhealthyCount:   unhealthyCount,
		HealthWindow:     windowLabel,
	})
}

func clearRuntimeOverview(c *gin.Context) {
	balancer.ClearCircuitBreakers()
	op.StatsChannelRecentClear()
	resp.Success(c, nil)
}

func circuitStateRank(state string) int {
	switch state {
	case "open":
		return 0
	case "half_open":
		return 1
	default:
		return 2
	}
}

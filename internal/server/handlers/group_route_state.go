package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/transformer/capability"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

type groupRouteStateResponse struct {
	GroupID                int                        `json:"group_id"`
	GroupName              string                     `json:"group_name"`
	RequestModel           string                     `json:"request_model"`
	ClientProtocol         capability.Protocol        `json:"client_protocol,omitempty"`
	Mode                   model.GroupMode            `json:"mode"`
	CandidateCount         int                        `json:"candidate_count"`
	AvailableCount         int                        `json:"available_count"`
	ProtocolAvailableCount *int                       `json:"protocol_available_count,omitempty"`
	NextChannelID          int                        `json:"next_channel_id,omitempty"`
	NextModelName          string                     `json:"next_model_name,omitempty"`
	NextProtocolChannelID  int                        `json:"next_protocol_channel_id,omitempty"`
	NextProtocolModelName  string                     `json:"next_protocol_model_name,omitempty"`
	ExactOrder             bool                       `json:"exact_order"`
	OrderNote              string                     `json:"order_note,omitempty"`
	Sticky                 *groupRouteStickyState     `json:"sticky,omitempty"`
	Candidates             []groupRouteCandidateState `json:"candidates"`
}

type groupRouteStickyState struct {
	ChannelID    int   `json:"channel_id"`
	ChannelKeyID int   `json:"channel_key_id"`
	ExpiresAt    int64 `json:"expires_at"`
}

type groupRouteCandidateState struct {
	GroupItemID          int                              `json:"group_item_id"`
	ChannelID            int                              `json:"channel_id"`
	ChannelName          string                           `json:"channel_name"`
	ModelName            string                           `json:"model_name"`
	Priority             int                              `json:"priority"`
	Weight               int                              `json:"weight"`
	Enabled              bool                             `json:"enabled"`
	Available            bool                             `json:"available"`
	Sticky               bool                             `json:"sticky,omitempty"`
	KeyCount             int                              `json:"key_count"`
	AvailableKeyCount    int                              `json:"available_key_count"`
	Reason               string                           `json:"reason,omitempty"`
	RecoverAtUnix        int64                            `json:"recover_at_unix,omitempty"`
	CooldownSeconds      int                              `json:"cooldown_seconds,omitempty"`
	Protocol             capability.Protocol              `json:"upstream_protocol,omitempty"`
	ProtocolSupported    *bool                            `json:"protocol_supported,omitempty"`
	ProtocolReason       string                           `json:"protocol_reason,omitempty"`
	TransformMode        capability.TransformMode         `json:"transform_mode,omitempty"`
	ProtocolCapability   *capability.TransformCapability  `json:"protocol_capability,omitempty"`
	ProtocolCapabilities []capability.TransformCapability `json:"protocol_capabilities,omitempty"`
	Keys                 []groupRouteKeyState             `json:"keys,omitempty"`
}

type groupRouteKeyState struct {
	KeyID     int                      `json:"key_id"`
	Remark    string                   `json:"remark,omitempty"`
	Enabled   bool                     `json:"enabled"`
	Available bool                     `json:"available"`
	Circuit   balancer.CircuitSnapshot `json:"circuit"`
	Reason    string                   `json:"reason,omitempty"`
}

func getGroupRouteState(c *gin.Context) {
	groupID, err := strconv.Atoi(c.Param("id"))
	if err != nil || groupID <= 0 {
		resp.InvalidParam(c)
		return
	}
	group, err := op.GroupGet(groupID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, err.Error())
		return
	}

	requestModel := strings.TrimSpace(c.Query("model"))
	if requestModel == "" {
		requestModel = group.Name
	}
	clientProtocol, hasClientProtocol, ok := parseRouteStateClientProtocol(c)
	if !ok {
		return
	}
	preview := balancer.PreviewCandidates(*group)
	state := groupRouteStateResponse{
		GroupID:        group.ID,
		GroupName:      group.Name,
		RequestModel:   requestModel,
		ClientProtocol: clientProtocol,
		Mode:           group.Mode,
		CandidateCount: len(preview.Items),
		ExactOrder:     preview.Exact,
		OrderNote:      preview.Note,
		Candidates:     make([]groupRouteCandidateState, 0, len(preview.Items)),
	}
	if hasClientProtocol {
		count := 0
		state.ProtocolAvailableCount = &count
	}

	var sticky *balancer.SessionEntry
	if rawAPIKeyID := strings.TrimSpace(c.Query("api_key_id")); rawAPIKeyID != "" && group.SessionKeepTime > 0 {
		apiKeyID, parseErr := strconv.Atoi(rawAPIKeyID)
		if parseErr != nil || apiKeyID <= 0 {
			resp.InvalidParam(c)
			return
		}
		sticky = balancer.GetSticky(apiKeyID, requestModel, time.Duration(group.SessionKeepTime)*time.Second)
		if sticky != nil {
			state.Sticky = &groupRouteStickyState{
				ChannelID:    sticky.ChannelID,
				ChannelKeyID: sticky.ChannelKeyID,
				ExpiresAt:    sticky.Timestamp.Add(time.Duration(group.SessionKeepTime) * time.Second).Unix(),
			}
		}
	}

	items := preview.Items
	if sticky != nil {
		items = append([]model.GroupItem(nil), preview.Items...)
		for i, item := range items {
			if item.ChannelID == sticky.ChannelID {
				copy(items[1:i+1], items[0:i])
				items[0] = item
				if state.OrderNote == "" {
					state.OrderNote = "sticky candidate preferred"
				} else {
					state.OrderNote += "; sticky candidate preferred"
				}
				break
			}
		}
	}

	for _, item := range items {
		candidate := groupRouteCandidateState{
			GroupItemID: item.ID,
			ChannelID:   item.ChannelID,
			ModelName:   item.ModelName,
			Priority:    item.Priority,
			Weight:      item.Weight,
			Enabled:     true,
			Available:   true,
			Sticky:      sticky != nil && sticky.ChannelID == item.ChannelID,
		}
		channel, err := op.ChannelGet(item.ChannelID, c.Request.Context())
		if err != nil {
			candidate.Enabled = false
			candidate.Available = false
			candidate.ChannelName = "channel_" + strconv.Itoa(item.ChannelID)
			candidate.Reason = "channel not found: " + err.Error()
			state.Candidates = append(state.Candidates, candidate)
			continue
		}
		candidate.ChannelName = channel.Name
		applyRouteStateProtocolDiagnostics(&candidate, channel.Type, clientProtocol, hasClientProtocol)
		if !channel.Enabled {
			candidate.Enabled = false
			candidate.Available = false
			candidate.Reason = "channel disabled"
			state.Candidates = append(state.Candidates, candidate)
			continue
		}

		for _, key := range channel.Keys {
			keyState := groupRouteKeyState{KeyID: key.ID, Remark: key.Remark, Enabled: key.Enabled, Available: key.Enabled}
			if !key.Enabled || strings.TrimSpace(key.ChannelKey) == "" {
				keyState.Available = false
				keyState.Reason = "key disabled or empty"
			} else {
				snapshot := balancer.InspectCircuit(channel.ID, key.ID, item.ModelName)
				keyState.Circuit = snapshot
				if snapshot.Tripped {
					keyState.Available = false
					keyState.Reason = "circuit breaker " + snapshot.StateName
				}
			}
			candidate.KeyCount++
			if keyState.Available {
				candidate.AvailableKeyCount++
			}
			candidate.Keys = append(candidate.Keys, keyState)
		}
		if candidate.AvailableKeyCount == 0 {
			candidate.Available = false
			candidate.Reason = "no available key"
			for _, key := range candidate.Keys {
				if key.Circuit.Tripped && (candidate.RecoverAtUnix == 0 || key.Circuit.RecoverAtUnix < candidate.RecoverAtUnix) {
					candidate.RecoverAtUnix = key.Circuit.RecoverAtUnix
					candidate.CooldownSeconds = key.Circuit.CooldownSeconds
				}
			}
		} else {
			state.AvailableCount++
			if state.NextChannelID == 0 {
				state.NextChannelID = item.ChannelID
				state.NextModelName = item.ModelName
			}
			if hasClientProtocol && candidate.ProtocolSupported != nil && *candidate.ProtocolSupported {
				*state.ProtocolAvailableCount = *state.ProtocolAvailableCount + 1
				if state.NextProtocolChannelID == 0 {
					state.NextProtocolChannelID = item.ChannelID
					state.NextProtocolModelName = item.ModelName
				}
			}
		}
		state.Candidates = append(state.Candidates, candidate)
	}

	resp.Success(c, state)
}

func parseRouteStateClientProtocol(c *gin.Context) (capability.Protocol, bool, bool) {
	raw := strings.TrimSpace(c.Query("client_protocol"))
	if raw == "" {
		return "", false, true
	}
	protocol, ok := capability.ParseProtocol(raw)
	if !ok {
		resp.Error(c, http.StatusBadRequest, "invalid client_protocol: "+raw)
		return "", false, false
	}
	return protocol, true, true
}

func applyRouteStateProtocolDiagnostics(candidate *groupRouteCandidateState, channelType outbound.OutboundType, clientProtocol capability.Protocol, hasClientProtocol bool) {
	candidate.Protocol = capability.OutboundProtocol(channelType)
	if hasClientProtocol {
		cap := capability.EvaluateOutbound(clientProtocol, channelType)
		supported := cap.Supported
		candidate.ProtocolSupported = &supported
		candidate.TransformMode = cap.Mode
		candidate.ProtocolCapability = &cap
		if !cap.Supported {
			candidate.ProtocolReason = cap.MissingReason
		} else if len(cap.LossyReasons) > 0 {
			candidate.ProtocolReason = strings.Join(cap.LossyReasons, "; ")
		}
		return
	}

	clientProtocols := capability.ClientProtocols()
	candidate.ProtocolCapabilities = make([]capability.TransformCapability, 0, len(clientProtocols))
	for _, protocol := range clientProtocols {
		candidate.ProtocolCapabilities = append(candidate.ProtocolCapabilities, capability.EvaluateOutbound(protocol, channelType))
	}
}

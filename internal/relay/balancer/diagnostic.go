package balancer

import (
	"sort"
	"sync/atomic"

	"github.com/bestruirui/octopus/internal/model"
)

// CandidatePreview describes the candidate order used by diagnostics. Exact is
// false for strategies whose production order is intentionally randomized.
type CandidatePreview struct {
	Items []model.GroupItem
	Exact bool
	Note  string
}

// PreviewCandidates returns a read-only view of the route order without
// mutating round-robin counters or session/circuit state.
func PreviewCandidates(group model.Group) CandidatePreview {
	items := append([]model.GroupItem(nil), group.Items...)
	if len(items) == 0 {
		return CandidatePreview{Items: items, Exact: true}
	}

	switch group.Mode {
	case model.GroupModeRoundRobin:
		idx := int((atomic.LoadUint64(&roundRobinCounter) + 1) % uint64(len(items)))
		ordered := make([]model.GroupItem, len(items))
		for i := range items {
			ordered[i] = items[(idx+i)%len(items)]
		}
		return CandidatePreview{Items: ordered, Exact: true, Note: "next round-robin order"}
	case model.GroupModeFailover:
		return CandidatePreview{Items: sortByPriority(items), Exact: true, Note: "priority order"}
	case model.GroupModeWeighted:
		sort.SliceStable(items, func(i, j int) bool {
			wi := items[i].Weight
			if wi <= 0 {
				wi = 1
			}
			wj := items[j].Weight
			if wj <= 0 {
				wj = 1
			}
			if wi != wj {
				return wi > wj
			}
			if items[i].Priority != items[j].Priority {
				return items[i].Priority < items[j].Priority
			}
			return items[i].ID < items[j].ID
		})
		return CandidatePreview{Items: items, Exact: false, Note: "diagnostic order by weight; production order is weighted-random"}
	case model.GroupModeRandom:
		fallthrough
	default:
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Priority != items[j].Priority {
				return items[i].Priority < items[j].Priority
			}
			if items[i].ChannelID != items[j].ChannelID {
				return items[i].ChannelID < items[j].ChannelID
			}
			return items[i].ID < items[j].ID
		})
		return CandidatePreview{Items: items, Exact: false, Note: "diagnostic order only; production order is random"}
	}
}

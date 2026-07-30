package balancer

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

// Failover routing must be fully deterministic: the diagnostic preview labels
// the failover order as Exact, so any nondeterminism between sortByPriority and
// PreviewCandidates would show operators a route order that production does not
// actually use.
func TestFailoverCandidatesAreDeterministicForEqualPriority(t *testing.T) {
	items := []model.GroupItem{
		{ID: 7, ChannelID: 20, ModelName: "d", Priority: 1},
		{ID: 3, ChannelID: 10, ModelName: "b", Priority: 1},
		{ID: 9, ChannelID: 10, ModelName: "c", Priority: 1},
		{ID: 1, ChannelID: 30, ModelName: "a", Priority: 0},
	}

	want := []int{1, 3, 9, 7}
	balancer := &Failover{}
	for attempt := 0; attempt < 20; attempt++ {
		got := balancer.Candidates(items)
		if len(got) != len(want) {
			t.Fatalf("attempt %d: expected %d candidates, got %d", attempt, len(want), len(got))
		}
		for i, id := range want {
			if got[i].ID != id {
				t.Fatalf("attempt %d: candidate %d = item %d, want item %d (order %#v)", attempt, i, got[i].ID, id, got)
			}
		}
	}
}

// The failover preview claims Exact, so it must report the same order the
// balancer will really use.
func TestFailoverPreviewMatchesProductionOrder(t *testing.T) {
	items := []model.GroupItem{
		{ID: 4, ChannelID: 15, ModelName: "b", Priority: 2},
		{ID: 2, ChannelID: 15, ModelName: "a", Priority: 2},
		{ID: 8, ChannelID: 5, ModelName: "c", Priority: 2},
	}
	group := model.Group{Mode: model.GroupModeFailover, Items: items}

	preview := PreviewCandidates(group)
	if !preview.Exact {
		t.Fatal("failover preview is expected to be exact")
	}
	production := (&Failover{}).Candidates(items)
	if len(preview.Items) != len(production) {
		t.Fatalf("preview has %d items, production has %d", len(preview.Items), len(production))
	}
	for i := range production {
		if preview.Items[i].ID != production[i].ID {
			t.Fatalf("preview order %#v does not match production order %#v", preview.Items, production)
		}
	}
}

// sortByPriority must not reorder the caller's slice, because group items are
// shared state read by concurrent requests.
func TestSortByPriorityDoesNotMutateInput(t *testing.T) {
	items := []model.GroupItem{
		{ID: 5, ChannelID: 2, Priority: 9},
		{ID: 6, ChannelID: 1, Priority: 1},
	}

	if got := sortByPriority(items); got[0].ID != 6 {
		t.Fatalf("expected lowest priority first, got %#v", got)
	}
	if items[0].ID != 5 || items[1].ID != 6 {
		t.Fatalf("input slice was mutated: %#v", items)
	}
}

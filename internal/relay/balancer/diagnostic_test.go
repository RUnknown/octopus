package balancer

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestPreviewCandidatesMarksRoundRobinAsSnapshot(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	group := model.Group{
		Mode: model.GroupModeRoundRobin,
		Items: []model.GroupItem{
			{ID: 1, ChannelID: 10, ModelName: "first"},
			{ID: 2, ChannelID: 20, ModelName: "second"},
		},
	}

	preview := PreviewCandidates(group)
	if preview.Exact {
		t.Fatal("round-robin preview must not claim an exact next route")
	}
	if len(preview.Items) != 2 || preview.Items[0].ID != 2 {
		t.Fatalf("unexpected round-robin snapshot order: %#v", preview.Items)
	}
	if !strings.Contains(preview.Note, "snapshot") {
		t.Fatalf("expected snapshot warning, got %q", preview.Note)
	}
}

func TestAttemptSpanPreservesExplicitFalseRetryability(t *testing.T) {
	group := model.Group{
		Mode:  model.GroupModeFailover,
		Items: []model.GroupItem{{ID: 1, ChannelID: 10, ModelName: "test"}},
	}
	iter := NewIterator(group, 0, "test")
	if !iter.Next() {
		t.Fatal("expected one candidate")
	}
	span := iter.StartAttempt(10, 20, "channel")
	span.SetRetryable(false)
	span.End(model.AttemptFailed, 400, "bad request")

	attempts := iter.Attempts()
	if len(attempts) != 1 || attempts[0].Retryable == nil {
		t.Fatalf("expected explicit retryability, got %#v", attempts)
	}
	if *attempts[0].Retryable {
		t.Fatal("expected retryable=false to be preserved")
	}
	payload, err := json.Marshal(attempts[0])
	if err != nil {
		t.Fatalf("marshal attempt: %v", err)
	}
	if !strings.Contains(string(payload), `"retryable":false`) {
		t.Fatalf("expected explicit false in JSON, got %s", payload)
	}
}

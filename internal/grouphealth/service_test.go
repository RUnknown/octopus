package grouphealth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"gorm.io/gorm"
)

func setupGroupHealthTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-group-health-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func TestGroupRunLockRejectsConcurrentUseAndIsRemovedAfterUse(t *testing.T) {
	const groupID = 987654
	unlock, acquired := tryLockGroup(groupID)
	if !acquired {
		t.Fatal("expected first group lock attempt to succeed")
	}
	runLocksMu.Lock()
	_, existsWhileLocked := runLocks[groupID]
	runLocksMu.Unlock()
	if !existsWhileLocked {
		t.Fatal("expected active group lock to be registered")
	}
	if secondUnlock, secondAcquired := tryLockGroup(groupID); secondAcquired || secondUnlock != nil {
		t.Fatal("expected concurrent group lock attempt to be rejected")
	}
	unlock()
	runLocksMu.Lock()
	_, existsAfterUnlock := runLocks[groupID]
	runLocksMu.Unlock()
	if existsAfterUnlock {
		t.Fatal("unused group lock should be removed")
	}
	thirdUnlock, thirdAcquired := tryLockGroup(groupID)
	if !thirdAcquired {
		t.Fatal("expected group lock to be reusable after unlock")
	}
	thirdUnlock()
}

type recordingGroupHealthRepository struct {
	snapshot       model.GroupHealthSnapshot
	appendErr      error
	cancelOnAppend context.CancelFunc
	finishCalls    int
	finishCtxErr   error
	finishStatus   model.GroupHealthStatus
	finishMessage  string
	finishErrs     []error
	finishStatuses []model.GroupHealthStatus
}

func (r *recordingGroupHealthRepository) CreateRunningSnapshot(_ context.Context, group model.Group, probeMode model.GroupHealthProbeMode) (*model.GroupHealthSnapshot, error) {
	r.snapshot = model.GroupHealthSnapshot{
		ID:        1,
		GroupID:   group.ID,
		GroupName: group.Name,
		GroupMode: group.Mode,
		ProbeMode: probeMode,
		Status:    model.GroupHealthStatusRunning,
		StartedAt: time.Now(),
	}
	return &r.snapshot, nil
}

func (r *recordingGroupHealthRepository) AppendAttempt(context.Context, int, model.GroupHealthAttempt) error {
	if r.cancelOnAppend != nil {
		r.cancelOnAppend()
	}
	return r.appendErr
}

func (r *recordingGroupHealthRepository) FinishSnapshot(ctx context.Context, _ int, status model.GroupHealthStatus, _ *int, _ int64, message string, _ time.Time) error {
	r.finishCalls++
	r.finishCtxErr = ctx.Err()
	r.finishStatus = status
	r.finishMessage = message
	r.finishStatuses = append(r.finishStatuses, status)
	if r.finishCalls <= len(r.finishErrs) {
		return r.finishErrs[r.finishCalls-1]
	}
	return nil
}

func (*recordingGroupHealthRepository) GetLatestSnapshotByGroupID(context.Context, int) (*model.GroupHealthSnapshot, error) {
	return nil, gorm.ErrRecordNotFound
}

func (*recordingGroupHealthRepository) GetRunningSnapshotByGroupID(context.Context, int) (*model.GroupHealthSnapshot, error) {
	return nil, gorm.ErrRecordNotFound
}

func (*recordingGroupHealthRepository) ListGroupHealthViews(context.Context) ([]model.GroupHealthGroupView, error) {
	return nil, nil
}

func (*recordingGroupHealthRepository) GetGroupHealthViewByID(context.Context, int) (*model.GroupHealthGroupView, error) {
	return nil, gorm.ErrRecordNotFound
}

func TestRunGroupHealthFinalizesSnapshotAfterCanceledIntermediateError(t *testing.T) {
	baseCtx := setupGroupHealthTestDB(t)
	channel := &model.Channel{
		Name:     "group-health-finalize",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "http://127.0.0.1:1/v1"}},
		Model:    "probe-model",
	}
	if err := op.ChannelCreate(channel, baseCtx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}
	group := &model.Group{Name: "finalize-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, baseCtx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "probe-model", Priority: 1, Weight: 1}, baseCtx); err != nil {
		t.Fatalf("GroupItemAdd: %v", err)
	}

	ctx, cancel := context.WithCancel(baseCtx)
	expectedErr := errors.New("append attempt failed")
	repo := &recordingGroupHealthRepository{appendErr: expectedErr, cancelOnAppend: cancel}
	service := NewService(repo, nil)
	err := service.RunGroupHealth(ctx, group.ID)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected append error, got %v", err)
	}
	if repo.finishCalls != 1 {
		t.Fatalf("expected one snapshot finalization, got %d", repo.finishCalls)
	}
	if repo.finishCtxErr != nil {
		t.Fatalf("expected independent finalization context, got %v", repo.finishCtxErr)
	}
	if repo.finishStatus != model.GroupHealthStatusFailed {
		t.Fatalf("expected failed snapshot, got %s", repo.finishStatus)
	}
	if !strings.Contains(repo.finishMessage, expectedErr.Error()) {
		t.Fatalf("expected finalization message to contain the cause, got %q", repo.finishMessage)
	}
}

func TestRunGroupHealthJobsBoundsConcurrency(t *testing.T) {
	groupIDs := make([]int, 24)
	for i := range groupIDs {
		groupIDs[i] = i + 1
	}
	var active atomic.Int32
	var maximum atomic.Int32
	var completed atomic.Int32
	runGroupHealthJobs(context.Background(), groupIDs, 3, func(context.Context, int) error {
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		completed.Add(1)
		return nil
	})
	if got := maximum.Load(); got > 3 {
		t.Fatalf("worker concurrency exceeded limit: %d", got)
	}
	if got := completed.Load(); got != int32(len(groupIDs)) {
		t.Fatalf("expected %d completed jobs, got %d", len(groupIDs), got)
	}
}

func TestRunGroupHealthFinalizeRetryPreservesComputedResult(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_retry","object":"response","status":"completed"}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "group-health-finalize-retry",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "probe-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-retry"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate: %v", err)
	}
	group := &model.Group{Name: "finalize-retry-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "probe-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd: %v", err)
	}

	expectedErr := errors.New("finish snapshot failed")
	repo := &recordingGroupHealthRepository{finishErrs: []error{expectedErr, nil}}
	service := NewService(repo, &Prober{CandidateTimeout: 5 * time.Second})
	err := service.RunGroupHealth(ctx, group.ID)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected first finish error, got %v", err)
	}
	if repo.finishCalls != 2 {
		t.Fatalf("expected one retry after finish failure, got %d calls", repo.finishCalls)
	}
	for i, status := range repo.finishStatuses {
		if status != model.GroupHealthStatusSuccess {
			t.Fatalf("finish call %d changed computed status to %s", i+1, status)
		}
	}
}

func TestRunGroupHealthFailoverDoesNotMutateRuntimeStats(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)

	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer firstServer.Close()

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","status":"completed"}`))
	}))
	defer secondServer.Close()

	firstChannel := &model.Channel{
		Name:     "group-health-first",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: firstServer.URL + "/v1"}},
		Model:    "probe-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-first", Remark: "first"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate first failed: %v", err)
	}

	secondChannel := &model.Channel{
		Name:     "group-health-second",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: secondServer.URL + "/v1"}},
		Model:    "probe-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-second", Remark: "second"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate second failed: %v", err)
	}

	group := &model.Group{Name: "probe-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "probe-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd first failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "probe-model", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd second failed: %v", err)
	}

	statsBefore := op.StatsTotalGet()
	logsBefore, err := op.RelayLogList(ctx, nil, nil, nil, 1, 100)
	if err != nil {
		t.Fatalf("RelayLogList failed: %v", err)
	}

	service := NewService(op.NewGroupHealthRepository(), &Prober{CandidateTimeout: 5 * time.Second})
	if err := service.RunGroupHealth(ctx, group.ID); err != nil {
		t.Fatalf("RunGroupHealth failed: %v", err)
	}

	view, err := service.GetGroupHealthViewByID(ctx, group.ID)
	if err != nil {
		t.Fatalf("GetGroupHealthViewByID failed: %v", err)
	}
	if view.Latest == nil {
		t.Fatal("expected latest snapshot")
	}
	if view.Latest.Status != model.GroupHealthStatusPartial {
		t.Fatalf("expected partial status, got %s", view.Latest.Status)
	}
	if len(view.Latest.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(view.Latest.Attempts))
	}
	if view.Latest.Attempts[0].Status != model.GroupHealthAttemptStatusFailed {
		t.Fatalf("expected first attempt failed, got %s", view.Latest.Attempts[0].Status)
	}
	if view.Latest.Attempts[1].Status != model.GroupHealthAttemptStatusSuccess {
		t.Fatalf("expected second attempt success, got %s", view.Latest.Attempts[1].Status)
	}
	if view.Latest.SuccessfulChannelID == nil || *view.Latest.SuccessfulChannelID != secondChannel.ID {
		t.Fatalf("expected successful channel %d, got %#v", secondChannel.ID, view.Latest.SuccessfulChannelID)
	}

	statsAfter := op.StatsTotalGet()
	if statsAfter != statsBefore {
		t.Fatalf("expected stats total unchanged, before=%+v after=%+v", statsBefore, statsAfter)
	}

	logsAfter, err := op.RelayLogList(ctx, nil, nil, nil, 1, 100)
	if err != nil {
		t.Fatalf("RelayLogList failed after run: %v", err)
	}
	if len(logsAfter) != len(logsBefore) {
		t.Fatalf("expected relay log count unchanged, before=%d after=%d", len(logsBefore), len(logsAfter))
	}

	reloadedFirst, err := op.ChannelGet(firstChannel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet first failed: %v", err)
	}
	reloadedSecond, err := op.ChannelGet(secondChannel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet second failed: %v", err)
	}
	if reloadedFirst.Keys[0].TotalCost != 0 || reloadedSecond.Keys[0].TotalCost != 0 {
		t.Fatalf("expected key total cost unchanged")
	}
	if reloadedFirst.Keys[0].StatusCode != 0 || reloadedSecond.Keys[0].StatusCode != 0 {
		t.Fatalf("expected key status code unchanged")
	}
	if reloadedFirst.Keys[0].LastUseTimeStamp != 0 || reloadedSecond.Keys[0].LastUseTimeStamp != 0 {
		t.Fatalf("expected key last use timestamp unchanged")
	}
}

func TestRunGroupHealthReturnsAlreadyRunning(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)

	group := &model.Group{Name: "running-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}

	repo := op.NewGroupHealthRepository()
	if _, err := repo.CreateRunningSnapshot(ctx, *group, model.GroupHealthProbeModeStandard); err != nil {
		t.Fatalf("CreateRunningSnapshot failed: %v", err)
	}

	service := NewService(repo, nil)
	err := service.RunGroupHealth(ctx, group.ID)
	if err == nil {
		t.Fatal("expected ErrGroupHealthAlreadyRunning")
	}
	if !errors.Is(err, ErrGroupHealthAlreadyRunning) {
		t.Fatalf("expected ErrGroupHealthAlreadyRunning, got %v", err)
	}
}

func TestRunGroupHealthFullProbeDoesNotSkipRemainingFailoverCandidates(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)

	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_first","object":"response","status":"completed"}`))
	}))
	defer firstServer.Close()

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer secondServer.Close()

	firstChannel := &model.Channel{
		Name:     "group-health-full-first",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: firstServer.URL + "/v1"}},
		Model:    "probe-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-full-first", Remark: "first"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate first failed: %v", err)
	}

	secondChannel := &model.Channel{
		Name:     "group-health-full-second",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: secondServer.URL + "/v1"}},
		Model:    "probe-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "sk-full-second", Remark: "second"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate second failed: %v", err)
	}

	group := &model.Group{Name: "probe-full-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "probe-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd first failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "probe-model", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd second failed: %v", err)
	}

	service := NewService(op.NewGroupHealthRepository(), &Prober{CandidateTimeout: 5 * time.Second})
	if err := service.RunGroupHealth(ctx, group.ID, model.GroupHealthProbeModeFull); err != nil {
		t.Fatalf("RunGroupHealth full failed: %v", err)
	}

	view, err := service.GetGroupHealthViewByID(ctx, group.ID)
	if err != nil {
		t.Fatalf("GetGroupHealthViewByID failed: %v", err)
	}
	if view.Latest == nil {
		t.Fatal("expected latest snapshot")
	}
	if view.Latest.ProbeMode != model.GroupHealthProbeModeFull {
		t.Fatalf("expected full probe mode, got %s", view.Latest.ProbeMode)
	}
	if view.Latest.Status != model.GroupHealthStatusPartial {
		t.Fatalf("expected partial status, got %s", view.Latest.Status)
	}
	if len(view.Latest.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(view.Latest.Attempts))
	}
	if view.Latest.Attempts[0].Status != model.GroupHealthAttemptStatusSuccess {
		t.Fatalf("expected first attempt success, got %s", view.Latest.Attempts[0].Status)
	}
	if view.Latest.Attempts[1].Status != model.GroupHealthAttemptStatusFailed {
		t.Fatalf("expected second attempt failed, got %s", view.Latest.Attempts[1].Status)
	}
	if view.Latest.Attempts[1].HTTPStatus != http.StatusServiceUnavailable {
		t.Fatalf("expected second attempt http status %d, got %d", http.StatusServiceUnavailable, view.Latest.Attempts[1].HTTPStatus)
	}
}

func TestRunGroupHealthHonorsChannelSkipProbe(t *testing.T) {
	ctx := setupGroupHealthTestDB(t)
	skipped := &model.Channel{
		Name: "skip-probe-channel", Type: outbound.OutboundTypeOpenAIChat, Enabled: true,
		SkipHealthProbe: true, BaseUrls: []model.BaseUrl{{URL: "http://127.0.0.1:1/v1"}},
		Model: "probe-model", Keys: []model.ChannelKey{{Enabled: true, ChannelKey: "sk-skip"}},
	}
	if err := op.ChannelCreate(skipped, ctx); err != nil {
		t.Fatalf("ChannelCreate skipped: %v", err)
	}
	group := &model.Group{Name: "skip-probe-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: skipped.ID, ModelName: "probe-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd: %v", err)
	}
	service := NewService(op.NewGroupHealthRepository(), &Prober{CandidateTimeout: time.Second})
	if err := service.RunGroupHealth(ctx, group.ID); err != nil {
		t.Fatalf("RunGroupHealth: %v", err)
	}
	view, err := service.GetGroupHealthViewByID(ctx, group.ID)
	if err != nil || view.Latest == nil || len(view.Latest.Attempts) != 1 {
		t.Fatalf("unexpected view=%#v err=%v", view, err)
	}
	if view.Latest.Status != model.GroupHealthStatusPartial {
		t.Fatalf("expected all-skipped health check to be partial, got %s", view.Latest.Status)
	}
	if attempt := view.Latest.Attempts[0]; attempt.Status != model.GroupHealthAttemptStatusSkipped || attempt.ErrorMessage != "channel health probe disabled" {
		t.Fatalf("unexpected attempt: %#v", attempt)
	}
}

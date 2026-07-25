package grouphealth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

var ErrGroupHealthAlreadyRunning = errors.New("group health check already running")

type Repository interface {
	CreateRunningSnapshot(ctx context.Context, group model.Group, probeMode model.GroupHealthProbeMode) (*model.GroupHealthSnapshot, error)
	AppendAttempt(ctx context.Context, snapshotID int, attempt model.GroupHealthAttempt) error
	FinishSnapshot(ctx context.Context, snapshotID int, status model.GroupHealthStatus, successfulChannelID *int, durationMS int64, message string, finishedAt time.Time) error
	GetLatestSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error)
	GetRunningSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error)
	ListGroupHealthViews(ctx context.Context) ([]model.GroupHealthGroupView, error)
	GetGroupHealthViewByID(ctx context.Context, groupID int) (*model.GroupHealthGroupView, error)
}

type Service struct {
	repo   Repository
	prober *Prober
}

const (
	defaultGroupHealthWorkers  = 2
	maxGroupHealthWorkers      = 32
	groupHealthFinalizeTimeout = 5 * time.Second
)

var runLocks = make(map[int]struct{})
var runLocksMu sync.Mutex

func NewService(repo Repository, prober *Prober) *Service {
	if repo == nil {
		repo = op.NewGroupHealthRepository()
	}
	if prober == nil {
		prober = NewProber()
	}
	return &Service{
		repo:   repo,
		prober: prober,
	}
}

func tryLockGroup(groupID int) (func(), bool) {
	runLocksMu.Lock()
	if _, exists := runLocks[groupID]; exists {
		runLocksMu.Unlock()
		return nil, false
	}
	runLocks[groupID] = struct{}{}
	runLocksMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			runLocksMu.Lock()
			delete(runLocks, groupID)
			runLocksMu.Unlock()
		})
	}, true
}

// normalizeProbeMode returns the effective probe mode from a prioritized list.
// An empty list defaults to model.GroupHealthProbeModeStandard, and only the
// first element is considered. model.GroupHealthProbeModeFull is honored only
// when it appears first; all other cases fall back to Standard semantics.
func normalizeProbeMode(probeModes []model.GroupHealthProbeMode) model.GroupHealthProbeMode {
	if len(probeModes) == 0 {
		return model.GroupHealthProbeModeStandard
	}
	if probeModes[0] == model.GroupHealthProbeModeFull {
		return model.GroupHealthProbeModeFull
	}
	return model.GroupHealthProbeModeStandard
}

func resolveChannelName(ctx context.Context, channelID int) string {
	channel, err := op.ChannelGet(channelID, ctx)
	if err != nil {
		return fmt.Sprintf("channel-%d", channelID)
	}
	return channel.Name
}

func (s *Service) RunGroupHealth(ctx context.Context, groupID int, probeModes ...model.GroupHealthProbeMode) (runErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	unlock, acquired := tryLockGroup(groupID)
	if !acquired {
		return ErrGroupHealthAlreadyRunning
	}
	defer unlock()

	if _, err := s.repo.GetRunningSnapshotByGroupID(ctx, groupID); err == nil {
		return ErrGroupHealthAlreadyRunning
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	group, err := op.GroupGet(groupID, ctx)
	if err != nil {
		return err
	}

	probeMode := normalizeProbeMode(probeModes)

	snapshot, err := s.repo.CreateRunningSnapshot(ctx, *group, probeMode)
	if err != nil {
		return err
	}
	snapshotFinished := false
	fallbackStatus := model.GroupHealthStatusFailed
	var fallbackSuccessfulChannelID *int
	fallbackMessage := ""
	defer func() {
		if snapshotFinished {
			return
		}
		finishedAt := time.Now()
		message := fallbackMessage
		if message == "" {
			message = "health check aborted"
		}
		if runErr != nil && fallbackMessage == "" {
			message = fmt.Sprintf("health check aborted: %v", runErr)
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), groupHealthFinalizeTimeout)
		defer cancel()
		if finishErr := s.repo.FinishSnapshot(
			finishCtx,
			snapshot.ID,
			fallbackStatus,
			fallbackSuccessfulChannelID,
			finishedAt.Sub(snapshot.StartedAt).Milliseconds(),
			message,
			finishedAt,
		); finishErr != nil && runErr == nil {
			runErr = finishErr
		}
	}()

	items := append([]model.GroupItem(nil), group.Items...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		if items[i].Weight != items[j].Weight {
			return items[i].Weight > items[j].Weight
		}
		if items[i].ChannelID != items[j].ChannelID {
			return items[i].ChannelID < items[j].ChannelID
		}
		return items[i].ID < items[j].ID
	})

	var successfulChannelID *int
	message := "all candidates failed"
	stopAfterSuccess := group.Mode == model.GroupModeFailover && probeMode != model.GroupHealthProbeModeFull
	successFound := false
	firstSuccessIndex := -1
	attemptedCount := 0
	successCount := 0
	skippedCount := 0

	for index, item := range items {
		channel, err := op.ChannelGet(item.ChannelID, ctx)
		if err != nil {
			attemptedCount++
			appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
				GroupItemID:  item.ID,
				ChannelID:    item.ChannelID,
				ChannelName:  fmt.Sprintf("channel-%d", item.ChannelID),
				ModelName:    item.ModelName,
				Priority:     item.Priority,
				Weight:       item.Weight,
				Status:       model.GroupHealthAttemptStatusFailed,
				ErrorMessage: fmt.Sprintf("failed to load channel: %v", err),
			})
			if appendErr != nil {
				return appendErr
			}
			continue
		}

		if channel.SkipHealthProbe {
			skippedCount++
			if appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
				GroupItemID: item.ID, ChannelID: item.ChannelID, ChannelName: channel.Name,
				ModelName: item.ModelName, Priority: item.Priority, Weight: item.Weight,
				Status:       model.GroupHealthAttemptStatusSkipped,
				ErrorMessage: "channel health probe disabled",
			}); appendErr != nil {
				return appendErr
			}
			continue
		}

		usedKey := channel.GetChannelKey()
		if usedKey.ID == 0 || strings.TrimSpace(usedKey.ChannelKey) == "" {
			attemptedCount++
			appendErr := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
				GroupItemID:  item.ID,
				ChannelID:    item.ChannelID,
				ChannelName:  channel.Name,
				ModelName:    item.ModelName,
				Priority:     item.Priority,
				Weight:       item.Weight,
				Status:       model.GroupHealthAttemptStatusFailed,
				ErrorMessage: "no available key",
			})
			if appendErr != nil {
				return appendErr
			}
			continue
		}

		result := s.prober.RunCandidate(ctx, *channel, usedKey, item.ModelName)
		attemptedCount++
		attempt := model.GroupHealthAttempt{
			GroupItemID:  item.ID,
			ChannelID:    item.ChannelID,
			ChannelName:  channel.Name,
			ChannelKeyID: usedKey.ID,
			KeyRemark:    usedKey.Remark,
			ModelName:    item.ModelName,
			Priority:     item.Priority,
			Weight:       item.Weight,
			HTTPStatus:   result.HTTPStatus,
			DurationMS:   result.DurationMS,
			ErrorMessage: result.ErrorMessage,
		}
		if result.Success {
			attempt.Status = model.GroupHealthAttemptStatusSuccess
		} else {
			attempt.Status = model.GroupHealthAttemptStatusFailed
		}
		if err := s.repo.AppendAttempt(ctx, snapshot.ID, attempt); err != nil {
			return err
		}

		if result.Success {
			successFound = true
			successCount++
			if firstSuccessIndex == -1 {
				firstSuccessIndex = index
				successfulChannelID = &item.ChannelID
			}
			if stopAfterSuccess {
				for _, skipped := range items[index+1:] {
					channelName := fmt.Sprintf("channel-%d", skipped.ChannelID)
					if skippedChannel, getErr := op.ChannelGet(skipped.ChannelID, ctx); getErr == nil {
						channelName = skippedChannel.Name
					}
					if err := s.repo.AppendAttempt(ctx, snapshot.ID, model.GroupHealthAttempt{
						GroupItemID: skipped.ID,
						ChannelID:   skipped.ChannelID,
						ChannelName: channelName,
						ModelName:   skipped.ModelName,
						Priority:    skipped.Priority,
						Weight:      skipped.Weight,
						Status:      model.GroupHealthAttemptStatusSkipped,
					}); err != nil {
						return err
					}
				}
				break
			}
		}
	}

	finalStatus := model.GroupHealthStatusFailed
	if !successFound && len(items) == 0 {
		message = "group has no items"
	} else if !successFound && attemptedCount == 0 && skippedCount > 0 {
		finalStatus = model.GroupHealthStatusPartial
		message = fmt.Sprintf("all %d candidates skipped (health probe disabled)", skippedCount)
	} else if successFound {
		successChannelName := resolveChannelName(ctx, items[firstSuccessIndex].ChannelID)
		switch {
		case stopAfterSuccess && firstSuccessIndex == 0:
			finalStatus = model.GroupHealthStatusSuccess
			message = fmt.Sprintf("candidate %s succeeded", successChannelName)
		case stopAfterSuccess:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("candidate %s succeeded after failover", successChannelName)
		case skippedCount > 0 && successCount == attemptedCount:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("%d/%d candidates succeeded; %d skipped", successCount, attemptedCount, skippedCount)
		case successCount == attemptedCount:
			finalStatus = model.GroupHealthStatusSuccess
			message = fmt.Sprintf("all %d candidates succeeded", successCount)
		default:
			finalStatus = model.GroupHealthStatusPartial
			message = fmt.Sprintf("%d/%d candidates succeeded", successCount, attemptedCount)
		}
	}

	finishedAt := time.Now()
	durationMS := finishedAt.Sub(snapshot.StartedAt).Milliseconds()
	fallbackStatus = finalStatus
	fallbackSuccessfulChannelID = successfulChannelID
	fallbackMessage = message
	if err := s.repo.FinishSnapshot(ctx, snapshot.ID, finalStatus, successfulChannelID, durationMS, message, finishedAt); err != nil {
		return err
	}
	snapshotFinished = true
	return nil
}

func (s *Service) RunAllGroupHealth(ctx context.Context, maxConcurrency int, probeModes ...model.GroupHealthProbeMode) {
	probeMode := normalizeProbeMode(probeModes)
	groups, err := op.GroupList(ctx)
	if err != nil {
		return
	}
	groupIDs := make([]int, 0, len(groups))
	for _, group := range groups {
		groupIDs = append(groupIDs, group.ID)
	}
	runGroupHealthJobs(ctx, groupIDs, maxConcurrency, func(runCtx context.Context, groupID int) error {
		return s.RunGroupHealth(runCtx, groupID, probeMode)
	})
}

func runGroupHealthJobs(ctx context.Context, groupIDs []int, maxConcurrency int, run func(context.Context, int) error) {
	if len(groupIDs) == 0 || run == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if maxConcurrency <= 0 {
		maxConcurrency = defaultGroupHealthWorkers
	}
	if maxConcurrency > maxGroupHealthWorkers {
		maxConcurrency = maxGroupHealthWorkers
	}
	if maxConcurrency > len(groupIDs) {
		maxConcurrency = len(groupIDs)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(maxConcurrency)
	for range maxConcurrency {
		go func() {
			defer wg.Done()
			for groupID := range jobs {
				_ = run(ctx, groupID)
			}
		}()
	}

sendJobs:
	for _, groupID := range groupIDs {
		select {
		case jobs <- groupID:
		case <-ctx.Done():
			break sendJobs
		}
	}
	close(jobs)
	wg.Wait()
}

func (s *Service) ListGroupHealthViews(ctx context.Context) ([]model.GroupHealthGroupView, error) {
	return s.repo.ListGroupHealthViews(ctx)
}

func (s *Service) GetGroupHealthViewByID(ctx context.Context, groupID int) (*model.GroupHealthGroupView, error) {
	return s.repo.GetGroupHealthViewByID(ctx, groupID)
}

func (s *Service) GetRunningSnapshotByGroupID(ctx context.Context, groupID int) (*model.GroupHealthSnapshot, error) {
	return s.repo.GetRunningSnapshotByGroupID(ctx, groupID)
}

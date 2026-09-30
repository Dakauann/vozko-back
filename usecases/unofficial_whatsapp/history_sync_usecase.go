package unofficial_whatsapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"

	uw "vozko/domain/unofficial_whatsapp"
)

const (
	historyPageSize     = 500
	historyPageInterval = 750 * time.Millisecond
	historyPassTimeout  = 15 * time.Minute
	historyLeaseMargin  = 5 * time.Minute
	historyClaimLimit   = 4
	historyAdoptTimeout = 10 * time.Minute
)

type historyImporter interface {
	importHistory(ctx context.Context, instance *uw.Instance, env *uw.Envelope, windowFrom time.Time) historyImport
}

type HistorySyncDeps struct {
	Runs      uw.HistorySyncRepository
	Instances uw.InstanceRepository
	Servers   uw.ServerRepository
	Source    uw.HistoryAPI
	Importer  *HandleWebhookUseCase
	Handover  *LineHandover
}

type HistorySyncUseCase struct {
	runs      uw.HistorySyncRepository
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	source    uw.HistoryAPI
	importer  historyImporter
	handover  *LineHandover

	policy       uw.HistorySyncPolicy
	owner        string
	pageSize     int
	pageInterval time.Duration
	passTimeout  time.Duration
	now          func() time.Time
	sleep        func(context.Context, time.Duration) error
	spawn        func(func())
}

func NewHistorySyncUseCase(d HistorySyncDeps) *HistorySyncUseCase {
	return &HistorySyncUseCase{
		runs:         d.Runs,
		instances:    d.Instances,
		servers:      d.Servers,
		source:       d.Source,
		importer:     d.Importer,
		handover:     d.Handover,
		policy:       uw.DefaultHistorySyncPolicy(),
		owner:        workerIdentity(),
		pageSize:     historyPageSize,
		pageInterval: historyPageInterval,
		passTimeout:  historyPassTimeout,
		now:          nowUTC,
		sleep:        sleepCtx,
		spawn:        func(fn func()) { go fn() },
	}
}

func (uc *HistorySyncUseCase) SetWindow(window time.Duration) {
	if window > 0 {
		uc.policy.Window = window
	}
}

func (uc *HistorySyncUseCase) InstanceConnected(ctx context.Context, instance *uw.Instance) {
	snapshot := *instance
	uc.spawn(func() {
		adoptCtx, cancel := context.WithTimeout(context.Background(), historyAdoptTimeout)
		defer cancel()
		if err := uc.handover.Adopt(adoptCtx, &snapshot); err != nil {
			log.Printf("[unofficial-whatsapp][line] instance %s: handover on connect failed, the next history pass retries it: %v",
				snapshot.ID, err)
		}
	})
	if !instance.ImportHistory {
		log.Printf("[unofficial-whatsapp][history] instance %s: connected with history import switched off; nothing queued", instance.ID)
		return
	}
	if _, err := uc.Schedule(ctx, instance, uw.HistorySyncTriggerConnect); err != nil {
		log.Printf("[unofficial-whatsapp][history] instance %s: could not queue the import on connect: %v", instance.ID, err)
	}
}

func (uc *HistorySyncUseCase) Schedule(ctx context.Context, instance *uw.Instance, trigger uw.HistorySyncTrigger) (*uw.HistorySync, error) {
	now := uc.now()
	if active, err := uc.resumeActive(ctx, instance.ID, now); err == nil {
		return active, nil
	} else if !errors.Is(err, uw.ErrHistorySyncNotFound) {
		return nil, err
	}

	run, err := uw.NewHistorySync(instance, trigger, now, uc.policy)
	if err != nil {
		return nil, err
	}
	if err := uc.runs.Create(ctx, run); err != nil {
		if errors.Is(err, uw.ErrHistorySyncActive) {
			return uc.resumeActive(ctx, instance.ID, now)
		}
		return nil, err
	}
	log.Printf("[unofficial-whatsapp][history] instance %s: queued %s import %s, window from %s, polling until %s",
		instance.ID, trigger, run.ID, run.WindowFrom.Format(time.RFC3339), run.PollUntil.Format(time.RFC3339))
	return run, nil
}

func (uc *HistorySyncUseCase) resumeActive(ctx context.Context, instanceID string, now time.Time) (*uw.HistorySync, error) {
	active, err := uc.runs.FindActive(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	pollUntil := now.Add(uc.policy.PollWindow)
	if err := uc.runs.Resume(ctx, active.ID, now, pollUntil); err != nil {
		return nil, err
	}
	active.Resume(now, uc.policy)
	log.Printf("[unofficial-whatsapp][history] instance %s: import %s already active, polling again now", instanceID, active.ID)
	return active, nil
}

func (uc *HistorySyncUseCase) Request(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) (*uw.HistorySync, error) {
	instance, err := uc.visibleInstance(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return nil, err
	}
	if !instance.ImportHistory {
		return nil, uw.ErrHistoryImportOff
	}
	if !instance.SessionLive() {
		return nil, uw.ErrInstanceNotConnected
	}
	latest, err := uc.runs.FindLatest(ctx, instance.ID)
	switch {
	case err == nil && !latest.AllowsManualRetryAt(uc.now(), uc.policy):
		return nil, uw.ErrHistorySyncCooldown
	case err != nil && !errors.Is(err, uw.ErrHistorySyncNotFound):
		return nil, err
	}
	return uc.Schedule(ctx, instance, uw.HistorySyncTriggerManual)
}

func (uc *HistorySyncUseCase) Latest(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) (*uw.HistorySync, error) {
	instance, err := uc.visibleInstance(ctx, instanceID, workspaceID, scope)
	if err != nil {
		return nil, err
	}
	return uc.runs.FindLatest(ctx, instance.ID)
}

func (uc *HistorySyncUseCase) visibleInstance(ctx context.Context, instanceID, workspaceID string, scope uw.DepartmentScope) (*uw.Instance, error) {
	instance, err := uc.instances.FindByID(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if err := EnsureVisible(instance, workspaceID, scope); err != nil {
		return nil, err
	}
	return instance, nil
}

func (uc *HistorySyncUseCase) Execute(ctx context.Context) error {
	now := uc.now()
	claimed, err := uc.runs.ClaimDue(ctx, now, uc.owner, now.Add(uc.passTimeout+historyLeaseMargin), historyClaimLimit)
	if err != nil {
		return fmt.Errorf("unofficial whatsapp: claim history imports: %w", err)
	}
	for _, run := range claimed {
		uc.process(ctx, run)
	}
	return nil
}

func (uc *HistorySyncUseCase) process(ctx context.Context, run *uw.HistorySync) {
	instance, err := uc.instances.FindByID(ctx, run.InstanceID)
	switch {
	case errors.Is(err, uw.ErrInstanceNotFound):
		run.Stop(uw.HistorySyncCancelled, "número removido", uc.now())
		uc.save(ctx, run, "instance removed")
		return
	case err != nil:
		run.FailAttempt(err.Error(), true, uc.now(), uc.policy)
		uc.save(ctx, run, "instance lookup failed")
		return
	case !instance.ImportHistory:
		run.Stop(uw.HistorySyncCancelled, "importação de histórico desativada para este número", uc.now())
		uc.save(ctx, run, "history import switched off")
		return
	case !instance.SessionLive():
		run.Pause(uc.now(), uc.policy)
		uc.save(ctx, run, fmt.Sprintf("paused, instance is %s", instance.Status))
		return
	}

	if err := uc.handover.Adopt(ctx, instance); err != nil {
		run.FailAttempt("não foi possível transferir as conversas do número anterior: "+err.Error(), true, uc.now(), uc.policy)
		uc.save(ctx, run, "line handover failed, import held back so it cannot split threads")
		return
	}

	server, err := uc.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		run.FailAttempt(err.Error(), true, uc.now(), uc.policy)
		uc.save(ctx, run, "server lookup failed")
		return
	}

	started := uc.now()
	passCtx, cancel := context.WithTimeout(ctx, uc.passTimeout)
	pass, err := uc.sweep(passCtx, run, instance, uw.RefFor(server, instance))
	cancel()
	uc.settle(ctx, run, pass, err, started)
}

func (uc *HistorySyncUseCase) sweep(ctx context.Context, run *uw.HistorySync, instance *uw.Instance, ref uw.InstanceRef) (uw.HistoryPass, error) {
	var pass uw.HistoryPass
	offset := 0
	for {
		if pass.Pages > 0 {
			if err := uc.sleep(ctx, uc.pageInterval); err != nil {
				return pass, err
			}
		}
		page, err := uc.source.FindMessages(ctx, ref, uw.FindMessagesInput{Limit: uc.pageSize, Offset: offset})
		if err != nil {
			return pass, err
		}

		imported := uc.importer.importHistory(ctx, instance, uw.HistoryEnvelope(page.Messages), run.WindowFrom)
		imported.pass.Pages = 1
		pass.Add(imported.pass)
		log.Printf("[unofficial-whatsapp][history] instance %s: import %s page offset=%d returned=%d imported=%d duplicate=%d skipped=%d failed=%d",
			instance.ID, run.ID, offset, page.Returned,
			imported.pass.Imported, imported.pass.Duplicate, imported.pass.Skipped, imported.pass.Failed)

		switch {
		case page.Returned < uc.pageSize:
			return pass, nil
		case imported.reachedWindow:
			return pass, nil
		case run.MessagesImported+pass.Imported >= uc.policy.MaxMessages:
			pass.CapReached = true
			return pass, nil
		}
		offset += page.Returned
	}
}

func (uc *HistorySyncUseCase) settle(ctx context.Context, run *uw.HistorySync, pass uw.HistoryPass, err error, started time.Time) {
	now := uc.now()
	provErr, isProvider := uw.AsProviderError(err)

	switch {
	case err == nil:
		run.CompletePass(pass, now, uc.policy)
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil:
		log.Printf("[unofficial-whatsapp][history] instance %s: import %s pass hit its %s budget; the next poll continues",
			run.InstanceID, run.ID, uc.passTimeout)
		run.CompletePass(pass, now, uc.policy)
	case isProvider && provErr.IsRestriction():
		run.FailPass(pass, err.Error(), false, now, uc.policy)
		run.Stop(uw.HistorySyncPartial, "o WhatsApp está restringindo este número; importação interrompida", now)
	case isProvider && provErr.NeedsReconnect():
		run.FailPass(pass, err.Error(), true, now, uc.policy)
		run.Pause(now, uc.policy)
	case isProvider && !provErr.Retryable():
		run.FailPass(pass, err.Error(), false, now, uc.policy)
	default:
		run.FailPass(pass, err.Error(), true, now, uc.policy)
	}

	uc.save(ctx, run, fmt.Sprintf("pass took %s: pages=%d seen=%d imported=%d duplicate=%d skipped=%d failed=%d",
		now.Sub(started).Round(time.Millisecond), pass.Pages, pass.Seen, pass.Imported, pass.Duplicate, pass.Skipped, pass.Failed))
}

func (uc *HistorySyncUseCase) save(ctx context.Context, run *uw.HistorySync, note string) {
	if err := uc.runs.Save(ctx, run); err != nil {
		log.Printf("[unofficial-whatsapp][history] instance %s: import %s could not be saved (%s): %v",
			run.InstanceID, run.ID, note, err)
		return
	}
	log.Printf("[unofficial-whatsapp][history] instance %s: import %s %s (%s); totals imported=%d duplicate=%d skipped=%d failed=%d passes=%d oldest=%s next=%s%s",
		run.InstanceID, run.ID, run.Status, note,
		run.MessagesImported, run.MessagesDuplicate, run.MessagesSkipped, run.MessagesFailed, run.Passes,
		formatOptionalTime(run.OldestMessageAt), run.NextPollAt.Format(time.RFC3339), reasonSuffix(run))
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return "none"
	}
	return t.Format(time.RFC3339)
}

func reasonSuffix(run *uw.HistorySync) string {
	switch {
	case run.Reason != "":
		return " reason=" + run.Reason
	case run.LastError != "":
		return " last_error=" + run.LastError
	}
	return ""
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func workerIdentity() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "worker"
	}
	id := host + "-" + uuid.NewString()[:8]
	if len(id) > 64 {
		id = id[len(id)-64:]
	}
	return id
}

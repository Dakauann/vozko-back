package container

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	wsdelivery "vozko/delivery/ws"
	"vozko/domain/crm_telemetry"
	dialer_infra "vozko/infra/dialer"
	"vozko/infra/voip/audio"
	crm_telemetry_usecase "vozko/usecases/crm_telemetry"
	dialer_usecase "vozko/usecases/dialer"
	queue_usecase "vozko/usecases/dialer/queue"
)

// durableQueueSink publishes ACD queue lifecycle events to crm_telemetry (no DB on dialer path).
type durableQueueSink struct {
	pub crm_telemetry.Publisher
}

func (s *durableQueueSink) QueueEvent(ev queue_usecase.Event) {
	if s == nil || s.pub == nil || ev.WorkspaceID == "" {
		return
	}
	at := ev.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_ = s.pub.Publish(crm_telemetry.KindQueueEvent, crm_telemetry.QueueEventPayload{
		WorkspaceID: ev.WorkspaceID,
		TransferID:  ev.TransferID,
		CallID:      ev.CallID,
		TargetKind:  string(ev.Target.Kind),
		TargetID:    ev.Target.ID,
		Type:        ev.Type,
		Position:    ev.Position,
		WaitedMS:    ev.WaitedMS,
		OccurredAt:  at,
	})
}

func (c *Container) initDialerTransferStack() {
	sessions := dialer_infra.NewInProcSessionRegistry()
	// Gate which endpoints ring per the member's ring-channel selection, resolved
	// from the CACHED workspace repo (cache hit, invalidated on change).
	sessions.SetRingResolver(dialer_infra.NewMemberRingResolver(c.repositories.workspace))
	calls := dialer_infra.NewInProcCallRegistry()
	store := dialer_infra.NewInProcTransferStore()
	park := dialer_infra.NewInProcParkRegistry()

	executor := wsdelivery.NewDialerTransferExecutor(sessions, calls)
	executor.SetParkRegistry(park)
	// Hold audio resolution chain: the workspace's selected track (builtin or
	// uploaded hold_music media, cached decoded per workspace) -> the global
	// HOLD_MUSIC_PATH asset -> the generated comfort tone. Never dead air.
	holdMusic := dialer_infra.NewWorkspaceHoldMusicProvider(
		c.repositories.workspaceConfig,
		c.repositories.media,
		audio.DecodeMP3AsTelephonyPCM,
		dialerHoldAudioFactory(),
		log.Default(),
	)
	executor.SetHoldAudio(holdMusic.Source)
	executor.SetFallbackAnnouncement(dialerFallbackAnnouncement())

	// ACD waiting-line director: a caller who can't be placed immediately is queued
	// (parked + rung to the next free agent) instead of returned busy. Policy comes
	// from the workspace config (bounds guarantee no infinite wait); candidates reuse
	// the SAME reservation-aware availability as the roulette, scoped to the target.
	queueDirector := queue_usecase.New(queue_usecase.Config{
		Policy:     queue_usecase.NewConfigPolicyResolver(c.repositories.workspaceConfig),
		Candidates: queue_usecase.NewCandidateSource(sessions, c.repositories.workspaceDepartment),
		Events:     &durableQueueSink{pub: c.services.crmTelemetryPublisher},
		Logger:     log.Default(),
	})

	var transferTel dialer_usecase.TransferTelemetry
	if c.services.crmTelemetryEmitter != nil {
		transferTel = crm_telemetry_usecase.NewTransferAdapter(c.services.crmTelemetryEmitter)
	}
	transferUC, err := dialer_usecase.NewCallTransferUseCase(dialer_usecase.CallTransferUseCaseConfig{
		Sessions:  sessions,
		Calls:     calls,
		Store:     store,
		Executor:  executor,
		Logger:    log.Default(),
		OfferTTL:  dialerTransferOfferTTL,
		Queue:     queueDirector,
		Telemetry: transferTel,
	})
	if err != nil {

		panic("container: failed to build CallTransferUseCase: " + err.Error())
	}

	executor.SetAborter(transferUC)

	reaperCtx, cancel := context.WithCancel(context.Background())
	c.dialerTransferReaperCancel = cancel
	go runDialerTransferReaper(reaperCtx, transferUC)

	usernameResolver := newDialerUsernameResolver(c.repositories.user)

	// Ring-channels self-service: persists the member's selection (cached repo) and
	// applies it to their live endpoints via the concrete registry.
	c.services.setRingChannelsUC = dialer_usecase.NewSetRingChannelsUseCase(c.repositories.workspace, sessions)

	c.services.dialerSessions = sessions
	c.services.dialerCalls = calls
	c.services.dialerTransferStore = store
	c.services.dialerTransferUC = transferUC
	c.services.dialerUsernameResolver = usernameResolver

	_ = executor
}

// dialerHoldAudioFactory resolves what a held/parked caller hears: a music-on-hold
// MP3 decoded ONCE at boot when HOLD_MUSIC_PATH is set (same pattern as the
// KEYBOARD_SOUND_PATH ambience bed), otherwise the generated comfort tone, never
// dead air.
func dialerHoldAudioFactory() dialer_infra.HoldAudioFactory {
	path := strings.TrimSpace(os.Getenv("HOLD_MUSIC_PATH"))
	if path == "" {
		return func(string) dialer_infra.HoldAudioSource { return dialer_infra.NewComfortToneSource() }
	}
	pcm, err := audio.LoadMP3AsTelephonyPCM(path)
	if err != nil || len(pcm) == 0 {
		log.Printf("[DialerTransfer] HOLD_MUSIC_PATH %q unusable (%v); falling back to the generated comfort tone", path, err)
		return func(string) dialer_infra.HoldAudioSource { return dialer_infra.NewComfortToneSource() }
	}
	log.Printf("[DialerTransfer] music on hold loaded from %s (%d bytes of telephony PCM)", path, len(pcm))
	return func(string) dialer_infra.HoldAudioSource { return dialer_infra.NewLoopingPCMSource(pcm) }
}

// dialerFallbackAnnouncement resolves the optional announcement played to a
// parked caller nobody could take, right before hanging up
// (TRANSFER_FALLBACK_AUDIO_PATH). Absent, the fallback hangs up directly.
func dialerFallbackAnnouncement() []byte {
	path := strings.TrimSpace(os.Getenv("TRANSFER_FALLBACK_AUDIO_PATH"))
	if path == "" {
		return nil
	}
	pcm, err := audio.LoadMP3AsTelephonyPCM(path)
	if err != nil || len(pcm) == 0 {
		log.Printf("[DialerTransfer] TRANSFER_FALLBACK_AUDIO_PATH %q unusable (%v); fallback will hang up without an announcement", path, err)
		return nil
	}
	log.Printf("[DialerTransfer] transfer fallback announcement loaded from %s (%d bytes)", path, len(pcm))
	return pcm
}

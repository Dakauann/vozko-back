package geocoding_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"vozko/domain/address"
	"vozko/domain/geo"
	"vozko/domain/geocoding"
	"vozko/domain/metrics"
)

const providerCallTimeout = 10 * time.Second

var errChainIncomplete = errors.New("geocoding chain: a required dependency is missing")

type ChainDeps struct {
	Reference geo.Reference
	Providers map[geocoding.Provider]geo.Geocoder
	Settings  geocoding.SettingsStore
	Usage     geocoding.UsageStore
	Answers   geocoding.AnswerCache
	Pauses    geocoding.PauseStore
	Metrics   metrics.GeocodingMetricsRecorder
	Now       func() time.Time
}

type Chain struct {
	deps ChainDeps
	now  func() time.Time
	mu   sync.Mutex
	held map[geocoding.Provider]geocoding.ProviderPause
}

type BatchResult struct {
	Settlements []geocoding.Settlement
	Release     []string
}

func NewChain(deps ChainDeps) (*Chain, error) {
	missing := map[string]bool{
		"reference": deps.Reference == nil,
		"settings":  deps.Settings == nil,
		"usage":     deps.Usage == nil,
		"answers":   deps.Answers == nil,
		"pauses":    deps.Pauses == nil,
		"metrics":   deps.Metrics == nil,
	}
	for name, absent := range missing {
		if absent {
			return nil, fmt.Errorf("%w: %s", errChainIncomplete, name)
		}
	}
	for name, provider := range deps.Providers {
		if provider == nil {
			return nil, fmt.Errorf("%w: provider %s", errChainIncomplete, name)
		}
	}
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Chain{deps: deps, now: now, held: map[geocoding.Provider]geocoding.ProviderPause{}}, nil
}

func (c *Chain) hold(pause geocoding.ProviderPause) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.held[pause.Provider] = geocoding.LongerPause(c.held[pause.Provider], pause)
}

func (c *Chain) heldPause(provider geocoding.Provider, now time.Time) geocoding.ProviderPause {
	c.mu.Lock()
	defer c.mu.Unlock()
	pause := c.held[provider]
	if !pause.ActiveAt(now) {
		delete(c.held, provider)
		return geocoding.ProviderPause{}
	}
	return pause
}

func (c *Chain) share(ctx context.Context, pause geocoding.ProviderPause) {
	if err := c.deps.Pauses.OpenProviderPause(ctx, pause); err != nil {
		log.Printf("[geocoding] the %s pause could not be shared with other replicas, this replica holds it alone: %v", pause.Provider, err)
	}
}

type pauseState struct {
	pause      geocoding.ProviderPause
	active     bool
	unreadable bool
}

type batch struct {
	chain      *Chain
	claims     []geocoding.Claim
	deadline   time.Time
	now        time.Time
	down       []geo.UnavailableReason
	index      geo.ReferenceIndex
	settings   map[string]geocoding.Settings
	settingsOK bool
	answers    map[geocoding.AnswerKey]geocoding.Answer
	answersOK  bool
	pauses     map[geocoding.Provider]pauseState
	refused    map[geocoding.Provider]int
	blocked    map[string]geocoding.ProviderStep
}

func (c *Chain) Resolve(ctx context.Context, claims []geocoding.Claim, deadline time.Time) BatchResult {
	b := &batch{
		chain: c, claims: claims, deadline: deadline, now: c.now(),
		pauses: map[geocoding.Provider]pauseState{}, refused: map[geocoding.Provider]int{}, blocked: map[string]geocoding.ProviderStep{},
	}
	b.down = make([]geo.UnavailableReason, len(claims))
	b.readReference(ctx)
	candidates := make([][]geo.Fix, len(claims))
	wanted := map[string]bool{}
	skips := make([]geocoding.SkipReason, len(claims))
	for i, claim := range claims {
		if geocoding.StaleClaim(claim.Address, claim.Fingerprint) {
			skips[i] = geocoding.SkipStale
			continue
		}
		if b.down[i] != "" {
			skips[i] = geocoding.SkipReferenceDown
			continue
		}
		candidates[i] = b.index.Candidates(claim.Address.Postal, b.now)
		skips[i] = geocoding.WantsProvider(claim.Address, candidates[i])
		if skips[i] == "" {
			wanted[claim.WorkspaceID] = true
		}
	}
	b.readSettings(ctx, wanted)
	b.readAnswers(ctx, skips)
	var out BatchResult
	for i, claim := range claims {
		step := geocoding.Skipped(skips[i])
		if skips[i] == "" {
			step = b.providerStep(ctx, claim)
		}
		resolution, err := geocoding.Resolve(geocoding.Attempt{
			Address: claim.Address, Fingerprint: claim.Fingerprint, Attempts: claim.Attempts,
			ReferenceDown: b.down[i], Candidates: candidates[i], Provider: step, Now: b.now,
		})
		if err != nil {
			log.Printf("[geocoding] address %s of workspace %s left for a later sweep: %v", claim.AddressID, claim.WorkspaceID, err)
			out.Release = append(out.Release, claim.AddressID)
			continue
		}
		out.Settlements = append(out.Settlements, geocoding.Settlement{Claim: claim, Resolution: resolution})
	}
	return out
}

func (b *batch) readReference(ctx context.Context) {
	coverage, err := b.chain.deps.Reference.Coverage(ctx)
	if err != nil {
		log.Printf("[geocoding] reference coverage unreadable: %v", err)
		b.markDown(geo.ReasonReferenceDown)
		return
	}
	var covered []address.Postal
	for i, claim := range b.claims {
		if coverage.Covers(claim.Address.Postal) {
			covered = append(covered, claim.Address.Postal)
			continue
		}
		b.down[i] = geo.ReasonReferenceNotLoaded
	}
	if len(covered) == 0 {
		return
	}
	index, err := b.chain.deps.Reference.Lookup(ctx, covered)
	if err != nil {
		log.Printf("[geocoding] reference lookup failed: %v", err)
		b.markDown(geo.ReasonReferenceDown)
		return
	}
	b.index = index
}

func (b *batch) markDown(reason geo.UnavailableReason) {
	for i := range b.down {
		if b.down[i] == "" {
			b.down[i] = reason
		}
	}
}

func (b *batch) readSettings(ctx context.Context, wanted map[string]bool) {
	if len(wanted) == 0 {
		return
	}
	ids := make([]string, 0, len(wanted))
	for id := range wanted {
		ids = append(ids, id)
	}
	settings, err := b.chain.deps.Settings.SettingsOf(ctx, ids)
	if err != nil {
		log.Printf("[geocoding] provider settings unreadable: %v", err)
		return
	}
	b.settings, b.settingsOK = settings, true
}

func (b *batch) readAnswers(ctx context.Context, skips []geocoding.SkipReason) {
	b.answers = map[geocoding.AnswerKey]geocoding.Answer{}
	var keys []geocoding.AnswerKey
	for i, claim := range b.claims {
		if skips[i] == "" && b.settingsOK && b.settings[claim.WorkspaceID].Enabled() {
			keys = append(keys, keyOfClaim(claim))
		}
	}
	keys = geocoding.AnswerKeysOf(keys)
	if len(keys) == 0 {
		b.answersOK = true
		return
	}
	answers, err := b.chain.deps.Answers.Answers(ctx, keys)
	if err != nil {
		log.Printf("[geocoding] stored provider answers unreadable: %v", err)
		return
	}
	for key, answer := range answers {
		b.answers[key] = answer
	}
	b.answersOK = true
}

func keyOfClaim(claim geocoding.Claim) geocoding.AnswerKey {
	return geocoding.AnswerKey{WorkspaceID: claim.WorkspaceID, Fingerprint: claim.Fingerprint}
}

func (b *batch) providerStep(ctx context.Context, claim geocoding.Claim) geocoding.ProviderStep {
	if !b.settingsOK {
		return geocoding.Answered(geo.Unavailable(geo.ReasonSettingsUnavailable, 0))
	}
	settings := b.settings[claim.WorkspaceID]
	if !settings.Enabled() {
		return geocoding.Skipped(geocoding.SkipProviderOff)
	}
	if !b.answersOK {
		return geocoding.Answered(geo.Unavailable(geo.ReasonAnswersUnavailable, 0))
	}
	key := keyOfClaim(claim)
	if answer, stored := b.answers[key]; stored {
		b.chain.deps.Metrics.IncGeocodingAnswerReused()
		return geocoding.Answered(answer.Outcome())
	}
	if step, blocked := b.blocked[claim.WorkspaceID]; blocked {
		return step
	}
	provider, ok := b.chain.deps.Providers[settings.Provider]
	if !ok {
		return geocoding.Answered(geo.Unavailable(geo.ReasonProviderNotConfigured, 0))
	}
	if !b.chain.now().Before(b.deadline) {
		return geocoding.Deferred()
	}
	if paused, held := b.pauseStep(ctx, settings.Provider); held {
		return paused
	}
	slot, err := settings.SlotAt(b.now)
	if errors.Is(err, geocoding.ErrNoRoom) {
		return geocoding.Skipped(geocoding.SkipNoCeiling)
	}
	if err != nil {
		return geocoding.Skipped(geocoding.SkipProviderOff)
	}
	taken, usage, err := b.chain.deps.Usage.TakeSlot(ctx, claim.WorkspaceID, slot)
	if err != nil {
		log.Printf("[geocoding] usage of workspace %s unreadable: %v", claim.WorkspaceID, err)
		return geocoding.Answered(geo.Unavailable(geo.ReasonUsageUnavailable, 0))
	}
	if !taken {
		step, exhaustion := slot.Refused(usage)
		b.blocked[claim.WorkspaceID] = step
		if exhaustion != geocoding.ExhaustedNone {
			b.chain.deps.Metrics.IncGeocodingQuotaHit(string(exhaustion))
		}
		return step
	}
	outcome := b.call(ctx, settings.Provider, provider, claim)
	return geocoding.Answered(b.afterAnswer(ctx, settings.Provider, key, outcome))
}

func (b *batch) pauseStep(ctx context.Context, provider geocoding.Provider) (geocoding.ProviderStep, bool) {
	state, read := b.pauses[provider]
	if !read {
		state = b.readPause(ctx, provider)
		b.pauses[provider] = state
	}
	switch {
	case state.unreadable:
		return geocoding.Answered(geo.Unavailable(geo.ReasonPauseUnavailable, 0)), true
	case state.active:
		return geocoding.Answered(state.pause.Outcome(b.now)), true
	}
	return geocoding.ProviderStep{}, false
}

func (b *batch) readPause(ctx context.Context, provider geocoding.Provider) pauseState {
	shared, open, err := b.chain.deps.Pauses.ProviderPause(ctx, provider)
	if err != nil {
		log.Printf("[geocoding] pause state of %s unreadable, no call is made: %v", provider, err)
		return pauseState{unreadable: true}
	}
	if !open {
		shared = geocoding.ProviderPause{}
	}
	local := b.chain.heldPause(provider, b.now)
	if local.Until.After(shared.Until) {
		b.chain.share(ctx, local)
	}
	pause := geocoding.LongerPause(shared, local)
	active := pause.ActiveAt(b.now)
	if active {
		b.chain.deps.Metrics.SetGeocodingProviderPausedUntil(string(pause.Provider), string(pause.Reason), pause.Until)
	}
	return pauseState{pause: pause, active: active}
}

func (b *batch) openPause(ctx context.Context, pause geocoding.ProviderPause) geo.Outcome {
	b.chain.hold(pause)
	b.pauses[pause.Provider] = pauseState{pause: pause, active: true}
	b.chain.deps.Metrics.IncGeocodingProviderPause(string(pause.Provider), string(pause.Reason))
	b.chain.deps.Metrics.SetGeocodingProviderPausedUntil(string(pause.Provider), string(pause.Reason), pause.Until)
	log.Printf("[geocoding] %s refused (%s): every workspace is paused until %s", pause.Provider, pause.Reason, pause.Until.Format(time.RFC3339))
	b.chain.share(ctx, pause)
	return geo.Unavailable(pause.Reason, pause.Remaining(b.now))
}

func (b *batch) afterAnswer(ctx context.Context, provider geocoding.Provider, key geocoding.AnswerKey, outcome geo.Outcome) geo.Outcome {
	if pause, opened := geocoding.PauseAfter(provider, outcome, b.now); opened {
		return b.openPause(ctx, pause)
	}
	if geocoding.RefusalOf(outcome) == geocoding.RefusalQuery {
		b.refused[provider]++
		if pause, opened := geocoding.PauseAfterQueryRefusals(provider, b.refused[provider], b.now); opened {
			return b.openPause(ctx, pause)
		}
	}
	answer, kept := geocoding.AnswerOf(provider, key, outcome, b.chain.now())
	if !kept {
		return outcome
	}
	b.answers[key] = answer
	if err := b.chain.deps.Answers.Remember(ctx, answer); err != nil {
		log.Printf("[geocoding] the %s answer for a text of workspace %s could not be stored: %v", provider, key.WorkspaceID, err)
	}
	return outcome
}

func (b *batch) call(ctx context.Context, name geocoding.Provider, provider geo.Geocoder, claim geocoding.Claim) geo.Outcome {
	callCtx, cancel := context.WithTimeout(ctx, providerCallTimeout)
	defer cancel()
	started := b.chain.now()
	outcome, err := provider.Geocode(callCtx, geo.Query{WorkspaceID: claim.WorkspaceID, Postal: claim.Address.Postal})
	elapsed := b.chain.now().Sub(started)
	if err != nil {
		log.Printf("[geocoding] %s failed for address %s: %v", name, claim.AddressID, err)
		b.chain.deps.Metrics.ObserveGeocodingProvider(string(name), metrics.GeocodingProviderError, elapsed)
		return geo.Unavailable(geo.ReasonProviderDown, 0)
	}
	b.chain.deps.Metrics.ObserveGeocodingProvider(string(name), providerResult(outcome), elapsed)
	return outcome
}

func providerResult(o geo.Outcome) string {
	switch o.Kind {
	case geo.OutcomeLocated:
		return metrics.GeocodingProviderOK
	case geo.OutcomeNotFound:
		return metrics.GeocodingProviderNotFound
	case geo.OutcomeAmbiguous:
		return metrics.GeocodingProviderAmbiguous
	}
	return metrics.GeocodingProviderUnavailable
}

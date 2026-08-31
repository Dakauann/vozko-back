package inbox_assignment_usecase

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

const (
	// DefaultRescueBatch caps how many stalled conversations one tick moves.
	// Oldest first, so a saturated batch always makes progress on whoever has
	// been waiting longest rather than re-picking an arbitrary page.
	DefaultRescueBatch = 200

	// MaxRescueHops bounds how far one conversation may walk the ring before
	// the sweep gives up and unassigns it. The effective cap is
	// min(len(ring), MaxRescueHops): a ring of three is exhausted after three
	// hops, and a ring of forty does not get forty chances to annoy forty
	// people about one conversation.
	MaxRescueHops = 5

	// TriggerRescueExhausted is the reason stamped on the unassignment when the
	// ring has been walked and nobody took the conversation.
	TriggerRescueExhausted = "rescue_exhausted"
)

// rescueConfigReader is the workspace-config slice the sweep needs: the
// filtered policy list that drives it, and the full config for the one or two
// workspaces that actually have work.
type rescueConfigReader interface {
	ListRoulettePolicies(ctx context.Context) ([]wsc.RoulettePolicy, error)
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

// rescueHistoryReader is the ownership-interval slice the sweep needs.
type rescueHistoryReader interface {
	ListOpenOlderThan(workspaceIDs []string, trigger string, olderThan time.Time, limit int) ([]*ia.AssignmentHistory, error)
	CountRescuesSinceHandout(workspaceID, entryID, entryType string) (int, error)
}

// rescueStatusReader tells the sweep to leave finished conversations alone.
type rescueStatusReader interface {
	GetConversationStatus(entryID, entryType string) conversation.ConversationStatus
}

// RescueJob moves a conversation on when the agent it was handed to never
// opened it.
//
// It exists because the last_seen mode can hand a conversation to somebody who
// is not looking at the screen. Without it, "distribute to whoever was online
// recently" would sometimes mean "park this customer in an away agent's
// backlog", which is worse than the online-only behaviour it replaces.
//
// Everything it does goes through AssignmentService, the same choke point a
// manual reassignment uses, so the history interval, the telemetry and the
// timeline event cannot diverge from any other ownership change.
type RescueJob struct {
	cfg       rescueConfigReader
	history   rescueHistoryReader
	attention ia.EntryAttentionReader
	status    rescueStatusReader
	assign    *AssignmentService
	batch     int
	disabled  bool
	now       func() time.Time
}

func NewRescueJob(
	cfg rescueConfigReader,
	history rescueHistoryReader,
	attention ia.EntryAttentionReader,
	status rescueStatusReader,
	assign *AssignmentService,
) *RescueJob {
	batch := DefaultRescueBatch
	if v := strings.TrimSpace(os.Getenv("ASSIGNMENT_RESCUE_BATCH")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 1000 {
				n = 1000
			}
			batch = n
		}
	}
	disabled := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ASSIGNMENT_RESCUE_DISABLED"))) {
	case "1", "true", "yes", "on":
		disabled = true
	}
	return &RescueJob{
		cfg:       cfg,
		history:   history,
		attention: attention,
		status:    status,
		assign:    assign,
		batch:     batch,
		disabled:  disabled,
		now:       time.Now,
	}
}

// SetClock is for tests. Production uses time.Now.
func (j *RescueJob) SetClock(now func() time.Time) {
	if now != nil {
		j.now = now
	}
}

// Execute runs one sweep. It never returns an error for a single failed entry:
// one bad conversation must not stop the rest of the batch.
func (j *RescueJob) Execute(ctx context.Context) error {
	if j == nil || j.disabled || j.cfg == nil || j.history == nil || j.assign == nil {
		return nil
	}

	policies, err := j.cfg.ListRoulettePolicies(ctx)
	if err != nil {
		log.Printf("[assignment_rescue] policy list error: %v", err)
		return nil
	}
	if len(policies) == 0 {
		// The cost for every workspace on the default mode: one indexed read,
		// then nothing.
		return nil
	}

	byWorkspace := make(map[string]wsc.RoulettePolicy, len(policies))
	workspaceIDs := make([]string, 0, len(policies))
	minAfter := time.Duration(0)
	for _, p := range policies {
		byWorkspace[p.WorkspaceID] = p
		workspaceIDs = append(workspaceIDs, p.WorkspaceID)
		if minAfter == 0 || p.RescueAfter < minAfter {
			minAfter = p.RescueAfter
		}
	}

	now := j.now().UTC()
	// One query for every eligible workspace, bounded by the shortest deadline
	// among them; each candidate is then re-checked against its own.
	open, err := j.history.ListOpenOlderThan(workspaceIDs, ia.TriggerInboundRR, now.Add(-minAfter), j.batch)
	if err != nil {
		log.Printf("[assignment_rescue] candidate list error: %v", err)
		return nil
	}
	if len(open) == 0 {
		return nil
	}
	if len(open) == j.batch {
		log.Printf("[assignment_rescue] batch full at %d; the remainder is picked up next tick", j.batch)
	}

	// One tick resolves each workspace's config and each (workspace, department)
	// ring ONCE, however many stalled conversations it holds.
	//
	// Without this, a workspace with a full batch of 200 stalled conversations
	// ran 200 identical config reads and 200 identical presence queries — the
	// presence one an IN over up to 500 user ids. Neither answer can change
	// within a tick that takes seconds, and the roster underneath is already
	// cached for a minute, so resolving per candidate bought nothing and cost
	// the database everything.
	tick := &rescueTick{
		now:     now,
		configs: make(map[string]*wsc.WorkspaceConfig, len(policies)),
		pools:   make(map[string]Pool, len(policies)),
	}

	moved, exhausted, skipped := 0, 0, 0
	for _, h := range open {
		switch j.rescueOne(ctx, h, byWorkspace[h.WorkspaceID], tick) {
		case outcomeMoved:
			moved++
		case outcomeExhausted:
			exhausted++
		default:
			skipped++
		}
	}
	if moved > 0 || exhausted > 0 {
		log.Printf("[assignment_rescue] moved=%d exhausted=%d skipped=%d of %d candidate(s) across %d workspace(s), %d ring(s) resolved",
			moved, exhausted, skipped, len(open), len(tick.configs), len(tick.pools))
	}
	return nil
}

// rescueTick memoizes the two answers that are per-workspace rather than
// per-conversation, for the lifetime of a single sweep.
type rescueTick struct {
	now     time.Time
	configs map[string]*wsc.WorkspaceConfig
	pools   map[string]Pool
	// configErr remembers a failed read so a broken workspace is not retried
	// once per candidate.
	configErr map[string]bool
}

func (t *rescueTick) config(ctx context.Context, cfg rescueConfigReader, workspaceID string) (*wsc.WorkspaceConfig, error) {
	if c, ok := t.configs[workspaceID]; ok {
		return c, nil
	}
	if t.configErr[workspaceID] {
		return nil, errTickConfigAlreadyFailed
	}
	c, err := cfg.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		if t.configErr == nil {
			t.configErr = map[string]bool{}
		}
		t.configErr[workspaceID] = true
		return nil, err
	}
	t.configs[workspaceID] = c
	return c, nil
}

func (t *rescueTick) pool(r *CandidateResolver, workspaceID, departmentID string, skipAdmins bool, cfg *wsc.WorkspaceConfig) Pool {
	key := workspaceID + "|" + departmentID
	if p, ok := t.pools[key]; ok {
		return p
	}
	p := r.Resolve(workspaceID, departmentID, skipAdmins, cfg)
	t.pools[key] = p
	return p
}

// errTickConfigAlreadyFailed is logged once per workspace per tick rather than
// once per conversation.
var errTickConfigAlreadyFailed = errors.New("workspace config already failed this tick")

type rescueOutcome int

const (
	outcomeSkipped rescueOutcome = iota
	outcomeMoved
	outcomeExhausted
)

func (j *RescueJob) rescueOne(ctx context.Context, h *ia.AssignmentHistory, policy wsc.RoulettePolicy, tick *rescueTick) rescueOutcome {
	now := tick.now
	if h == nil || h.EntryID == "" || h.AssignedActorID == "" {
		return outcomeSkipped
	}
	// Each workspace's own deadline, not the shortest one the query used.
	if now.Sub(h.StartedAt) <= policy.RescueAfter {
		return outcomeSkipped
	}
	// An AI owner is not a roulette hand-out and has its own hand-off path.
	if actor.IsAI(h.AssignedActorID) {
		return outcomeSkipped
	}
	if j.status != nil && j.status.GetConversationStatus(h.EntryID, h.EntryType) == conversation.ConversationStatusFinished {
		return outcomeSkipped
	}
	if j.attention != nil {
		attended, err := j.attention.AttendedSince(h.EntryID, h.EntryType, h.AssignedActorID, h.StartedAt)
		if err != nil {
			log.Printf("[assignment_rescue] attention check failed for %s (%s): %v", h.EntryID, h.EntryType, err)
			return outcomeSkipped
		}
		if attended {
			return outcomeSkipped
		}
	}

	cfg, err := tick.config(ctx, j.cfg, h.WorkspaceID)
	if err != nil {
		if !errors.Is(err, errTickConfigAlreadyFailed) {
			log.Printf("[assignment_rescue] config read failed for workspace %s: %v", h.WorkspaceID, err)
		}
		return outcomeSkipped
	}
	// The admin may have switched the mode off between the policy list and
	// here. Re-checking costs one cached read and avoids moving a conversation
	// under a policy that no longer applies.
	if !cfg.RouletteRescueActive() {
		return outcomeSkipped
	}

	skipAdmins := cfg.SkipAdminAssignment
	// The same resolver the assignment used, so the rescue can never walk a
	// different ring than the one the conversation came from.
	pool := tick.pool(j.assign.Candidates(), h.WorkspaceID, h.DepartmentID, skipAdmins, cfg)
	if len(pool.Ring) == 0 {
		// Nobody to hand it to. Leaving the current owner beats unassigning
		// into a workspace where nobody is eligible anyway.
		log.Printf("[assignment_rescue] entry %s (%s) has no eligible ring; leaving it with %s", h.EntryID, h.EntryType, h.AssignedActorID)
		return outcomeSkipped
	}

	hops, err := j.history.CountRescuesSinceHandout(h.WorkspaceID, h.EntryID, h.EntryType)
	if err != nil {
		log.Printf("[assignment_rescue] hop count failed for %s (%s): %v", h.EntryID, h.EntryType, err)
		return outcomeSkipped
	}
	hopCap := MaxRescueHops
	if len(pool.Ring) < hopCap {
		hopCap = len(pool.Ring)
	}
	if hops >= hopCap {
		if err := j.assign.UnassignSystem(h.EntryID, h.EntryType, h.WorkspaceID, TriggerRescueExhausted); err != nil {
			log.Printf("[assignment_rescue] unassign failed for %s (%s): %v", h.EntryID, h.EntryType, err)
			return outcomeSkipped
		}
		log.Printf("[assignment_rescue] entry %s (%s) exhausted after %d hop(s) over a ring of %d; unassigned so anyone can pick it up",
			h.EntryID, h.EntryType, hops, len(pool.Ring))
		return outcomeExhausted
	}

	next := ia.NextAfter(pool.Ring, h.AssignedActorID)
	if next == "" || next == h.AssignedActorID {
		log.Printf("[assignment_rescue] entry %s (%s) has nobody else in a ring of %d; leaving it with %s",
			h.EntryID, h.EntryType, len(pool.Ring), h.AssignedActorID)
		return outcomeSkipped
	}

	// AssignManual, not a direct repo write: it closes the ownership interval,
	// opens the next one, publishes telemetry and writes the timeline event.
	// The round-robin pointer is deliberately NOT advanced — a rescue repairs
	// one conversation, it is not a turn of the wheel, and moving the pointer
	// would make the next inbound conversation skip an agent who did nothing
	// wrong.
	if err := j.assign.AssignManual(h.EntryID, h.EntryType, h.BusinessPhoneID, h.WorkspaceID, next, actor.SystemID, ia.TriggerRescue); err != nil {
		log.Printf("[assignment_rescue] reassign failed for %s (%s): %v", h.EntryID, h.EntryType, err)
		return outcomeSkipped
	}
	log.Printf("[assignment_rescue] entry %s (%s) moved %s → %s after %s unattended (hop %d/%d)",
		h.EntryID, h.EntryType, h.AssignedActorID, next, now.Sub(h.StartedAt).Round(time.Second), hops+1, hopCap)
	return outcomeMoved
}

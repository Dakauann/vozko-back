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
	wd "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

const (
	DefaultRescueBatch = 200

	MaxRescueHops = 5

	TriggerRescueExhausted = "rescue_exhausted"
)

type rescueConfigReader interface {
	ListRoulettePolicies(ctx context.Context) ([]wsc.RoulettePolicy, error)
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type rescueHistoryReader interface {
	ListOpenOlderThan(workspaceIDs []string, triggers []string, olderThan time.Time, limit int) ([]*ia.AssignmentHistory, error)
	CountRescuesSinceHandout(workspaceID, entryID, entryType string) (int, error)
}

type rescueStatusReader interface {
	GetConversationStatus(entryID, entryType string) conversation.ConversationStatus
}

type rescueDepartmentScheduleReader interface {
	ListWorkingHours(workspaceIDs []string) ([]wd.DepartmentSchedule, error)
}

type RescueJob struct {
	cfg         rescueConfigReader
	history     rescueHistoryReader
	attention   ia.EntryAttentionReader
	status      rescueStatusReader
	assign      *AssignmentService
	departments rescueDepartmentScheduleReader
	batch       int
	disabled    bool
	now         func() time.Time
}

func (j *RescueJob) SetDepartmentSchedules(r rescueDepartmentScheduleReader) {
	if j != nil {
		j.departments = r
	}
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

func (j *RescueJob) SetClock(now func() time.Time) {
	if now != nil {
		j.now = now
	}
}

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
		return nil
	}

	now := j.now().UTC()

	schedules := j.resolveSchedules(policies)

	byWorkspace := make(map[string]wsc.RoulettePolicy, len(policies))
	workspaceIDs := make([]string, 0, len(policies))
	minAfter := time.Duration(0)
	closed := 0
	for _, p := range policies {
		if !schedules.workspaceCanHaveWorkNow(p.WorkspaceID, now) {
			closed++
			continue
		}
		byWorkspace[p.WorkspaceID] = p
		workspaceIDs = append(workspaceIDs, p.WorkspaceID)
		if minAfter == 0 || p.RescueAfter < minAfter {
			minAfter = p.RescueAfter
		}
	}
	if closed > 0 {
		log.Printf("[assignment_rescue] %d workspace(s) outside working hours this tick%s", closed, schedules.reopenHint(policies, now))
	}
	if len(workspaceIDs) == 0 {
		return nil
	}
	open, err := j.history.ListOpenOlderThan(workspaceIDs, ia.RescueCandidateTriggers, now.Add(-minAfter), j.batch)
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

	tick := &rescueTick{
		now:       now,
		configs:   make(map[string]*wsc.WorkspaceConfig, len(policies)),
		pools:     make(map[string]Pool, len(policies)),
		schedules: schedules,
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

type rescueTick struct {
	now       time.Time
	configs   map[string]*wsc.WorkspaceConfig
	pools     map[string]Pool
	schedules *tickSchedules
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
	schedule := tick.schedules.forEntry(h.WorkspaceID, h.DepartmentID)
	if schedule.Elapsed(h.StartedAt, now) <= policy.RescueAfter {
		return outcomeSkipped
	}
	if !schedule.IsOpen(now) {
		return outcomeSkipped
	}
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
	if !cfg.RouletteRescueActive() {
		return outcomeSkipped
	}

	skipAdmins := cfg.SkipAdminAssignment
	pool := tick.pool(j.assign.Candidates(), h.WorkspaceID, h.DepartmentID, skipAdmins, cfg)
	if len(pool.Ring) == 0 {
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

	if err := j.assign.AssignManual(h.EntryID, h.EntryType, h.BusinessPhoneID, h.WorkspaceID, next, actor.SystemID, ia.TriggerRescue); err != nil {
		log.Printf("[assignment_rescue] reassign failed for %s (%s): %v", h.EntryID, h.EntryType, err)
		return outcomeSkipped
	}
	log.Printf("[assignment_rescue] entry %s (%s) moved %s → %s after %s unattended (hop %d/%d)",
		h.EntryID, h.EntryType, h.AssignedActorID, next, now.Sub(h.StartedAt).Round(time.Second), hops+1, hopCap)
	return outcomeMoved
}

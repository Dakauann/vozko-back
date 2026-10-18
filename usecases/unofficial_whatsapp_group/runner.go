package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/ai"
	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const (
	analysisLease      = 10 * time.Minute
	analysesPerSweep   = 5
	batchesPerDailyRun = 5
	transcriptBudget   = 24_000
	analysisMaxTokens  = 4096
	analysisTemp       = 0.3
	failureReasonRunes = 400
	runWriteAttempts   = 3
)

func (s *Service) RunDueAnalyses(ctx context.Context) error {
	for i := 0; i < analysesPerSweep; i++ {
		now := s.d.Clock()
		a, err := s.d.Analyses.ClaimNext(ctx, now, s.d.Owner, now.Add(analysisLease))
		if err != nil {
			return err
		}
		if a == nil {
			return nil
		}
		if err := s.runAnalysis(ctx, a); err != nil {
			log.Printf("[groups] analysis %s did not finish: %v", a.ID, err)
		}
	}
	return nil
}

type batchStop int

const (
	stopDrained batchStop = iota
	stopFull
	stopFailed
	stopUnpaid
	stopLeaseLost
	stopSettling
)

type batchOutcome struct {
	consumed int
	stop     batchStop
	cursor   *uwg.Cursor
}

func (s *Service) runAnalysis(ctx context.Context, a *uwg.Analysis) error {
	now := s.d.Clock()
	loc := s.location(ctx, a.WorkspaceID)
	g, err := s.d.Groups.FindByID(ctx, a.WorkspaceID, a.GroupID, uwg.Scope{})
	if err != nil && !errors.Is(err, uwg.ErrGroupNotFound) {
		return err
	}
	if err != nil || !g.Followed() {
		return s.finishIdle(ctx, a, now, loc)
	}
	conv, err := s.d.Messages.GroupConversation(ctx, g.PrimaryInstanceID, g.JID)
	if errors.Is(err, uwg.ErrNoGroupConversation) {
		return s.finishIdle(ctx, a, now, loc)
	}
	if err != nil {
		return err
	}
	names, err := s.memberNames(ctx, g.ID)
	if err != nil {
		return err
	}

	outcome := s.analyseBatches(ctx, a, g, conv, names, loc)
	if outcome.cursor != nil {
		a.Advance(*outcome.cursor)
	}
	switch outcome.stop {
	case stopLeaseLost:
		return ErrAnalysisLeaseLost
	case stopFailed:
		a.RecordFailure(now)
		return s.d.Analyses.Finish(ctx, a, s.d.Owner, outcome.consumed)
	case stopUnpaid:
		retry := now.Add(uwg.FirstFailureBackoff)
		a.NextRunAt = &retry
		return s.d.Analyses.Finish(ctx, a, s.d.Owner, outcome.consumed)
	}
	if outcome.stop == stopSettling && a.Trigger == uwg.TriggerMessageCount {
		retry := now.Add(uwg.ArrivalGrace)
		a.NextRunAt = &retry
		return s.d.Analyses.Finish(ctx, a, s.d.Owner, outcome.consumed)
	}
	a.RecordSuccess()
	a.LastRunAt = &now
	a.NextRunAt = nil
	consumed := outcome.consumed
	if a.Trigger == uwg.TriggerScheduled {
		next := a.NextRun(now, loc)
		if outcome.stop == stopFull {
			next = now
		}
		a.NextRunAt = &next
	} else if outcome.stop == stopDrained {
		consumed = a.PendingMessages
	}
	return s.d.Analyses.Finish(ctx, a, s.d.Owner, consumed)
}

var ErrAnalysisLeaseLost = errors.New("another worker took over this analysis")

func (s *Service) analyseBatches(ctx context.Context, a *uwg.Analysis, g *uwg.Group, conv string, names map[string]string, loc *time.Location) batchOutcome {
	var out batchOutcome
	now := s.d.Clock()
	after := a.Cursor()
	batches := 1
	if a.Trigger == uwg.TriggerScheduled {
		batches = batchesPerDailyRun
	}
	for i := 0; i < batches; i++ {
		if !s.spend.Allow(a.WorkspaceID) {
			out.stop = stopUnpaid
			return out
		}
		if err := s.d.Analyses.Extend(ctx, a.ID, s.d.Owner, s.d.Clock().Add(analysisLease)); err != nil {
			log.Printf("[groups] analysis %s lost its lease: %v", a.ID, err)
			out.stop = stopLeaseLost
			return out
		}
		lines, err := s.d.Messages.TextAfter(ctx, conv, after, a.MessagesPerCycle)
		if err != nil {
			log.Printf("[groups] could not read the messages of analysis %s: %v", a.ID, err)
			out.stop = stopFailed
			return out
		}
		lines, waiting := uwg.SettledLines(lines, now)
		if waiting && (a.Trigger == uwg.TriggerMessageCount || len(lines) == 0) {
			out.stop = stopSettling
			return out
		}
		if len(lines) == 0 {
			out.stop = stopDrained
			return out
		}
		full := len(lines) == a.MessagesPerCycle
		lines = withinBudget(lines)
		run := s.analyse(ctx, a, g, lines, names, loc)
		if run.Status != uwg.RunCompleted {
			out.stop = stopFailed
			return out
		}
		last := lines[len(lines)-1]
		after = uwg.Cursor{At: last.At, ID: last.ID}
		out.cursor = &after
		out.consumed += len(lines)
		if !full {
			out.stop = stopDrained
			return out
		}
		out.stop = stopFull
	}
	return out
}

func withinBudget(lines []uwg.TranscriptLine) []uwg.TranscriptLine {
	used := 0
	for i, l := range lines {
		used += len([]rune(l.Text)) + len([]rune(l.Author)) + 24
		if used > transcriptBudget && i > 0 {
			return lines[:i]
		}
	}
	return lines
}

func (s *Service) analyse(ctx context.Context, a *uwg.Analysis, g *uwg.Group, lines []uwg.TranscriptLine, names map[string]string, loc *time.Location) *uwg.Run {
	started := s.d.Clock()
	cycle, err := s.d.Runs.NextCycle(ctx, a.ID)
	if err != nil {
		cycle = 0
	}
	from, to := lines[0].At, lines[len(lines)-1].At
	run := &uwg.Run{
		AnalysisID: a.ID, WorkspaceID: a.WorkspaceID, GroupID: g.ID, Cycle: cycle, Kind: a.RunKind(),
		MessagesAnalyzed: len(lines), PeriodFrom: &from, PeriodTo: &to, Model: s.d.Model, StartedAt: started,
	}
	if cycle == 0 {
		return s.failRun(ctx, run, err)
	}
	out, err := s.d.AI.Generate(ctx, ai.GenerateInput{
		WorkspaceID:      a.WorkspaceID,
		BillingReference: fmt.Sprintf("groupanalysis:%s:%d", a.ID, cycle),
		SessionID:        "groupanalysis:" + a.ID,
		Model:            s.d.Model,
		SystemPrompt:     analysisSystemPrompt,
		Messages:         []ai.Message{{Role: ai.RoleUser, Content: buildAnalysisMessage(a, g.Subject, lines, names, loc)}},
		Temperature:      analysisTemp,
		MaxTokens:        analysisMaxTokens,
		ResponseFormat: &ai.ResponseFormat{
			Type:                  ai.ResponseFormatJSONSchema,
			JSONSchemaName:        "group_analysis",
			JSONSchemaDescription: "Análise estruturada de um trecho de conversa de grupo do WhatsApp.",
			JSONSchema:            uwg.ResponseSchema(a.TrackedFields),
			JSONSchemaStrict:      true,
		},
	})
	if err != nil {
		return s.failRun(ctx, run, err)
	}
	run.PromptTokens, run.CompletionTokens = out.Usage.PromptTokens, out.Usage.CompletionTokens
	if out.FinishReason == "length" {
		return s.failRun(ctx, run, errors.New("the model answer was cut off"))
	}
	result, err := parseAnalysis(out.Message.Content, a.TrackedFields)
	if err != nil {
		return s.failRun(ctx, run, err)
	}
	run.Status, run.Result, run.CompletedAt = uwg.RunCompleted, result, s.d.Clock()
	if err := s.storeRun(ctx, run); err != nil {
		log.Printf("[groups] could not store the paid run %d of analysis %s: %v", cycle, a.ID, err)
		run.Status = uwg.RunFailed
	}
	return run
}

func (s *Service) storeRun(ctx context.Context, run *uwg.Run) error {
	var err error
	for attempt := 0; attempt < runWriteAttempts; attempt++ {
		if err = s.d.Runs.Create(ctx, run); err == nil {
			return nil
		}
	}
	return err
}

func (s *Service) failRun(ctx context.Context, run *uwg.Run, cause error) *uwg.Run {
	reason, _ := shared.TruncateRunes(cause.Error(), failureReasonRunes)
	run.Status, run.FailureReason, run.CompletedAt = uwg.RunFailed, reason, s.d.Clock()
	if run.Cycle > 0 {
		if err := s.storeRun(ctx, run); err != nil {
			log.Printf("[groups] could not record the failure of analysis %s: %v", run.AnalysisID, err)
		}
	}
	return run
}

func (s *Service) memberNames(ctx context.Context, groupID string) (map[string]string, error) {
	return s.d.Members.Names(ctx, groupID)
}

func (s *Service) finishIdle(ctx context.Context, a *uwg.Analysis, now time.Time, loc *time.Location) error {
	a.LastRunAt = &now
	a.NextRunAt = nil
	if a.Trigger == uwg.TriggerScheduled {
		next := a.NextRun(now, loc)
		a.NextRunAt = &next
	}
	return s.d.Analyses.Finish(ctx, a, s.d.Owner, a.PendingMessages)
}

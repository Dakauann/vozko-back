package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"time"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const runMessagesPage = 100

type AnalysisInput struct {
	GroupID          string
	Name             string
	Prompt           string
	Trigger          uwg.Trigger
	MessagesPerCycle int
	ScheduleHour     int
	ScheduleMinute   int
	TrackedFields    []uwg.TrackedField
}

func (s *Service) CreateAnalysis(ctx context.Context, a Actor, in AnalysisInput) (*uwg.Analysis, error) {
	_, g, err := s.visibleGroup(ctx, a, in.GroupID)
	if err != nil {
		return nil, err
	}
	if !g.Followed() {
		return nil, uwg.ErrGroupNotFollowed
	}
	in.GroupID = g.ID
	analysis, err := s.DraftAnalysis(a, in)
	if err != nil {
		return nil, err
	}
	s.schedule(ctx, analysis, *analysis.AnalyzedUntil)
	if err := s.d.Analyses.Create(ctx, analysis); err != nil {
		return nil, err
	}
	return analysis, nil
}

func (s *Service) DraftAnalysis(a Actor, in AnalysisInput) (*uwg.Analysis, error) {
	now := s.d.Clock()
	analysis := &uwg.Analysis{
		WorkspaceID: a.WorkspaceID, GroupID: in.GroupID, Name: in.Name, Prompt: in.Prompt,
		Trigger: in.Trigger, MessagesPerCycle: in.MessagesPerCycle,
		ScheduleHour: in.ScheduleHour, ScheduleMinute: in.ScheduleMinute,
		TrackedFields: in.TrackedFields, AnalyzedUntil: &now, CreatedByID: a.UserID,
	}
	analysis.Normalize()
	if err := analysis.Validate(); err != nil {
		return nil, err
	}
	return analysis, nil
}

func (s *Service) schedule(ctx context.Context, a *uwg.Analysis, now time.Time) {
	if a.Trigger != uwg.TriggerScheduled {
		a.NextRunAt = nil
		return
	}
	next := a.NextRun(now, s.location(ctx, a.WorkspaceID))
	a.NextRunAt = &next
}

type AnalysisPatch struct {
	Name             *string
	Prompt           *string
	Status           *uwg.AnalysisStatus
	Trigger          *uwg.Trigger
	MessagesPerCycle *int
	ScheduleHour     *int
	ScheduleMinute   *int
	TrackedFields    *[]uwg.TrackedField
}

func (p AnalysisPatch) reschedules() bool {
	return p.Trigger != nil || p.ScheduleHour != nil || p.ScheduleMinute != nil || p.Status != nil
}

func (s *Service) UpdateAnalysis(ctx context.Context, a Actor, id string, p AnalysisPatch) (*uwg.Analysis, error) {
	analysis, _, err := s.visibleAnalysis(ctx, a, id)
	if err != nil {
		return nil, err
	}
	assign(&analysis.Name, p.Name)
	assign(&analysis.Prompt, p.Prompt)
	assign(&analysis.Status, p.Status)
	assign(&analysis.Trigger, p.Trigger)
	assign(&analysis.MessagesPerCycle, p.MessagesPerCycle)
	assign(&analysis.ScheduleHour, p.ScheduleHour)
	assign(&analysis.ScheduleMinute, p.ScheduleMinute)
	assign(&analysis.TrackedFields, p.TrackedFields)
	analysis.Normalize()
	if err := analysis.Validate(); err != nil {
		return nil, err
	}
	if p.reschedules() {
		s.schedule(ctx, analysis, s.d.Clock())
	}
	if err := s.d.Analyses.Save(ctx, analysis); err != nil {
		return nil, err
	}
	return s.d.Analyses.FindByID(ctx, a.WorkspaceID, id)
}

func assign[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

func (s *Service) DeleteAnalysis(ctx context.Context, a Actor, id string) error {
	if _, _, err := s.visibleAnalysis(ctx, a, id); err != nil {
		return err
	}
	return s.d.Analyses.Delete(ctx, a.WorkspaceID, id)
}

type AnalysisListInput struct {
	Status     uwg.AnalysisStatus
	Trigger    uwg.Trigger
	GroupID    string
	Search     string
	Sort       uwg.AnalysisSort
	Pagination shared.Pagination
}

func (s *Service) ListAnalyses(ctx context.Context, a Actor, in AnalysisListInput) (*shared.PaginatedResult[*uwg.AnalysisRow], error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.d.Analyses.List(ctx, uwg.AnalysisFilter{
		WorkspaceID: a.WorkspaceID, Scope: n.scope, Status: in.Status, Trigger: in.Trigger,
		GroupID: in.GroupID, Search: in.Search, Sort: in.Sort, Pagination: in.Pagination,
	})
}

func (s *Service) GetAnalysis(ctx context.Context, a Actor, id string, days int) (*AnalysisDetail, error) {
	analysis, g, err := s.visibleAnalysis(ctx, a, id)
	if err != nil {
		return nil, err
	}
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	if days == 0 {
		days = uwg.OverviewWindowDays
	}
	since := time.Time{}
	if days > 0 {
		since = s.d.Clock().AddDate(0, 0, -days)
	} else {
		days = 0
	}
	runs, err := s.d.Runs.Since(ctx, a.WorkspaceID, id, since)
	if err != nil {
		return nil, err
	}
	detail := &AnalysisDetail{
		Analysis: analysis, Group: g, Overview: uwg.BuildOverview(runs, topItems),
		WindowDays: days, Timezone: s.location(ctx, a.WorkspaceID).String(),
	}
	detail.ParticipantGroups, err = s.participantGroups(ctx, a.WorkspaceID, n.scope, runs)
	return detail, err
}

func (s *Service) participantGroups(ctx context.Context, workspaceID string, scope uwg.Scope, runs []uwg.Run) (map[string][]uwg.GroupRef, error) {
	out := map[string][]uwg.GroupRef{}
	keyOf := map[string]string{}
	keys := []string{}
	for _, run := range runs {
		if run.Result == nil {
			continue
		}
		for _, p := range run.Result.Participants {
			if p.JID == "" {
				continue
			}
			key := uwg.PersonKey(uw.PhoneFromJID(p.JID), p.JID)
			if _, seen := keyOf[key]; seen {
				continue
			}
			keyOf[key] = p.JID
			keys = append(keys, key)
		}
	}
	groups, err := s.d.Members.GroupsOf(ctx, workspaceID, keys, scope)
	if err != nil {
		return nil, err
	}
	for key, refs := range groups {
		if len(refs) >= 2 {
			out[keyOf[key]] = refs
		}
	}
	return out, nil
}

type RunHistory struct {
	Runs              *shared.PaginatedResult[*uwg.Run]
	ParticipantGroups map[string][]uwg.GroupRef
}

func (s *Service) AnalysisRuns(ctx context.Context, a Actor, id string, p shared.Pagination) (*RunHistory, error) {
	if _, _, err := s.visibleAnalysis(ctx, a, id); err != nil {
		return nil, err
	}
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	page, err := s.d.Runs.ListByAnalysis(ctx, a.WorkspaceID, id, p)
	if err != nil {
		return nil, err
	}
	runs := make([]uwg.Run, 0, len(page.Items))
	for _, r := range page.Items {
		runs = append(runs, *r)
	}
	groups, err := s.participantGroups(ctx, a.WorkspaceID, n.scope, runs)
	if err != nil {
		return nil, err
	}
	return &RunHistory{Runs: page, ParticipantGroups: groups}, nil
}

func (s *Service) RunMessages(ctx context.Context, a Actor, id, runID string, offset int) ([]uwg.TranscriptLine, error) {
	_, g, err := s.visibleAnalysis(ctx, a, id)
	if err != nil {
		return nil, err
	}
	run, err := s.d.Runs.FindByID(ctx, a.WorkspaceID, id, runID)
	if err != nil {
		return nil, err
	}
	if run.PeriodFrom == nil || run.PeriodTo == nil {
		return []uwg.TranscriptLine{}, nil
	}
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	instanceID, ok := s.readableSeat(ctx, g, n)
	if !ok {
		return []uwg.TranscriptLine{}, nil
	}
	conv, err := s.d.Messages.GroupConversation(ctx, instanceID, g.JID)
	if errors.Is(err, uwg.ErrNoGroupConversation) {
		return []uwg.TranscriptLine{}, nil
	}
	if err != nil {
		return nil, err
	}
	lines, err := s.d.Messages.Between(ctx, conv, *run.PeriodFrom, *run.PeriodTo, offset, runMessagesPage)
	if err != nil {
		return nil, err
	}
	names, err := s.memberNames(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	for i := range lines {
		if !lines[i].Outbound {
			lines[i].AuthorName = authorOf(lines[i], names).name
		}
	}
	return lines, nil
}

func (s *Service) visibleAnalysis(ctx context.Context, a Actor, id string) (*uwg.Analysis, *uwg.Group, error) {
	analysis, err := s.d.Analyses.FindByID(ctx, a.WorkspaceID, id)
	if err != nil {
		return nil, nil, err
	}
	_, g, err := s.visibleGroup(ctx, a, analysis.GroupID)
	if errors.Is(err, uwg.ErrGroupNotFound) {
		return nil, nil, uwg.ErrAnalysisNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return analysis, g, nil
}

package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

const (
	listWindow      = uwg.OverviewWindowDays * 24 * time.Hour
	listRunsPerRow  = 20
	detailRunsLimit = 200
	topItems        = 8
	cardItems       = 3
)

func (s *Service) Discover(ctx context.Context, a Actor) (*DiscoverResult, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	result := &DiscoverResult{Failed: []string{}}
	seen := map[string]*uwg.Group{}
	for _, instance := range n.inScope {
		if !instance.SessionLive() {
			continue
		}
		result.Numbers++
		listed, err := s.listSeats(ctx, instance)
		if err != nil {
			log.Printf("[groups] discovery on number %s failed: %v", instance.ID, err)
			result.Failed = append(result.Failed, instance.ID)
			continue
		}
		for _, seat := range listed {
			g, err := s.recordSeat(ctx, a.WorkspaceID, instance.ID, seat, ownNumbers(n.everyone))
			if err != nil {
				return nil, err
			}
			seen[g.ID] = g
		}
	}
	for _, g := range seen {
		if err := s.refreshPrimary(ctx, g, n.everyone); err != nil {
			return nil, err
		}
	}
	result.Groups = len(seen)
	return result, nil
}

func (s *Service) listSeats(ctx context.Context, instance *uw.Instance) ([]*uw.Group, error) {
	ref, err := s.ref(ctx, instance)
	if err != nil {
		return nil, err
	}
	return s.d.GroupAPI.ListGroups(ctx, ref, false)
}

func (s *Service) recordSeat(ctx context.Context, workspaceID, instanceID string, seat *uw.Group, own uwg.OwnNumbers) (*uwg.Group, error) {
	seat.WorkspaceID, seat.InstanceID = workspaceID, instanceID
	if err := s.d.Seats.Upsert(ctx, seat); err != nil {
		return nil, err
	}
	g := &uwg.Group{WorkspaceID: workspaceID, JID: seat.JID, Origin: uwg.OriginOf(seat, own)}
	g.AbsorbSeat(seat)
	if err := s.d.Groups.UpsertSeen(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) refreshPrimary(ctx context.Context, g *uwg.Group, instances []*uw.Instance) error {
	seats, err := s.d.Seats.ListByJIDs(ctx, g.WorkspaceID, []string{g.JID})
	if err != nil {
		return err
	}
	primary, _ := uwg.ChoosePrimary(presencesOf(seats[g.JID], instancesByID(instances)))
	if primary == g.PrimaryInstanceID {
		return nil
	}
	g.PrimaryInstanceID = primary
	return s.d.Groups.SetPrimary(ctx, g.WorkspaceID, g.ID, primary)
}

func instancesByID(instances []*uw.Instance) map[string]*uw.Instance {
	out := make(map[string]*uw.Instance, len(instances))
	for _, i := range instances {
		out[i.ID] = i
	}
	return out
}

func presencesOf(seats []*uw.Group, byID map[string]*uw.Instance) []uwg.Presence {
	out := make([]uwg.Presence, 0, len(seats))
	for _, seat := range seats {
		instance := byID[seat.InstanceID]
		out = append(out, uwg.Presence{Seat: seat, Connected: instance != nil && instance.SessionLive()})
	}
	return out
}

type ListGroupsInput struct {
	Search      string
	Followed    *bool
	Origin      uwg.Origin
	Category    *string
	InstanceID  string
	AdminsOnly  *bool
	HasAnalysis *bool
	Sort        uwg.GroupSort
	Pagination  shared.Pagination
}

func (s *Service) ListGroups(ctx context.Context, a Actor, in ListGroupsInput) (*shared.PaginatedResult[*GroupRow], error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	if in.InstanceID != "" {
		if _, ok := n.visible(in.InstanceID); !ok {
			return nil, uwg.ErrNumberOutOfScope
		}
	}
	page, err := s.d.Groups.List(ctx, uwg.GroupFilter{
		WorkspaceID: a.WorkspaceID, Scope: n.scope, Search: in.Search, Followed: in.Followed,
		Origin: in.Origin, Category: in.Category, InstanceID: in.InstanceID, AdminsOnly: in.AdminsOnly,
		HasAnalysis: in.HasAnalysis, Sort: in.Sort, Pagination: in.Pagination,
	})
	if err != nil {
		return nil, err
	}
	rows, err := s.rowsOf(ctx, a.WorkspaceID, n, page.Items, listRunsPerRow)
	if err != nil {
		return nil, err
	}
	return &shared.PaginatedResult[*GroupRow]{
		Items: rows, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages,
	}, nil
}

func (s *Service) GroupsByID(ctx context.Context, a Actor, ids []string) ([]*GroupRow, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	unique := shared.DistinctTrimmed(ids)
	groups, err := s.d.Groups.FindByIDs(ctx, a.WorkspaceID, unique, n.scope)
	if err != nil {
		return nil, err
	}
	if len(groups) != len(unique) {
		return nil, uwg.ErrGroupNotFound
	}
	return s.rowsOf(ctx, a.WorkspaceID, n, groups, listRunsPerRow)
}

func (s *Service) rowsOf(ctx context.Context, workspaceID string, n numbers, groups []*uwg.Group, runsPerGroup int) ([]*GroupRow, error) {
	ids := make([]string, 0, len(groups))
	jids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids, jids = append(ids, g.ID), append(jids, g.JID)
	}
	since := s.d.Clock().Add(-listWindow)
	seats, err := s.d.Seats.ListByJIDs(ctx, workspaceID, jids)
	if err != nil {
		return nil, err
	}
	activity, err := s.d.Stats.GroupActivity(ctx, workspaceID, ids, since)
	if err != nil {
		return nil, err
	}
	runs, err := s.d.Runs.RecentForGroups(ctx, workspaceID, ids, since, runsPerGroup)
	if err != nil {
		return nil, err
	}
	analysed, err := s.d.Analyses.GroupsWithActive(ctx, workspaceID, ids)
	if err != nil {
		return nil, err
	}
	running, err := s.d.Campaigns.RunningForGroups(ctx, workspaceID, jids, n.scope)
	if err != nil {
		return nil, err
	}
	rows := make([]*GroupRow, 0, len(groups))
	for _, g := range groups {
		rows = append(rows, &GroupRow{
			Group:            g,
			Numbers:          numberSeats(seats[g.JID], n),
			Activity:         activity[g.ID],
			Summary:          summaryOf(runs[g.ID]),
			HasAnalysis:      analysed[g.ID],
			RunningCampaigns: running[g.JID],
		})
	}
	return rows, nil
}

func numberSeats(seats []*uw.Group, n numbers) []NumberSeat {
	out := []NumberSeat{}
	for _, seat := range seats {
		instance, ok := n.visible(seat.InstanceID)
		if !ok {
			continue
		}
		presence := uwg.Presence{Seat: seat, Connected: instance.SessionLive()}
		out = append(out, NumberSeat{
			InstanceID: instance.ID, Label: instance.Label(), Phone: instance.PhoneNumber,
			Connected: presence.Connected, Admin: presence.IsAdmin(), CanPost: presence.CanPost(),
		})
	}
	return out
}

func summaryOf(runs []uwg.Run) *GroupSummary {
	if len(runs) == 0 {
		return nil
	}
	o := uwg.BuildOverview(runs, cardItems)
	if o.Completed == 0 {
		return nil
	}
	return &GroupSummary{
		Sentiment: o.Dominant, Positivity: o.AvgPositivity, Cycles: o.Completed, Messages: o.Messages,
		HighAlertPct: o.HighAlertPct, Themes: o.Themes, Mentions: o.Mentions, LastRunAt: o.LastRunAt,
	}
}

func (s *Service) GetGroup(ctx context.Context, a Actor, id string) (*GroupDetail, error) {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if err := s.refreshPrimary(ctx, g, n.everyone); err != nil {
		return nil, err
	}
	rows, err := s.rowsOf(ctx, a.WorkspaceID, n, []*uwg.Group{g}, detailRunsLimit)
	if err != nil {
		return nil, err
	}
	detail := &GroupDetail{GroupRow: *rows[0], Growth: []uwg.DailyPoint{}, Analyses: []*uwg.AnalysisRow{}}

	if instanceID, ok := s.readableSeat(ctx, g, n); ok {
		conv, err := s.d.Messages.GroupConversation(ctx, instanceID, g.JID)
		if err != nil && !errors.Is(err, uwg.ErrNoGroupConversation) {
			return nil, err
		}
		detail.ConversationID = conv
	}

	now := s.d.Clock()
	if detail.Members, err = s.d.Members.Counts(ctx, g.ID, now.Add(-listWindow)); err != nil {
		return nil, err
	}
	loc := s.location(ctx, a.WorkspaceID)
	window := uwg.LastDays(uwg.GrowthWindowDays, now, loc)
	growth, err := s.d.Stats.DailyGrowth(ctx, uwg.StatsScope{WorkspaceID: a.WorkspaceID, Scope: n.scope}, g.ID, window, loc)
	if err != nil {
		return nil, err
	}
	detail.Growth = uwg.FillDaily(window, loc, growth)

	runs, err := s.d.Runs.RecentForGroups(ctx, a.WorkspaceID, []string{g.ID}, now.Add(-listWindow), detailRunsLimit)
	if err != nil {
		return nil, err
	}
	detail.Overview = uwg.BuildOverview(runs[g.ID], topItems)
	detail.LatestRun = latestCompleted(runs[g.ID])

	if detail.Series, err = s.seriesOf(ctx, a.WorkspaceID, n, g); err != nil {
		return nil, err
	}
	analyses, err := s.d.Analyses.List(ctx, uwg.AnalysisFilter{WorkspaceID: a.WorkspaceID, Scope: n.scope, GroupID: g.ID,
		Pagination: shared.Pagination{Page: 1, PageSize: shared.MaxPageSize}})
	if err != nil {
		return nil, err
	}
	detail.Analyses = analyses.Items
	return detail, nil
}

func latestCompleted(runs []uwg.Run) *uwg.Run {
	var latest *uwg.Run
	for i := range runs {
		if runs[i].Completed() && (latest == nil || runs[i].CompletedAt.After(latest.CompletedAt)) {
			latest = &runs[i]
		}
	}
	return latest
}

func (s *Service) seriesOf(ctx context.Context, workspaceID string, n numbers, g *uwg.Group) ([]SeriesSibling, error) {
	siblings := []SeriesSibling{}
	if g.SeriesID == "" {
		return siblings, nil
	}
	page, err := s.d.Groups.List(ctx, uwg.GroupFilter{WorkspaceID: workspaceID, Scope: n.scope, SeriesID: g.SeriesID,
		Sort: uwg.SortOldest, Pagination: shared.Pagination{Page: 1, PageSize: shared.MaxPageSize}})
	if err != nil {
		return nil, err
	}
	for _, member := range page.Items {
		siblings = append(siblings, SeriesSibling{GroupID: member.ID, Subject: member.Subject, Sequence: member.SeriesSequence, Members: member.MemberCount})
	}
	sort.Slice(siblings, func(i, j int) bool { return siblings[i].Sequence < siblings[j].Sequence })
	return siblings, nil
}

func (s *Service) visibleGroup(ctx context.Context, a Actor, id string) (numbers, *uwg.Group, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return numbers{}, nil, err
	}
	g, err := s.d.Groups.FindByID(ctx, a.WorkspaceID, id, n.scope)
	if err != nil {
		return numbers{}, nil, err
	}
	return n, g, nil
}

func (s *Service) visibleSeats(ctx context.Context, g *uwg.Group, n numbers) ([]*uw.Group, error) {
	seats, err := s.d.Seats.ListByJIDs(ctx, g.WorkspaceID, []string{g.JID})
	if err != nil {
		return nil, err
	}
	var out []*uw.Group
	for _, seat := range seats[g.JID] {
		if _, ok := n.visible(seat.InstanceID); ok {
			out = append(out, seat)
		}
	}
	return out, nil
}

func (s *Service) readableSeat(ctx context.Context, g *uwg.Group, n numbers) (string, bool) {
	if _, ok := n.visible(g.PrimaryInstanceID); ok {
		return g.PrimaryInstanceID, true
	}
	seats, err := s.visibleSeats(ctx, g, n)
	if err != nil || len(seats) == 0 {
		return "", false
	}
	return seats[0].InstanceID, true
}

func (s *Service) Follow(ctx context.Context, a Actor, id string) (*uwg.Group, error) {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if g.Followed() {
		return g, nil
	}
	if err := s.follow(ctx, g); err != nil {
		return nil, err
	}
	if err := s.syncThrough(ctx, a, n, g); err != nil {
		return nil, err
	}
	return s.d.Groups.FindByID(ctx, a.WorkspaceID, id, n.scope)
}

func (s *Service) Unfollow(ctx context.Context, a Actor, id string) (*uwg.Group, error) {
	_, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	g.FollowedAt, g.MembersSyncedAt = nil, nil
	if err := s.d.Groups.SetFollowed(ctx, g.WorkspaceID, g.ID, nil); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Service) follow(ctx context.Context, g *uwg.Group) error {
	now := s.d.Clock()
	g.FollowedAt, g.MembersSyncedAt = &now, nil
	return s.d.Groups.SetFollowed(ctx, g.WorkspaceID, g.ID, &now)
}

func (s *Service) Refresh(ctx context.Context, a Actor, id string) (*uwg.Group, error) {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if err := s.syncThrough(ctx, a, n, g); err != nil {
		return nil, err
	}
	if err := s.refreshPrimary(ctx, g, n.everyone); err != nil {
		return nil, err
	}
	return s.d.Groups.FindByID(ctx, a.WorkspaceID, id, n.scope)
}

func (s *Service) syncThrough(ctx context.Context, a Actor, n numbers, g *uwg.Group) error {
	instanceID, ok := s.readableSeat(ctx, g, n)
	if !ok {
		return uwg.ErrNoConnectedNumber
	}
	seat, err := s.d.SeatSync.Get(ctx, a.WorkspaceID, instanceID, g.JID, true, a.Departments)
	if err != nil {
		return err
	}
	return s.applySeat(ctx, g, seat, n.everyone)
}

func (s *Service) applySeat(ctx context.Context, g *uwg.Group, seat *uw.Group, instances []*uw.Instance) error {
	g.AbsorbSeat(seat)
	if !g.Followed() || len(seat.Participants) == 0 {
		return s.d.Groups.Save(ctx, g)
	}
	if err := s.d.Groups.Save(ctx, g); err != nil {
		return err
	}
	current, err := s.d.Members.All(ctx, g.ID)
	if err != nil {
		return err
	}
	now := s.d.Clock()
	baseline := g.MembersSyncedAt == nil
	diff := uwg.DiffMembers(g.ID, g.WorkspaceID, current, seat.Participants, ownNumbers(instances), now, baseline)
	if err := s.d.Members.Apply(ctx, g, diff, now); err != nil {
		return err
	}
	g.MembersSyncedAt = &now
	return nil
}

type UpdateGroupInput struct {
	ApplyToSeries      bool
	Category           *string
	Subject            *string
	Description        *string
	ImageURL           *string
	AdminsOnlyMessages *bool
	AdminsOnlyEdit     *bool
}

func (in UpdateGroupInput) touchesWhatsApp() bool {
	return in.Subject != nil || in.Description != nil || in.ImageURL != nil ||
		in.AdminsOnlyMessages != nil || in.AdminsOnlyEdit != nil
}

func (s *Service) UpdateGroup(ctx context.Context, a Actor, id string, in UpdateGroupInput) (*uwg.Group, error) {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if !in.ApplyToSeries || g.SeriesID == "" {
		return s.updateOne(ctx, a, n, g, in)
	}
	page, err := s.d.Groups.List(ctx, uwg.GroupFilter{WorkspaceID: a.WorkspaceID, Scope: n.scope, SeriesID: g.SeriesID,
		Pagination: shared.Pagination{Page: 1, PageSize: shared.MaxPageSize}})
	if err != nil {
		return nil, err
	}
	var failed []string
	for _, member := range page.Items {
		change := in
		if in.Subject != nil {
			name := uwg.SeriesName(*in.Subject, member.SeriesSequence)
			change.Subject = &name
		}
		if _, err := s.updateOne(ctx, a, n, member, change); err != nil {
			log.Printf("[groups] could not update group %s of series %s: %v", member.ID, g.SeriesID, err)
			failed = append(failed, member.Subject)
		}
	}
	if len(failed) > 0 {
		return nil, fmt.Errorf("%w: %s", uwg.ErrSeriesPartlyUpdated, strings.Join(failed, ", "))
	}
	return s.d.Groups.FindByID(ctx, a.WorkspaceID, id, n.scope)
}

func (s *Service) updateOne(ctx context.Context, a Actor, n numbers, g *uwg.Group, in UpdateGroupInput) (*uwg.Group, error) {
	if in.Category != nil {
		category, err := uwg.NormalizeCategory(*in.Category)
		if err != nil {
			return nil, err
		}
		g.Category = category
		if err := s.d.Groups.Save(ctx, g); err != nil {
			return nil, err
		}
	}
	if in.touchesWhatsApp() {
		seat, err := s.changeOnWhatsApp(ctx, a, n, g, in)
		if err != nil {
			return nil, err
		}
		if err := s.applySeat(ctx, g, seat, n.everyone); err != nil {
			return nil, err
		}
	}
	return s.d.Groups.FindByID(ctx, a.WorkspaceID, g.ID, n.scope)
}

func (s *Service) adminSeat(ctx context.Context, g *uwg.Group, n numbers) (string, error) {
	seats, err := s.visibleSeats(ctx, g, n)
	if err != nil {
		return "", err
	}
	for _, seat := range seats {
		if seat.WeAreAdmin {
			return seat.InstanceID, nil
		}
	}
	return "", uw.ErrNotGroupAdmin
}

var memberActions = map[uw.GroupAction]bool{
	uw.GroupActionAdd: true, uw.GroupActionRemove: true, uw.GroupActionPromote: true, uw.GroupActionDemote: true,
}

func (s *Service) ManageMembers(ctx context.Context, a Actor, id string, action uw.GroupAction, participants []string) (*uwg.Group, error) {
	if !memberActions[action] {
		return nil, uw.ErrInvalidGroupAction
	}
	participants = shared.DistinctTrimmed(participants)
	if len(participants) == 0 {
		return nil, uw.ErrNoParticipants
	}
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return nil, err
	}
	instanceID, err := s.adminSeat(ctx, g, n)
	if err != nil {
		return nil, err
	}
	seat, err := s.d.SeatSync.UpdateParticipants(ctx, uwuc.UpdateParticipantsInput{
		WorkspaceID: a.WorkspaceID, InstanceID: instanceID, GroupJID: g.JID, Scope: a.Departments,
		Action: action, Participants: participants,
	})
	if err != nil {
		return nil, err
	}
	if err := s.applySeat(ctx, g, seat, n.everyone); err != nil {
		return nil, err
	}
	return s.d.Groups.FindByID(ctx, a.WorkspaceID, id, n.scope)
}

func (s *Service) LeaveGroup(ctx context.Context, a Actor, id string, instanceIDs []string) error {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return err
	}
	if len(instanceIDs) == 0 {
		return uwg.ErrNoNumbersSelected
	}
	for _, instanceID := range instanceIDs {
		if _, ok := n.visible(instanceID); !ok {
			return uwg.ErrNumberOutOfScope
		}
	}
	for _, instanceID := range instanceIDs {
		if err := s.d.SeatSync.Leave(ctx, a.WorkspaceID, instanceID, g.JID, a.Departments); err != nil {
			return err
		}
	}
	return s.refreshPrimary(ctx, g, n.everyone)
}

func (s *Service) InviteLink(ctx context.Context, a Actor, id string) (string, error) {
	n, g, err := s.visibleGroup(ctx, a, id)
	if err != nil {
		return "", err
	}
	instanceID, err := s.adminSeat(ctx, g, n)
	if err != nil {
		return "", err
	}
	return s.d.SeatSync.InviteLink(ctx, a.WorkspaceID, instanceID, g.JID, a.Departments)
}

func (s *Service) Categories(ctx context.Context, a Actor) ([]string, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	return s.d.Groups.Categories(ctx, a.WorkspaceID, n.scope)
}

func (s *Service) changeOnWhatsApp(ctx context.Context, a Actor, n numbers, g *uwg.Group, in UpdateGroupInput) (*uw.Group, error) {
	instanceID, err := s.adminSeat(ctx, g, n)
	if err != nil {
		return nil, err
	}
	var seat *uw.Group
	if in.Subject != nil {
		if seat, err = s.d.SeatSync.UpdateName(ctx, uwuc.UpdateNameInput{
			WorkspaceID: a.WorkspaceID, InstanceID: instanceID, GroupJID: g.JID, Name: *in.Subject, Scope: a.Departments,
		}); err != nil {
			return nil, err
		}
	}
	if in.Description != nil {
		if seat, err = s.d.SeatSync.UpdateDescription(ctx, uwuc.UpdateDescriptionInput{
			WorkspaceID: a.WorkspaceID, InstanceID: instanceID, GroupJID: g.JID, Description: *in.Description, Scope: a.Departments,
		}); err != nil {
			return nil, err
		}
	}
	if in.ImageURL != nil {
		if seat, err = s.d.SeatSync.UpdateImage(ctx, a.WorkspaceID, instanceID, g.JID, *in.ImageURL, a.Departments); err != nil {
			return nil, err
		}
	}
	if in.AdminsOnlyMessages != nil || in.AdminsOnlyEdit != nil {
		if seat, err = s.d.SeatSync.UpdateSettings(ctx, uwuc.UpdateSettingsInput{
			WorkspaceID: a.WorkspaceID, InstanceID: instanceID, GroupJID: g.JID, Scope: a.Departments,
			AdminsOnlyMessages: in.AdminsOnlyMessages, AdminsOnlyEdit: in.AdminsOnlyEdit,
		}); err != nil {
			return nil, err
		}
	}
	return seat, nil
}

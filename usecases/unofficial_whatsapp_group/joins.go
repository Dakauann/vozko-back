package unofficial_whatsapp_group_usecase

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const (
	joinLease     = 5 * time.Minute
	joinsPerSweep = 20
)

func (s *Service) PreviewInvite(ctx context.Context, a Actor, inviteLink string) (*InvitePreview, error) {
	link, err := uwg.NormalizeInviteLink(inviteLink)
	if err != nil {
		return nil, err
	}
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	instance := firstConnected(n.inScope)
	if instance == nil {
		return nil, uwg.ErrNoConnectedNumber
	}
	ref, err := s.ref(ctx, instance)
	if err != nil {
		return nil, err
	}
	info, err := s.d.GroupAPI.InviteInfo(ctx, ref, link)
	if err != nil {
		return nil, err
	}
	preview := &InvitePreview{
		InviteLink: link, JID: info.JID, Subject: info.Subject, Description: info.Description,
		Members: info.ParticipantCount, Announce: info.Announce, AlreadyIn: []string{},
	}
	seats, err := s.d.Seats.ListByJIDs(ctx, a.WorkspaceID, []string{info.JID})
	if err != nil {
		return nil, err
	}
	for _, seat := range seats[info.JID] {
		if _, ok := n.visible(seat.InstanceID); ok {
			preview.AlreadyIn = append(preview.AlreadyIn, seat.InstanceID)
		}
	}
	return preview, nil
}

func firstConnected(instances []*uw.Instance) *uw.Instance {
	for _, i := range instances {
		if i.SessionLive() {
			return i
		}
	}
	return nil
}

func (n numbers) usable(instanceIDs []string) ([]*uw.Instance, error) {
	if len(instanceIDs) == 0 {
		return nil, uwg.ErrNoNumbersSelected
	}
	out := make([]*uw.Instance, 0, len(instanceIDs))
	for _, id := range instanceIDs {
		instance, ok := n.visible(id)
		if !ok {
			return nil, uwg.ErrNumberOutOfScope
		}
		if !instance.SessionLive() {
			return nil, uw.ErrInstanceNotConnected
		}
		out = append(out, instance)
	}
	return out, nil
}

type JoinInput struct {
	InviteLink  string
	InstanceIDs []string
	Follow      bool
}

func (s *Service) JoinGroup(ctx context.Context, a Actor, in JoinInput) (*JoinResult, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	if _, err := n.usable(in.InstanceIDs); err != nil {
		return nil, err
	}
	planID := uuid.NewString()
	tasks, err := uwg.PlanJoins(uwg.JoinPlanInput{
		PlanID: planID, WorkspaceID: a.WorkspaceID, InviteLink: in.InviteLink,
		InstanceIDs: in.InstanceIDs, Follow: in.Follow, CreatedByID: a.UserID,
	}, s.d.Clock())
	if err != nil {
		return nil, err
	}
	tasks[0].Lease(s.d.Owner, s.d.Clock().Add(joinLease))
	if err := s.d.Joins.CreateMany(ctx, tasks); err != nil {
		return nil, err
	}
	g, err := s.runJoin(ctx, &tasks[0])
	if err != nil && tasks[0].Status == uwg.JoinFailed {
		return nil, err
	}
	return &JoinResult{PlanID: planID, Group: g, Tasks: tasks}, nil
}

func (s *Service) RunDueJoins(ctx context.Context) error {
	for i := 0; i < joinsPerSweep; i++ {
		now := s.d.Clock()
		task, err := s.d.Joins.ClaimNext(ctx, now, s.d.Owner, now.Add(joinLease))
		if err != nil {
			return err
		}
		if task == nil {
			return nil
		}
		if _, err := s.runJoin(ctx, task); err != nil {
			log.Printf("[groups] join %s with number %s did not finish: %v", task.ID, task.InstanceID, err)
		}
	}
	return nil
}

func (s *Service) runJoin(ctx context.Context, task *uwg.JoinTask) (*uwg.Group, error) {
	now := s.d.Clock()
	instance, err := s.d.Instances.FindByID(ctx, task.InstanceID)
	if err != nil || !instance.SessionLive() {
		return nil, s.retryJoin(ctx, task, now, uw.ErrInstanceNotConnected)
	}
	ref, err := s.ref(ctx, instance)
	if err != nil {
		return nil, s.retryJoin(ctx, task, now, err)
	}
	joined, err := s.d.GroupAPI.JoinGroup(ctx, ref, task.InviteLink)
	if err != nil {
		if permanentJoinFailure(err) {
			return nil, s.failPlan(ctx, task, err)
		}
		return nil, s.retryJoin(ctx, task, now, err)
	}
	g, err := s.adoptGroup(ctx, instance, joined.JID, task.CreatedByID, task.Follow)
	if err != nil {
		return nil, s.retryJoin(ctx, task, now, err)
	}
	task.Status, task.GroupJID, task.Error = uwg.JoinDone, joined.JID, ""
	if err := s.d.Joins.Save(ctx, task); err != nil {
		return nil, err
	}
	return g, nil
}

func permanentJoinFailure(err error) bool {
	provErr, ok := uw.AsProviderError(err)
	return ok && !provErr.Retryable()
}

func (s *Service) retryJoin(ctx context.Context, task *uwg.JoinTask, now time.Time, cause error) error {
	task.Retry(now, cause.Error())
	if err := s.d.Joins.Save(ctx, task); err != nil {
		return err
	}
	if task.Status == uwg.JoinFailed {
		if err := s.d.Joins.FailRemaining(ctx, task.PlanID, cause.Error()); err != nil {
			return err
		}
	}
	return cause
}

func (s *Service) failPlan(ctx context.Context, task *uwg.JoinTask, cause error) error {
	task.Status, task.Error = uwg.JoinFailed, cause.Error()
	if err := s.d.Joins.Save(ctx, task); err != nil {
		return err
	}
	if err := s.d.Joins.FailRemaining(ctx, task.PlanID, cause.Error()); err != nil {
		return err
	}
	return cause
}

func (s *Service) adoptGroup(ctx context.Context, instance *uw.Instance, groupJID, createdByID string, follow bool) (*uwg.Group, error) {
	seat, err := s.d.SeatSync.Get(ctx, instance.WorkspaceID, instance.ID, groupJID, true, uw.Unrestricted())
	if err != nil {
		return nil, err
	}
	g := &uwg.Group{WorkspaceID: instance.WorkspaceID, JID: groupJID}
	g.AbsorbSeat(seat)
	if err := s.d.Groups.UpsertSeen(ctx, g); err != nil {
		return nil, err
	}
	if g.CreatedByID == "" {
		g.CreatedByID = createdByID
	}
	instances, err := s.workspaceInstances(ctx, instance.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if err := s.refreshPrimary(ctx, g, instances); err != nil {
		return nil, err
	}
	if follow && !g.Followed() {
		if err := s.follow(ctx, g); err != nil {
			return nil, err
		}
	}
	if err := s.applySeat(ctx, g, seat, instances); err != nil {
		return nil, err
	}
	return s.d.Groups.FindByID(ctx, g.WorkspaceID, g.ID, uwg.Scope{})
}

func (s *Service) CancelJoin(ctx context.Context, a Actor, planID string) (int64, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return 0, err
	}
	cancelled, err := s.d.Joins.CancelPlan(ctx, a.WorkspaceID, planID, n.scope)
	if err != nil {
		return 0, err
	}
	if cancelled == 0 {
		return 0, uwg.ErrJoinNotFound
	}
	return cancelled, nil
}

type ManagedGroupInput struct {
	Name        string
	Description string
	Category    string
	InstanceIDs []string
	AdminsOnly  bool
	SkipFollow  bool
}

type seriesSlot struct {
	id       string
	sequence int
}

func (s *Service) CreateManagedGroup(ctx context.Context, a Actor, in ManagedGroupInput) (*ManagedGroupResult, error) {
	return s.createManaged(ctx, a, in, nil)
}

func (s *Service) createManaged(ctx context.Context, a Actor, in ManagedGroupInput, series *seriesSlot) (*ManagedGroupResult, error) {
	if err := uw.ValidateGroupName(in.Name); err != nil {
		return nil, err
	}
	if err := uw.ValidateGroupTopic(in.Description); err != nil {
		return nil, err
	}
	category, err := uwg.NormalizeCategory(in.Category)
	if err != nil {
		return nil, err
	}
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	instances, err := n.usable(in.InstanceIDs)
	if err != nil {
		return nil, err
	}
	creator, others := instances[0], instances[1:]
	ref, err := s.ref(ctx, creator)
	if err != nil {
		return nil, err
	}
	phones := make([]string, 0, len(others))
	for _, o := range others {
		if o.PhoneNumber != "" {
			phones = append(phones, o.PhoneNumber)
		}
	}
	created, err := s.d.GroupAPI.CreateGroup(ctx, ref, in.Name, phones)
	if err != nil {
		return nil, err
	}
	warnings := s.configureManaged(ctx, ref, created.JID, in, phones)

	g, err := s.adoptGroup(ctx, creator, created.JID, a.UserID, !in.SkipFollow)
	if err != nil {
		return nil, err
	}
	for _, o := range others {
		if _, err := s.d.SeatSync.Get(ctx, a.WorkspaceID, o.ID, created.JID, false, uw.Unrestricted()); err != nil {
			log.Printf("[groups] number %s is not in the new group %s yet: %v", o.ID, created.JID, err)
		}
	}
	g.Origin, g.Category, g.CreatedByID = uwg.OriginManaged, category, a.UserID
	if series != nil {
		g.SeriesID, g.SeriesSequence = series.id, series.sequence
	}
	if err := s.d.Groups.Save(ctx, g); err != nil {
		return nil, err
	}
	if err := s.refreshPrimary(ctx, g, n.everyone); err != nil {
		return nil, err
	}
	stored, err := s.d.Groups.FindByID(ctx, a.WorkspaceID, g.ID, uwg.Scope{})
	if err != nil {
		return nil, err
	}
	return &ManagedGroupResult{Group: stored, Warnings: warnings}, nil
}

func (s *Service) configureManaged(ctx context.Context, ref uw.InstanceRef, groupJID string, in ManagedGroupInput, others []string) []uwg.SetupWarning {
	warnings := []uwg.SetupWarning{}
	note := func(step uwg.SetupStep, err error) {
		if err != nil {
			log.Printf("[groups] setting up %s of group %s failed: %v", step, groupJID, err)
			warnings = append(warnings, uwg.SetupWarning{Step: step, Reason: err.Error()})
		}
	}
	if in.Description != "" {
		note(uwg.SetupDescription, s.d.GroupAPI.UpdateGroupDescription(ctx, ref, groupJID, in.Description))
	}
	if in.AdminsOnly {
		note(uwg.SetupAdminsOnly, s.d.GroupAPI.UpdateAnnounce(ctx, ref, groupJID, true))
	}
	if len(others) > 0 {
		note(uwg.SetupPromote, s.d.GroupAPI.UpdateParticipants(ctx, ref, uw.UpdateParticipantsInput{
			GroupJID: groupJID, Action: uw.GroupActionPromote, Participants: others,
		}))
	}
	return warnings
}

func (s *Service) NextInSeries(ctx context.Context, a Actor, groupID string) (*ManagedGroupResult, error) {
	n, source, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	if source.Origin != uwg.OriginManaged {
		return nil, uwg.ErrSeriesNeedsManagedGroup
	}
	if source.SeriesID == "" {
		source.SeriesID, source.SeriesSequence = source.ID, 1
		if err := s.d.Groups.Save(ctx, source); err != nil {
			return nil, err
		}
	}
	sequence, err := s.d.Groups.NextSeriesSequence(ctx, a.WorkspaceID, source.SeriesID)
	if err != nil {
		return nil, err
	}
	seats, err := s.visibleSeats(ctx, source, n)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(seats))
	if _, ok := n.visible(source.PrimaryInstanceID); ok {
		ids = append(ids, source.PrimaryInstanceID)
	}
	for _, seat := range seats {
		instance, _ := n.visible(seat.InstanceID)
		if seat.InstanceID != source.PrimaryInstanceID && instance.SessionLive() {
			ids = append(ids, seat.InstanceID)
		}
	}
	return s.createManaged(ctx, a, ManagedGroupInput{
		Name: uwg.SeriesName(source.Subject, sequence), Description: source.Description,
		Category: source.Category, InstanceIDs: ids, AdminsOnly: source.Announce,
	}, &seriesSlot{id: source.SeriesID, sequence: sequence})
}

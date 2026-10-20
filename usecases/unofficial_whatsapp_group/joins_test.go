package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const invite = "chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234?mode=r_c"

func (w *world) at(now time.Time) {
	w.svc.d.Clock = func() time.Time { return now }
}

func TestPreviewShowsTheGroupAndWhichNumbersAreAlreadyIn(t *testing.T) {
	w := newWorld()
	w.api.info = &uw.Group{JID: "120363888000000001@g.us", Subject: "Feira de Trocas", ParticipantCount: 744}
	w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: "n2", JID: "120363888000000001@g.us"})

	preview, err := w.svc.PreviewInvite(context.Background(), everyone(), invite)
	if err != nil {
		t.Fatal(err)
	}
	if preview.InviteLink != "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234" || preview.Subject != "Feira de Trocas" || preview.Members != 744 {
		t.Fatalf("preview = %+v", preview)
	}
	if len(preview.AlreadyIn) != 1 || preview.AlreadyIn[0] != "n2" {
		t.Fatalf("already in = %v", preview.AlreadyIn)
	}
}

func TestJoiningRunsTheFirstNumberNowAndSpacesTheOthers(t *testing.T) {
	w := newWorld()

	result, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}, Follow: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.api.joined) != 1 || w.api.joined[0] != "n1" {
		t.Fatalf("joined = %v, want only the first number now", w.api.joined)
	}
	if result.Group == nil || result.Group.Origin != uwg.OriginExternal || !result.Group.Followed() {
		t.Fatalf("group = %+v, want an external followed group", result.Group)
	}
	tasks := w.joins.snapshot()
	if len(tasks) != 2 || tasks[0].Status != uwg.JoinDone || tasks[1].Status != uwg.JoinPending || !tasks[1].DueAt.Equal(testNow.Add(uwg.JoinSpacing)) {
		t.Fatalf("tasks = %+v", tasks)
	}

	w.at(testNow.Add(uwg.JoinSpacing))
	if err := w.svc.RunDueJoins(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(w.api.joined) != 2 || w.api.joined[1] != "n2" || w.joins.snapshot()[1].Status != uwg.JoinDone {
		t.Fatalf("after ten minutes joined = %v tasks = %+v", w.api.joined, w.joins.snapshot())
	}
}

func TestJoiningRefusesNumbersOutsideTheScopeOrOffline(t *testing.T) {
	w := newWorld()
	if _, err := w.svc.JoinGroup(context.Background(), salesOnly(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n2"}}); !errors.Is(err, uwg.ErrNumberOutOfScope) {
		t.Fatalf("join with another department's number = %v", err)
	}
	w.instance("n1").Status = uw.StatusDisconnected
	if _, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1"}}); !errors.Is(err, uw.ErrInstanceNotConnected) {
		t.Fatalf("join with an offline number = %v", err)
	}
	if len(w.api.joined) != 0 {
		t.Fatalf("a refused join reached WhatsApp")
	}
}

func TestARevokedInviteStopsTheWholePlan(t *testing.T) {
	w := newWorld()
	w.api.joinErr = &uw.ProviderError{HTTPStatus: 400, Message: "invalid invite code"}

	if _, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}}); err == nil {
		t.Fatalf("a revoked invite was accepted")
	}
	for _, task := range w.joins.snapshot() {
		if task.Status != uwg.JoinFailed {
			t.Fatalf("task %d = %s, want the plan stopped", task.Sequence, task.Status)
		}
	}
}

func TestATemporaryFailureIsRetriedLater(t *testing.T) {
	w := newWorld()
	if _, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}}); err != nil {
		t.Fatal(err)
	}
	w.api.joinErr = &uw.ProviderError{HTTPStatus: 503, Message: "busy"}
	w.at(testNow.Add(uwg.JoinSpacing))
	_ = w.svc.RunDueJoins(context.Background())

	task := w.joins.snapshot()[1]
	if task.Status != uwg.JoinPending || task.Attempts != 1 || !task.DueAt.After(testNow.Add(uwg.JoinSpacing)) {
		t.Fatalf("task = %+v, want a later retry", task)
	}
}

func TestCreatingAManagedGroupAddsAndPromotesTheOtherNumbers(t *testing.T) {
	w := newWorld()
	category := "Ofertas"

	result, err := w.svc.CreateManagedGroup(context.Background(), everyone(), ManagedGroupInput{
		Name: "Ofertas VIP #1", Description: "Ofertas da semana", Category: category,
		InstanceIDs: []string{"n1", "n2"}, AdminsOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	g := result.Group
	if len(w.api.created) != 1 || w.api.created[0] != "Ofertas VIP #1|5511970001202" {
		t.Fatalf("created = %v, want n1 creating with n2 as participant", w.api.created)
	}
	if len(w.api.promoted) != 1 || w.api.promoted[0][0] != "5511970001202" {
		t.Fatalf("promoted = %v", w.api.promoted)
	}
	if len(w.api.announced) != 1 || !w.api.announced[0] || len(w.api.describing) != 1 {
		t.Fatalf("settings announce=%v description=%v", w.api.announced, w.api.describing)
	}
	if g.Origin != uwg.OriginManaged || g.Category != "Ofertas" || !g.Followed() || g.CreatedByID != "u1" {
		t.Fatalf("group = %+v", g)
	}
}

func TestTheNextGroupInASeriesKeepsTheNameAndNumbers(t *testing.T) {
	w := newWorld()
	created, err := w.svc.CreateManagedGroup(context.Background(), everyone(), ManagedGroupInput{Name: "Ofertas VIP #1", InstanceIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatal(err)
	}
	first := created.Group
	w.api.created = nil
	next, err := w.svc.NextInSeries(context.Background(), everyone(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := next.Group
	if len(w.api.created) != 1 || !strings.HasPrefix(w.api.created[0], "Ofertas VIP #2|") {
		t.Fatalf("created = %v", w.api.created)
	}
	if second.SeriesID != first.ID || second.SeriesSequence != 2 {
		t.Fatalf("series = %s #%d, want the first group as the series", second.SeriesID, second.SeriesSequence)
	}
	if w.groups.get(first.ID).SeriesID != first.ID || w.groups.get(first.ID).SeriesSequence != 1 {
		t.Fatalf("the first group was not placed at the start of its series")
	}
	detail, err := w.svc.GetGroup(context.Background(), everyone(), second.ID)
	if err != nil || len(detail.Series) != 2 || detail.Series[0].GroupID != first.ID || detail.Series[1].Sequence != 2 {
		t.Fatalf("series = %+v, %v, want both groups in order", detail.Series, err)
	}
}

func TestCancellingAJoinPlanStopsWhatIsStillWaiting(t *testing.T) {
	w := newWorld()
	result, _ := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}})

	cancelled, err := w.svc.CancelJoin(context.Background(), everyone(), result.PlanID)
	if err != nil || cancelled != 1 || w.joins.snapshot()[1].Status != uwg.JoinCancelled {
		t.Fatalf("cancel = %d, %v", cancelled, err)
	}
}

func TestARescheduledFirstJoinStillReturnsThePlan(t *testing.T) {
	w := newWorld()
	w.api.joinErr = &uw.ProviderError{HTTPStatus: 503, Message: "busy"}

	result, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}})
	if err != nil || result == nil || result.PlanID == "" {
		t.Fatalf("join = %+v, %v, want the plan back with a pending retry", result, err)
	}
	if first := w.joins.snapshot()[0]; first.Status != uwg.JoinPending || first.Attempts != 1 {
		t.Fatalf("first task = %+v, want a pending retry", first)
	}
}

func TestTheSweepDoesNotRunTheJoinTheRequestIsRunning(t *testing.T) {
	w := newWorld()
	w.api.duringJoin = func() {
		if err := w.svc.RunDueJoins(context.Background()); err != nil {
			t.Error(err)
		}
	}
	if _, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1"}}); err != nil {
		t.Fatal(err)
	}
	if len(w.api.joined) != 1 {
		t.Fatalf("joined = %v, want the first number to join once", w.api.joined)
	}
}

func TestCancellingOnlyStopsJoinsOfTheCallersNumbers(t *testing.T) {
	w := newWorld()
	w.api.joinErr = &uw.ProviderError{HTTPStatus: 503, Message: "busy"}
	result, _ := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1", "n2"}})

	cancelled, err := w.svc.CancelJoin(context.Background(), salesOnly(), result.PlanID)
	if err != nil || cancelled != 1 {
		t.Fatalf("cancel = %d, %v, want only the sales number cancelled", cancelled, err)
	}
	for _, task := range w.joins.snapshot() {
		if task.InstanceID == "n2" && task.Status == uwg.JoinCancelled {
			t.Fatalf("another department's join was cancelled")
		}
	}
	if _, err := w.svc.CancelJoin(context.Background(), salesOnly(), result.PlanID); !errors.Is(err, uwg.ErrJoinNotFound) {
		t.Fatalf("second cancel = %v, want not found", err)
	}
}

func TestAGroupCreatedOnWhatsAppIsReturnedEvenWhenASetupStepFails(t *testing.T) {
	w := newWorld()
	w.api.promoteErr = &uw.ProviderError{HTTPStatus: 503, Message: "busy"}

	result, err := w.svc.CreateManagedGroup(context.Background(), everyone(), ManagedGroupInput{Name: "Ofertas VIP #1", InstanceIDs: []string{"n1", "n2"}})
	if err != nil {
		t.Fatalf("create = %v, want the created group back so nobody creates it twice", err)
	}
	if result.Group == nil || result.Group.Origin != uwg.OriginManaged {
		t.Fatalf("group = %+v", result.Group)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Step != uwg.SetupPromote {
		t.Fatalf("warnings = %+v, want the failed promotion reported", result.Warnings)
	}
}

func TestAChangeCanReachEveryGroupOfTheSeries(t *testing.T) {
	w := newWorld()
	created, err := w.svc.CreateManagedGroup(context.Background(), everyone(), ManagedGroupInput{Name: "Ofertas VIP #1", InstanceIDs: []string{"n1"}})
	if err != nil {
		t.Fatal(err)
	}
	first := created.Group
	w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: first.JID, Subject: first.Subject, WeAreAdmin: true, WeCanSend: true})
	next, err := w.svc.NextInSeries(context.Background(), everyone(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: next.Group.JID, Subject: next.Group.Subject, WeAreAdmin: true, WeCanSend: true})

	name, category := "Clube de Ofertas", "Ofertas"
	if _, err := w.svc.UpdateGroup(context.Background(), everyone(), first.ID, UpdateGroupInput{Subject: &name, Category: &category, ApplyToSeries: true}); err != nil {
		t.Fatal(err)
	}
	renamed := strings.Join(w.seatSync.renamed, ",")
	if !strings.Contains(renamed, "Clube de Ofertas #1") || !strings.Contains(renamed, "Clube de Ofertas #2") {
		t.Fatalf("renamed = %v, want each group of the series renamed with its number", w.seatSync.renamed)
	}
	if w.groups.get(next.Group.ID).Category != "Ofertas" {
		t.Fatalf("the category did not reach the second group of the series")
	}
}

func TestJoiningWithACategoryFilesTheGroupUnderIt(t *testing.T) {
	w := newWorld()

	result, err := w.svc.JoinGroup(context.Background(), everyone(), JoinInput{InviteLink: invite, InstanceIDs: []string{"n1"}, Follow: true, Category: "Clientes VIP"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Group == nil || result.Group.Category != "Clientes VIP" {
		t.Fatalf("group = %+v, want it filed under the chosen category", result.Group)
	}
}

const feiraJID = "120363888000000001@g.us"

func (w *world) feiraWithAdmin(t *testing.T, admin bool) *uwg.Group {
	g := w.groupWithSeats(t, feiraJID, "Feira de Trocas", "n1")
	seat, err := w.seats.FindByJID(context.Background(), "n1", feiraJID)
	if err != nil {
		t.Fatal(err)
	}
	seat.WeAreAdmin = admin
	return g
}

func TestAddingNumbersJoinsThemThroughAFreshLinkFromAnAdminNumber(t *testing.T) {
	w := newWorld()
	g := w.feiraWithAdmin(t, true)

	result, err := w.svc.AddNumbers(context.Background(), everyone(), g.ID, []string{"n2"})
	if err != nil {
		t.Fatal(err)
	}
	tasks := w.joins.snapshot()
	if len(tasks) != 1 || tasks[0].InviteLink != "https://chat.whatsapp.com/abcdefghij" || tasks[0].InstanceID != "n2" {
		t.Fatalf("tasks = %+v", tasks)
	}
	if len(w.api.joined) != 1 || w.api.joined[0] != "n2" || result.PlanID != tasks[0].PlanID {
		t.Fatalf("joined = %v result = %+v", w.api.joined, result)
	}
}

func TestAddingNumbersKeepsTheGroupCategoryAndFollow(t *testing.T) {
	w := newWorld()
	g := w.feiraWithAdmin(t, true)
	stored := w.groups.get(g.ID)
	stored.Category = "Clientes VIP"
	followed := testNow
	stored.FollowedAt = &followed

	if _, err := w.svc.AddNumbers(context.Background(), everyone(), g.ID, []string{"n2"}); err != nil {
		t.Fatal(err)
	}
	task := w.joins.snapshot()[0]
	if task.Category != "Clientes VIP" || !task.Follow {
		t.Fatalf("task = %+v, want the group category and follow carried over", task)
	}
}

func TestAddingNumbersFallsBackToTheLinkAnEarlierJoinUsed(t *testing.T) {
	w := newWorld()
	g := w.feiraWithAdmin(t, false)
	w.joins.tasks = append(w.joins.tasks, uwg.JoinTask{
		ID: "old", PlanID: "p-old", WorkspaceID: ws, InstanceID: "n1", GroupJID: feiraJID,
		InviteLink: "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234", Status: uwg.JoinDone,
	})

	if _, err := w.svc.AddNumbers(context.Background(), everyone(), g.ID, []string{"n2"}); err != nil {
		t.Fatal(err)
	}
	tasks := w.joins.snapshot()
	if len(tasks) != 2 || tasks[1].InviteLink != "https://chat.whatsapp.com/Ky3pQm8Lx2AbCdEfGh1234" {
		t.Fatalf("tasks = %+v", tasks)
	}
}

func TestAddingNumbersRefusesWhenNoLinkCanBeFound(t *testing.T) {
	w := newWorld()
	g := w.feiraWithAdmin(t, false)

	if _, err := w.svc.AddNumbers(context.Background(), everyone(), g.ID, []string{"n2"}); !errors.Is(err, uwg.ErrNoInviteLink) {
		t.Fatalf("err = %v, want ErrNoInviteLink", err)
	}
	if len(w.api.joined) != 0 || len(w.joins.snapshot()) != 0 {
		t.Fatalf("a refused add reached WhatsApp or queued a join")
	}
}

func TestAddingNumbersRefusesANumberAlreadyInTheGroupOrOutsideTheScope(t *testing.T) {
	w := newWorld()
	g := w.feiraWithAdmin(t, true)

	if _, err := w.svc.AddNumbers(context.Background(), everyone(), g.ID, []string{"n1"}); !errors.Is(err, uwg.ErrNumberAlreadyInGroup) {
		t.Fatalf("adding a number already inside = %v", err)
	}
	if _, err := w.svc.AddNumbers(context.Background(), salesOnly(), g.ID, []string{"n2"}); !errors.Is(err, uwg.ErrNumberOutOfScope) {
		t.Fatalf("adding another department's number = %v", err)
	}
	if len(w.api.joined) != 0 {
		t.Fatalf("a refused add reached WhatsApp")
	}
}

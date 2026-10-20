package teamchat_usecase

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
	"vozko/domain/workspace"
)

const (
	testWorkspace = "11111111-1111-4111-8111-111111111111"
	ana           = "aaaaaaaa-0000-4000-8000-000000000001"
	bia           = "aaaaaaaa-0000-4000-8000-000000000002"
	caio          = "aaaaaaaa-0000-4000-8000-000000000003"
	dani          = "aaaaaaaa-0000-4000-8000-000000000004"
	outsider      = "aaaaaaaa-0000-4000-8000-000000000009"
)

var full = []workspace.Action{workspace.ActionCreate}

type harness struct {
	svc       *Service
	chats     *memoryChats
	access    *fakeAccess
	announcer *fakeAnnouncer
	wake      *fakeWake
	clock     *fakeClock
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		chats:     newMemoryChats(),
		access:    &fakeAccess{members: map[string]bool{}, grants: map[string]map[workspace.Action]bool{}},
		announcer: &fakeAnnouncer{},
		wake:      &fakeWake{fires: map[string]time.Time{}},
		clock:     &fakeClock{now: time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)},
	}
	for _, id := range []string{ana, bia, caio} {
		h.access.grant(id, full...)
	}
	h.access.members[dani] = true
	ids := &sequentialIDs{}
	svc, err := New(Deps{
		Chats:     h.chats,
		Access:    h.access,
		Members:   fakeDirectory{access: h.access},
		People:    fakePeople{},
		Announcer: h.announcer,
		Wake:      h.wake,
		Clock:     h.clock,
		NewID:     ids.next,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.svc = svc
	return h
}

func as(id string) shared.Person { return shared.Person{UserID: id} }

func (h *harness) tick(d time.Duration) { h.clock.now = h.clock.now.Add(d) }

func (h *harness) dm(t *testing.T, me, other string) teamchat.ThreadView {
	t.Helper()
	view, err := h.svc.OpenDirect(context.Background(), as(me), testWorkspace, other)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (h *harness) group(t *testing.T) teamchat.ThreadView {
	t.Helper()
	view, err := h.svc.CreateGroup(context.Background(), as(ana), testWorkspace, "Vendas", []string{bia, caio})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (h *harness) say(t *testing.T, who, threadID, body string, mentions ...string) teamchat.AuthoredMessage {
	t.Helper()
	h.tick(time.Second)
	sent, err := h.svc.Send(context.Background(), as(who), testWorkspace, threadID, teamchat.Draft{Body: body, Mentions: mentions, ClientID: body + who})
	if err != nil {
		t.Fatal(err)
	}
	return sent.Message
}

func TestNewRefusesMissingDependencies(t *testing.T) {
	if _, err := New(Deps{}); err == nil {
		t.Fatal("a service without its dependencies must not be built")
	}
}

func TestMembershipOpensTheChatAndOnlyGroupCreationNeedsARole(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if _, err := h.svc.Directory(ctx, as(outsider), testWorkspace); !errors.Is(err, teamchat.ErrForbidden) {
		t.Fatalf("someone outside the workspace: %v", err)
	}
	if _, err := h.svc.Summary(ctx, shared.Person{UserID: outsider, SystemAdmin: true}, testWorkspace); !errors.Is(err, teamchat.ErrForbidden) {
		t.Fatalf("a system admin outside the workspace: %v", err)
	}
	if _, err := h.svc.OpenDirect(ctx, as(dani), testWorkspace, ana); err != nil {
		t.Fatalf("a member without any role talks to colleagues: %v", err)
	}
	if _, err := h.svc.CreateGroup(ctx, as(dani), testWorkspace, "x", []string{ana}); !errors.Is(err, teamchat.ErrForbidden) {
		t.Fatalf("group without team_chat:create: %v", err)
	}
	h.access.grant(dani, workspace.ActionCreate)
	if _, err := h.svc.CreateGroup(ctx, as(dani), testWorkspace, "x", []string{ana}); err != nil {
		t.Fatalf("group with team_chat:create: %v", err)
	}
}

func TestTheDirectoryListsEveryWorkspaceMember(t *testing.T) {
	h := newHarness(t)
	people, err := h.svc.Directory(context.Background(), as(ana), testWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, p := range people {
		got = append(got, p.Name)
		if p.Picture == "" {
			t.Fatalf("pictures come from the user record: %+v", p)
		}
	}
	if !reflect.DeepEqual(got, []string{"Ana", "Bia", "Caio", "Dani"}) {
		t.Fatalf("directory = %v, want every member sorted by name", got)
	}
}

func TestOpenDirectReturnsTheSameThreadAndRefusesPeopleWithoutTheChat(t *testing.T) {
	h := newHarness(t)
	first := h.dm(t, ana, bia)
	again := h.dm(t, bia, ana)
	if first.Thread.ID != again.Thread.ID {
		t.Fatalf("two direct threads for one pair: %s and %s", first.Thread.ID, again.Thread.ID)
	}
	if len(first.Members) != 2 || first.Members[0].Name == "" {
		t.Fatalf("members = %+v", first.Members)
	}
	if _, err := h.svc.OpenDirect(context.Background(), as(ana), testWorkspace, outsider); !errors.Is(err, teamchat.ErrMemberUnavailable) {
		t.Fatalf("someone outside the workspace: %v", err)
	}
	if _, err := h.svc.OpenDirect(context.Background(), as(ana), testWorkspace, ana); !errors.Is(err, teamchat.ErrSamePerson) {
		t.Fatalf("yourself: %v", err)
	}
}

func TestCreateGroupWritesItsNoticeAndTellsTheOthers(t *testing.T) {
	h := newHarness(t)
	view := h.group(t)
	if view.Thread.LastSeq != 1 || view.LastMessage == nil || view.LastMessage.Event != teamchat.EventCreated {
		t.Fatalf("view = %+v", view)
	}
	if view.Me.Role != teamchat.RoleAdmin || view.Unread != 0 {
		t.Fatalf("the creator is admin and has read the notice: %+v", view.Me)
	}
	threads := h.announcer.of("thread")
	if len(threads) != 1 || !reflect.DeepEqual(threads[0].recipients, []string{bia, caio}) {
		t.Fatalf("thread announcements = %+v", threads)
	}
	if _, err := h.svc.CreateGroup(context.Background(), as(ana), testWorkspace, "x", []string{bia, outsider}); !errors.Is(err, teamchat.ErrMemberUnavailable) {
		t.Fatalf("someone outside the workspace: %v", err)
	}
}

func TestSendKeepsMentionsOfMembersAndReachesEveryMember(t *testing.T) {
	h := newHarness(t)
	view := h.group(t)
	h.announcer.reset()
	msg := h.say(t, bia, view.Thread.ID, "@Ana @Dani olha", ana, dani)
	if !reflect.DeepEqual(msg.Mentions, []string{ana}) {
		t.Fatalf("mentions = %v, want only members of the thread", msg.Mentions)
	}
	if msg.AuthorName != "Bia" || msg.Seq != 2 {
		t.Fatalf("msg = %+v", msg)
	}
	sent := h.announcer.of("message")
	if len(sent) != 1 || !reflect.DeepEqual(sent[0].recipients, []string{ana, bia, caio}) {
		t.Fatalf("message announcements = %+v", sent)
	}
}

func TestARepeatedClientIDNeverSendsTwice(t *testing.T) {
	h := newHarness(t)
	view := h.dm(t, ana, bia)
	draft := teamchat.Draft{Body: "oi", ClientID: "c-1"}
	first, err := h.svc.Send(context.Background(), as(ana), testWorkspace, view.Thread.ID, draft)
	if err != nil || !first.Created {
		t.Fatalf("first = %+v, %v", first, err)
	}
	again, err := h.svc.Send(context.Background(), as(ana), testWorkspace, view.Thread.ID, draft)
	if err != nil || again.Created || again.Message.ID != first.Message.ID {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if len(h.announcer.of("message")) != 1 {
		t.Fatalf("a retry announced again: %+v", h.announcer.of("message"))
	}
}

func TestSendRefusals(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dm := h.dm(t, ana, bia)
	other := h.dm(t, ana, caio)
	quoted := h.say(t, ana, other.Thread.ID, "noutra conversa")

	if _, err := h.svc.Send(ctx, as(caio), testWorkspace, dm.Thread.ID, teamchat.Draft{Body: "x"}); !errors.Is(err, teamchat.ErrNotFound) {
		t.Fatalf("a non-member sees not found: %v", err)
	}
	if _, err := h.svc.Send(ctx, as(ana), testWorkspace, dm.Thread.ID, teamchat.Draft{Body: "x", ReplyToID: quoted.ID}); !errors.Is(err, teamchat.ErrReplyOutsideThread) {
		t.Fatalf("reply to another thread: %v", err)
	}
	h.access.leave(bia)
	if _, err := h.svc.Send(ctx, as(ana), testWorkspace, dm.Thread.ID, teamchat.Draft{Body: "x"}); !errors.Is(err, teamchat.ErrRecipientUnavailable) {
		t.Fatalf("bia lost the chat: %v", err)
	}
}

func TestMessagesPageBackAndCatchUp(t *testing.T) {
	h := newHarness(t)
	view := h.dm(t, ana, bia)
	for _, body := range []string{"1", "2", "3", "4", "5"} {
		h.say(t, ana, view.Thread.ID, body)
	}
	ctx := context.Background()
	latest, err := h.svc.Messages(ctx, as(bia), testWorkspace, teamchat.MessageQuery{ThreadID: view.Thread.ID, Limit: 2})
	if err != nil || !latest.HasMore || bodies(latest) != "45" {
		t.Fatalf("latest = %s has_more=%v err=%v", bodies(latest), latest.HasMore, err)
	}
	older, _ := h.svc.Messages(ctx, as(bia), testWorkspace, teamchat.MessageQuery{ThreadID: view.Thread.ID, BeforeSeq: 2, Limit: 2})
	if older.HasMore || bodies(older) != "1" {
		t.Fatalf("older = %s has_more=%v", bodies(older), older.HasMore)
	}
	newer, _ := h.svc.Messages(ctx, as(bia), testWorkspace, teamchat.MessageQuery{ThreadID: view.Thread.ID, AfterSeq: 2, Limit: 2})
	if !newer.HasMore || bodies(newer) != "34" {
		t.Fatalf("newer = %s has_more=%v", bodies(newer), newer.HasMore)
	}
	if _, err := h.svc.Messages(ctx, as(caio), testWorkspace, teamchat.MessageQuery{ThreadID: view.Thread.ID}); !errors.Is(err, teamchat.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
	if _, err := h.svc.Messages(ctx, as(bia), testWorkspace, teamchat.MessageQuery{ThreadID: view.Thread.ID, AfterSeq: 1, BeforeSeq: 3}); !errors.Is(err, teamchat.ErrPageDirection) {
		t.Fatalf("both directions: %v", err)
	}
}

func bodies(page teamchat.MessagePage) string {
	out := ""
	for _, m := range page.Messages {
		out += m.Body
	}
	return out
}

func TestThreadsShowUnreadMentionsAndPage(t *testing.T) {
	h := newHarness(t)
	group := h.group(t)
	dm := h.dm(t, ana, bia)
	h.say(t, ana, dm.Thread.ID, "oi bia")
	h.say(t, caio, group.Thread.ID, "@Bia veja", bia)
	ctx := context.Background()

	page, err := h.svc.Threads(ctx, as(bia), testWorkspace, "", 1)
	if err != nil || len(page.Threads) != 1 || page.Next == "" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	top := page.Threads[0]
	if top.Thread.ID != group.Thread.ID || top.Unread != 2 || top.UnreadMentions != 1 || top.LastMessage.AuthorName != "Caio" {
		t.Fatalf("top = %+v", top)
	}
	rest, err := h.svc.Threads(ctx, as(bia), testWorkspace, page.Next, 5)
	if err != nil || len(rest.Threads) != 1 || rest.Threads[0].Thread.ID != dm.Thread.ID || rest.Next != "" {
		t.Fatalf("rest = %+v, %v", rest, err)
	}
	if _, err := h.svc.Threads(ctx, as(bia), testWorkspace, "not-a-cursor", 5); !errors.Is(err, shared.ErrKeysetInvalid) {
		t.Fatalf("bad cursor: %v", err)
	}
	totals, _ := h.svc.Summary(ctx, as(bia), testWorkspace)
	if totals.UnreadThreads != 2 || totals.UnreadMessages != 3 {
		t.Fatalf("totals = %+v", totals)
	}
}

func TestMarkReadWritesOnlyWhenItMovesAndTellsTheMembers(t *testing.T) {
	h := newHarness(t)
	dm := h.dm(t, ana, bia)
	msg := h.say(t, ana, dm.Thread.ID, "oi")
	ctx := context.Background()
	read, err := h.svc.MarkRead(ctx, as(bia), testWorkspace, dm.Thread.ID, msg.Seq)
	if err != nil || read != msg.Seq {
		t.Fatalf("read = %d, %v", read, err)
	}
	marks := h.announcer.of("read")
	if len(marks) != 1 || !reflect.DeepEqual(marks[0].recipients, []string{ana, bia}) {
		t.Fatalf("read announcements = %+v", marks)
	}
	writes := h.chats.markReads
	if _, err := h.svc.MarkRead(ctx, as(bia), testWorkspace, dm.Thread.ID, msg.Seq); err != nil {
		t.Fatal(err)
	}
	if h.chats.markReads != writes || len(h.announcer.of("read")) != 1 {
		t.Fatal("reading the same message again must not write or announce")
	}
}

func TestOnlyTheAuthorEditsOrDeletes(t *testing.T) {
	h := newHarness(t)
	dm := h.dm(t, ana, bia)
	msg := h.say(t, ana, dm.Thread.ID, "oi")
	ctx := context.Background()
	if _, err := h.svc.Edit(ctx, as(bia), testWorkspace, msg.ID, "x"); !errors.Is(err, teamchat.ErrNotAuthor) {
		t.Fatalf("bia edits: %v", err)
	}
	if _, err := h.svc.Edit(ctx, as(caio), testWorkspace, msg.ID, "x"); !errors.Is(err, teamchat.ErrMessageNotFound) {
		t.Fatalf("a non-member edits: %v", err)
	}
	edited, err := h.svc.Edit(ctx, as(ana), testWorkspace, msg.ID, "olá")
	if err != nil || edited.Body != "olá" || edited.EditedAt == nil {
		t.Fatalf("edited = %+v, %v", edited, err)
	}
	deleted, err := h.svc.Delete(ctx, as(ana), testWorkspace, msg.ID)
	if err != nil || deleted.Body != "" || deleted.DeletedAt == nil {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	if len(h.announcer.of("message_updated")) != 2 {
		t.Fatalf("changes announced = %+v", h.announcer.of("message_updated"))
	}
}

func TestGroupAdministration(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view := h.group(t)
	id := view.Thread.ID
	if _, err := h.svc.Rename(ctx, as(bia), testWorkspace, id, "x"); !errors.Is(err, teamchat.ErrNotAdmin) {
		t.Fatalf("a member renames: %v", err)
	}
	renamed, err := h.svc.Rename(ctx, as(ana), testWorkspace, id, "Vendas Sul")
	if err != nil || renamed.Thread.Title != "Vendas Sul" || renamed.LastMessage.Event != teamchat.EventRenamed {
		t.Fatalf("renamed = %+v, %v", renamed, err)
	}
	if err := h.svc.RemoveMember(ctx, as(bia), testWorkspace, id, caio); !errors.Is(err, teamchat.ErrNotAdmin) {
		t.Fatalf("a member removes: %v", err)
	}
	h.announcer.reset()
	if err := h.svc.RemoveMember(ctx, as(ana), testWorkspace, id, caio); err != nil {
		t.Fatal(err)
	}
	removed := h.announcer.of("thread_removed")
	if len(removed) != 1 || !reflect.DeepEqual(removed[0].recipients, []string{caio}) {
		t.Fatalf("removed = %+v", removed)
	}
	if _, err := h.svc.Thread(ctx, as(caio), testWorkspace, id); !errors.Is(err, teamchat.ErrNotFound) {
		t.Fatalf("caio still reads the group: %v", err)
	}
	if _, err := h.svc.AddMembers(ctx, as(ana), testWorkspace, id, []string{outsider}); !errors.Is(err, teamchat.ErrMemberUnavailable) {
		t.Fatalf("someone outside the workspace: %v", err)
	}
	added, err := h.svc.AddMembers(ctx, as(ana), testWorkspace, id, []string{caio})
	if err != nil || len(added.Members) != 3 {
		t.Fatalf("added = %+v, %v", added, err)
	}
}

func TestTheLastAdminLeavingHandsTheGroupOn(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view := h.group(t)
	h.announcer.reset()
	if err := h.svc.Leave(ctx, as(ana), testWorkspace, view.Thread.ID); err != nil {
		t.Fatal(err)
	}
	after, err := h.svc.Thread(ctx, as(bia), testWorkspace, view.Thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	admins := 0
	for _, p := range after.Members {
		if p.Role == teamchat.RoleAdmin {
			admins++
		}
	}
	if admins != 1 || len(after.Members) != 2 || after.LastMessage.Event != teamchat.EventMemberLeft {
		t.Fatalf("after = %+v", after)
	}
	if got := h.announcer.of("thread_removed"); len(got) != 1 || got[0].recipients[0] != ana {
		t.Fatalf("thread_removed = %+v", got)
	}
	dm := h.dm(t, bia, caio)
	if err := h.svc.Leave(ctx, as(bia), testWorkspace, dm.Thread.ID); !errors.Is(err, teamchat.ErrDirectThread) {
		t.Fatalf("leaving a direct thread: %v", err)
	}
}

func TestMutedThreadsLeaveTheBadgeUnlessTheyMentionYou(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	view := h.group(t)
	if err := h.svc.Mute(ctx, as(bia), testWorkspace, view.Thread.ID, true); err != nil {
		t.Fatal(err)
	}
	h.say(t, caio, view.Thread.ID, "geral")
	totals, _ := h.svc.Summary(ctx, as(bia), testWorkspace)
	if totals.UnreadThreads != 0 {
		t.Fatalf("muted totals = %+v", totals)
	}
	h.say(t, caio, view.Thread.ID, "@Bia", bia)
	totals, _ = h.svc.Summary(ctx, as(bia), testWorkspace)
	if totals.UnreadThreads != 1 {
		t.Fatalf("a mention must reach the badge, totals = %+v", totals)
	}
	thread, _ := h.svc.Thread(ctx, as(bia), testWorkspace, view.Thread.ID)
	if !thread.Me.Muted {
		t.Fatal("the view must say the thread is muted")
	}
}

func TestTypingReachesOnlyTheOthers(t *testing.T) {
	h := newHarness(t)
	view := h.group(t)
	if err := h.svc.Typing(context.Background(), as(bia), testWorkspace, view.Thread.ID); err != nil {
		t.Fatal(err)
	}
	typing := h.announcer.of("typing")
	if len(typing) != 1 || !reflect.DeepEqual(typing[0].recipients, []string{ana, caio}) {
		t.Fatalf("typing = %+v", typing)
	}
}

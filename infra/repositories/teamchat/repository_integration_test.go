package teamchat_repository

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

var start = time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)

func chatRepo(t *testing.T, prefix string) *Repository {
	t.Helper()
	return New(repotest.IsolatedDB(t, prefix,
		&schema.TeamChatThread{}, &schema.TeamChatMember{}, &schema.TeamChatMessage{}, &schema.TeamChatScheduledMessage{}))
}

type people struct{ ws, ana, bia, caio string }

func newPeople() people {
	return people{ws: uuid.NewString(), ana: uuid.NewString(), bia: uuid.NewString(), caio: uuid.NewString()}
}

func direct(t *testing.T, repo *Repository, p people, a, b string) teamchat.Thread {
	t.Helper()
	thread, members, err := teamchat.NewDirect(uuid.NewString(), p.ws, a, b, start)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := repo.CreateThread(context.Background(), thread, members, nil)
	if err != nil || !created {
		t.Fatalf("create direct: created=%v err=%v", created, err)
	}
	return stored
}

func group(t *testing.T, repo *Repository, p people) teamchat.Thread {
	t.Helper()
	thread, members, err := teamchat.NewGroup(uuid.NewString(), p.ws, p.ana, "Vendas", []string{p.bia, p.caio}, start)
	if err != nil {
		t.Fatal(err)
	}
	notice := teamchat.NewSystem(uuid.NewString(), thread, p.ana, teamchat.EventCreated, nil, "", start)
	stored, _, err := repo.CreateThread(context.Background(), thread, members, &notice)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func send(t *testing.T, repo *Repository, thread teamchat.Thread, author, body string, at time.Time, mentions ...string) teamchat.Message {
	t.Helper()
	msg, err := teamchat.NewText(uuid.NewString(), thread, author, teamchat.Draft{Body: body, Mentions: mentions, ClientID: uuid.NewString()}, at)
	if err != nil {
		t.Fatal(err)
	}
	stored, created, err := repo.Append(context.Background(), msg)
	if err != nil || !created {
		t.Fatalf("append: created=%v err=%v", created, err)
	}
	return stored
}

func TestADirectThreadExistsOncePerPairAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_direct")
	p := newPeople()
	first := direct(t, repo, p, p.ana, p.bia)

	again, members, _ := teamchat.NewDirect(uuid.NewString(), p.ws, p.bia, p.ana, start.Add(time.Hour))
	stored, created, err := repo.CreateThread(ctx, again, members, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created || stored.ID != first.ID {
		t.Fatalf("second create returned %+v created=%v, want the first thread", stored, created)
	}
	found, err := repo.FindDirect(ctx, p.ws, teamchat.DirectKey(p.ana, p.bia))
	if err != nil || found.ID != first.ID {
		t.Fatalf("find direct = %+v, %v", found, err)
	}
	if _, err := repo.FindThread(ctx, uuid.NewString(), first.ID); !errors.Is(err, teamchat.ErrNotFound) {
		t.Fatalf("another workspace: %v", err)
	}
}

func TestAppendNumbersMessagesAndIsIdempotentPerClientIDAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_append")
	p := newPeople()
	thread := direct(t, repo, p, p.ana, p.bia)

	first := send(t, repo, thread, p.ana, "oi", start.Add(time.Minute))
	if first.Seq != 1 {
		t.Fatalf("first seq = %d", first.Seq)
	}
	draft := teamchat.Draft{Body: "tudo bem?", ClientID: "retry-1"}
	msg, _ := teamchat.NewText(uuid.NewString(), thread, p.bia, draft, start.Add(2*time.Minute))
	stored, created, err := repo.Append(ctx, msg)
	if err != nil || !created || stored.Seq != 2 {
		t.Fatalf("second = %+v created=%v err=%v", stored, created, err)
	}
	retry, _ := teamchat.NewText(uuid.NewString(), thread, p.bia, draft, start.Add(3*time.Minute))
	repeated, created, err := repo.Append(ctx, retry)
	if err != nil || created || repeated.ID != stored.ID || repeated.Seq != 2 {
		t.Fatalf("retry = %+v created=%v err=%v, want the stored message", repeated, created, err)
	}

	after, err := repo.FindThread(ctx, p.ws, thread.ID)
	if err != nil || after.LastSeq != 2 || !after.LastMessageAt.Equal(start.Add(2*time.Minute)) {
		t.Fatalf("thread = %+v, %v", after, err)
	}
	roster, _ := repo.Roster(ctx, thread.ID)
	bia, _ := roster.Active(p.bia)
	ana, _ := roster.Active(p.ana)
	if bia.LastReadSeq != 2 || ana.LastReadSeq != 1 {
		t.Fatalf("authors read their own messages: ana=%d bia=%d", ana.LastReadSeq, bia.LastReadSeq)
	}
}

func TestConcurrentAppendsNeverShareASequenceAgainstPostgres(t *testing.T) {
	repo := chatRepo(t, "team_chat_concurrent")
	p := newPeople()
	thread := group(t, repo, p)
	const writers = 20
	var wg sync.WaitGroup
	seqs := make(chan int64, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			msg, _ := teamchat.NewText(uuid.NewString(), thread, p.bia, teamchat.Draft{Body: "x", ClientID: uuid.NewString()}, start.Add(time.Minute))
			stored, _, err := repo.Append(context.Background(), msg)
			if err != nil {
				t.Error(err)
				return
			}
			seqs <- stored.Seq
		}()
	}
	wg.Wait()
	close(seqs)
	got := []int64{}
	for s := range seqs {
		got = append(got, s)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	for i, s := range got {
		if s != int64(i+2) {
			t.Fatalf("sequences = %v, want 2..%d without gaps or repeats", got, writers+1)
		}
	}
}

func TestThreadsArePagedByLatestActivityAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_threads")
	p := newPeople()
	older := direct(t, repo, p, p.ana, p.bia)
	newer := group(t, repo, p)
	send(t, repo, older, p.bia, "antiga", start.Add(time.Minute))
	send(t, repo, newer, p.caio, "nova", start.Add(2*time.Minute))
	direct(t, repo, p, p.bia, p.caio)

	page, err := repo.ListThreads(ctx, teamchat.ThreadQuery{WorkspaceID: p.ws, UserID: p.ana, Limit: 1})
	if err != nil || len(page) != 1 || page[0].Thread.ID != newer.ID || page[0].Me.Role != teamchat.RoleAdmin {
		t.Fatalf("first page = %+v, %v", page, err)
	}
	cursor := shared.Keyset{At: page[0].Thread.LastMessageAt, ID: page[0].Thread.ID}
	next, err := repo.ListThreads(ctx, teamchat.ThreadQuery{WorkspaceID: p.ws, UserID: p.ana, After: &cursor, Limit: 5})
	if err != nil || len(next) != 1 || next[0].Thread.ID != older.ID {
		t.Fatalf("second page = %+v, %v", next, err)
	}
}

func TestUnreadCountsMentionsAndMutesAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_unread")
	p := newPeople()
	dm := direct(t, repo, p, p.ana, p.bia)
	team := group(t, repo, p)
	send(t, repo, dm, p.bia, "um", start.Add(time.Minute))
	send(t, repo, dm, p.bia, "dois", start.Add(2*time.Minute))
	send(t, repo, team, p.caio, "geral", start.Add(3*time.Minute))
	send(t, repo, team, p.caio, "@ana olha isso", start.Add(4*time.Minute), p.ana)

	totals, err := repo.UnreadTotals(ctx, p.ws, p.ana)
	if err != nil || totals.UnreadThreads != 2 || totals.UnreadMessages != 4 {
		t.Fatalf("totals = %+v, %v", totals, err)
	}
	mentions, err := repo.UnreadMentions(ctx, p.ana, []string{dm.ID, team.ID})
	if err != nil || mentions[team.ID] != 1 || mentions[dm.ID] != 0 {
		t.Fatalf("mentions = %v, %v", mentions, err)
	}

	if err := repo.SetMuted(ctx, dm.ID, p.ana, true); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMuted(ctx, team.ID, p.ana, true); err != nil {
		t.Fatal(err)
	}
	totals, _ = repo.UnreadTotals(ctx, p.ws, p.ana)
	if totals.UnreadThreads != 1 || totals.UnreadMessages != 2 {
		t.Fatalf("muted totals = %+v, want only the group that mentions ana", totals)
	}

	read, err := repo.MarkRead(ctx, team.ID, p.ana, 99)
	if err != nil || read != 3 {
		t.Fatalf("mark read clamps to the last message: %d, %v", read, err)
	}
	read, err = repo.MarkRead(ctx, team.ID, p.ana, 1)
	if err != nil || read != 3 {
		t.Fatalf("mark read never moves back: %d, %v", read, err)
	}
	mentions, _ = repo.UnreadMentions(ctx, p.ana, []string{team.ID})
	if mentions[team.ID] != 0 {
		t.Fatalf("read mentions still counted: %v", mentions)
	}
}

func TestMessagesPageBothWaysOldestFirstAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_messages")
	p := newPeople()
	thread := direct(t, repo, p, p.ana, p.bia)
	for i := 0; i < 5; i++ {
		send(t, repo, thread, p.ana, "m", start.Add(time.Duration(i+1)*time.Minute))
	}
	seqs := func(ms []teamchat.Message) []int64 {
		out := []int64{}
		for _, m := range ms {
			out = append(out, m.Seq)
		}
		return out
	}
	latest, _ := repo.ListMessages(ctx, teamchat.MessageQuery{ThreadID: thread.ID, Limit: 2})
	before, _ := repo.ListMessages(ctx, teamchat.MessageQuery{ThreadID: thread.ID, BeforeSeq: 4, Limit: 2})
	after, _ := repo.ListMessages(ctx, teamchat.MessageQuery{ThreadID: thread.ID, AfterSeq: 1, Limit: 2})
	for name, c := range map[string]struct{ got, want []int64 }{
		"latest": {seqs(latest), []int64{4, 5}},
		"before": {seqs(before), []int64{2, 3}},
		"after":  {seqs(after), []int64{2, 3}},
	} {
		if len(c.got) != len(c.want) || (len(c.got) == 2 && (c.got[0] != c.want[0] || c.got[1] != c.want[1])) {
			t.Errorf("%s = %v, want %v", name, c.got, c.want)
		}
	}
	last, err := repo.LastMessages(ctx, []teamchat.Thread{{ID: thread.ID, LastSeq: 5}, {ID: uuid.NewString(), LastSeq: 0}})
	if err != nil || len(last) != 1 || last[thread.ID].Seq != 5 {
		t.Fatalf("last = %+v, %v", last, err)
	}
}

func TestEditAndDeleteAreStoredAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_edit")
	p := newPeople()
	thread := direct(t, repo, p, p.ana, p.bia)
	msg := send(t, repo, thread, p.ana, "oi", start.Add(time.Minute), p.bia)
	edited, _ := msg.EditBy(p.ana, "olá", start.Add(2*time.Minute))
	if err := repo.SaveMessage(ctx, edited); err != nil {
		t.Fatal(err)
	}
	deleted, _ := edited.DeleteBy(p.ana, start.Add(3*time.Minute))
	if err := repo.SaveMessage(ctx, deleted); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindMessage(ctx, p.ws, msg.ID)
	if err != nil || got.Body != "" || len(got.Mentions) != 0 || got.EditedAt == nil || got.DeletedAt == nil {
		t.Fatalf("got = %+v, %v", got, err)
	}
	if _, err := repo.FindMessage(ctx, uuid.NewString(), msg.ID); !errors.Is(err, teamchat.ErrMessageNotFound) {
		t.Fatalf("another workspace: %v", err)
	}
}

func TestLeavingAndRenamingWriteTheirNoticeAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_members")
	p := newPeople()
	thread := group(t, repo, p)
	roster, _ := repo.Roster(ctx, thread.ID)
	changes, err := roster.Leaving(p.ana, start.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	notice := teamchat.NewSystem(uuid.NewString(), thread, p.ana, teamchat.EventMemberLeft, []string{p.ana}, "", start.Add(time.Minute))
	stored, err := repo.ChangeMembers(ctx, thread, changes, notice)
	if err != nil || stored.Seq != 2 {
		t.Fatalf("notice = %+v, %v", stored, err)
	}
	roster, _ = repo.Roster(ctx, thread.ID)
	if _, ok := roster.Active(p.ana); ok {
		t.Fatal("ana left and is still active")
	}
	admins := 0
	for _, id := range roster.ActiveUserIDs() {
		if m, _ := roster.Active(id); m.Role == teamchat.RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("one remaining member inherits the group, got %d admins in %+v", admins, roster)
	}
	rows, _ := repo.ListThreads(ctx, teamchat.ThreadQuery{WorkspaceID: p.ws, UserID: p.ana, Limit: 10})
	if len(rows) != 0 {
		t.Fatalf("a person who left still lists the thread: %+v", rows)
	}

	renamed, _ := thread.Renamed("Vendas Sul")
	rename := teamchat.NewSystem(uuid.NewString(), renamed, p.bia, teamchat.EventRenamed, nil, renamed.Title, start.Add(2*time.Minute))
	if _, err := repo.Rename(ctx, renamed, rename); err != nil {
		t.Fatal(err)
	}
	after, _ := repo.FindThread(ctx, p.ws, thread.ID)
	if after.Title != "Vendas Sul" || after.LastSeq != 3 {
		t.Fatalf("after rename = %+v", after)
	}

	rejoin, err := roster.Joining(after, []string{p.ana}, start.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	added := teamchat.NewSystem(uuid.NewString(), after, p.bia, teamchat.EventMembersAdded, []string{p.ana}, "", start.Add(3*time.Minute))
	if _, err := repo.ChangeMembers(ctx, after, rejoin, added); err != nil {
		t.Fatal(err)
	}
	roster, _ = repo.Roster(ctx, thread.ID)
	back, ok := roster.Active(p.ana)
	if !ok || back.Role != teamchat.RoleMember || back.LastReadSeq != 3 {
		t.Fatalf("rejoined = %+v ok=%v", back, ok)
	}
}

func TestScheduledMessagesMoveOnlyFromPendingAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	repo := chatRepo(t, "team_chat_scheduled")
	p := newPeople()
	thread := direct(t, repo, p, p.ana, p.bia)
	soon, _ := teamchat.NewScheduled(uuid.NewString(), thread, p.ana, teamchat.Draft{Body: "lembrete", Mentions: []string{p.bia}}, start.Add(time.Hour), start)
	later, _ := teamchat.NewScheduled(uuid.NewString(), thread, p.ana, teamchat.Draft{Body: "depois"}, start.Add(3*time.Hour), start)
	for _, s := range []teamchat.Scheduled{later, soon} {
		if err := repo.CreateScheduled(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := repo.ListScheduled(ctx, thread.ID, p.ana)
	if err != nil || len(listed) != 2 || listed[0].ID != soon.ID || len(listed[0].Mentions) != 1 {
		t.Fatalf("listed = %+v, %v", listed, err)
	}
	due, err := repo.DueScheduled(ctx, start.Add(2*time.Hour), 10)
	if err != nil || len(due) != 1 || due[0].ID != soon.ID {
		t.Fatalf("due = %+v, %v", due, err)
	}
	sent, _ := soon.Sent(uuid.NewString(), start.Add(time.Hour))
	if err := repo.SaveScheduled(ctx, sent); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveScheduled(ctx, sent); !errors.Is(err, teamchat.ErrScheduledFinal) {
		t.Fatalf("second save of a final message: %v", err)
	}
	stored, err := repo.FindScheduled(ctx, soon.ID)
	if err != nil || stored.Status != teamchat.ScheduledSent || stored.SentMessageID != sent.SentMessageID {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if _, err := repo.FindScheduled(ctx, uuid.NewString()); !errors.Is(err, teamchat.ErrScheduledNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

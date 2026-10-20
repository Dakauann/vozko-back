package teamchat_usecase

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"vozko/domain/shared"
	"vozko/domain/teamchat"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

type memoryChats struct {
	mu        sync.Mutex
	threads   map[string]teamchat.Thread
	members   map[string]map[string]teamchat.Member
	messages  []teamchat.Message
	scheduled map[string]teamchat.Scheduled
	markReads int
}

func newMemoryChats() *memoryChats {
	return &memoryChats{
		threads:   map[string]teamchat.Thread{},
		members:   map[string]map[string]teamchat.Member{},
		scheduled: map[string]teamchat.Scheduled{},
	}
}

func (m *memoryChats) CreateThread(_ context.Context, t teamchat.Thread, members []teamchat.Member, notice *teamchat.Message) (teamchat.Thread, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t.DirectKey != "" {
		for _, existing := range m.threads {
			if existing.WorkspaceID == t.WorkspaceID && existing.DirectKey == t.DirectKey {
				return existing, false, nil
			}
		}
	}
	m.threads[t.ID] = t
	m.upsert(members)
	if notice != nil {
		if _, _, err := m.appendLocked(*notice); err != nil {
			return teamchat.Thread{}, false, err
		}
	}
	return m.threads[t.ID], true, nil
}

func (m *memoryChats) upsert(members []teamchat.Member) {
	for _, member := range members {
		if m.members[member.ThreadID] == nil {
			m.members[member.ThreadID] = map[string]teamchat.Member{}
		}
		m.members[member.ThreadID][member.UserID] = member
	}
}

func (m *memoryChats) FindThread(_ context.Context, workspaceID, id string) (teamchat.Thread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.threads[id]
	if !ok || t.WorkspaceID != workspaceID {
		return teamchat.Thread{}, teamchat.ErrNotFound
	}
	return t, nil
}

func (m *memoryChats) FindDirect(_ context.Context, workspaceID, key string) (teamchat.Thread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.threads {
		if t.WorkspaceID == workspaceID && t.DirectKey == key {
			return t, nil
		}
	}
	return teamchat.Thread{}, teamchat.ErrNotFound
}

func (m *memoryChats) Roster(ctx context.Context, threadID string) (teamchat.Roster, error) {
	rosters, _ := m.Rosters(ctx, []string{threadID})
	return rosters[threadID], nil
}

func (m *memoryChats) Rosters(_ context.Context, ids []string) (map[string]teamchat.Roster, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]teamchat.Roster{}
	for _, id := range ids {
		roster := teamchat.Roster{}
		for _, member := range m.members[id] {
			roster = append(roster, member)
		}
		sort.Slice(roster, func(i, j int) bool { return roster[i].UserID < roster[j].UserID })
		out[id] = roster
	}
	return out, nil
}

func (m *memoryChats) ListThreads(_ context.Context, q teamchat.ThreadQuery) ([]teamchat.ThreadRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := []teamchat.ThreadRow{}
	for id, members := range m.members {
		me, ok := members[q.UserID]
		t := m.threads[id]
		if !ok || !me.IsActive() || t.WorkspaceID != q.WorkspaceID {
			continue
		}
		if q.After != nil && !before(t, *q.After) {
			continue
		}
		rows = append(rows, teamchat.ThreadRow{Thread: t, Me: me})
	}
	sort.Slice(rows, func(i, j int) bool {
		return before(rows[j].Thread, shared.Keyset{At: rows[i].Thread.LastMessageAt, ID: rows[i].Thread.ID})
	})
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

func before(t teamchat.Thread, k shared.Keyset) bool {
	if t.LastMessageAt.Equal(k.At) {
		return t.ID < k.ID
	}
	return t.LastMessageAt.Before(k.At)
}

func (m *memoryChats) ChangeMembers(_ context.Context, _ teamchat.Thread, members []teamchat.Member, notice teamchat.Message) (teamchat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsert(members)
	stored, _, err := m.appendLocked(notice)
	return stored, err
}

func (m *memoryChats) Rename(_ context.Context, t teamchat.Thread, notice teamchat.Message) (teamchat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.threads[t.ID]
	current.Title = t.Title
	m.threads[t.ID] = current
	stored, _, err := m.appendLocked(notice)
	return stored, err
}

func (m *memoryChats) MarkRead(_ context.Context, threadID, userID string, seq int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.markReads++
	member := m.members[threadID][userID]
	member.LastReadSeq = teamchat.ReadUpTo(member.LastReadSeq, seq, m.threads[threadID].LastSeq)
	m.members[threadID][userID] = member
	return member.LastReadSeq, nil
}

func (m *memoryChats) SetMuted(_ context.Context, threadID, userID string, muted bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	member := m.members[threadID][userID]
	member.Muted = muted
	m.members[threadID][userID] = member
	return nil
}

func (m *memoryChats) UnreadTotals(_ context.Context, workspaceID, userID string) (teamchat.Totals, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	totals := teamchat.Totals{}
	for id, members := range m.members {
		me, ok := members[userID]
		t := m.threads[id]
		if !ok || !me.IsActive() || t.WorkspaceID != workspaceID {
			continue
		}
		if me.Muted && m.mentionsLocked(userID, id, me.LastReadSeq) == 0 {
			continue
		}
		if unread := me.Unread(t.LastSeq); unread > 0 {
			totals.UnreadThreads++
			totals.UnreadMessages += unread
		}
	}
	return totals, nil
}

func (m *memoryChats) UnreadMentions(_ context.Context, userID string, ids []string) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int64{}
	for _, id := range ids {
		if n := m.mentionsLocked(userID, id, m.members[id][userID].LastReadSeq); n > 0 {
			out[id] = n
		}
	}
	return out, nil
}

func (m *memoryChats) mentionsLocked(userID, threadID string, after int64) int64 {
	var n int64
	for _, msg := range m.messages {
		if msg.ThreadID == threadID && msg.Seq > after && !msg.IsDeleted() && msg.Mentioned(userID) {
			n++
		}
	}
	return n
}

func (m *memoryChats) Append(_ context.Context, msg teamchat.Message) (teamchat.Message, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.appendLocked(msg)
}

func (m *memoryChats) appendLocked(msg teamchat.Message) (teamchat.Message, bool, error) {
	if msg.ClientID != "" {
		for _, existing := range m.messages {
			if existing.ThreadID == msg.ThreadID && existing.AuthorID == msg.AuthorID && existing.ClientID == msg.ClientID {
				return existing, false, nil
			}
		}
	}
	t, ok := m.threads[msg.ThreadID]
	if !ok {
		return teamchat.Message{}, false, teamchat.ErrNotFound
	}
	t.LastSeq++
	t.LastMessageAt = msg.CreatedAt
	m.threads[t.ID] = t
	msg.Seq = t.LastSeq
	m.messages = append(m.messages, msg)
	if author, ok := m.members[t.ID][msg.AuthorID]; ok && author.IsActive() {
		author.LastReadSeq = max(author.LastReadSeq, msg.Seq)
		m.members[t.ID][msg.AuthorID] = author
	}
	return msg, true, nil
}

func (m *memoryChats) ListMessages(_ context.Context, q teamchat.MessageQuery) ([]teamchat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	matching := []teamchat.Message{}
	for _, msg := range m.messages {
		if msg.ThreadID != q.ThreadID {
			continue
		}
		if (q.AfterSeq > 0 && msg.Seq <= q.AfterSeq) || (q.BeforeSeq > 0 && msg.Seq >= q.BeforeSeq) {
			continue
		}
		matching = append(matching, msg)
	}
	sort.Slice(matching, func(i, j int) bool { return matching[i].Seq < matching[j].Seq })
	if len(matching) > q.Limit {
		if q.AfterSeq > 0 {
			matching = matching[:q.Limit]
		} else {
			matching = matching[len(matching)-q.Limit:]
		}
	}
	return matching, nil
}

func (m *memoryChats) LastMessages(_ context.Context, threads []teamchat.Thread) (map[string]teamchat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]teamchat.Message{}
	for _, t := range threads {
		for _, msg := range m.messages {
			if msg.ThreadID == t.ID && msg.Seq == t.LastSeq {
				out[t.ID] = msg
			}
		}
	}
	return out, nil
}

func (m *memoryChats) FindMessage(_ context.Context, workspaceID, id string) (teamchat.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.messages {
		if msg.ID == id && msg.WorkspaceID == workspaceID {
			return msg, nil
		}
	}
	return teamchat.Message{}, teamchat.ErrMessageNotFound
}

func (m *memoryChats) SaveMessage(_ context.Context, msg teamchat.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, existing := range m.messages {
		if existing.ID == msg.ID {
			m.messages[i] = msg
			return nil
		}
	}
	return teamchat.ErrMessageNotFound
}

func (m *memoryChats) CreateScheduled(_ context.Context, s teamchat.Scheduled) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduled[s.ID] = s
	return nil
}

func (m *memoryChats) FindScheduled(_ context.Context, id string) (teamchat.Scheduled, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.scheduled[id]
	if !ok {
		return teamchat.Scheduled{}, teamchat.ErrScheduledNotFound
	}
	return s, nil
}

func (m *memoryChats) ListScheduled(_ context.Context, threadID, authorID string) ([]teamchat.Scheduled, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []teamchat.Scheduled{}
	for _, s := range m.scheduled {
		if s.ThreadID == threadID && s.AuthorID == authorID && s.Status == teamchat.ScheduledPending {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SendAt.Before(out[j].SendAt) })
	return out, nil
}

func (m *memoryChats) SaveScheduled(_ context.Context, s teamchat.Scheduled) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scheduled[s.ID].Status != teamchat.ScheduledPending {
		return teamchat.ErrScheduledFinal
	}
	m.scheduled[s.ID] = s
	return nil
}

func (m *memoryChats) DueScheduled(_ context.Context, now time.Time, limit int) ([]teamchat.Scheduled, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []teamchat.Scheduled{}
	for _, s := range m.scheduled {
		if s.Due(now) && len(out) < limit {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memoryChats) textMessages(threadID string) []teamchat.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []teamchat.Message{}
	for _, msg := range m.messages {
		if msg.ThreadID == threadID && msg.Kind == teamchat.MessageText {
			out = append(out, msg)
		}
	}
	return out
}

type fakeAccess struct {
	members map[string]bool
	grants  map[string]map[workspace.Action]bool
}

func (a *fakeAccess) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	if isSystemAdmin {
		return true
	}
	return workspaceID == testWorkspace && resource == string(workspace.ResourceTeamChat) && a.grants[userID][workspace.Action(action)]
}

func (a *fakeAccess) IsWorkspaceMember(userID, workspaceID string) bool {
	return workspaceID == testWorkspace && a.members[userID]
}

func (a *fakeAccess) grant(userID string, actions ...workspace.Action) {
	a.members[userID] = true
	a.grants[userID] = map[workspace.Action]bool{}
	for _, action := range actions {
		a.grants[userID][action] = true
	}
}

func (a *fakeAccess) leave(userID string) {
	delete(a.members, userID)
}

type fakeDirectory struct{ access *fakeAccess }

func (d fakeDirectory) ListMembers(workspaceID string) ([]*workspace.Member, error) {
	out := []*workspace.Member{}
	for id := range d.access.members {
		out = append(out, &workspace.Member{WorkspaceID: workspaceID, UserID: id, Username: names[id]})
	}
	return out, nil
}

var names = map[string]string{ana: "Ana", bia: "Bia", caio: "Caio", dani: "Dani"}

type fakePeople struct{}

func (fakePeople) FindByIDs(ids []string) ([]*user.User, error) {
	out := []*user.User{}
	for _, id := range ids {
		if name, ok := names[id]; ok {
			out = append(out, &user.User{ID: id, Username: name, Picture: "https://cdn.test/" + id + ".png"})
		}
	}
	return out, nil
}

type announcement struct {
	kind       string
	recipients []string
	threadID   string
	messageID  string
	seq        int64
}

type fakeAnnouncer struct {
	mu   sync.Mutex
	sent []announcement
}

func (f *fakeAnnouncer) record(a announcement) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sort.Strings(a.recipients)
	f.sent = append(f.sent, a)
}

func (f *fakeAnnouncer) AnnounceTeamMessage(_ string, to []string, m teamchat.AuthoredMessage) {
	f.record(announcement{kind: "message", recipients: to, threadID: m.ThreadID, messageID: m.ID, seq: m.Seq})
}

func (f *fakeAnnouncer) AnnounceTeamMessageChanged(_ string, to []string, m teamchat.AuthoredMessage) {
	f.record(announcement{kind: "message_updated", recipients: to, threadID: m.ThreadID, messageID: m.ID})
}

func (f *fakeAnnouncer) AnnounceTeamRead(_ string, to []string, mark teamchat.ReadMark) {
	f.record(announcement{kind: "read", recipients: to, threadID: mark.ThreadID, seq: mark.Seq})
}

func (f *fakeAnnouncer) AnnounceTeamThread(_ string, to []string, threadID string) {
	f.record(announcement{kind: "thread", recipients: to, threadID: threadID})
}

func (f *fakeAnnouncer) AnnounceTeamThreadRemoved(_ string, to []string, threadID string) {
	f.record(announcement{kind: "thread_removed", recipients: to, threadID: threadID})
}

func (f *fakeAnnouncer) AnnounceTeamTyping(_ string, to []string, threadID, _ string) {
	f.record(announcement{kind: "typing", recipients: to, threadID: threadID})
}

func (f *fakeAnnouncer) AnnounceTeamScheduled(_ string, userID string, s teamchat.Scheduled) {
	f.record(announcement{kind: "scheduled:" + string(s.Status), recipients: []string{userID}, threadID: s.ThreadID})
}

func (f *fakeAnnouncer) of(kind string) []announcement {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []announcement{}
	for _, a := range f.sent {
		if a.kind == kind {
			out = append(out, a)
		}
	}
	return out
}

func (f *fakeAnnouncer) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = nil
}

type fakeWake struct{ fires map[string]time.Time }

func (w *fakeWake) ScheduleFire(id string, at time.Time) error {
	w.fires[id] = at
	return nil
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type sequentialIDs struct{ n int }

func (s *sequentialIDs) next() string {
	s.n++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.n)
}

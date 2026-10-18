package notification_usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"vozko/domain/notification"
	"vozko/domain/shared"
)

type memoryCenter struct {
	mu          sync.Mutex
	seq         int
	records     map[string]notification.Record
	order       []string
	recipients  map[string]*memoryRecipient
	prefs       map[string]notification.Preferences
	broadcasts  map[string]notification.Broadcast
	createErr   error
	addErr      error
	prefsErr    error
	purgeBudget int
}

type memoryRecipient struct {
	notification.Recipient
	ReadAt *time.Time
	Held   bool
}

func newMemoryCenter() *memoryCenter {
	return &memoryCenter{
		records:    map[string]notification.Record{},
		recipients: map[string]*memoryRecipient{},
		prefs:      map[string]notification.Preferences{},
		broadcasts: map[string]notification.Broadcast{},
	}
}

func recipientKey(notificationID, userID string) string { return notificationID + "/" + userID }

func (m *memoryCenter) Create(_ context.Context, r notification.Record) (notification.Record, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return notification.Record{}, false, m.createErr
	}
	if r.DedupKey != "" {
		for _, existing := range m.records {
			if existing.WorkspaceID == r.WorkspaceID && existing.DedupKey == r.DedupKey {
				return existing, false, nil
			}
		}
	}
	m.seq++
	r.ID = fmt.Sprintf("n-%d", m.seq)
	m.records[r.ID] = r
	m.order = append(m.order, r.ID)
	return r, true, nil
}

func (m *memoryCenter) Get(_ context.Context, id string) (notification.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok {
		return notification.Record{}, notification.ErrNotFound
	}
	return r, nil
}

func (m *memoryCenter) AddRecipients(_ context.Context, rs []notification.Recipient) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.addErr != nil {
		return m.addErr
	}
	for _, r := range rs {
		key := recipientKey(r.NotificationID, r.UserID)
		if _, exists := m.recipients[key]; !exists {
			m.recipients[key] = &memoryRecipient{Recipient: r}
		}
	}
	return nil
}

func (m *memoryCenter) MarkDelivered(_ context.Context, id string, n int, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.records[id]
	r.Status, r.RecipientCount, r.DeliveredAt = notification.StatusDelivered, n, &at
	m.records[id] = r
	return nil
}

func (m *memoryCenter) StalePending(_ context.Context, before time.Time, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, id := range m.order {
		r := m.records[id]
		if r.Status == notification.StatusPending && r.CreatedAt.Before(before) && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *memoryCenter) PurgeBefore(_ context.Context, cutoff time.Time, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, id := range append([]string(nil), m.order...) {
		if n == limit {
			break
		}
		if r, ok := m.records[id]; ok && r.CreatedAt.Before(cutoff) {
			delete(m.records, id)
			n++
		}
	}
	m.purgeBudget += n
	return n, nil
}

func (m *memoryCenter) inboxOf(v notification.Viewer) []notification.Item {
	var items []notification.Item
	for _, r := range m.recipients {
		if r.WorkspaceID != v.WorkspaceID || r.UserID != v.UserID {
			continue
		}
		item := m.records[r.NotificationID].Item()
		item.ReadAt = r.ReadAt
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items
}

func (m *memoryCenter) List(_ context.Context, v notification.Viewer, q notification.ListQuery) ([]notification.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []notification.Item
	for _, item := range m.inboxOf(v) {
		if (q.Category != "" && item.Category != q.Category) || (q.UnreadOnly && item.ReadAt != nil) {
			continue
		}
		if q.Paged && item.ID >= q.Before.ID {
			continue
		}
		if len(out) < q.Limit {
			out = append(out, item)
		}
	}
	return out, nil
}

func (m *memoryCenter) Summary(_ context.Context, v notification.Viewer, _ int) (notification.Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := notification.Summary{ByCategory: map[notification.Category]int{}}
	for _, item := range m.inboxOf(v) {
		if item.ReadAt == nil {
			s.Unread++
			s.ByCategory[item.Category]++
		}
	}
	return s, nil
}

func (m *memoryCenter) mark(v notification.Viewer, ids []string, at *time.Time) int {
	n := 0
	for _, id := range ids {
		if r, ok := m.recipients[recipientKey(id, v.UserID)]; ok && r.WorkspaceID == v.WorkspaceID {
			r.ReadAt = at
			n++
		}
	}
	return n
}

func (m *memoryCenter) MarkRead(_ context.Context, v notification.Viewer, ids []string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mark(v, ids, &at), nil
}

func (m *memoryCenter) MarkUnread(_ context.Context, v notification.Viewer, ids []string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mark(v, ids, nil), nil
}

func (m *memoryCenter) MarkAllRead(_ context.Context, v notification.Viewer, c notification.Category, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.recipients {
		if r.WorkspaceID == v.WorkspaceID && r.UserID == v.UserID && r.ReadAt == nil && (c == "" || r.Category == c) {
			r.ReadAt = &at
			n++
		}
	}
	return n, nil
}

func (m *memoryCenter) Preferences(_ context.Context, v notification.Viewer) (notification.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.prefs[v.WorkspaceID+"/"+v.UserID]
	p.WorkspaceID, p.UserID = v.WorkspaceID, v.UserID
	return p, nil
}

func (m *memoryCenter) PreferencesFor(_ context.Context, workspaceID string, userIDs []string) (map[string]notification.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.prefsErr != nil {
		return nil, m.prefsErr
	}
	out := map[string]notification.Preferences{}
	for _, id := range userIDs {
		if p, ok := m.prefs[workspaceID+"/"+id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

func (m *memoryCenter) SavePreferences(_ context.Context, p notification.Preferences) (notification.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prefs[p.WorkspaceID+"/"+p.UserID] = p
	return p, nil
}

func (m *memoryCenter) HoldForDigest(_ context.Context, notificationID string, userIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range userIDs {
		if r, ok := m.recipients[recipientKey(notificationID, id)]; ok {
			r.Held = true
		}
	}
	return nil
}

func (m *memoryCenter) WorkspacesWithHeldDigests(_ context.Context, upTo time.Time, afterID string, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, r := range m.recipients {
		if r.Held && !r.CreatedAt.After(upTo) && r.WorkspaceID > afterID && !seen[r.WorkspaceID] {
			seen[r.WorkspaceID] = true
			out = append(out, r.WorkspaceID)
		}
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryCenter) HeldDigests(_ context.Context, workspaceID string, upTo time.Time, itemLimit int) ([]notification.Digest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byUser := map[string]*notification.Digest{}
	var order []string
	keys := make([]string, 0, len(m.recipients))
	for k := range m.recipients {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))
	for _, k := range keys {
		r := m.recipients[k]
		if r.WorkspaceID != workspaceID || !r.Held || r.ReadAt != nil || r.CreatedAt.After(upTo) {
			continue
		}
		d, ok := byUser[r.UserID]
		if !ok {
			d = &notification.Digest{WorkspaceID: workspaceID, UserID: r.UserID}
			byUser[r.UserID] = d
			order = append(order, r.UserID)
		}
		d.Total++
		if len(d.Items) < itemLimit {
			d.Items = append(d.Items, m.records[r.NotificationID].Item())
		}
	}
	sort.Strings(order)
	out := make([]notification.Digest, 0, len(order))
	for _, id := range order {
		out = append(out, *byUser[id])
	}
	return out, nil
}

func (m *memoryCenter) ReleaseDigests(_ context.Context, workspaceID string, upTo time.Time, keepUserIDs []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	keep := map[string]bool{}
	for _, id := range keepUserIDs {
		keep[id] = true
	}
	for _, r := range m.recipients {
		if r.WorkspaceID == workspaceID && r.Held && !r.CreatedAt.After(upTo) && !keep[r.UserID] {
			r.Held = false
		}
	}
	return nil
}

func (m *memoryCenter) heldFor(userID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.recipients {
		if r.UserID == userID && r.Held {
			n++
		}
	}
	return n
}

func (m *memoryCenter) CreateBroadcast(_ context.Context, b notification.Broadcast) (notification.Broadcast, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	b.ID = fmt.Sprintf("b-%d", m.seq)
	m.broadcasts[b.ID] = b
	return b, nil
}

func (m *memoryCenter) Broadcast(_ context.Context, id string) (notification.Broadcast, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.broadcasts[id]
	if !ok {
		return notification.Broadcast{}, notification.ErrNotFound
	}
	return b, nil
}

func (m *memoryCenter) CompleteBroadcast(_ context.Context, id string, n int, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.broadcasts[id]
	b.Status, b.WorkspaceCount, b.CompletedAt = notification.BroadcastCompleted, n, &at
	m.broadcasts[id] = b
	return nil
}

func (m *memoryCenter) Broadcasts(_ context.Context, before shared.Keyset, paged bool, limit int) ([]notification.Broadcast, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var all []notification.Broadcast
	for _, b := range m.broadcasts {
		if !paged || b.ID < before.ID {
			all = append(all, b)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func (m *memoryCenter) StaleQueued(_ context.Context, before time.Time, limit int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, b := range m.broadcasts {
		if b.Status == notification.BroadcastQueued && b.CreatedAt.Before(before) && len(out) < limit {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *memoryCenter) recipientsOf(notificationID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, r := range m.recipients {
		if r.NotificationID == notificationID {
			out = append(out, r.UserID)
		}
	}
	sort.Strings(out)
	return out
}

type fixedDirectory struct {
	members    map[string][]notification.Member
	workspaces []string
	err        error
}

func (d fixedDirectory) Members(_ context.Context, workspaceID string) ([]notification.Member, error) {
	return d.members[workspaceID], d.err
}

func (d fixedDirectory) WorkspaceName(_ context.Context, workspaceID string) (string, error) {
	return "Loja " + workspaceID, d.err
}

func (d fixedDirectory) WorkspaceIDs(_ context.Context, after string, limit int) ([]string, error) {
	var out []string
	for _, id := range d.workspaces {
		if id > after && len(out) < limit {
			out = append(out, id)
		}
	}
	return out, d.err
}

type grantedPermissions map[string]bool

func (g grantedPermissions) HasWorkspacePermission(userID, workspaceID, resource, action string, _ bool) bool {
	return g[workspaceID+"/"+userID+"/"+resource+":"+action]
}

type pushedBatch struct {
	workspaceID string
	userIDs     []string
	item        notification.Item
}

type recordingPusher struct {
	mu      sync.Mutex
	batches []pushedBatch
}

func (p *recordingPusher) PushNotification(workspaceID string, userIDs []string, item notification.Item) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.batches = append(p.batches, pushedBatch{workspaceID, append([]string(nil), userIDs...), item})
}

type recordingQueue struct {
	mu         sync.Mutex
	fanouts    []string
	broadcasts []string
	err        error
}

func (q *recordingQueue) EnqueueFanout(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.fanouts = append(q.fanouts, id)
	return nil
}

func (q *recordingQueue) EnqueueBroadcast(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.broadcasts = append(q.broadcasts, id)
	return nil
}

type ownedImages struct {
	saved [][]byte
	err   error
}

const ownedImagePrefix = "https://cdn.vozko.test/notifications/images/"

func (o *ownedImages) Save(_ context.Context, data []byte, img notification.Image) (string, error) {
	if o.err != nil {
		return "", o.err
	}
	o.saved = append(o.saved, data)
	return ownedImagePrefix + fmt.Sprintf("%d", len(o.saved)) + img.Ext, nil
}

func (o *ownedImages) Owns(url string) bool { return strings.HasPrefix(url, ownedImagePrefix) }

type recordingNotifier struct {
	mu   sync.Mutex
	sent []notification.Notification
	err  error
	seen map[string]bool
}

func (n *recordingNotifier) Notify(req notification.Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.err != nil {
		return n.err
	}
	if n.seen == nil {
		n.seen = map[string]bool{}
	}
	if n.seen[req.DedupKey] {
		return nil
	}
	n.seen[req.DedupKey] = true
	n.sent = append(n.sent, req)
	return nil
}

func (n *recordingNotifier) emails() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.sent))
	for _, s := range n.sent {
		out = append(out, s.Email)
	}
	sort.Strings(out)
	return out
}

var errBoom = errors.New("boom")

type steppingClock struct{ at time.Time }

func (c *steppingClock) now() time.Time {
	c.at = c.at.Add(time.Second)
	return c.at
}

func newClock() *steppingClock {
	return &steppingClock{at: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
}

func (d fixedDirectory) MembersOf(_ context.Context, workspaceIDs []string) (map[string][]notification.Member, error) {
	out := map[string][]notification.Member{}
	for _, id := range workspaceIDs {
		if members, ok := d.members[id]; ok {
			out[id] = members
		}
	}
	return out, d.err
}

func (m *memoryCenter) BroadcastStats(_ context.Context, ids []string) (map[string]notification.BroadcastStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := map[string]notification.BroadcastStats{}
	for _, r := range m.recipients {
		broadcastID := m.records[r.NotificationID].BroadcastID
		if !wanted[broadcastID] {
			continue
		}
		s := out[broadcastID]
		s.Recipients++
		if r.ReadAt != nil {
			s.Read++
		}
		out[broadcastID] = s
	}
	return out, nil
}

func (m *memoryCenter) BroadcastRecipients(_ context.Context, broadcastID string, q notification.RecipientQuery) ([]notification.BroadcastRecipient, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []notification.BroadcastRecipient
	for _, r := range m.recipients {
		if m.records[r.NotificationID].BroadcastID != broadcastID {
			continue
		}
		if (q.Status == notification.ReadStatusRead && r.ReadAt == nil) || (q.Status == notification.ReadStatusUnread && r.ReadAt != nil) {
			continue
		}
		key := r.NotificationID + "/" + r.UserID
		if q.Paged && key <= q.After.NotificationID+"/"+q.After.UserID {
			continue
		}
		rows = append(rows, notification.BroadcastRecipient{
			NotificationID: r.NotificationID, UserID: r.UserID, Name: r.UserID, WorkspaceID: r.WorkspaceID, ReadAt: r.ReadAt,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].NotificationID+"/"+rows[i].UserID < rows[j].NotificationID+"/"+rows[j].UserID
	})
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
	}
	return rows, nil
}

package unofficial_whatsapp_group_repository

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"

	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database/sqlexpr"
)

type statsReader struct{ db *gorm.DB }

func NewStatsReader(db *gorm.DB) uwg.StatsReader { return &statsReader{db: db} }

const groupMessagesFilter = `m.entry_type = 'unofficial_whatsapp' AND m.deleted_at IS NULL
	AND m.message_type <> 'reaction' AND m.created_at >= ? AND m.created_at <= ?`

func followedGroups(s uwg.StatsScope) (string, []any) {
	where, args := scopeSQL("wg", s.Scope)
	return `fg AS (
		SELECT wg.id, wg.subject, wg.jid, wg.origin, wg.category, wg.description, wg.member_count, wg.primary_instance_id, wg.workspace_id
		  FROM unofficial_whatsapp_workspace_groups wg
		 WHERE wg.workspace_id = ? AND wg.deleted_at IS NULL AND wg.followed_at IS NOT NULL AND ` + where + `)`,
		append([]any{s.WorkspaceID}, args...)
}

func followedConversations(s uwg.StatsScope) (string, []any) {
	fg, args := followedGroups(s)
	return fg + `, conv AS (
		SELECT wg.id AS group_id, wg.subject, pc.id AS conversation_id
		  FROM fg wg ` + primaryConversationJoin + `)`, args
}

func (r *statsReader) GroupTotals(ctx context.Context, s uwg.StatsScope) (uwg.GroupTotals, error) {
	where, args := scopeSQL("wg", s.Scope)
	base := `FROM unofficial_whatsapp_workspace_groups wg WHERE wg.workspace_id = ? AND wg.deleted_at IS NULL AND ` + where
	all := append([]any{s.WorkspaceID}, args...)
	var counts struct{ Groups, Followed, Open, Closed int64 }
	if err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) AS groups,
		COUNT(*) FILTER (WHERE wg.followed_at IS NOT NULL) AS followed,
		COUNT(*) FILTER (WHERE wg.followed_at IS NOT NULL AND NOT wg.announce) AS open,
		COUNT(*) FILTER (WHERE wg.followed_at IS NOT NULL AND wg.announce) AS closed `+base, all...).Scan(&counts).Error; err != nil {
		return uwg.GroupTotals{}, err
	}
	t := uwg.GroupTotals{Groups: counts.Groups, Followed: counts.Followed, Open: counts.Open, Closed: counts.Closed, Categories: []uwg.CategoryCount{}}
	err := r.db.WithContext(ctx).Raw(`SELECT wg.category, COUNT(*) AS count `+base+
		` AND wg.followed_at IS NOT NULL GROUP BY wg.category ORDER BY COUNT(*) DESC, wg.category`, all...).Scan(&t.Categories).Error
	return t, err
}

func (r *statsReader) PeopleTotals(ctx context.Context, s uwg.StatsScope) (uwg.PeopleTotals, error) {
	fg, args := followedGroups(s)
	var t uwg.PeopleTotals
	err := r.db.WithContext(ctx).Raw(`WITH `+fg+`
		SELECT COUNT(*) AS memberships, COUNT(DISTINCT m.person_key) AS unique
		  FROM unofficial_whatsapp_group_members m JOIN fg ON fg.id = m.group_id
		 WHERE m.status = 'active' AND m.our_number = false`, args...).Scan(&t).Error
	return t, err
}

func (r *statsReader) DailyMessages(ctx context.Context, s uwg.StatsScope, w uwg.Window, loc *time.Location) (map[string]uwg.DailyCount, error) {
	conv, args := followedConversations(s)
	type row struct {
		Day          string
		Messages     int64
		ActiveGroups int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`WITH `+conv+`
		SELECT to_char(m.created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
		       COUNT(*) AS messages, COUNT(DISTINCT conv.group_id) AS active_groups
		  FROM conv JOIN conversation_messages m ON m.entry_id = conv.conversation_id
		 WHERE `+groupMessagesFilter+`
		 GROUP BY 1`, append(args, loc.String(), w.From, w.To)...).Scan(&rows).Error
	out := make(map[string]uwg.DailyCount, len(rows))
	for _, rw := range rows {
		out[rw.Day] = uwg.DailyCount{Messages: rw.Messages, ActiveGroups: rw.ActiveGroups}
	}
	return out, err
}

func (r *statsReader) TopGroupsByMessages(ctx context.Context, s uwg.StatsScope, w uwg.Window, limit int) ([]uwg.GroupCount, error) {
	conv, args := followedConversations(s)
	var out []uwg.GroupCount
	err := r.db.WithContext(ctx).Raw(`WITH `+conv+`
		SELECT conv.group_id, conv.subject, COUNT(*) AS count
		  FROM conv JOIN conversation_messages m ON m.entry_id = conv.conversation_id
		 WHERE `+groupMessagesFilter+`
		 GROUP BY conv.group_id, conv.subject
		 ORDER BY COUNT(*) DESC, conv.subject
		 LIMIT ?`, append(args, w.From, w.To, limit)...).Scan(&out).Error
	return out, err
}

func (r *statsReader) ResponseTimes(ctx context.Context, s uwg.StatsScope, w uwg.Window, loc *time.Location) (map[string]uwg.ResponseTimePoint, error) {
	conv, args := followedConversations(s)
	type row struct {
		Day        string
		AvgMinutes float64
		Samples    int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`WITH `+conv+`, ordered AS (
		SELECT to_char(m.created_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day, conv.group_id, m.created_at, m.from_participant,
		       LAG(m.from_participant) OVER (PARTITION BY conv.group_id, to_char(m.created_at AT TIME ZONE ?, 'YYYY-MM-DD') ORDER BY m.created_at) AS prev_from,
		       LAG(m.created_at) OVER (PARTITION BY conv.group_id, to_char(m.created_at AT TIME ZONE ?, 'YYYY-MM-DD') ORDER BY m.created_at) AS prev_at
		  FROM conv JOIN conversation_messages m ON m.entry_id = conv.conversation_id
		 WHERE `+groupMessagesFilter+`)
		SELECT day, AVG(EXTRACT(EPOCH FROM (created_at - prev_at)) / 60.0) AS avg_minutes, COUNT(*) AS samples
		  FROM ordered
		 WHERE prev_from IS NOT NULL AND prev_from <> from_participant
		 GROUP BY day`, append(args, loc.String(), loc.String(), loc.String(), w.From, w.To)...).Scan(&rows).Error
	out := make(map[string]uwg.ResponseTimePoint, len(rows))
	for _, rw := range rows {
		out[rw.Day] = uwg.ResponseTimePoint{AvgMinutes: rw.AvgMinutes, Samples: rw.Samples}
	}
	return out, err
}

func (r *statsReader) DailyGrowth(ctx context.Context, s uwg.StatsScope, groupID string, w uwg.Window, loc *time.Location) (map[string]uwg.DailyCount, error) {
	fg, args := followedGroups(s)
	filter := ""
	if groupID != "" {
		filter = " AND e.group_id = ?"
	}
	type row struct {
		Day    string
		Joins  int64
		Leaves int64
	}
	query := `WITH ` + fg + `
		SELECT to_char(e.occurred_at AT TIME ZONE ?, 'YYYY-MM-DD') AS day,
		       COUNT(*) FILTER (WHERE e.kind = 'join') AS joins,
		       COUNT(*) FILTER (WHERE e.kind = 'leave') AS leaves
		  FROM unofficial_whatsapp_group_member_events e JOIN fg ON fg.id = e.group_id
		 WHERE e.our_number = false AND e.occurred_at >= ? AND e.occurred_at <= ?` + filter + `
		 GROUP BY 1`
	args = append(args, loc.String(), w.From, w.To)
	if groupID != "" {
		args = append(args, groupID)
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&rows).Error
	out := make(map[string]uwg.DailyCount, len(rows))
	for _, rw := range rows {
		out[rw.Day] = uwg.DailyCount{Joins: rw.Joins, Leaves: rw.Leaves}
	}
	return out, err
}

func (r *statsReader) TopGrowing(ctx context.Context, s uwg.StatsScope, w uwg.Window, limit int) ([]uwg.GroupGrowth, error) {
	fg, args := followedGroups(s)
	var out []uwg.GroupGrowth
	err := r.db.WithContext(ctx).Raw(`WITH `+fg+`
		SELECT fg.id AS group_id, fg.subject,
		       COUNT(*) FILTER (WHERE e.kind = 'join') AS joins,
		       COUNT(*) FILTER (WHERE e.kind = 'leave') AS leaves,
		       COUNT(*) FILTER (WHERE e.kind = 'join') - COUNT(*) FILTER (WHERE e.kind = 'leave') AS net
		  FROM unofficial_whatsapp_group_member_events e JOIN fg ON fg.id = e.group_id
		 WHERE e.our_number = false AND e.occurred_at >= ? AND e.occurred_at <= ?
		 GROUP BY fg.id, fg.subject
		 ORDER BY net DESC, joins DESC, fg.subject
		 LIMIT ?`, append(args, w.From, w.To, limit)...).Scan(&out).Error
	return out, err
}

func (r *statsReader) GroupActivity(ctx context.Context, workspaceID string, groupIDs []string, since time.Time) (map[string]uwg.GroupActivity, error) {
	out := map[string]uwg.GroupActivity{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	type row struct {
		GroupID      string
		Messages     int64
		LastActivity *time.Time
		Joins        int64
		Leaves       int64
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`
		SELECT wg.id AS group_id,
		       (SELECT COUNT(*) FROM conversation_messages m
		         WHERE m.entry_id = pc.id AND m.entry_type = 'unofficial_whatsapp' AND m.deleted_at IS NULL
		           AND m.message_type <> 'reaction' AND m.created_at >= ?) AS messages,
		       (SELECT c.last_message_at FROM unofficial_whatsapp_conversations c WHERE c.id = pc.id) AS last_activity,
		       (SELECT COUNT(*) FROM unofficial_whatsapp_group_member_events e
		         WHERE e.group_id = wg.id AND e.kind = 'join' AND e.our_number = false AND e.occurred_at >= ?) AS joins,
		       (SELECT COUNT(*) FROM unofficial_whatsapp_group_member_events e
		         WHERE e.group_id = wg.id AND e.kind = 'leave' AND e.our_number = false AND e.occurred_at >= ?) AS leaves
		  FROM unofficial_whatsapp_workspace_groups wg `+primaryConversationLeftJoin+`
		 WHERE wg.workspace_id = ? AND wg.id IN ?`, since, since, since, workspaceID, groupIDs).Scan(&rows).Error
	for _, rw := range rows {
		out[rw.GroupID] = uwg.GroupActivity{Messages: rw.Messages, LastActivity: rw.LastActivity, Joins: rw.Joins, Leaves: rw.Leaves}
	}
	return out, err
}

func (r *statsReader) CrossGroupPeople(ctx context.Context, f uwg.PeopleFilter) (*shared.PaginatedResult[*uwg.Person], error) {
	pagination := shared.NormalizePagination(f.Pagination)
	fg, args := followedGroups(f.StatsScope)
	args = append(args, f.MinGroups)
	search := ""
	if s := strings.TrimSpace(f.Search); s != "" {
		search = ` AND (bool_or(m.display_name ILIKE ? ESCAPE '\') OR MAX(m.phone) LIKE ? ESCAPE '\' OR bool_or(m.jid ILIKE ? ESCAPE '\'))`
		like := sqlexpr.LikeContains(s)
		args = append(args, like, like, like)
	}
	people := `WITH ` + fg + `, people AS (
		SELECT m.person_key,
		       COALESCE(NULLIF(MAX(m.display_name), ''), MAX(m.phone), MIN(m.jid)) AS name,
		       MAX(m.phone) AS phone, MIN(m.jid) AS jid,
		       (ARRAY_AGG(m.lead_id) FILTER (WHERE m.lead_id IS NOT NULL))[1] AS lead_id,
		       COUNT(DISTINCT m.group_id) AS group_count,
		       MAX(m.joined_at) AS last_join_at,
		       json_agg(DISTINCT jsonb_build_object('groupId', fg.id, 'subject', fg.subject)) AS groups
		  FROM unofficial_whatsapp_group_members m JOIN fg ON fg.id = m.group_id
		 WHERE m.status = 'active' AND m.our_number = false
		 GROUP BY m.person_key
		HAVING COUNT(DISTINCT m.group_id) >= ?` + search + `)`

	var total int64
	if err := r.db.WithContext(ctx).Raw(people+` SELECT COUNT(*) FROM people`, args...).Scan(&total).Error; err != nil {
		return nil, err
	}
	type row struct {
		PersonKey  string
		Name       string
		Phone      string
		JID        string `gorm:"column:jid"`
		LeadID     *string
		LeadName   string
		GroupCount int
		LastJoinAt *time.Time
		Groups     []byte
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(people+`
		SELECT people.*, COALESCE(l.name, '') AS lead_name
		  FROM people LEFT JOIN leads l ON l.id = people.lead_id AND l.deleted_at IS NULL
		 ORDER BY people.group_count DESC, lower(people.name), people.person_key
		 OFFSET ? LIMIT ?`, append(args, pagination.Offset(), pagination.PageSize)...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	items := make([]*uwg.Person, 0, len(rows))
	for _, rw := range rows {
		p := &uwg.Person{PersonKey: rw.PersonKey, Name: rw.Name, Phone: rw.Phone, JID: rw.JID,
			LeadID: value(rw.LeadID), LeadName: rw.LeadName, GroupCount: rw.GroupCount, LastJoinAt: rw.LastJoinAt, Groups: []uwg.GroupRef{}}
		if len(rw.Groups) > 0 {
			if err := json.Unmarshal(rw.Groups, &p.Groups); err != nil {
				return nil, err
			}
		}
		items = append(items, p)
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *statsReader) Membership(ctx context.Context, f uwg.MembershipFilter) ([]uwg.MembershipStat, error) {
	conv, convArgs := followedConversations(f.StatsScope)
	filter := ""
	var filterArgs []any
	if f.Origin.Valid() {
		filter += " AND fg.origin = ?"
		filterArgs = append(filterArgs, string(f.Origin))
	}
	if f.Category != nil {
		filter += " AND fg.category = ?"
		filterArgs = append(filterArgs, strings.TrimSpace(*f.Category))
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		like := sqlexpr.LikeContains(s)
		filter += ` AND (fg.subject ILIKE ? ESCAPE '\' OR fg.description ILIKE ? ESCAPE '\')`
		filterArgs = append(filterArgs, like, like)
	}
	var rows []uwg.MembershipStat
	err := r.db.WithContext(ctx).Raw(`WITH `+conv+`, events AS (
		SELECT e.group_id,
		       COUNT(*) FILTER (WHERE e.kind = 'join') AS joins,
		       COUNT(*) FILTER (WHERE e.kind = 'leave') AS leaves
		  FROM unofficial_whatsapp_group_member_events e JOIN fg ON fg.id = e.group_id
		 WHERE e.our_number = false AND e.occurred_at >= ? AND e.occurred_at <= ?
		 GROUP BY e.group_id),
		messages AS (
		SELECT conv.group_id, COUNT(*) AS messages
		  FROM conv JOIN conversation_messages m ON m.entry_id = conv.conversation_id
		 WHERE `+groupMessagesFilter+`
		 GROUP BY conv.group_id)
		SELECT fg.id AS group_id, fg.subject, fg.origin, fg.category, fg.member_count AS members,
		       (SELECT COUNT(*) FROM unofficial_whatsapp_groups seat
		         WHERE seat.workspace_id = fg.workspace_id AND seat.jid = fg.jid AND seat.deleted_at IS NULL) AS numbers,
		       (SELECT MAX(e.occurred_at) FROM unofficial_whatsapp_group_member_events e
		         WHERE e.group_id = fg.id AND e.our_number = false) AS last_member_event_at,
		       GREATEST(fg.member_count - COALESCE(ev.joins, 0) + COALESCE(ev.leaves, 0), 0) AS base,
		       COALESCE(ev.joins, 0) AS joins, COALESCE(ev.leaves, 0) AS leaves,
		       COALESCE(ms.messages, 0) AS messages,
		       (SELECT c.last_message_at FROM conv JOIN unofficial_whatsapp_conversations c ON c.id = conv.conversation_id
		         WHERE conv.group_id = fg.id) AS last_activity
		  FROM fg
		  LEFT JOIN events ev ON ev.group_id = fg.id
		  LEFT JOIN messages ms ON ms.group_id = fg.id
		 WHERE TRUE`+filter, append(append(convArgs, f.Window.From, f.Window.To, f.Window.From, f.Window.To), filterArgs...)...).Scan(&rows).Error
	return rows, err
}

package unofficial_whatsapp_group_repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database"
	"vozko/infra/database/schema"
)

type memberRepository struct{ db *gorm.DB }

func NewMemberRepository(db *gorm.DB) uwg.MemberRepository { return &memberRepository{db: db} }

func (r *memberRepository) All(ctx context.Context, groupID string) ([]uwg.Member, error) {
	var rows []schema.UnofficialWhatsAppGroupMember
	if err := r.db.WithContext(ctx).Where("group_id = ?", groupID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return membersToDomain(rows), nil
}

func (r *memberRepository) ByPersonKeys(ctx context.Context, groupID string, keys []string) ([]uwg.Member, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var rows []schema.UnofficialWhatsAppGroupMember
	if err := r.db.WithContext(ctx).Where("group_id = ? AND person_key IN ?", groupID, keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	return membersToDomain(rows), nil
}

func (r *memberRepository) Apply(ctx context.Context, g *uwg.Group, diff uwg.MemberDiff, at time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(diff.Upserts) > 0 {
			rows := make([]schema.UnofficialWhatsAppGroupMember, 0, len(diff.Upserts))
			for _, m := range diff.Upserts {
				rows = append(rows, memberToRow(m))
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "group_id"}, {Name: "person_key"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"jid", "lid", "phone", "display_name", "role", "status",
					"joined_at", "left_at", "our_number", "updated_at",
				}),
			}).CreateInBatches(&rows, 500).Error; err != nil {
				return err
			}
		}
		if len(diff.Events) > 0 {
			events := make([]schema.UnofficialWhatsAppGroupMemberEvent, 0, len(diff.Events))
			for _, e := range diff.Events {
				events = append(events, schema.UnofficialWhatsAppGroupMemberEvent{
					WorkspaceID: e.WorkspaceID, GroupID: e.GroupID, PersonKey: e.PersonKey,
					Kind: string(e.Kind), OurNumber: e.OurNumber, OccurredAt: e.OccurredAt,
				})
			}
			if err := tx.CreateInBatches(&events, 500).Error; err != nil {
				return err
			}
		}
		if err := linkLeads(tx, g, diff.Upserts, at); err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE unofficial_whatsapp_workspace_groups
			   SET member_count = (SELECT COUNT(*) FROM unofficial_whatsapp_group_members
			                        WHERE group_id = ? AND status = 'active' AND our_number = false),
			       members_synced_at = ?, updated_at = ?
			 WHERE id = ?`, g.ID, at, at, g.ID).Error
	})
}

func (r *memberRepository) List(ctx context.Context, workspaceID string, f uwg.MemberFilter) (*shared.PaginatedResult[*uwg.MemberRow], error) {
	pagination := shared.NormalizePagination(f.Pagination)
	otherWhere, otherArgs := scopeSQL("og", f.Scope)
	otherGroups := `(SELECT COUNT(DISTINCT om.group_id) FROM unofficial_whatsapp_group_members om
		JOIN unofficial_whatsapp_workspace_groups og ON og.id = om.group_id AND og.deleted_at IS NULL AND og.followed_at IS NOT NULL
		WHERE om.workspace_id = m.workspace_id AND om.person_key = m.person_key
		  AND om.group_id <> m.group_id AND om.status = 'active' AND ` + otherWhere + `)`

	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Unscoped().Table("unofficial_whatsapp_group_members m").
			Where("m.workspace_id = ? AND m.group_id = ?", workspaceID, f.GroupID)
		if f.Status.Valid() {
			q = q.Where("m.status = ?", string(f.Status))
		}
		if f.AdminsOnly {
			q = q.Where("m.role IN ?", []string{string(uw.GroupRoleAdmin), string(uw.GroupRoleSuperAdmin)})
		}
		if search := strings.TrimSpace(f.Search); search != "" {
			clause, args := memberSearchSQL(search)
			q = q.Where(clause, args...)
		}
		if f.LeadLinked != nil {
			if *f.LeadLinked {
				q = q.Where("m.lead_id IS NOT NULL")
			} else {
				q = q.Where("m.lead_id IS NULL")
			}
		}
		if f.InOtherOnly {
			q = q.Where(otherGroups+" > 0", otherArgs...)
		}
		return q
	}

	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, err
	}

	type row struct {
		schema.UnofficialWhatsAppGroupMember
		LeadName    string
		ContactName string
		PictureURL  string
		OtherGroups int
	}
	var rows []row
	args := append([]any{}, otherArgs...)
	if err := base().
		Select("m.*, COALESCE(l.name, '') AS lead_name, COALESCE(ct.name, '') AS contact_name, COALESCE(ct.picture_url, '') AS picture_url, "+otherGroups+" AS other_groups", args...).
		Joins("LEFT JOIN leads l ON l.id = m.lead_id AND l.deleted_at IS NULL").
		Joins(memberContact).
		Order("CASE m.status WHEN 'active' THEN 0 ELSE 1 END, m.our_number DESC, CASE m.role WHEN 'superadmin' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END, lower(m.display_name), m.person_key").
		Offset(pagination.Offset()).Limit(pagination.PageSize).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*uwg.MemberRow, 0, len(rows))
	for i := range rows {
		items = append(items, &uwg.MemberRow{
			Member:      memberToDomain(&rows[i].UnofficialWhatsAppGroupMember),
			LeadName:    rows[i].LeadName,
			ContactName: rows[i].ContactName,
			PictureURL:  rows[i].PictureURL,
			OtherGroups: rows[i].OtherGroups,
		})
	}
	return shared.NewPaginatedResult(items, pagination, total), nil
}

func (r *memberRepository) Counts(ctx context.Context, groupID string, since time.Time) (uwg.MemberCounts, error) {
	var c uwg.MemberCounts
	err := r.db.WithContext(ctx).Raw(`
		SELECT
		  COUNT(*) FILTER (WHERE status = 'active' AND our_number = false) AS active,
		  COUNT(*) FILTER (WHERE status = 'left' AND our_number = false AND left_at >= ?) AS left30d,
		  COUNT(*) FILTER (WHERE status = 'active' AND lead_id IS NOT NULL) AS with_lead,
		  COUNT(*) FILTER (WHERE status = 'active' AND phone = '') AS hidden
		FROM unofficial_whatsapp_group_members WHERE group_id = ?`, since, groupID).Scan(&c).Error
	return c, err
}

func (r *memberRepository) SetLead(ctx context.Context, groupID, personKey, leadID string) error {
	return r.db.WithContext(ctx).Model(&schema.UnofficialWhatsAppGroupMember{}).
		Where("group_id = ? AND person_key = ? AND lead_id IS NULL", groupID, personKey).
		Updates(map[string]any{"lead_id": leadID, "updated_at": time.Now().UTC()}).Error
}

const memberContact = `LEFT JOIN LATERAL (
	SELECT COALESCE(NULLIF(ct.contact_name, ''), NULLIF(ct.verified_name, ''), NULLIF(ct.name, ''), '') AS name, ct.picture_url
	  FROM unofficial_whatsapp_contacts ct
	 WHERE ct.workspace_id = m.workspace_id AND ct.deleted_at IS NULL AND ct.is_group = false
	   AND ((m.phone <> '' AND ct.phone_number = m.phone) OR ct.jid = m.jid)
	 ORDER BY (ct.picture_url <> '') DESC, ct.updated_at DESC
	 LIMIT 1) ct ON TRUE`

func (r *memberRepository) Names(ctx context.Context, groupID string) (map[string]string, error) {
	type row struct {
		PersonKey string
		Name      string
	}
	var rows []row
	if err := r.db.WithContext(ctx).Raw(`SELECT m.person_key, COALESCE(NULLIF(m.display_name, ''), NULLIF(ct.name, ''), l.name, '') AS name
		FROM unofficial_whatsapp_group_members m
		LEFT JOIN leads l ON l.id = m.lead_id AND l.deleted_at IS NULL `+memberContact+`
		WHERE m.group_id = ?`, groupID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rows))
	for _, rw := range rows {
		if rw.Name != "" {
			names[rw.PersonKey] = rw.Name
		}
	}
	return names, nil
}

func (r *memberRepository) GroupsOf(ctx context.Context, workspaceID string, personKeys []string, scope uwg.Scope) (map[string][]uwg.GroupRef, error) {
	out := map[string][]uwg.GroupRef{}
	if len(personKeys) == 0 {
		return out, nil
	}
	where, args := scopeSQL("wg", scope)
	type row struct {
		PersonKey string
		GroupID   string
		Subject   string
	}
	var rows []row
	query := `SELECT m.person_key, wg.id AS group_id, wg.subject
		FROM unofficial_whatsapp_group_members m
		JOIN unofficial_whatsapp_workspace_groups wg ON wg.id = m.group_id AND wg.deleted_at IS NULL AND wg.followed_at IS NOT NULL
		WHERE m.workspace_id = ? AND m.person_key IN ? AND m.status = 'active' AND ` + where + `
		ORDER BY lower(wg.subject)`
	if err := r.db.WithContext(ctx).Raw(query, append([]any{workspaceID, personKeys}, args...)...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, rw := range rows {
		out[rw.PersonKey] = append(out[rw.PersonKey], uwg.GroupRef{GroupID: rw.GroupID, Subject: rw.Subject})
	}
	return out, nil
}

func memberToRow(m uwg.Member) schema.UnofficialWhatsAppGroupMember {
	return schema.UnofficialWhatsAppGroupMember{
		ID: m.ID, GroupID: m.GroupID, WorkspaceID: m.WorkspaceID, PersonKey: truncate(m.PersonKey, 96),
		JID: truncate(m.JID, 64), LID: truncate(m.LID, 64), Phone: truncate(m.Phone, 32),
		DisplayName: truncate(m.DisplayName, 255), Role: string(m.Role), Status: string(m.Status),
		OurNumber: m.OurNumber, JoinedAt: m.JoinedAt, LeftAt: m.LeftAt, LeadID: optional(m.LeadID),
	}
}

func memberToDomain(r *schema.UnofficialWhatsAppGroupMember) uwg.Member {
	return uwg.Member{
		ID: r.ID, GroupID: r.GroupID, WorkspaceID: r.WorkspaceID, PersonKey: r.PersonKey,
		JID: r.JID, LID: r.LID, Phone: r.Phone, DisplayName: r.DisplayName,
		Role: uw.GroupRole(r.Role), Status: uwg.MemberStatus(r.Status),
		JoinedAt: r.JoinedAt, LeftAt: r.LeftAt, LeadID: value(r.LeadID), OurNumber: r.OurNumber,
	}
}

func membersToDomain(rows []schema.UnofficialWhatsAppGroupMember) []uwg.Member {
	out := make([]uwg.Member, 0, len(rows))
	for i := range rows {
		out = append(out, memberToDomain(&rows[i]))
	}
	return out
}

func memberSearchSQL(search string) (string, []any) {
	like := database.LikeContains(search)
	if digits := uw.NormalizePhone(search); digits != "" {
		return `(m.display_name ILIKE ? ESCAPE '\' OR m.phone LIKE ? ESCAPE '\')`, []any{like, database.LikeContains(digits)}
	}
	return `m.display_name ILIKE ? ESCAPE '\'`, []any{like}
}

func linkLeads(tx *gorm.DB, g *uwg.Group, members []uwg.Member, at time.Time) error {
	byNumber := map[string][]string{}
	for _, m := range members {
		if m.LeadID != "" {
			continue
		}
		for _, n := range m.LeadNumbers() {
			byNumber[n] = append(byNumber[n], m.PersonKey)
		}
	}
	if len(byNumber) == 0 {
		return nil
	}
	numbers := make([]string, 0, len(byNumber))
	for n := range byNumber {
		numbers = append(numbers, n)
	}
	type leadRow struct {
		ID     string
		Number string
	}
	var leads []leadRow
	if err := tx.Raw(`SELECT id, number FROM leads WHERE workspace_id = ? AND number IN ? AND deleted_at IS NULL`,
		g.WorkspaceID, numbers).Scan(&leads).Error; err != nil {
		return err
	}
	assigned := map[string]string{}
	for _, l := range leads {
		for _, key := range byNumber[l.Number] {
			if _, done := assigned[key]; !done {
				assigned[key] = l.ID
			}
		}
	}
	for key, leadID := range assigned {
		if err := tx.Model(&schema.UnofficialWhatsAppGroupMember{}).
			Where("group_id = ? AND person_key = ? AND lead_id IS NULL", g.ID, key).
			Updates(map[string]any{"lead_id": leadID, "updated_at": at}).Error; err != nil {
			return err
		}
	}
	return nil
}

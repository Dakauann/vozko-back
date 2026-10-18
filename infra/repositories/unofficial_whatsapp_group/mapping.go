package unofficial_whatsapp_group_repository

import (
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/database/schema"
)

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func groupToRow(g *uwg.Group) schema.UnofficialWhatsAppWorkspaceGroup {
	return schema.UnofficialWhatsAppWorkspaceGroup{
		ID:                g.ID,
		WorkspaceID:       g.WorkspaceID,
		JID:               g.JID,
		Subject:           truncate(g.Subject, 255),
		Description:       truncate(g.Description, 1024),
		Origin:            string(g.Origin),
		Category:          g.Category,
		Announce:          g.Announce,
		Locked:            g.Locked,
		MemberCount:       g.MemberCount,
		PictureURL:        truncate(g.PictureURL, 1024),
		CreatedByID:       optional(g.CreatedByID),
		PrimaryInstanceID: optional(g.PrimaryInstanceID),
		SeriesID:          optional(g.SeriesID),
		SeriesSequence:    g.SeriesSequence,
		FollowedAt:        g.FollowedAt,
		MembersSyncedAt:   g.MembersSyncedAt,
	}
}

func groupToDomain(r *schema.UnofficialWhatsAppWorkspaceGroup) *uwg.Group {
	return &uwg.Group{
		ID:                r.ID,
		WorkspaceID:       r.WorkspaceID,
		JID:               r.JID,
		Subject:           r.Subject,
		Description:       r.Description,
		Origin:            uwg.Origin(r.Origin),
		Category:          r.Category,
		Announce:          r.Announce,
		Locked:            r.Locked,
		MemberCount:       r.MemberCount,
		PictureURL:        r.PictureURL,
		CreatedByID:       value(r.CreatedByID),
		PrimaryInstanceID: value(r.PrimaryInstanceID),
		SeriesID:          value(r.SeriesID),
		SeriesSequence:    r.SeriesSequence,
		FollowedAt:        r.FollowedAt,
		MembersSyncedAt:   r.MembersSyncedAt,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

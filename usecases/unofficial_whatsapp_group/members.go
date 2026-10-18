package unofficial_whatsapp_group_usecase

import (
	"context"
	"strings"
	"time"

	"vozko/domain/report"
	"vozko/domain/shared"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func (s *Service) ListMembers(ctx context.Context, a Actor, groupID string, f uwg.MemberFilter) (*shared.PaginatedResult[*uwg.MemberRow], error) {
	n, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	f.GroupID, f.Scope = g.ID, n.scope
	return s.d.Members.List(ctx, a.WorkspaceID, f)
}

func (s *Service) GroupCampaigns(ctx context.Context, a Actor, groupID string) ([]uwg.GroupCampaign, error) {
	n, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	return s.d.Campaigns.ForGroup(ctx, a.WorkspaceID, g.JID, n.scope, shared.MaxPageSize)
}

func (s *Service) AddMembersToLeads(ctx context.Context, a Actor, groupID string, personKeys []string) (*LeadLinkResult, error) {
	_, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	members, err := s.d.Members.ByPersonKeys(ctx, g.ID, personKeys)
	if err != nil {
		return nil, err
	}
	result := &LeadLinkResult{}
	for _, m := range members {
		switch {
		case m.LeadID != "":
			result.AlreadyLinked++
		case m.Hidden():
			result.WithoutPhone++
		default:
			leadID, err := s.d.Leads.EnsureLeadForPhone(ctx, a.WorkspaceID, m.Phone, m.DisplayName)
			if err != nil {
				return result, err
			}
			if err := s.d.Members.SetLead(ctx, g.ID, m.PersonKey, leadID); err != nil {
				return result, err
			}
			result.Linked++
		}
	}
	return result, nil
}

type MemberExport struct {
	FileName string
	Document string
}

func (s *Service) ExportMembers(ctx context.Context, a Actor, groupID string) (*MemberExport, error) {
	n, g, err := s.visibleGroup(ctx, a, groupID)
	if err != nil {
		return nil, err
	}
	sheet := report.CSVSection{Header: []string{"Nome", "Telefone", "JID", "Status", "Papel", "Entrou em", "Saiu em", "Lead", "Outros grupos", "Seu número"}}
	for page := 1; ; page++ {
		rows, err := s.d.Members.List(ctx, a.WorkspaceID, uwg.MemberFilter{GroupID: g.ID, Scope: n.scope,
			Pagination: shared.Pagination{Page: page, PageSize: shared.MaxPageSize}})
		if err != nil {
			return nil, err
		}
		for _, m := range rows.Items {
			sheet.Rows = append(sheet.Rows, []report.CSVCell{
				report.Text(memberName(m)), report.Text(m.Phone), report.Text(m.JID), report.Text(memberStatusLabel(m.Status)),
				report.Text(memberRoleLabel(m.Member)), report.Text(formatDate(m.JoinedAt)), report.Text(formatDate(m.LeftAt)),
				report.Text(m.LeadName), report.Int(int64(m.OtherGroups)), report.Text(yesNo(m.OurNumber)),
			})
		}
		if page >= rows.TotalPages || len(rows.Items) == 0 {
			break
		}
	}
	return &MemberExport{FileName: report.Filename("csv", "membros", strings.ToLower(g.Subject)), Document: report.BuildCSVDocument([]report.CSVSection{sheet}, true)}, nil
}

func memberName(m *uwg.MemberRow) string {
	if strings.TrimSpace(m.DisplayName) == "" && m.ContactName != "" {
		return m.ContactName
	}
	return m.Label()
}

func memberStatusLabel(status uwg.MemberStatus) string {
	if status == uwg.MemberActive {
		return "Ativo"
	}
	return "Saiu"
}

func memberRoleLabel(m uwg.Member) string {
	if m.IsAdmin() {
		return "Admin"
	}
	return "Membro"
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func yesNo(v bool) string {
	if v {
		return "Sim"
	}
	return "Não"
}

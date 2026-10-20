package unofficial_whatsapp_group_usecase

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"vozko/domain/report"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

const exportDateLayout = "02/01/2006 15:04"

var memberExportHeader = []string{
	"Grupo", "JID do grupo", "Nossos números no grupo", "Nome", "Telefone", "JID", "LID", "Status", "Papel",
	"Entrou em", "Saiu em", "Lead", "Outros grupos", "Seu número",
}

type MemberExport struct {
	FileName string
	Document string
}

type MemberExportInput struct {
	GroupIDs []string
	SeriesID string
}

func (s *Service) ExportMembers(ctx context.Context, a Actor, groupID string) (*MemberExport, error) {
	return s.ExportGroupMembers(ctx, a, MemberExportInput{GroupIDs: []string{groupID}})
}

func (s *Service) ExportGroupMembers(ctx context.Context, a Actor, in MemberExportInput) (*MemberExport, error) {
	n, err := s.numbersOf(ctx, a)
	if err != nil {
		return nil, err
	}
	groups, err := s.exportedGroups(ctx, a.WorkspaceID, n, in)
	if err != nil {
		return nil, err
	}
	jids := make([]string, 0, len(groups))
	for _, g := range groups {
		jids = append(jids, g.JID)
	}
	seats, err := s.d.Seats.ListByJIDs(ctx, a.WorkspaceID, jids)
	if err != nil {
		return nil, err
	}
	loc := s.location(ctx, a.WorkspaceID)
	sheet := report.CSVSection{Header: memberExportHeader}
	for _, g := range groups {
		ours := strings.Join(seatLabels(numberSeats(seats[g.JID], n)), ", ")
		for page := 1; ; page++ {
			rows, err := s.d.Members.List(ctx, a.WorkspaceID, uwg.MemberFilter{GroupID: g.ID, Scope: n.scope,
				Pagination: shared.Pagination{Page: page, PageSize: shared.MaxPageSize}})
			if err != nil {
				return nil, err
			}
			for _, m := range rows.Items {
				sheet.Rows = append(sheet.Rows, []report.CSVCell{
					report.Text(g.Subject), report.Text(g.JID), report.Text(ours),
					report.Text(memberName(m)), report.Text(m.Phone), report.Text(m.JID), report.Text(m.LID),
					report.Text(memberStatusLabel(m.Status)), report.Text(memberRoleLabel(m.Role)),
					report.Text(formatDate(m.JoinedAt, loc)), report.Text(formatDate(m.LeftAt, loc)),
					report.Text(m.LeadName), report.Int(int64(m.OtherGroups)), report.Text(ourNumberLabel(m.Member, n)),
				})
			}
			if page >= rows.TotalPages || len(rows.Items) == 0 {
				break
			}
		}
	}
	return &MemberExport{FileName: exportFileName(groups, in.SeriesID), Document: report.BuildCSVDocument([]report.CSVSection{sheet}, true)}, nil
}

func (s *Service) exportedGroups(ctx context.Context, workspaceID string, n numbers, in MemberExportInput) ([]*uwg.Group, error) {
	ids := shared.DistinctTrimmed(in.GroupIDs)
	seriesID := strings.TrimSpace(in.SeriesID)
	if len(ids) == 0 && seriesID == "" {
		return nil, uwg.ErrNoGroupsToExport
	}
	var out []*uwg.Group
	seen := map[string]bool{}
	if len(ids) > 0 {
		found, err := s.d.Groups.FindByIDs(ctx, workspaceID, ids, n.scope)
		if err != nil {
			return nil, err
		}
		if len(found) != len(ids) {
			return nil, uwg.ErrGroupNotFound
		}
		byID := make(map[string]*uwg.Group, len(found))
		for _, g := range found {
			byID[g.ID] = g
		}
		for _, id := range ids {
			out, seen[id] = append(out, byID[id]), true
		}
	}
	if seriesID == "" {
		return out, nil
	}
	series, err := s.seriesGroups(ctx, workspaceID, n, seriesID)
	if err != nil {
		return nil, err
	}
	if len(series) == 0 {
		return nil, uwg.ErrGroupNotFound
	}
	for _, g := range series {
		if !seen[g.ID] {
			out, seen[g.ID] = append(out, g), true
		}
	}
	return out, nil
}

func (s *Service) seriesGroups(ctx context.Context, workspaceID string, n numbers, seriesID string) ([]*uwg.Group, error) {
	var out []*uwg.Group
	for page := 1; ; page++ {
		result, err := s.d.Groups.List(ctx, uwg.GroupFilter{WorkspaceID: workspaceID, Scope: n.scope, SeriesID: seriesID,
			Sort: uwg.SortOldest, Pagination: shared.Pagination{Page: page, PageSize: shared.MaxPageSize}})
		if err != nil {
			return nil, err
		}
		out = append(out, result.Items...)
		if page >= result.TotalPages || len(result.Items) == 0 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SeriesSequence < out[j].SeriesSequence })
	return out, nil
}

func seatLabels(seats []NumberSeat) []string {
	labels := make([]string, 0, len(seats))
	for _, seat := range seats {
		labels = append(labels, seat.Label)
	}
	return labels
}

func ourNumberLabel(m uwg.Member, n numbers) string {
	if !m.OurNumber {
		return "Não"
	}
	for _, instance := range n.inScope {
		if m.AnswersTo([]string{instance.PhoneNumber, instance.JID, instance.LID}) {
			return instance.Label()
		}
	}
	return "Sim"
}

func exportFileName(groups []*uwg.Group, seriesID string) string {
	switch {
	case strings.TrimSpace(seriesID) != "" && len(groups) > 0:
		return report.Filename("csv", "membros", "serie", strings.ToLower(groups[0].Subject))
	case len(groups) == 1:
		return report.Filename("csv", "membros", strings.ToLower(groups[0].Subject))
	}
	return report.Filename("csv", "membros", strconv.Itoa(len(groups)), "grupos")
}

func memberName(m *uwg.MemberRow) string {
	if strings.TrimSpace(m.DisplayName) == "" && m.ContactName != "" {
		return m.ContactName
	}
	return m.Label()
}

func memberStatusLabel(status uwg.MemberStatus) string {
	switch status {
	case uwg.MemberActive:
		return "Ativo"
	case uwg.MemberRemoved:
		return "Removido"
	}
	return "Saiu"
}

func memberRoleLabel(role uw.GroupRole) string {
	switch role {
	case uw.GroupRoleSuperAdmin:
		return "Superadmin"
	case uw.GroupRoleAdmin:
		return "Admin"
	}
	return "Membro"
}

func formatDate(t *time.Time, loc *time.Location) string {
	if t == nil {
		return ""
	}
	return t.In(loc).Format(exportDateLayout)
}

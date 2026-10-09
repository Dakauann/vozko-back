package actor_repository

import (
	"context"
	"strings"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

const membersByEmailSQL = "SELECT lower(u.email) AS email, u.id::text AS id FROM users u" +
	" JOIN workspace_members m ON m.user_id = u.id" +
	" WHERE m.workspace_id = ? AND u.disabled_at IS NULL AND lower(u.email) = ANY(?)"

type MemberEmails struct {
	db *gorm.DB
}

func NewMemberEmails(db *gorm.DB) *MemberEmails {
	return &MemberEmails{db: db}
}

type memberEmailRow struct {
	Email string
	ID    string
}

func (m *MemberEmails) UserIDsByEmail(ctx context.Context, workspaceID string, emails []string) (map[string]string, error) {
	out := map[string]string{}
	wanted := make(pq.StringArray, 0, len(emails))
	for _, e := range emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			wanted = append(wanted, e)
		}
	}
	if strings.TrimSpace(workspaceID) == "" || len(wanted) == 0 {
		return out, nil
	}
	var rows []memberEmailRow
	if err := m.db.WithContext(ctx).Raw(membersByEmailSQL, workspaceID, wanted).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.Email] = row.ID
	}
	return out, nil
}

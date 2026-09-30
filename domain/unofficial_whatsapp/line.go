package unofficial_whatsapp

import (
	"context"
	"strings"
)

type LineTransfer struct {
	Contacts          int
	ContactsMerged    int
	Conversations     int
	ConversationsKept int
	Groups            int
}

func (t LineTransfer) Moved() bool {
	return t.Contacts+t.ContactsMerged+t.Conversations+t.Groups > 0
}

type LineRepository interface {
	ListSameNumber(ctx context.Context, instance *Instance) ([]*Instance, error)
	Transfer(ctx context.Context, fromInstanceID, toInstanceID string) (LineTransfer, error)
}

func BareJID(jid string) string {
	jid = strings.TrimSpace(jid)
	user, domain, found := strings.Cut(jid, "@")
	if !found {
		return jid
	}
	user, _, _ = strings.Cut(user, ":")
	return user + "@" + domain
}

func SameLine(a, b *Instance) bool {
	if a == nil || b == nil || a.ID == b.ID {
		return false
	}
	if a.WorkspaceID == "" || a.WorkspaceID != b.WorkspaceID {
		return false
	}
	if a.PhoneNumber == "" || a.PhoneNumber != b.PhoneNumber {
		return false
	}
	return sameDepartment(a.DepartmentID, b.DepartmentID)
}

func InheritsLine(successor, predecessor *Instance) bool {
	if !SameLine(successor, predecessor) || successor.Status != StatusConnected {
		return false
	}
	if predecessor.Status == StatusBanned {
		return false
	}
	return predecessor.DeletedAt != nil || predecessor.Status != StatusConnected
}

func sameDepartment(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

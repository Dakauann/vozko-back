package notification_usecase

import (
	"log"
	"strings"

	"vozko/domain/notification"
	"vozko/domain/user"
	"vozko/domain/workspace"
)

// Narrow readers so the resolver depends only on what it needs (and is easy to
// fake in tests); the concrete repositories satisfy them implicitly.
type workspaceOwnerReader interface {
	GetWorkspaceByID(id string) (*workspace.Workspace, error)
}

type userEmailReader interface {
	FindByID(id string) (*user.User, error)
}

type ownerEmailResolver struct {
	workspaces workspaceOwnerReader
	users      userEmailReader
}

func NewOwnerEmailResolver(workspaces workspaceOwnerReader, users userEmailReader) notification.OwnerEmailResolver {
	return &ownerEmailResolver{workspaces: workspaces, users: users}
}

func (r *ownerEmailResolver) OwnerEmail(workspaceID string) (string, error) {
	ws, err := r.workspaces.GetWorkspaceByID(workspaceID)
	if err != nil {
		return "", err
	}
	if ws == nil || ws.OwnerID == "" {
		return "", nil
	}
	u, err := r.users.FindByID(ws.OwnerID)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", nil
	}
	return u.Email, nil
}

type notifier struct {
	publisher notification.PublishEmailUseCase
	dedup     notification.Dedup
	resolver  notification.OwnerEmailResolver
}

func NewNotifier(publisher notification.PublishEmailUseCase, dedup notification.Dedup, resolver notification.OwnerEmailResolver) notification.Notifier {
	return &notifier{publisher: publisher, dedup: dedup, resolver: resolver}
}

func (n *notifier) Notify(req notification.Notification) error {
	email := strings.TrimSpace(req.Email)
	if email == "" && req.WorkspaceID != "" && n.resolver != nil {
		resolved, err := n.resolver.OwnerEmail(req.WorkspaceID)
		if err != nil {
			log.Printf("[notify] resolve owner email for workspace %s failed: %v", req.WorkspaceID, err)
			return err
		}
		email = strings.TrimSpace(resolved)
	}
	if email == "" {
		// No recipient (e.g. unassigned workspace): nothing to send, not an error.
		return nil
	}

	if req.DedupKey != "" && n.dedup != nil {
		first, err := n.dedup.FirstTime(req.DedupKey, req.DedupTTL)
		if err != nil {
			log.Printf("[notify] dedup check failed for %s (suppressing to avoid spam): %v", req.DedupKey, err)
			return err
		}
		if !first {
			return nil // already notified for this logical event
		}
	}

	if err := n.publisher.Publish(email, req.Subject, req.Template, req.Placeholders); err != nil {
		if req.DedupKey != "" && n.dedup != nil {
			_ = n.dedup.Clear(req.DedupKey) // release the guard so a retry can re-send
		}
		return err
	}
	return nil
}

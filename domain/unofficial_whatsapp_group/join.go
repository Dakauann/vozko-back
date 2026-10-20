package unofficial_whatsapp_group

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNoNumbersSelected = errors.New("choose at least one number")
	ErrJoinNotFound      = errors.New("join not found")
)

const (
	JoinSpacing     = 10 * time.Minute
	JoinRetryDelay  = time.Minute
	MaxJoinAttempts = 3
)

type JoinStatus string

const (
	JoinPending   JoinStatus = "pending"
	JoinRunning   JoinStatus = "running"
	JoinDone      JoinStatus = "done"
	JoinFailed    JoinStatus = "failed"
	JoinCancelled JoinStatus = "cancelled"
)

func (s JoinStatus) Terminal() bool {
	return s == JoinDone || s == JoinFailed || s == JoinCancelled
}

type JoinTask struct {
	ID          string     `json:"id"`
	PlanID      string     `json:"planId"`
	WorkspaceID string     `json:"workspaceId"`
	InstanceID  string     `json:"instanceId"`
	InviteLink  string     `json:"inviteLink"`
	GroupJID    string     `json:"groupJid,omitempty"`
	Sequence    int        `json:"sequence"`
	Follow      bool       `json:"follow"`
	Category    string     `json:"category,omitempty"`
	Status      JoinStatus `json:"status"`
	Attempts    int        `json:"attempts"`
	Error       string     `json:"error,omitempty"`
	DueAt       time.Time  `json:"dueAt"`
	CreatedByID string     `json:"createdById,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LeaseOwner  string     `json:"-"`
	LeaseUntil  *time.Time `json:"-"`
}

func (t *JoinTask) Lease(owner string, until time.Time) {
	t.Status, t.LeaseOwner, t.LeaseUntil = JoinRunning, owner, &until
}

func (t JoinTask) Anchor() bool {
	return t.Sequence == 0
}

func (t JoinTask) CategoryFor(current string) string {
	if t.Category == "" || (current != "" && !t.Anchor()) {
		return current
	}
	return t.Category
}

func (t *JoinTask) Retry(now time.Time, reason string) {
	t.Attempts++
	t.Error = reason
	if t.Attempts >= MaxJoinAttempts {
		t.Status = JoinFailed
		return
	}
	t.Status = JoinPending
	t.DueAt = now.Add(JoinRetryDelay)
}

type JoinPlanInput struct {
	PlanID      string
	WorkspaceID string
	InviteLink  string
	InstanceIDs []string
	Follow      bool
	Category    string
	CreatedByID string
}

func PlanJoins(in JoinPlanInput, now time.Time) ([]JoinTask, error) {
	link, err := NormalizeInviteLink(in.InviteLink)
	if err != nil {
		return nil, err
	}
	category, err := NormalizeCategory(in.Category)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var tasks []JoinTask
	for _, id := range in.InstanceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		seq := len(tasks)
		tasks = append(tasks, JoinTask{
			PlanID: in.PlanID, WorkspaceID: in.WorkspaceID, InstanceID: id, InviteLink: link,
			Sequence: seq, Follow: in.Follow, Category: category, Status: JoinPending, CreatedByID: in.CreatedByID,
			DueAt: now.Add(time.Duration(seq) * JoinSpacing),
		})
	}
	if len(tasks) == 0 {
		return nil, ErrNoNumbersSelected
	}
	return tasks, nil
}

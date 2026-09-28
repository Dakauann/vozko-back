package balance

import (
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/billing"
	"vozko/domain/user"
)

var (
	ErrMonthlySendCapReached    = errors.New("monthly template send cap reached")
	ErrMonthlySendCapNotFound   = errors.New("monthly template send cap not found")
	ErrSendCapUnlockRequired    = errors.New("raising or removing the monthly send cap requires an unlock")
	ErrInvalidUnlockCode        = errors.New("invalid unlock code")
	ErrInvalidSendCapLimit      = errors.New("monthly send cap limit must be positive")
	ErrSendCapWorkspaceRequired = errors.New("monthly send cap requires a workspace")
	ErrInvalidSendCapLevel      = errors.New("invalid monthly send cap level")
	ErrSendCapForbidden         = errors.New("not allowed to manage monthly send caps")
)

const sendCapNearPercent = 80

type MonthlySendCap struct {
	WorkspaceID string
	Limit       int64
	UpdatedBy   string
	UpdatedAt   time.Time
	UnlockedBy  *string
	UnlockedAt  *time.Time
}

type SendCapLevel string

const (
	SendCapLevelOK      SendCapLevel = "ok"
	SendCapLevelNear    SendCapLevel = "near"
	SendCapLevelReached SendCapLevel = "reached"
)

type SendCapUsage struct {
	Cap           MonthlySendCap
	WorkspaceName string
	Used          int64
}

type MonthlySendCapReader interface {
	GetMonthlySendCap(workspaceID string) (*MonthlySendCap, error)
}

type MonthlySendSlots interface {
	TakeMonthlySendSlot(workspaceID, referenceID string, period time.Time) (bool, error)
	GiveBackMonthlySendSlot(workspaceID, referenceID string) error
}

type MonthlySendCapRepository interface {
	MonthlySendCapReader
	UpsertMonthlySendCap(cap MonthlySendCap) error
	DeleteMonthlySendCap(workspaceID string) error
	ListMonthlySendCapUsage(since time.Time) ([]SendCapUsage, error)
}

func NewMonthlySendCap(workspaceID string, limit int64, updatedBy string, now time.Time) (MonthlySendCap, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return MonthlySendCap{}, ErrSendCapWorkspaceRequired
	}
	return MonthlySendCap{WorkspaceID: workspaceID}.Relimited(limit, updatedBy, now)
}

func (c MonthlySendCap) Relimited(limit int64, updatedBy string, now time.Time) (MonthlySendCap, error) {
	if limit <= 0 {
		return MonthlySendCap{}, ErrInvalidSendCapLimit
	}
	c.Limit = limit
	c.UpdatedBy = updatedBy
	c.UpdatedAt = now
	return c, nil
}

func (c MonthlySendCap) Unlocked(limit int64, unlockedBy string, now time.Time) (MonthlySendCap, error) {
	relimited, err := c.Relimited(limit, unlockedBy, now)
	if err != nil {
		return MonthlySendCap{}, err
	}
	relimited.UnlockedBy = &unlockedBy
	relimited.UnlockedAt = &now
	return relimited, nil
}

func SendCapMonthStart(now time.Time) time.Time {
	local := now.In(billing.LocationBRT())
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
}

func (c MonthlySendCap) CheckRoom(used int64) error {
	if used >= c.Limit {
		return ErrMonthlySendCapReached
	}
	return nil
}

func SendCapChangeRequiresUnlock(current *MonthlySendCap, newLimit *int64) bool {
	if current == nil {
		return false
	}
	if newLimit == nil {
		return true
	}
	return *newLimit > current.Limit
}

func (u SendCapUsage) Remaining() int64 {
	if u.Used >= u.Cap.Limit {
		return 0
	}
	return u.Cap.Limit - u.Used
}

func (u SendCapUsage) Level() SendCapLevel {
	switch {
	case u.Used >= u.Cap.Limit:
		return SendCapLevelReached
	case u.Used*100 >= u.Cap.Limit*sendCapNearPercent:
		return SendCapLevelNear
	default:
		return SendCapLevelOK
	}
}

func (u SendCapUsage) Matches(level SendCapLevel) bool {
	return level == "" || u.Level() == level
}

func ParseSendCapLevel(raw string) (SendCapLevel, error) {
	level := SendCapLevel(strings.ToLower(strings.TrimSpace(raw)))
	switch level {
	case "", SendCapLevelOK, SendCapLevelNear, SendCapLevelReached:
		return level, nil
	default:
		return "", ErrInvalidSendCapLevel
	}
}

func VerifySendCapUnlockCode(email, code string) error {
	if !user.VerifySuperAdminPin(email, code) {
		return ErrInvalidUnlockCode
	}
	return nil
}

type SendCapActor struct {
	UserID      string
	Email       string
	SystemAdmin bool
}

func (a SendCapActor) CanManage() bool {
	return a.SystemAdmin && strings.TrimSpace(a.UserID) != ""
}

func (a SendCapActor) CanUnlock() bool {
	return a.CanManage() && user.IsSuperAdmin(a.Email)
}

func SortSendCapUsageByPressure(usages []SendCapUsage) {
	sort.SliceStable(usages, func(i, j int) bool {
		left := usages[i].Used * usages[j].Cap.Limit
		right := usages[j].Used * usages[i].Cap.Limit
		if left != right {
			return left > right
		}
		return usages[i].WorkspaceName < usages[j].WorkspaceName
	})
}

type SendCapListing struct {
	MonthStart time.Time
	Items      []SendCapUsage
	CanUnlock  bool
}

type UnlockMonthlySendCapInput struct {
	WorkspaceID string
	Limit       *int64
	Code        string
}

type ListMonthlySendCapsUseCase interface {
	Execute(actor SendCapActor, level SendCapLevel) (*SendCapListing, error)
}

type SetMonthlySendCapUseCase interface {
	Execute(actor SendCapActor, workspaceID string, limit int64) (*MonthlySendCap, error)
}

type UnlockMonthlySendCapUseCase interface {
	Execute(actor SendCapActor, input UnlockMonthlySendCapInput) (*MonthlySendCap, error)
}

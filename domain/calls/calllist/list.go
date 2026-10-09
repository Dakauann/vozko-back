package calllist

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

type Status string

const (
	StatusBuilding Status = "building"
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusArchived Status = "archived"
	StatusFailed   Status = "failed"
)

func (s Status) Valid() bool {
	switch s {
	case StatusBuilding, StatusActive, StatusPaused, StatusArchived, StatusFailed:
		return true
	}
	return false
}

const (
	MaxNameLength    = 120
	MaxAssignees     = 100
	MaxItems         = 100_000
	BuildLeaseStale  = 2 * time.Minute
	MaxBuildAttempts = 5
)

const (
	FailureBuild = "build_failed"
	FailureEmpty = "no_callable_lead"
)

type PhoneSource string

const (
	PhoneIdentity PhoneSource = "identity"
	PhoneContact  PhoneSource = "contact"
)

type PhoneChoice struct {
	Source PhoneSource     `json:"source"`
	Label  lead.PhoneLabel `json:"label,omitempty"`
}

func (c PhoneChoice) Validate() error {
	switch {
	case c.Source == PhoneIdentity && c.Label == "":
		return nil
	case c.Source == PhoneContact && c.Label.Valid():
		return nil
	}
	return ErrPhoneChoiceInvalid
}

func (c PhoneChoice) Pick(l *lead.Lead) string {
	if l == nil {
		return ""
	}
	for _, number := range l.DialNumbers() {
		if c.Source == PhoneIdentity && number.Identity {
			return number.Number
		}
		if c.Source == PhoneContact && !number.Identity && number.Label == c.Label {
			return number.Number
		}
	}
	return ""
}

type SkipReason string

const (
	SkipGone     SkipReason = "gone"
	SkipNoNumber SkipReason = SkipReason(lead.DialRefusedNoNumber)
)

type Skips map[SkipReason]int

func (r SkipReason) Reversible() bool {
	switch lead.DialRefusalReason(r) {
	case lead.DialRefusedUnknownPurpose:
		return true
	}
	return false
}

func (s Skips) Add(reason SkipReason) {
	if reason != "" {
		s[reason]++
	}
}

func (s Skips) Merge(other Skips) {
	for reason, count := range other {
		if reason != "" && count > 0 {
			s[reason] += count
		}
	}
}

func (s Skips) Total() int {
	total := 0
	for _, count := range s {
		total += count
	}
	return total
}

func Admission(l *lead.Lead, choice PhoneChoice, identities []*lead.Lead, c lead.DialContext) (string, SkipReason) {
	if l == nil {
		return "", SkipGone
	}
	number := choice.Pick(l)
	if number == "" {
		return "", SkipNoNumber
	}
	if reason := Readmit(l, number, identities, c); reason != "" {
		return "", reason
	}
	return number, ""
}

func Readmit(l *lead.Lead, phone string, identities []*lead.Lead, c lead.DialContext) SkipReason {
	if l == nil {
		return SkipGone
	}
	err := lead.CheckLeadDial(l, phone, identities, c)
	if err == nil {
		return ""
	}
	var refusal *lead.DialRefusal
	if errors.As(err, &refusal) {
		return SkipReason(refusal.Reason)
	}
	return SkipReason(lead.DialRefusedUnknownPurpose)
}

type Draft struct {
	ID          string
	WorkspaceID string
	Name        string
	CreatedBy   string
	AssigneeIDs []string
	Phone       PhoneChoice
}

func normalizedAssignees(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (d Draft) Normalized() Draft {
	d.ID = strings.TrimSpace(d.ID)
	d.WorkspaceID = strings.TrimSpace(d.WorkspaceID)
	d.Name = strings.TrimSpace(d.Name)
	d.CreatedBy = strings.TrimSpace(d.CreatedBy)
	d.AssigneeIDs = normalizedAssignees(d.AssigneeIDs)
	d.Phone.Label = lead.PhoneLabel(strings.TrimSpace(string(d.Phone.Label)))
	return d
}

func validateName(name string) error {
	switch {
	case name == "":
		return ErrNameRequired
	case utf8.RuneCountInString(name) > MaxNameLength:
		return ErrNameTooLong
	}
	return nil
}

func validateAssignees(ids []string) error {
	switch {
	case len(ids) == 0:
		return ErrAssigneesRequired
	case len(ids) > MaxAssignees:
		return ErrTooManyAssignees
	}
	return nil
}

func (d Draft) Validate() error {
	if d.WorkspaceID == "" {
		return ErrWorkspaceRequired
	}
	if d.CreatedBy == "" {
		return ErrActorRequired
	}
	if err := validateName(d.Name); err != nil {
		return err
	}
	if err := validateAssignees(d.AssigneeIDs); err != nil {
		return err
	}
	return d.Phone.Validate()
}

type List struct {
	ID            string
	WorkspaceID   string
	Name          string
	CreatedBy     string
	AssigneeIDs   []string
	Status        Status
	Phone         PhoneChoice
	Selected      int
	ItemCount     int
	ClosedCount   int
	CalledCount   int
	CallbackCount int
	Skipped       Skips
	FailureCode   string
	Build         shared.Lease
	BuildCursor   string
	BuiltAt       *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func NewList(d Draft, selected int, now time.Time) (*List, error) {
	d = d.Normalized()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	switch {
	case selected <= 0:
		return nil, ErrSelectionEmpty
	case selected > MaxItems:
		return nil, fmt.Errorf("%w: at most %d leads", ErrSelectionTooLarge, MaxItems)
	}
	l := &List{
		ID: d.ID, WorkspaceID: d.WorkspaceID, Name: d.Name, CreatedBy: d.CreatedBy, AssigneeIDs: d.AssigneeIDs,
		Status: StatusBuilding, Phone: d.Phone, Selected: selected, Skipped: Skips{}, CreatedAt: now, UpdatedAt: now,
	}
	return l, nil
}

func (l *List) DialContext() lead.DialContext {
	return lead.DialContext{Purpose: lead.DialCallList}
}

func (l *List) AssignedTo(userID string) bool {
	if strings.TrimSpace(userID) == "" {
		return false
	}
	for _, id := range l.AssigneeIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func (l *List) VisibleTo(userID string, manages bool) bool {
	return manages || l.AssignedTo(userID)
}

func (l *List) Workable(userID string) error {
	switch l.Status {
	case StatusActive:
	case StatusBuilding:
		return ErrListBuilding
	default:
		return ErrListNotActive
	}
	if !l.AssignedTo(userID) {
		return ErrNotAssignee
	}
	return nil
}

func (l *List) AcceptsOutcomes() error {
	if l.Status != StatusActive && l.Status != StatusPaused {
		return ErrListNotActive
	}
	return nil
}

type Change struct {
	Name        *string
	AssigneeIDs *[]string
	Status      *Status
	By          string
}

func (c Change) Empty() bool {
	return c.Name == nil && c.AssigneeIDs == nil && c.Status == nil
}

var managedTransitions = map[Status][]Status{
	StatusActive:   {StatusActive, StatusPaused, StatusArchived},
	StatusPaused:   {StatusPaused, StatusActive, StatusArchived},
	StatusArchived: {StatusArchived, StatusActive},
}

func (l *List) canMoveTo(to Status) bool {
	for _, allowed := range managedTransitions[l.Status] {
		if allowed == to {
			return true
		}
	}
	return false
}

func (l *List) Change(c Change, now time.Time) error {
	if c.Empty() {
		return ErrNothingToChange
	}
	switch l.Status {
	case StatusBuilding:
		return ErrListBuilding
	case StatusFailed:
		return ErrStatusTransition
	}
	next := *l
	if c.Name != nil {
		name := strings.TrimSpace(*c.Name)
		if err := validateName(name); err != nil {
			return err
		}
		next.Name = name
	}
	if c.AssigneeIDs != nil {
		assignees := normalizedAssignees(*c.AssigneeIDs)
		if err := validateAssignees(assignees); err != nil {
			return err
		}
		next.AssigneeIDs = assignees
	}
	if c.Status != nil {
		if !l.canMoveTo(*c.Status) {
			return fmt.Errorf("%w: from %s to %s", ErrStatusTransition, l.Status, *c.Status)
		}
		next.Status = *c.Status
	}
	next.UpdatedAt = now
	*l = next
	return nil
}

func (l *List) BuildClaimable(now time.Time) bool {
	return l.Status == StatusBuilding && l.Build.Claimable(now, BuildLeaseStale, MaxBuildAttempts)
}

package calllist

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrLeadNotAdmitted = errors.New("call list: the lead cannot be called from this list")
	ErrListFull        = errors.New("call list: the list already holds as many leads as a call list can")
)

type AdmissionRefusal struct {
	Reason SkipReason
}

func AdmissionRefused(reason SkipReason) *AdmissionRefusal {
	return &AdmissionRefusal{Reason: reason}
}

func (r *AdmissionRefusal) Error() string {
	return fmt.Sprintf("%s (%s)", ErrLeadNotAdmitted.Error(), r.Reason)
}

func (r *AdmissionRefusal) Is(target error) bool {
	return target == ErrLeadNotAdmitted
}

func (l *List) AcceptsNewLeads() error {
	switch l.Status {
	case StatusActive, StatusPaused:
	case StatusBuilding:
		return ErrListBuilding
	default:
		return ErrListNotActive
	}
	if l.ItemCount >= MaxItems {
		return ErrListFull
	}
	return nil
}

type LeadAddition struct {
	WorkspaceID string
	ListID      string
	LeadID      string
}

type Addition struct {
	WorkspaceID string
	ListID      string
	Items       []Item
	At          time.Time
}

package callhistory_usecase

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"vozko/domain/callrouting"
	"vozko/domain/calls/billing"
	"vozko/domain/calls/callhistory"
	"vozko/domain/calls/cdr"
	"vozko/domain/calls/recordings"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

var ErrWorkspaceRequired = errors.New("call history needs a workspace")

type CallRecords interface {
	List(filters cdr.ListFilters) (*shared.PaginatedResult[*cdr.Call], error)
	GetByCallID(callID string) (*cdr.Call, error)
}

type Charges interface {
	GetByCallIDs(callIDs []string) (map[string]*billing.CallBillingRecord, error)
}

type Recordings interface {
	GetByCallID(callID string) (*recordings.CallRecord, error)
}

type Contacts interface {
	FindByNumbers(workspaceID string, numbers []string) ([]*lead.Lead, error)
}

type Queues interface {
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*callrouting.Queue, error)
}

type Deps struct {
	Calls      CallRecords
	Transfers  callrouting.TransferHistory
	Charges    Charges
	Recordings Recordings
	Contacts   Contacts
	Names      callrouting.MemberNames
	Queues     Queues
}

type Viewer struct {
	WorkspaceID     string
	UserID          string
	SeesEveryone    bool
	HearsRecordings bool
}

type ListInput struct {
	Viewer    Viewer
	Page      int
	PageSize  int
	Direction *cdr.Direction
	Channel   *callhistory.Channel
	Answered  *bool
	MemberID  string
	From      *time.Time
	To        *time.Time
	Number    string
}

type History struct{ deps Deps }

func NewHistory(deps Deps) *History { return &History{deps: deps} }

func (h *History) List(ctx context.Context, input ListInput) (*shared.PaginatedResult[callhistory.Summary], error) {
	if strings.TrimSpace(input.Viewer.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	page, err := h.deps.Calls.List(listFilters(input))
	if err != nil {
		return nil, err
	}
	calls := make([]cdr.Call, len(page.Items))
	for i, call := range page.Items {
		calls[i] = *call
	}
	facts, err := h.gather(ctx, input.Viewer.WorkspaceID, calls)
	if err != nil {
		return nil, err
	}
	summaries := make([]callhistory.Summary, len(calls))
	for i, call := range calls {
		summaries[i] = facts.summary(call)
	}
	return &shared.PaginatedResult[callhistory.Summary]{
		Items: summaries, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages,
	}, nil
}

func (h *History) Get(ctx context.Context, viewer Viewer, callID string) (*callhistory.Detail, error) {
	if strings.TrimSpace(viewer.WorkspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	found, err := h.deps.Calls.GetByCallID(callID)
	if err != nil {
		return nil, err
	}
	if found.WorkspaceID != viewer.WorkspaceID {
		return nil, cdr.ErrCallNotFound
	}
	call := *found
	facts, err := h.gather(ctx, viewer.WorkspaceID, []cdr.Call{call})
	if err != nil {
		return nil, err
	}
	transfers := facts.transfers[call.CallID]
	people := callhistory.ParticipantsOf(call, transfers)
	if !viewer.SeesEveryone && !people.Includes(viewer.UserID) {
		return nil, cdr.ErrCallNotFound
	}
	recording, err := h.recording(viewer, call)
	if err != nil {
		return nil, err
	}
	queueNames, err := h.queueNames(ctx, viewer.WorkspaceID, transfers)
	if err != nil {
		return nil, err
	}

	detail := &callhistory.Detail{Summary: facts.summary(call)}
	for _, handler := range people.Handlers {
		detail.Handlers = append(detail.Handlers, facts.person(handler))
	}
	for _, entry := range callhistory.BuildTimeline(call, transfers, recording) {
		detail.Timeline = append(detail.Timeline, callhistory.NamedEntry{
			TimelineEntry: entry,
			Actor:         facts.personOrNil(entry.ActorID),
			Target:        facts.personOrNil(entry.TargetUserID),
			QueueName:     queueNames[entry.TargetQueueID],
		})
	}
	if recording != nil {
		detail.Recording = &callhistory.Recording{URL: recording.RecordingURL, DurationSec: recording.DurationSec}
	}
	return detail, nil
}

func listFilters(input ListInput) cdr.ListFilters {
	filters := cdr.ListFilters{
		WorkspaceID: input.Viewer.WorkspaceID,
		Direction:   input.Direction,
		Answered:    input.Answered,
		StartedFrom: input.From,
		StartedTo:   input.To,
	}
	filters.Pagination = shared.Pagination{Page: input.Page, PageSize: input.PageSize}
	switch {
	case !input.Viewer.SeesEveryone:
		filters.ParticipantID = &input.Viewer.UserID
	case strings.TrimSpace(input.MemberID) != "":
		member := strings.TrimSpace(input.MemberID)
		filters.ParticipantID = &member
	}
	if input.Channel != nil {
		source := callhistory.SourceOf(*input.Channel)
		filters.Source = &source
	}
	if digits := digitsOf(input.Number); digits != "" {
		filters.NumberDigits = &digits
	}
	return filters
}

func (h *History) recording(viewer Viewer, call cdr.Call) (*recordings.CallRecord, error) {
	if !viewer.HearsRecordings {
		return nil, nil
	}
	recording, err := h.deps.Recordings.GetByCallID(call.CallID)
	if err != nil || recording == nil || recording.WorkspaceID != call.WorkspaceID {
		return nil, err
	}
	return recording, nil
}

func (h *History) queueNames(ctx context.Context, workspaceID string, transfers []callrouting.TransferRecord) (map[string]string, error) {
	names := map[string]string{}
	if len(transfers) == 0 {
		return names, nil
	}
	queues, err := h.deps.Queues.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, queue := range queues {
		names[queue.ID] = queue.Name
	}
	return names, nil
}

func digitsOf(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

package mercadolivre

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

type QuestionStatus string

const (
	QuestionUnanswered       QuestionStatus = "UNANSWERED"
	QuestionAnswered         QuestionStatus = "ANSWERED"
	QuestionClosedUnanswered QuestionStatus = "CLOSED_UNANSWERED"
	QuestionUnderReview      QuestionStatus = "UNDER_REVIEW"
	QuestionBanned           QuestionStatus = "BANNED"
	QuestionDeleted          QuestionStatus = "DELETED"
	QuestionDisabled         QuestionStatus = "DISABLED"
)

func QuestionStatuses() []QuestionStatus {
	return []QuestionStatus{
		QuestionUnanswered, QuestionAnswered, QuestionClosedUnanswered, QuestionUnderReview,
		QuestionBanned, QuestionDeleted, QuestionDisabled,
	}
}

func ParseQuestionStatus(raw string) (QuestionStatus, error) {
	for _, s := range QuestionStatuses() {
		if string(s) == raw {
			return s, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownQuestionStatus, raw)
}

type AnswerStatus string

const (
	AnswerActive   AnswerStatus = "ACTIVE"
	AnswerDisabled AnswerStatus = "DISABLED"
	AnswerBanned   AnswerStatus = "BANNED"
)

func ParseAnswerStatus(raw string) (AnswerStatus, error) {
	switch s := AnswerStatus(raw); s {
	case AnswerActive, AnswerDisabled, AnswerBanned:
		return s, nil
	}
	return "", fmt.Errorf("%w: %q", ErrUnknownAnswerStatus, raw)
}

type AnswerSource string

const (
	AnsweredByOperator        AnswerSource = "operator"
	AnsweredByRule            AnswerSource = "rule"
	AnsweredByMercadoLivreApp AnswerSource = "mercadolivre_app"
)

func (s AnswerSource) Valid() bool {
	switch s {
	case AnsweredByOperator, AnsweredByRule, AnsweredByMercadoLivreApp:
		return true
	}
	return false
}

type Question struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	AccountID   string `json:"accountId"`
	ItemID      string `json:"itemId"`
	QuestionID  string `json:"questionId"`
	ContactID   string `json:"contactId,omitempty"`

	Text   string         `json:"text"`
	Status QuestionStatus `json:"status"`

	AnswerText       string       `json:"answerText,omitempty"`
	AnswerStatus     AnswerStatus `json:"answerStatus,omitempty"`
	AnsweredBy       AnswerSource `json:"answeredBy,omitempty"`
	AnsweredByUserID *string      `json:"answeredByUserId,omitempty"`
	RuleID           *string      `json:"ruleId,omitempty"`

	AskedAt       time.Time  `json:"askedAt"`
	AnsweredAt    *time.Time `json:"answeredAt,omitempty"`
	SuspectedSpam bool       `json:"suspectedSpam"`
	LastFetchedAt time.Time  `json:"lastFetchedAt"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (q *Question) CanAnswer() error {
	if q.Status != QuestionUnanswered {
		return fmt.Errorf("%w: %s", ErrQuestionNotOpen, q.Status)
	}
	return nil
}

func (q *Question) OwnedBy(accountID string) error {
	if accountID == "" || q.AccountID != accountID {
		return ErrQuestionNotOwned
	}
	return nil
}

func IsRuleEligible(q *Question, created bool) bool {
	return q != nil && created && q.Status == QuestionUnanswered
}

func (q *Question) Unanswered() bool { return q != nil && q.Status == QuestionUnanswered }

func QuestionFromRemote(account *Account, remote *RemoteQuestion, contactID string, now time.Time) (*Question, error) {
	if account == nil || remote == nil || !ValidNumericID(remote.ID) || !ValidItemID(remote.ItemID) {
		return nil, fmt.Errorf("%w: question", ErrInvalidResource)
	}
	if remote.SellerID == "" || remote.SellerID != account.MLUserID {
		return nil, fmt.Errorf("%w: %s", ErrQuestionNotOwned, remote.ID)
	}
	asked := remote.CreatedAt
	if asked.IsZero() {
		asked = now
	}
	q := &Question{
		WorkspaceID:   account.WorkspaceID,
		AccountID:     account.ID,
		ItemID:        remote.ItemID,
		QuestionID:    remote.ID,
		ContactID:     contactID,
		Text:          remote.Text,
		Status:        remote.Status,
		AskedAt:       asked,
		SuspectedSpam: remote.SuspectedSpam,
		LastFetchedAt: now,
	}
	if a := remote.Answer; a != nil {
		q.AnswerText, q.AnswerStatus, q.AnsweredBy = a.Text, a.Status, AnsweredByMercadoLivreApp
		if !a.CreatedAt.IsZero() {
			at := a.CreatedAt
			q.AnsweredAt = &at
		}
	}
	return q, nil
}

const (
	cursorOpen     = "u."
	cursorAnswered = "a."
)

type QuestionCursor struct {
	Unanswered bool
	AskedAt    time.Time
	ID         string
}

func CursorOf(q *Question) QuestionCursor {
	return QuestionCursor{Unanswered: q.Unanswered(), AskedAt: q.AskedAt, ID: q.ID}
}

func (c QuestionCursor) Encode() string {
	prefix := cursorAnswered
	if c.Unanswered {
		prefix = cursorOpen
	}
	return shared.Keyset{At: c.AskedAt, ID: prefix + c.ID}.Encode()
}

func ParseQuestionCursor(raw string) (*QuestionCursor, error) {
	keyset, found, err := shared.ParseKeyset(raw)
	if err != nil || !found {
		return nil, err
	}
	cursor := &QuestionCursor{AskedAt: keyset.At}
	switch {
	case strings.HasPrefix(keyset.ID, cursorOpen):
		cursor.Unanswered, cursor.ID = true, strings.TrimPrefix(keyset.ID, cursorOpen)
	case strings.HasPrefix(keyset.ID, cursorAnswered):
		cursor.ID = strings.TrimPrefix(keyset.ID, cursorAnswered)
	default:
		return nil, shared.ErrKeysetInvalid
	}
	if strings.TrimSpace(cursor.ID) == "" {
		return nil, shared.ErrKeysetInvalid
	}
	return cursor, nil
}

type QuestionList struct {
	Items []*Question
	Next  *QuestionCursor
}

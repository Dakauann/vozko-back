package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	mldomain "vozko/domain/mercadolivre"
)

const questionsAPIVersion = "4"

type QuestionService struct {
	c *Client
}

var _ mldomain.QuestionService = (*QuestionService)(nil)

func NewQuestionService(c *Client) (*QuestionService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: question service requires a client")
	}
	return &QuestionService{c: c}, nil
}

type answerWire struct {
	Text        string   `json:"text"`
	Status      string   `json:"status"`
	DateCreated flexTime `json:"date_created"`
}

type questionWire struct {
	ID                 mldomain.FlexibleID `json:"id"`
	SellerID           mldomain.FlexibleID `json:"seller_id"`
	BuyerID            mldomain.FlexibleID `json:"buyer_id"`
	From               *idRef              `json:"from"`
	ItemID             string              `json:"item_id"`
	Status             string              `json:"status"`
	Text               string              `json:"text"`
	Answer             *answerWire         `json:"answer"`
	Hold               bool                `json:"hold"`
	SuspectedSpam      bool                `json:"suspected_spam"`
	DeletedFromListing bool                `json:"deleted_from_listing"`
	DateCreated        flexTime            `json:"date_created"`
}

func (w questionWire) toDomain() (*mldomain.RemoteQuestion, error) {
	status, err := mldomain.ParseQuestionStatus(w.Status)
	if err != nil {
		return nil, err
	}
	q := &mldomain.RemoteQuestion{
		ID:                 w.ID.String(),
		ItemID:             w.ItemID,
		SellerID:           w.SellerID.String(),
		FromUserID:         w.BuyerID.String(),
		Text:               w.Text,
		Status:             status,
		Hold:               w.Hold,
		SuspectedSpam:      w.SuspectedSpam,
		DeletedFromListing: w.DeletedFromListing,
		CreatedAt:          w.DateCreated.Time,
	}
	if q.FromUserID == "" && w.From != nil {
		q.FromUserID = w.From.ID.String()
	}
	if w.Answer != nil {
		answerStatus, err := mldomain.ParseAnswerStatus(w.Answer.Status)
		if err != nil {
			return nil, err
		}
		q.Answer = &mldomain.RemoteAnswer{Text: w.Answer.Text, Status: answerStatus, CreatedAt: w.Answer.DateCreated.Time}
	}
	if !mldomain.ValidNumericID(q.ID) {
		return nil, &Error{Code: CodeDecode, Message: "question answer carries no id"}
	}
	return q, nil
}

func validQuestionID(id string) error {
	if !mldomain.ValidNumericID(id) {
		return fmt.Errorf("%w: question %q", mldomain.ErrInvalidResource, id)
	}
	return nil
}

func (s *QuestionService) GetQuestion(ctx context.Context, creds mldomain.Credentials, questionID string) (*mldomain.RemoteQuestion, error) {
	if err := validQuestionID(questionID); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("api_version", questionsAPIVersion)
	var w questionWire
	if err := s.c.getJSON(ctx, request{
		path:   "/questions/" + pathEscape(questionID),
		query:  q,
		family: FamilyQuestions,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	return w.toDomain()
}

type questionSearchWire struct {
	Total     int            `json:"total"`
	Limit     int            `json:"limit"`
	Questions []questionWire `json:"questions"`
	Filters   *struct {
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	} `json:"filters"`
}

func (s *QuestionService) SearchQuestions(ctx context.Context, creds mldomain.Credentials, search mldomain.QuestionSearch) (*mldomain.QuestionPage, error) {
	q := url.Values{}
	q.Set("seller_id", creds.UserID)
	q.Set("api_version", questionsAPIVersion)
	q.Set("sort_fields", "date_created")
	q.Set("sort_types", "DESC")
	q.Set("offset", strconv.Itoa(max(search.Offset, 0)))
	q.Set("limit", strconv.Itoa(boundLimit(search.Limit, mldomain.MaxQuestionSearchLimit)))
	if search.ItemID != "" {
		if !mldomain.ValidItemID(search.ItemID) {
			return nil, fmt.Errorf("%w: item %q", mldomain.ErrInvalidResource, search.ItemID)
		}
		q.Set("item", search.ItemID)
	}
	if search.Status != "" {
		if _, err := mldomain.ParseQuestionStatus(string(search.Status)); err != nil {
			return nil, err
		}
		q.Set("status", string(search.Status))
	}
	var w questionSearchWire
	if err := s.c.getJSON(ctx, request{
		path:   "/questions/search",
		query:  q,
		family: FamilyQuestions,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	page := &mldomain.QuestionPage{Total: w.Total, Limit: w.Limit}
	if w.Filters != nil {
		page.Offset = w.Filters.Offset
	}
	for _, item := range w.Questions {
		question, err := item.toDomain()
		if err != nil {
			return nil, err
		}
		page.Questions = append(page.Questions, question)
	}
	return page, nil
}

type answerBody struct {
	QuestionID json.Number `json:"question_id"`
	Text       string      `json:"text"`
}

func (s *QuestionService) Answer(ctx context.Context, creds mldomain.Credentials, questionID, text string) (*mldomain.RemoteQuestion, error) {
	if err := validQuestionID(questionID); err != nil {
		return nil, err
	}
	if err := mldomain.ValidateAnswerText(text); err != nil {
		return nil, err
	}
	var w questionWire
	resp, err := s.c.sendJSON(ctx, request{
		method: http.MethodPost,
		path:   "/answers",
		family: FamilyQuestions,
		creds:  creds,
	}, answerBody{QuestionID: json.Number(questionID), Text: text}, &w)
	if err != nil {
		return nil, markAcceptedButUnreadable(resp, err)
	}
	return w.toDomain()
}

func (s *QuestionService) DeleteQuestion(ctx context.Context, creds mldomain.Credentials, questionID string) error {
	if err := validQuestionID(questionID); err != nil {
		return err
	}
	_, err := s.c.do(ctx, request{
		method: http.MethodDelete,
		path:   "/questions/" + pathEscape(questionID),
		family: FamilyQuestions,
		creds:  creds,
	})
	return err
}

type blacklistBody struct {
	UserID json.Number `json:"user_id"`
}

func validAsker(creds mldomain.Credentials, askerID string) error {
	if !mldomain.ValidNumericID(askerID) || askerID == creds.UserID {
		return mldomain.ErrUserIDInvalid
	}
	return nil
}

func (s *QuestionService) BlockAsker(ctx context.Context, creds mldomain.Credentials, askerID string) error {
	if err := validAsker(creds, askerID); err != nil {
		return err
	}
	_, err := s.c.sendJSON(ctx, request{
		method: http.MethodPost,
		path:   "/users/" + pathEscape(creds.UserID) + "/questions_blacklist",
		family: FamilyQuestions,
		creds:  creds,
	}, blacklistBody{UserID: json.Number(askerID)}, nil)
	return err
}

func (s *QuestionService) UnblockAsker(ctx context.Context, creds mldomain.Credentials, askerID string) error {
	if err := validAsker(creds, askerID); err != nil {
		return err
	}
	_, err := s.c.do(ctx, request{
		method: http.MethodDelete,
		path:   "/users/" + pathEscape(creds.UserID) + "/questions_blacklist/" + pathEscape(askerID),
		family: FamilyQuestions,
		creds:  creds,
	})
	return err
}

type blacklistWire struct {
	Paging paging  `json:"paging"`
	Users  []idRef `json:"users"`
}

func (s *QuestionService) QuestionBlacklist(ctx context.Context, creds mldomain.Credentials, offset, limit int) (*mldomain.BlacklistPage, error) {
	q := url.Values{}
	q.Set("offset", strconv.Itoa(max(offset, 0)))
	q.Set("limit", strconv.Itoa(boundLimit(limit, mldomain.MaxBlacklistLimit)))
	var w blacklistWire
	if err := s.c.getJSON(ctx, request{
		path:   "/users/" + pathEscape(creds.UserID) + "/questions_blacklist",
		query:  q,
		family: FamilyQuestions,
		creds:  creds,
	}, &w); err != nil {
		return nil, err
	}
	page := &mldomain.BlacklistPage{Total: w.Paging.Total, Limit: w.Paging.Limit, Offset: w.Paging.Offset, UserIDs: make([]string, 0, len(w.Users))}
	for _, u := range w.Users {
		page.UserIDs = append(page.UserIDs, u.ID.String())
	}
	return page, nil
}

type responseTimeSlotWire struct {
	ResponseTime         int      `json:"response_time"`
	SalesPercentIncrease *float64 `json:"sales_percent_increase"`
}

type responseTimeWire struct {
	Total                *responseTimeSlotWire `json:"total"`
	Weekend              *responseTimeSlotWire `json:"weekend"`
	WeekdaysWorkingHours *responseTimeSlotWire `json:"weekdays_working_hours"`
	WeekdaysExtraHours   *responseTimeSlotWire `json:"weekdays_extra_hours"`
}

func (w *responseTimeSlotWire) toDomain() *mldomain.ResponseTimeSlot {
	if w == nil {
		return nil
	}
	return &mldomain.ResponseTimeSlot{Minutes: w.ResponseTime, SalesPercentIncrease: w.SalesPercentIncrease}
}

func (s *QuestionService) ResponseTime(ctx context.Context, creds mldomain.Credentials) (*mldomain.ResponseTime, error) {
	var w responseTimeWire
	err := s.c.getJSON(ctx, request{
		path:   "/users/" + pathEscape(creds.UserID) + "/questions/response_time",
		family: FamilyQuestions,
		creds:  creds,
	}, &w)
	if IsNotFound(err) {
		return &mldomain.ResponseTime{Available: false}, nil
	}
	if err != nil {
		return nil, err
	}
	return &mldomain.ResponseTime{
		Available:            true,
		Total:                w.Total.toDomain(),
		Weekend:              w.Weekend.toDomain(),
		WeekdaysWorkingHours: w.WeekdaysWorkingHours.toDomain(),
		WeekdaysExtraHours:   w.WeekdaysExtraHours.toDomain(),
	}, nil
}

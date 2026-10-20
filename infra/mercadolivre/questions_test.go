package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

func newQuestions(t *testing.T, fs *fakeServer, limiters *fakeLimiters) *QuestionService {
	t.Helper()
	s, err := NewQuestionService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGetQuestionUsesAPIVersion4(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "question_v4_doc_example.json")))
	limiters := newFakeLimiters()
	s := newQuestions(t, fs, limiters)
	q, err := s.GetQuestion(context.Background(), sellerCreds, "11751825075")
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodGet || r.Path != "/questions/11751825075" || r.Query.Get("api_version") != "4" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	assertBearer(t, r)
	if keys := limiters.keys(); keys[1] != "ml:app:questions" {
		t.Fatalf("limiter keys = %v", keys)
	}
	if q.ID != "11751825075" || q.SellerID != "179571326" || q.FromUserID != "56801932" || q.ItemID != "MLA739200576" ||
		q.Status != mldomain.QuestionAnswered || q.Text != "Texto de la pregunta." ||
		q.Answer == nil || q.Answer.Status != mldomain.AnswerBanned || q.Answer.Text != "" {
		t.Fatalf("question = %+v", q)
	}
	if !q.CreatedAt.Equal(time.Date(2021, 2, 8, 17, 51, 21, 746608612, time.UTC)) {
		t.Fatalf("created = %v", q.CreatedAt)
	}
}

func TestGetQuestionRefusesAnUnknownStatus(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"id":1,"status":"OPEN","item_id":"MLB1","seller_id":2,"from":{"id":3},"date_created":"2021-02-08T17:51:21Z"}`))
	s := newQuestions(t, fs, newFakeLimiters())
	if _, err := s.GetQuestion(context.Background(), sellerCreds, "1"); !errors.Is(err, mldomain.ErrUnknownQuestionStatus) {
		t.Fatalf("err = %v", err)
	}
}

func TestSearchQuestions(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "questions_search_doc_example.json")))
	s := newQuestions(t, fs, newFakeLimiters())
	page, err := s.SearchQuestions(context.Background(), sellerCreds, mldomain.QuestionSearch{
		ItemID: "MLB1623490410", Status: mldomain.QuestionUnanswered, Offset: 4, Limit: 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	q := r.Query
	if r.Path != "/questions/search" || q.Get("seller_id") != "415458330" || q.Get("api_version") != "4" ||
		q.Get("item") != "MLB1623490410" || q.Get("status") != "UNANSWERED" || q.Get("offset") != "4" ||
		q.Get("limit") != "50" || q.Get("sort_fields") != "date_created" || q.Get("sort_types") != "DESC" {
		t.Fatalf("query = %v", q)
	}
	if page.Total != 36 || page.Limit != 2 || page.Offset != 4 || len(page.Questions) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Questions[1].Status != mldomain.QuestionBanned || page.Questions[1].Answer != nil || page.Questions[0].FromUserID != "162981404" {
		t.Fatalf("questions = %+v %+v", page.Questions[0], page.Questions[1])
	}
}

func TestSearchQuestionsWithoutFilters(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"total":0,"limit":50,"questions":[]}`))
	s := newQuestions(t, fs, newFakeLimiters())
	if _, err := s.SearchQuestions(context.Background(), sellerCreds, mldomain.QuestionSearch{}); err != nil {
		t.Fatal(err)
	}
	q := fs.last().Query
	if _, ok := q["item"]; ok {
		t.Fatalf("query = %v", q)
	}
	if _, ok := q["status"]; ok {
		t.Fatalf("query = %v", q)
	}
	if _, err := s.SearchQuestions(context.Background(), sellerCreds, mldomain.QuestionSearch{ItemID: "MLA1"}); err == nil {
		t.Fatal("foreign item accepted")
	}
	if _, err := s.SearchQuestions(context.Background(), sellerCreds, mldomain.QuestionSearch{Status: "OPEN"}); err == nil {
		t.Fatal("unknown status accepted")
	}
}

func TestAnswer(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "answer_doc_example.json")))
	s := newQuestions(t, fs, newFakeLimiters())
	q, err := s.Answer(context.Background(), sellerCreds, "3957150025", "Test answer...")
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodPost || r.Path != "/answers" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatal(err)
	}
	if string(body["question_id"]) != "3957150025" || string(body["text"]) != `"Test answer..."` || len(body) != 2 {
		t.Fatalf("body = %s", r.Body)
	}
	if q.Status != mldomain.QuestionAnswered || q.Answer == nil || q.Answer.Status != mldomain.AnswerActive || q.Answer.Text != "Test answer..." {
		t.Fatalf("question = %+v", q)
	}
}

func TestAnswerRefusesBeforeCalling(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newQuestions(t, fs, newFakeLimiters())
	cases := []struct {
		id, text string
		err      error
	}{
		{"3957150025", "", mldomain.ErrAnswerEmpty},
		{"3957150025", strings.Repeat("a", 2001), mldomain.ErrAnswerTooLong},
		{"39a", "ok", mldomain.ErrInvalidResource},
	}
	for _, tc := range cases {
		if _, err := s.Answer(context.Background(), sellerCreds, tc.id, tc.text); !errors.Is(err, tc.err) {
			t.Errorf("Answer(%q, %d runes) err = %v, want %v", tc.id, len(tc.text), err, tc.err)
		}
	}
	if fs.count() != 0 {
		t.Fatal("refused answers must not reach Mercado Livre")
	}
}

func TestAnswerIsNotRetriedOnAServerError(t *testing.T) {
	fs := newFakeServer(t, jsonReply(500, `{}`))
	s := newQuestions(t, fs, newFakeLimiters())
	if _, err := s.Answer(context.Background(), sellerCreds, "1", "ok"); err == nil || fs.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
}

func TestDeleteQuestion(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `["Question deleted."]`))
	s := newQuestions(t, fs, newFakeLimiters())
	if err := s.DeleteQuestion(context.Background(), sellerCreds, "3957150025"); err != nil {
		t.Fatal(err)
	}
	if r := fs.last(); r.Method != http.MethodDelete || r.Path != "/questions/3957150025" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
}

func TestBlacklist(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newQuestions(t, fs, newFakeLimiters())
	if err := s.BlockAsker(context.Background(), sellerCreds, "70927648"); err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodPost || r.Path != "/users/415458330/questions_blacklist" || strings.TrimSpace(r.Body) != `{"user_id":70927648}` {
		t.Fatalf("%s %s %s", r.Method, r.Path, r.Body)
	}
	if err := s.UnblockAsker(context.Background(), sellerCreds, "70927648"); err != nil {
		t.Fatal(err)
	}
	if r := fs.last(); r.Method != http.MethodDelete || r.Path != "/users/415458330/questions_blacklist/70927648" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
	if err := s.BlockAsker(context.Background(), sellerCreds, "415458330"); err == nil {
		t.Fatal("a seller cannot block itself")
	}
	if err := s.BlockAsker(context.Background(), sellerCreds, "x"); err == nil {
		t.Fatal("malformed asker accepted")
	}
}

func TestResponseTime(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "response_time_doc_example.json")))
	s := newQuestions(t, fs, newFakeLimiters())
	rt, err := s.ResponseTime(context.Background(), sellerCreds)
	if err != nil {
		t.Fatal(err)
	}
	if fs.last().Path != "/users/415458330/questions/response_time" {
		t.Fatalf("path = %s", fs.last().Path)
	}
	if !rt.Available || rt.Total.Minutes != 22 || rt.WeekdaysExtraHours.Minutes != 72 ||
		rt.WeekdaysExtraHours.SalesPercentIncrease == nil || *rt.WeekdaysExtraHours.SalesPercentIncrease != 10 ||
		rt.Weekend.SalesPercentIncrease != nil {
		t.Fatalf("response time = %+v", rt)
	}
}

func TestResponseTimeNotFoundMeansNoQuestions(t *testing.T) {
	fs := newFakeServer(t, jsonReply(404, `{"message":"Response time not found for user id: 276274936","error":"not_found","status":404,"cause":[]}`))
	s := newQuestions(t, fs, newFakeLimiters())
	rt, err := s.ResponseTime(context.Background(), sellerCreds)
	if err != nil || rt.Available {
		t.Fatalf("response time = %+v, %v", rt, err)
	}
}

func TestQuestionBlacklistIsReadPageByPage(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "questions_blacklist_doc_example.json")))
	limiters := newFakeLimiters()
	s := newQuestions(t, fs, limiters)
	page, err := s.QuestionBlacklist(context.Background(), sellerCreds, 50, 500)
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Method != http.MethodGet || r.Path != "/users/415458330/questions_blacklist" ||
		r.Query.Get("offset") != "50" || r.Query.Get("limit") != "50" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	if keys := limiters.keys(); keys[1] != "ml:app:questions" {
		t.Fatalf("limiter keys = %v", keys)
	}
	want := []string{"70927648", "170577643", "82411799", "158470042", "194938540"}
	if strings.Join(page.UserIDs, ",") != strings.Join(want, ",") || page.Total != 5 || page.Limit != 50 || page.Offset != 0 {
		t.Fatalf("page = %+v", page)
	}
}

func TestAnEmptyQuestionBlacklistIsAnEmptyPage(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"paging":{"total":0,"limit":50,"offset":0},"users":[]}`))
	s := newQuestions(t, fs, newFakeLimiters())
	page, err := s.QuestionBlacklist(context.Background(), sellerCreds, -3, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.UserIDs) != 0 || page.Total != 0 || fs.last().Query.Get("offset") != "0" {
		t.Fatalf("page = %+v query = %v", page, fs.last().Query)
	}
}

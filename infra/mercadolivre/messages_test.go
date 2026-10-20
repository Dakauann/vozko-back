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

func newMessages(t *testing.T, fs *fakeServer, limiters *fakeLimiters) *MessageService {
	t.Helper()
	s, err := NewMessageService(newTestClient(t, fs, limiters))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestThreadNeverMarksMessagesRead(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "thread_doc_example.json")))
	limiters := newFakeLimiters()
	s := newMessages(t, fs, limiters)
	th, err := s.Thread(context.Background(), sellerCreds, mldomain.ThreadQuery{PackKey: "2000000089077943", Offset: 10, Limit: 20})
	if err != nil {
		t.Fatalf("Thread: %v", err)
	}
	r := fs.last()
	if r.Method != http.MethodGet || r.Path != "/messages/packs/2000000089077943/sellers/415458330" {
		t.Fatalf("%s %s", r.Method, r.Path)
	}
	assertBearer(t, r)
	q := r.Query
	if q.Get("mark_as_read") != "false" || q.Get("tag") != "post_sale" || q.Get("offset") != "10" || q.Get("limit") != "20" {
		t.Fatalf("query = %v", q)
	}
	if keys := limiters.keys(); keys[1] != "ml:app:messages:read" {
		t.Fatalf("limiter keys = %v", keys)
	}
	if th.Status != "active" || th.Substatus != "" || th.Path != "/packs/2000000089077943/seller/415458330" ||
		th.SellerMaxMessageLength != 350 || th.Total != 3 || th.Limit != 10 || len(th.Messages) != 1 {
		t.Fatalf("thread = %+v", th)
	}
	m := th.Messages[0]
	want := time.Date(2020, 12, 5, 20, 1, 46, 0, time.UTC)
	if m.ID != "fd1d2e37ad004ede9e0bf25d1215002d" || m.FromUserID != "123456789000" || m.ToUserID != "2332423234" ||
		m.Text != "Mensaje de test" || m.Status != mldomain.MessageAvailable || m.ModerationStatus != "clean" ||
		!m.CreatedAt.Equal(want) || m.ReadAt != nil || m.PackKey != "000011122344" ||
		m.ConversationPath != "/packs/2000000089077943/sellers/415458330/conversations/resale-seller" {
		t.Fatalf("message = %+v", m)
	}
}

func TestThreadDefaultsAndBounds(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "thread_blocked_doc_example.json")))
	s := newMessages(t, fs, newFakeLimiters())
	th, err := s.Thread(context.Background(), sellerCreds, mldomain.ThreadQuery{PackKey: "22175467", Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if fs.last().Query.Get("limit") != "50" {
		t.Fatalf("limit must be bounded, query = %v", fs.last().Query)
	}
	if th.Status != "blocked" || th.Substatus != "blocked_by_buyer" {
		t.Fatalf("thread = %+v", th)
	}
	state, err := th.State(nil, time.Now())
	if !errors.Is(err, mldomain.ErrMediationUnknown) || state.WindowOpen {
		t.Fatalf("state = %+v, %v", state, err)
	}
	for _, bad := range []string{"", "abc", "/packs/1"} {
		if _, err := s.Thread(context.Background(), sellerCreds, mldomain.ThreadQuery{PackKey: bad}); err == nil {
			t.Errorf("pack %q accepted", bad)
		}
	}
}

func TestThreadForOperatorIsTheOnlyReadThatMarksRead(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "thread_mark_read_doc_example.json")))
	s := newMessages(t, fs, newFakeLimiters())
	th, err := s.ThreadForOperator(context.Background(), sellerCreds, mldomain.ThreadQuery{PackKey: "2000000089077943"})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := fs.last().Query["mark_as_read"]; present {
		t.Fatalf("operator read must let Mercado Livre mark as read: %v", fs.last().Query)
	}
	m := th.Messages[0]
	if m.FromUserID != "415458330" || m.Status != mldomain.MessageStatus("IN_MODERATION") || m.ReadAt == nil ||
		len(m.Attachments) != 1 || m.Attachments[0].ID != "415460047_a96d8dea-38cd-4402-938e-80a1c134fc5d.pdf" ||
		m.Attachments[0].Size != 225677 || m.Attachments[0].OriginalName != "Ayuda-Memoria-Arduino-ELINSI.pdf" ||
		m.PackKey != "2000000089077943" {
		t.Fatalf("message = %+v", m)
	}
}

func TestGetMessageReadsBothDocumentedFormats(t *testing.T) {
	cases := []struct {
		fixture string
		id      string
		text    string
		from    string
	}{
		{"message_legacy_doc_example.json", "0033b582a1474fa98c02d229abcec43c", "Exemplo de texto", "123456789"},
		{"message_new_format_doc_example.json", "fd1d2e37ad004ede9e0bf25d1215002d", "Mensagem de teste", "123456789000"},
	}
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			fs := newFakeServer(t, jsonReply(200, fixture(t, tc.fixture)))
			s := newMessages(t, fs, newFakeLimiters())
			m, err := s.GetMessage(context.Background(), sellerCreds, tc.id)
			if err != nil {
				t.Fatal(err)
			}
			r := fs.last()
			if r.Path != "/messages/"+tc.id || r.Query.Get("tag") != "post_sale" {
				t.Fatalf("%s %v", r.Path, r.Query)
			}
			if r.Query.Get("mark_as_read") != "false" {
				t.Fatalf("background reads always pass mark_as_read=false: %v", r.Query)
			}
			if m.ID != tc.id || m.Text != tc.text || m.FromUserID != tc.from || m.Status != mldomain.MessageAvailable {
				t.Fatalf("message = %+v", m)
			}
		})
	}
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newMessages(t, fs, newFakeLimiters())
	for _, bad := range []string{"", "../x", "abc"} {
		if _, err := s.GetMessage(context.Background(), sellerCreds, bad); err == nil {
			t.Errorf("message id %q accepted", bad)
		}
	}
	if fs.count() != 0 {
		t.Fatal("invalid ids must not reach Mercado Livre")
	}
}

func TestGetMessageRefusesAnEmptyAnswer(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"paging":null,"messages":[]}`))
	s := newMessages(t, fs, newFakeLimiters())
	if _, err := s.GetMessage(context.Background(), sellerCreds, "fd1d2e37ad004ede9e0bf25d1215002d"); err == nil {
		t.Fatal("an answer without the message must be an error")
	}
}

func TestSendPostsTheDocumentedBody(t *testing.T) {
	fs := newFakeServer(t, jsonReply(201, fixture(t, "option_sent_doc_example.json")))
	limiters := newFakeLimiters()
	s := newMessages(t, fs, limiters)
	sent, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{
		PackKey:       "2000000089077943",
		ToUserID:      "3037675074",
		Text:          "Olá! Sua encomenda foi despachada. \U0001F69A",
		AttachmentIDs: []string{"415460047_a96d8dea-38cd-4402-938e-80a1c134fc5d.pdf"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	r := fs.last()
	if r.Method != http.MethodPost || r.Path != "/messages/packs/2000000089077943/sellers/415458330" || r.Query.Get("tag") != "post_sale" {
		t.Fatalf("%s %s %v", r.Method, r.Path, r.Query)
	}
	assertBearer(t, r)
	if keys := limiters.keys(); keys[1] != "ml:app:messages:write" {
		t.Fatalf("limiter keys = %v", keys)
	}
	for _, b := range []byte(r.Body) {
		if b >= 0x80 {
			t.Fatalf("body must be ASCII so the charset is never ambiguous: %s", r.Body)
		}
	}
	var body struct {
		From struct {
			UserID string `json:"user_id"`
		} `json:"from"`
		To struct {
			UserID string `json:"user_id"`
		} `json:"to"`
		Text        string   `json:"text"`
		Attachments []string `json:"attachments"`
	}
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.From.UserID != "415458330" || body.To.UserID != "3037675074" ||
		body.Text != "Olá! Sua encomenda foi despachada. \U0001F69A" || len(body.Attachments) != 1 {
		t.Fatalf("body = %+v", body)
	}
	if sent.ID != "94353d192b9640e8b1ed3c77aa406f39" || sent.Status != mldomain.MessageAvailable || sent.PendingReview {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestSendOmitsAttachmentsWhenThereAreNone(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "option_sent_doc_example.json")))
	s := newMessages(t, fs, newFakeLimiters())
	if _, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "oi"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fs.last().Body, "attachments") {
		t.Fatalf("attachments must be removed from the body: %s", fs.last().Body)
	}
}

func TestSendAcceptedForReview(t *testing.T) {
	fs := newFakeServer(t, jsonReply(202, `{"id":"94353d192b9640e8b1ed3c77aa406f39","status":"pending_review"}`))
	s := newMessages(t, fs, newFakeLimiters())
	sent, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "oi"})
	if err != nil {
		t.Fatal(err)
	}
	if !sent.PendingReview || sent.Status != mldomain.MessagePendingReview || sent.ID == "" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestSendModeratedComesBackAsAResult(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "option_sent_moderated_doc_example.json")))
	s := newMessages(t, fs, newFakeLimiters())
	sent, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "oi"})
	if err != nil {
		t.Fatal(err)
	}
	if sent.Status != mldomain.MessageModerated || sent.ModerationStatus != "rejected" || sent.ModerationReason != "out_of_place_language" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestSendRefusesBeforeCallingMercadoLivre(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	s := newMessages(t, fs, newFakeLimiters())
	cases := []struct {
		name string
		msg  mldomain.SendMessage
		err  error
	}{
		{"charset", mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "Total 10€"}, mldomain.ErrTextCharset},
		{"too long", mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: strings.Repeat("a", 351)}, mldomain.ErrTextTooLong},
		{"empty", mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: " "}, mldomain.ErrTextEmpty},
		{"no recipient", mldomain.SendMessage{PackKey: "1", Text: "oi"}, mldomain.ErrBuyerUnknown},
		{"recipient is the seller", mldomain.SendMessage{PackKey: "1", ToUserID: "415458330", Text: "oi"}, mldomain.ErrBuyerUnknown},
		{"bad pack", mldomain.SendMessage{PackKey: "x", ToUserID: "2", Text: "oi"}, mldomain.ErrPackUnknown},
		{"bad attachment id", mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "oi", AttachmentIDs: []string{"../etc"}}, mldomain.ErrMediaUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Send(context.Background(), sellerCreds, tc.msg); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
	if fs.count() != 0 {
		t.Fatal("refused sends must never reach Mercado Livre")
	}
}

func TestSendBlockedMapsTheSubstatus(t *testing.T) {
	fs := newFakeServer(t, jsonReply(403, `{"status_code":403,"code":"forbidden","message":"You can not send the message because a mediation is in process"}`))
	s := newMessages(t, fs, newFakeLimiters())
	_, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "oi"})
	sub, blocked := IsBlocked(err)
	if !blocked || sub != "blocked_by_mediation" || fs.count() != 1 {
		t.Fatalf("err = %v, attempts = %d", err, fs.count())
	}
}

func TestSendDocumentedForbiddenIsBlockedWithoutAKnownReason(t *testing.T) {
	fs := newFakeServer(t, jsonReply(403, `{"status_code":403,"code":"forbidden","message":"blocked_conversation_send_message_forbidden"}`))
	s := newMessages(t, fs, newFakeLimiters())
	_, err := s.Send(context.Background(), sellerCreds, mldomain.SendMessage{PackKey: "1", ToUserID: "2", Text: "oi"})
	if sub, blocked := IsBlocked(err); !blocked || sub != "" {
		t.Fatalf("err = %v", err)
	}
}

func TestUnread(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, fixture(t, "unread_doc_example.json")))
	s := newMessages(t, fs, newFakeLimiters())
	got, err := s.Unread(context.Background(), sellerCreds)
	if err != nil {
		t.Fatal(err)
	}
	r := fs.last()
	if r.Path != "/messages/unread" || r.Query.Get("role") != "seller" || r.Query.Get("tag") != "post_sale" {
		t.Fatalf("%s %v", r.Path, r.Query)
	}
	if len(got) != 1 || got[0].PackKey != "1977056109" || got[0].SellerID != "378136913" || got[0].Count != 1 {
		t.Fatalf("unread = %+v", got)
	}
}

func TestUnreadRefusesAnUnknownResource(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{"user_id":1,"results":[{"resource":"/orders/1","count":2}]}`))
	s := newMessages(t, fs, newFakeLimiters())
	if _, err := s.Unread(context.Background(), sellerCreds); !errors.Is(err, mldomain.ErrInvalidResource) {
		t.Fatalf("err = %v", err)
	}
}

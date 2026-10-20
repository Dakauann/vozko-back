package mercadolivre

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

func TestARemoteQuestionBecomesTheMirrorOfItsSeller(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	asked := now.Add(-time.Hour)
	remote := &RemoteQuestion{ID: "13000000001", ItemID: "MLB123", SellerID: "415458330", FromUserID: "123", Text: "Tem azul?", Status: QuestionUnanswered, CreatedAt: asked, SuspectedSpam: true}
	got, err := QuestionFromRemote(sellerAccount(), remote, "ct-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccountID != "acc-1" || got.WorkspaceID != "ws-1" || got.QuestionID != "13000000001" || got.ItemID != "MLB123" ||
		got.ContactID != "ct-1" || got.Text != "Tem azul?" || got.Status != QuestionUnanswered || !got.AskedAt.Equal(asked) ||
		!got.LastFetchedAt.Equal(now) || !got.SuspectedSpam || got.AnsweredBy != "" || got.AnswerText != "" {
		t.Fatalf("question = %+v", got)
	}
}

func TestAnAnswerSeenOnlyAtMercadoLivreIsCreditedToTheirApp(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	answered := now.Add(-time.Minute)
	remote := &RemoteQuestion{ID: "13000000001", ItemID: "MLB123", SellerID: "415458330", Status: QuestionAnswered, CreatedAt: now.Add(-time.Hour),
		Answer: &RemoteAnswer{Text: "Tem sim", Status: AnswerActive, CreatedAt: answered}}
	got, err := QuestionFromRemote(sellerAccount(), remote, "ct-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.AnsweredBy != AnsweredByMercadoLivreApp || got.AnswerText != "Tem sim" || got.AnswerStatus != AnswerActive ||
		got.AnsweredAt == nil || !got.AnsweredAt.Equal(answered) {
		t.Fatalf("question = %+v", got)
	}
}

func TestAQuestionOfAnotherSellerIsRefused(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		remote *RemoteQuestion
		want   error
	}{
		{"other seller", &RemoteQuestion{ID: "1", ItemID: "MLB1", SellerID: "999", Status: QuestionUnanswered}, ErrQuestionNotOwned},
		{"no seller", &RemoteQuestion{ID: "1", ItemID: "MLB1", Status: QuestionUnanswered}, ErrQuestionNotOwned},
		{"bad item", &RemoteQuestion{ID: "1", ItemID: "x", SellerID: "415458330", Status: QuestionUnanswered}, ErrInvalidResource},
		{"bad id", &RemoteQuestion{ID: "q", ItemID: "MLB1", SellerID: "415458330", Status: QuestionUnanswered}, ErrInvalidResource},
		{"nil", nil, ErrInvalidResource},
	}
	for _, tc := range cases {
		if _, err := QuestionFromRemote(sellerAccount(), tc.remote, "", now); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestQuestionCursorsRoundTripAndRefuseForgeries(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 123, time.UTC)
	for _, c := range []QuestionCursor{{Unanswered: true, AskedAt: at, ID: "row-1"}, {Unanswered: false, AskedAt: at, ID: "row-2"}} {
		got, err := ParseQuestionCursor(c.Encode())
		if err != nil || got == nil || *got != c {
			t.Fatalf("round trip %+v = %+v, %v", c, got, err)
		}
	}
	if got, err := ParseQuestionCursor(""); got != nil || err != nil {
		t.Fatalf("empty cursor = %+v, %v", got, err)
	}
	forged := []string{"not-base64!", shared.Keyset{At: at, ID: "row-1"}.Encode(), shared.Keyset{At: at, ID: "x.row-1"}.Encode(), shared.Keyset{At: at, ID: "u."}.Encode()}
	for _, raw := range forged {
		if _, err := ParseQuestionCursor(raw); !errors.Is(err, shared.ErrKeysetInvalid) {
			t.Errorf("ParseQuestionCursor(%q) err = %v", raw, err)
		}
	}
}

func TestTheCursorOfAQuestionFollowsItsOpenState(t *testing.T) {
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	open := CursorOf(&Question{ID: "row-1", Status: QuestionUnanswered, AskedAt: at})
	closed := CursorOf(&Question{ID: "row-2", Status: QuestionAnswered, AskedAt: at})
	if !open.Unanswered || closed.Unanswered || open.ID != "row-1" || !open.AskedAt.Equal(at) {
		t.Fatalf("open=%+v closed=%+v", open, closed)
	}
}

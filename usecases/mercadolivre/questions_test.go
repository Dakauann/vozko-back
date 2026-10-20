package mercadolivre

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	ca "vozko/domain/commentautomation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	"vozko/domain/webhook"
)

const questionID = "13000000001"

func (f *catalogFixture) askOpen(id string) *mldomain.RemoteQuestion {
	q := &mldomain.RemoteQuestion{ID: id, ItemID: "MLB1000", SellerID: sellerID, FromUserID: buyerID, Text: "Tem na cor azul?",
		Status: mldomain.QuestionUnanswered, CreatedAt: testNow.Add(-time.Minute)}
	f.api.questions[id] = q
	return q
}

func (f *catalogFixture) answerRule() *ca.Rule {
	rule := &ca.Rule{ID: "rule-1", WorkspaceID: "ws-1", Source: shared.EntryTypeMercadoLivre, AccountID: f.account.ID, Name: "Cor", Enabled: true,
		Match: ca.MatchContains, Keywords: []string{"azul"}, Actions: []ca.Action{ca.ActionPublicReply}, PublicReplyText: "Olá {{username}}, temos sim!"}
	f.rules.rules = append(f.rules.rules, rule)
	return rule
}

func questionJob(id string) mldomain.Job {
	return mldomain.Job{Kind: mldomain.JobFetchQuestion, Topic: "questions", SellerID: sellerID, ResourceID: id, Resource: "/questions/" + id, NotificationID: "n-" + id}
}

func TestAFirstSeenOpenQuestionRunsTheRulesThroughTheAnswerUseCase(t *testing.T) {
	f := newCatalogFixture(t)
	f.askOpen(questionID)
	f.answerRule()

	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
	if joined(f.api.answered) != questionID+"|Olá COMPRADOR, temos sim!" {
		t.Fatalf("answered = %v", f.api.answered)
	}
	q := f.questions.get(f.account.ID, questionID)
	if q == nil || q.Status != mldomain.QuestionAnswered || q.AnsweredBy != mldomain.AnsweredByRule || q.RuleID == nil || *q.RuleID != "rule-1" {
		t.Fatalf("mirror = %+v", q)
	}
	contact, err := f.contacts.FindByID(context.Background(), q.ContactID)
	if err != nil || contact.MLUserID != buyerID || contact.Nickname != "COMPRADOR" {
		t.Fatalf("asker = %+v %v", contact, err)
	}
	if f.metrics.count("answer/rule") != 1 || f.metrics.count("rules/claimed") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}

	f.api.questions[questionID].Status = mldomain.QuestionUnanswered
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
	if len(f.api.answered) != 1 {
		t.Fatal("a question seen again must never run its rules again")
	}
}

func TestADuplicateClaimSkipsTheRuleAndIsCounted(t *testing.T) {
	f := newCatalogFixture(t)
	f.askOpen(questionID)
	f.answerRule()
	f.claims.claimed = map[string]bool{"mercadolivre|" + questionID: true}
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
	if len(f.api.answered) != 0 || f.metrics.count("rules/duplicate") != 1 {
		t.Fatalf("answered=%v metrics=%v", f.api.answered, f.metrics.events)
	}
}

func TestAnAnswerGivenInTheMercadoLivreAppOnlyUpdatesTheMirror(t *testing.T) {
	f := newCatalogFixture(t)
	q := f.askOpen(questionID)
	q.Status = mldomain.QuestionAnswered
	q.Answer = &mldomain.RemoteAnswer{Text: "Temos", Status: mldomain.AnswerActive, CreatedAt: testNow}
	f.answerRule()
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
	mirror := f.questions.get(f.account.ID, questionID)
	if len(f.api.answered) != 0 || mirror.AnsweredBy != mldomain.AnsweredByMercadoLivreApp || mirror.AnswerText != "Temos" {
		t.Fatalf("answered=%v mirror=%+v", f.api.answered, mirror)
	}
}

func TestAQuestionOnAnotherSellersItemIsIgnored(t *testing.T) {
	f := newCatalogFixture(t)
	f.askOpen(questionID).SellerID = "999"
	f.answerRule()
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
	if len(f.questions.rows) != 0 || len(f.contacts.rows) != 0 || len(f.api.answered) != 0 || f.metrics.count("questions/not_owned") != 1 {
		t.Fatalf("rows=%d contacts=%d metrics=%v", len(f.questions.rows), len(f.contacts.rows), f.metrics.events)
	}
}

func TestOnlyRetryableRuleFailuresReachTheConsumer(t *testing.T) {
	f := newCatalogFixture(t)
	f.askOpen(questionID)
	f.answerRule()
	f.rules.listErr = errors.New("db down")
	err := f.asked.Handle(context.Background(), f.account, questionJob(questionID))
	if !ca.Retryable(err) {
		t.Fatalf("rules unavailable must be retried: %v", err)
	}
	if f.questions.get(f.account.ID, questionID) != nil || joined(f.questions.forgot) != questionID {
		t.Fatal("a retryable failure must leave the question unseen, so the retry is a first sight again")
	}

	f.rules.listErr = nil
	f.api.answerErr = &mlErr{msg: "refused"}
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatalf("an action failure is logged, never retried: %v", err)
	}
	if f.metrics.count("questions/rule_failed") != 1 {
		t.Fatalf("metrics = %v", f.metrics.events)
	}
}

func (f *catalogFixture) mirrorOpen(t *testing.T) {
	t.Helper()
	f.askOpen(questionID)
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
}

func TestAManualAnswerReReadsTheQuestionFirst(t *testing.T) {
	f := newCatalogFixture(t)
	f.mirrorOpen(t)
	operator := "user-1"
	answer := func(text string) error {
		_, err := f.asked.Answer(context.Background(), AnswerInput{WorkspaceID: "ws-1", AccountID: f.account.ID, QuestionID: questionID,
			Text: text, Source: mldomain.AnsweredByOperator, UserID: &operator})
		return err
	}
	if err := answer(strings.Repeat("a", 2001)); !errors.Is(err, mldomain.ErrAnswerTooLong) {
		t.Fatalf("too long = %v", err)
	}

	f.api.questions[questionID].Status = mldomain.QuestionClosedUnanswered
	if err := answer("Temos sim"); !errors.Is(err, mldomain.ErrQuestionNotOpen) {
		t.Fatalf("closed at Mercado Livre = %v", err)
	}
	if f.questions.get(f.account.ID, questionID).Status != mldomain.QuestionClosedUnanswered || len(f.api.answered) != 0 {
		t.Fatal("the re-read must refresh the mirror and send nothing")
	}

	f.api.questions[questionID].Status = mldomain.QuestionUnanswered
	f.questions.put(&mldomain.Question{ID: "q-" + questionID, AccountID: f.account.ID, WorkspaceID: "ws-1", QuestionID: questionID, ItemID: "MLB1000", Status: mldomain.QuestionUnanswered})
	if err := answer("Temos sim"); err != nil {
		t.Fatal(err)
	}
	mirror := f.questions.get(f.account.ID, questionID)
	if mirror.AnsweredBy != mldomain.AnsweredByOperator || mirror.AnsweredByUserID == nil || *mirror.AnsweredByUserID != "user-1" || f.metrics.count("answer/operator") != 1 {
		t.Fatalf("mirror = %+v", mirror)
	}
}

func TestEveryQuestionActionChecksTheQuestionBelongsToTheAccount(t *testing.T) {
	f := newCatalogFixture(t)
	f.mirrorOpen(t)
	ctx := context.Background()
	operator := "user-1"

	other := connectedAccount()
	other.ID, other.MLUserID = "acct-other", "777"
	f.accounts.put(other)
	if _, err := f.asked.Answer(ctx, AnswerInput{WorkspaceID: "ws-1", AccountID: other.ID, QuestionID: questionID, Text: "oi", Source: mldomain.AnsweredByOperator, UserID: &operator}); !errors.Is(err, mldomain.ErrQuestionNotOwned) {
		t.Fatalf("another account's token = %v", err)
	}
	if err := f.asked.Delete(ctx, "ws-1", other.ID, questionID); !errors.Is(err, mldomain.ErrQuestionNotOwned) {
		t.Fatalf("delete from another account = %v", err)
	}
	if err := f.asked.BlockAsker(ctx, "ws-1", other.ID, questionID); !errors.Is(err, mldomain.ErrQuestionNotOwned) {
		t.Fatalf("block from another account = %v", err)
	}
	f.api.questions[questionID].SellerID = "777"
	if err := f.asked.Delete(ctx, "ws-1", f.account.ID, questionID); !errors.Is(err, mldomain.ErrQuestionNotOwned) {
		t.Fatalf("a question whose seller changed = %v", err)
	}
	if _, err := f.asked.Answer(ctx, AnswerInput{WorkspaceID: "ws-2", AccountID: f.account.ID, QuestionID: questionID, Text: "oi", Source: mldomain.AnsweredByOperator, UserID: &operator}); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
	if _, err := f.asked.Answer(ctx, AnswerInput{WorkspaceID: "ws-1", AccountID: f.account.ID, QuestionID: questionID, Text: "oi", Source: mldomain.AnsweredByMercadoLivreApp}); err == nil {
		t.Fatal("only operators and rules answer from here")
	}
	if len(f.api.answered)+len(f.api.deleted)+len(f.api.blocked) != 0 {
		t.Fatal("a refused action reached Mercado Livre")
	}
}

func TestDeleteAndBlockActOnTheOwnedQuestion(t *testing.T) {
	f := newCatalogFixture(t)
	f.mirrorOpen(t)
	ctx := context.Background()
	if err := f.asked.BlockAsker(ctx, "ws-1", f.account.ID, questionID); err != nil {
		t.Fatal(err)
	}
	if err := f.asked.UnblockAsker(ctx, "ws-1", f.account.ID, questionID); err != nil {
		t.Fatal(err)
	}
	if err := f.asked.Delete(ctx, "ws-1", f.account.ID, questionID); err != nil {
		t.Fatal(err)
	}
	if joined(f.api.blocked) != buyerID || joined(f.api.unblocked) != buyerID || joined(f.api.deleted) != questionID {
		t.Fatalf("blocked=%v unblocked=%v deleted=%v", f.api.blocked, f.api.unblocked, f.api.deleted)
	}
	if f.questions.get(f.account.ID, questionID).Status != mldomain.QuestionDeleted {
		t.Fatal("a deleted question must be marked deleted")
	}
}

func TestAnAccountThatCannotAnswerRefusesBeforeCallingMercadoLivre(t *testing.T) {
	f := newCatalogFixture(t)
	f.mirrorOpen(t)
	acc := f.accounts.get(f.account.ID)
	acc.CanAnswerQuestions = false
	f.accounts.put(acc)
	operator := "user-1"
	if _, err := f.asked.Answer(context.Background(), AnswerInput{WorkspaceID: "ws-1", AccountID: f.account.ID, QuestionID: questionID, Text: "oi",
		Source: mldomain.AnsweredByOperator, UserID: &operator}); !errors.Is(err, ErrAccountUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheResponseTimeIsCachedPerAccount(t *testing.T) {
	f := newCatalogFixture(t)
	for i := 0; i < 3; i++ {
		rt, err := f.asked.ResponseTime(context.Background(), "ws-1", f.account.ID)
		if err != nil || !rt.Available || rt.Total.Minutes != 42 {
			t.Fatalf("response time = %+v %v", rt, err)
		}
	}
	if f.api.responses != 1 {
		t.Fatalf("Mercado Livre was asked %d times", f.api.responses)
	}
	if _, err := f.asked.ResponseTime(context.Background(), "ws-2", f.account.ID); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
}

func TestQuestionListsCarryTheAskerAndTheListing(t *testing.T) {
	f := newCatalogFixture(t)
	f.sellItems("MLB1000")
	_ = f.catalog.Handle(context.Background(), f.account, mldomain.Job{Kind: mldomain.JobFetchItem, ResourceID: "MLB1000"})
	f.mirrorOpen(t)
	page, err := f.asked.List(context.Background(), QuestionsQuery{WorkspaceID: "ws-1", AccountID: f.account.ID})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %+v %v", page, err)
	}
	item := page.Items[0]
	if item.Asker == nil || item.Asker.Nickname != "COMPRADOR" || item.Listing == nil || item.Listing.ItemID != "MLB1000" || item.Listing.ThumbnailURL == "" {
		t.Fatalf("view = %+v", item)
	}
	if _, err := f.asked.List(context.Background(), QuestionsQuery{WorkspaceID: "ws-1", AccountID: f.account.ID, Cursor: "%%"}); !errors.Is(err, shared.ErrKeysetInvalid) {
		t.Fatalf("forged cursor = %v", err)
	}
	if _, err := f.asked.List(context.Background(), QuestionsQuery{WorkspaceID: "ws-1", AccountID: f.account.ID, ItemID: "nope"}); !errors.Is(err, mldomain.ErrInvalidResource) {
		t.Fatalf("bad item = %v", err)
	}
}

func TestUnansweredQuestionsReconcileIntoTheSameJobs(t *testing.T) {
	f := newCatalogFixture(t)
	known := f.askOpen("13000000001")
	if err := f.asked.Handle(context.Background(), f.account, questionJob(known.ID)); err != nil {
		t.Fatal(err)
	}
	missing := f.askOpen("13000000002")
	stale := f.askOpen("13000000003")
	f.questions.put(&mldomain.Question{AccountID: f.account.ID, QuestionID: stale.ID, ItemID: "MLB1000", Status: mldomain.QuestionAnswered})
	f.api.searchPages = [][]*mldomain.RemoteQuestion{{known, missing, stale}}

	rec := NewUnansweredQuestionsReconciler(f.api, f.questions, NewJobPublisher(f.publisher))
	published, err := rec.Reconcile(context.Background(), f.account, f.creds, "reconcile:202610191200")
	if err != nil || published != 2 {
		t.Fatalf("published = %d %v", published, err)
	}
	var ids []string
	for _, raw := range f.publisher.published[webhook.TopicMercadoLivreQuestion] {
		var job mldomain.Job
		_ = json.Unmarshal(raw, &job)
		if job.Kind != mldomain.JobFetchQuestion || job.Marker != "reconcile:202610191200" {
			t.Fatalf("job = %+v", job)
		}
		ids = append(ids, job.ResourceID)
	}
	if joined(ids) != "13000000002,13000000003" {
		t.Fatalf("jobs for %v", ids)
	}
	if s := f.api.searches[0]; s.Status != mldomain.QuestionUnanswered || s.Limit != mldomain.MaxQuestionSearchLimit {
		t.Fatalf("search = %+v", s)
	}
}

func TestRuleActionsMercadoLivreCannotDoAreRefused(t *testing.T) {
	f := newCatalogFixture(t)
	actions := NewCommentActions(f.asked)
	rule := f.answerRule()
	for name, err := range map[string]error{
		"private": actions.ReplyPrivately(context.Background(), rule, questionID, "oi"),
		"hide":    actions.Hide(context.Background(), rule, questionID),
		"like":    actions.Like(context.Background(), rule, questionID),
	} {
		if !errors.Is(err, ca.ErrActionUnsupported) {
			t.Errorf("%s = %v", name, err)
		}
	}
	verifier := NewAccountOwnership(f.accounts)
	if err := verifier.VerifyAccount(context.Background(), "ws-1", f.account.ID); err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyAccount(context.Background(), "ws-2", f.account.ID); !errors.Is(err, mldomain.ErrAccountNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
}

func TestADeleteRuleGoesThroughTheDeleteUseCase(t *testing.T) {
	f := newCatalogFixture(t)
	f.askOpen(questionID)
	f.rules.rules = []*ca.Rule{{ID: "rule-2", WorkspaceID: "ws-1", Source: shared.EntryTypeMercadoLivre, AccountID: f.account.ID, Name: "Spam",
		Enabled: true, Match: ca.MatchContains, Keywords: []string{"azul"}, Actions: []ca.Action{ca.ActionDelete}}}
	if err := f.asked.Handle(context.Background(), f.account, questionJob(questionID)); err != nil {
		t.Fatal(err)
	}
	if joined(f.api.deleted) != questionID || f.metrics.count("delete/rule") != 1 || f.questions.get(f.account.ID, questionID).Status != mldomain.QuestionDeleted {
		t.Fatalf("deleted=%v metrics=%v", f.api.deleted, f.metrics.events)
	}
}

func TestABlockIsMirroredOnTheAskerOnlyAfterMercadoLivreConfirmed(t *testing.T) {
	f := newCatalogFixture(t)
	f.mirrorOpen(t)
	ctx := context.Background()
	asker := func() *mldomain.Contact {
		c, err := f.contacts.FindByUserID(ctx, f.account.ID, buyerID)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	f.api.blockErr = &mlErr{msg: "timeout", retryable: true}
	if err := f.asked.BlockAsker(ctx, "ws-1", f.account.ID, questionID); err == nil {
		t.Fatal("a refused block must fail")
	}
	if asker().QuestionsBlocked() {
		t.Fatal("a block Mercado Livre did not confirm must not be mirrored")
	}
	f.api.blockErr = nil
	if err := f.asked.BlockAsker(ctx, "ws-1", f.account.ID, questionID); err != nil {
		t.Fatal(err)
	}
	if got := asker(); got.QuestionsBlockedAt == nil || !got.QuestionsBlockedAt.Equal(testNow) {
		t.Fatalf("blocked at = %v", got.QuestionsBlockedAt)
	}
	page, err := f.asked.List(ctx, QuestionsQuery{WorkspaceID: "ws-1", AccountID: f.account.ID})
	if err != nil || len(page.Items) != 1 || !page.Items[0].Asker.QuestionsBlocked() {
		t.Fatalf("list = %+v %v", page, err)
	}
	f.api.blockErr = &mlErr{msg: "timeout", retryable: true}
	if err := f.asked.UnblockAsker(ctx, "ws-1", f.account.ID, questionID); err == nil || !asker().QuestionsBlocked() {
		t.Fatal("a refused unblock must leave the mirror blocked")
	}
	f.api.blockErr = nil
	if err := f.asked.UnblockAsker(ctx, "ws-1", f.account.ID, questionID); err != nil {
		t.Fatal(err)
	}
	if asker().QuestionsBlocked() {
		t.Fatal("an unblock must clear the mirror")
	}
}

func TestOneQuestionIsReadFromTheMirrorWithItsAskerAndListing(t *testing.T) {
	f := newCatalogFixture(t)
	f.sellItems("MLB1000")
	_ = f.catalog.Handle(context.Background(), f.account, mldomain.Job{Kind: mldomain.JobFetchItem, ResourceID: "MLB1000"})
	f.mirrorOpen(t)
	ctx := context.Background()
	reads := len(f.api.gets)
	view, err := f.asked.Get(ctx, "ws-1", f.account.ID, questionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.QuestionID != questionID || view.Asker == nil || view.Asker.Nickname != "COMPRADOR" || view.Listing == nil || view.Listing.ItemID != "MLB1000" {
		t.Fatalf("view = %+v", view)
	}
	if len(f.api.gets) != reads {
		t.Fatal("reading one question must not call Mercado Livre")
	}
	other := connectedAccount()
	other.ID, other.MLUserID = "acct-2", "777"
	f.accounts.put(other)
	cases := []struct {
		name                           string
		workspace, account, questionID string
		want                           error
	}{
		{"unknown question", "ws-1", f.account.ID, "13000000999", mldomain.ErrQuestionNotFound},
		{"malformed id", "ws-1", f.account.ID, "abc", mldomain.ErrQuestionNotFound},
		{"another account", "ws-1", other.ID, questionID, mldomain.ErrQuestionNotFound},
		{"another workspace", "ws-2", f.account.ID, questionID, mldomain.ErrAccountNotFound},
	}
	for _, tc := range cases {
		if _, err := f.asked.Get(ctx, tc.workspace, tc.account, tc.questionID); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

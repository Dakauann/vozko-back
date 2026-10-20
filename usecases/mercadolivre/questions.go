package mercadolivre

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/cache"
	ca "vozko/domain/commentautomation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	cauc "vozko/usecases/commentautomation"
)

const responseTimeTTL = time.Hour

var errAnswerSource = errors.New("mercadolivre: an answer from here is given by an operator or a rule, with who gave it")

type RuleEvaluator interface {
	Evaluate(ctx context.Context, source shared.EntryType, accountID, commentID string, subject ca.Subject, actions cauc.Actions) error
}

type countingClaims struct {
	claims  ca.ExecutionClaims
	metrics Metrics
}

func (c countingClaims) Claim(ctx context.Context, source shared.EntryType, commentID, ruleID string) (bool, error) {
	claimed, err := c.claims.Claim(ctx, source, commentID, ruleID)
	switch {
	case err != nil:
		c.metrics.AddMercadoLivreEvent("rules", "claim_failed")
	case claimed:
		c.metrics.AddMercadoLivreEvent("rules", "claimed")
	default:
		c.metrics.AddMercadoLivreEvent("rules", "duplicate")
	}
	return claimed, err
}

func (c countingClaims) PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	return c.claims.PurgeOlderThan(ctx, cutoff)
}

func NewRuleEvaluator(rules ca.Repository, claims ca.ExecutionClaims, metrics Metrics) *cauc.Evaluator {
	if claims == nil || metrics == nil {
		panic("mercadolivre: the rule evaluator requires execution claims and metrics")
	}
	return cauc.NewEvaluator(rules, countingClaims{claims: claims, metrics: metrics})
}

type QuestionsDeps struct {
	Accounts  mldomain.AccountRepository
	Contacts  mldomain.ContactRepository
	Questions mldomain.QuestionRepository
	Listings  *Listings
	Services  Services
	Tokens    *TokenSource
	Rules     RuleEvaluator
	Memo      cache.Memo
	Metrics   Metrics
}

type Questions struct {
	accounts  mldomain.AccountRepository
	contacts  mldomain.ContactRepository
	questions mldomain.QuestionRepository
	listings  *Listings
	svc       Services
	tokens    *TokenSource
	rules     RuleEvaluator
	memo      cache.Memo
	metrics   Metrics
	actions   cauc.Actions
	now       Clock
}

func NewQuestions(d QuestionsDeps) *Questions {
	if d.Accounts == nil || d.Contacts == nil || d.Questions == nil || d.Listings == nil || d.Tokens == nil || d.Rules == nil ||
		d.Memo == nil || d.Metrics == nil || d.Services.Questions == nil || d.Services.Users == nil || d.Services.Errors == nil {
		panic("mercadolivre: questions require accounts, contacts, the question mirror, listings, the question and user services, an error classifier, tokens, rules, a memo and metrics")
	}
	q := &Questions{
		accounts: d.Accounts, contacts: d.Contacts, questions: d.Questions, listings: d.Listings, svc: d.Services,
		tokens: d.Tokens, rules: d.Rules, memo: d.Memo, metrics: d.Metrics, now: utcNow,
	}
	q.actions = NewCommentActions(q)
	return q
}

func (q *Questions) credentials(ctx context.Context, account *mldomain.Account) (mldomain.Credentials, error) {
	creds, err := q.tokens.Credentials(ctx, account)
	if errors.Is(err, mldomain.ErrNeedsReconnect) {
		return mldomain.Credentials{}, fmt.Errorf("%w: %v", ErrAccountUnavailable, err)
	}
	return creds, err
}

func (q *Questions) apiErr(ctx context.Context, account *mldomain.Account, err error) error {
	return classifyAPIError(ctx, q.svc.Errors, q.tokens, q.metrics, account, err)
}

func (q *Questions) Handle(ctx context.Context, account *mldomain.Account, job mldomain.Job) error {
	if job.Kind != mldomain.JobFetchQuestion {
		return fmt.Errorf("%w: %s", ErrJobKindNotHandled, job.Kind)
	}
	if !account.CanReadQuestionsNow() {
		return fmt.Errorf("%w: questions", ErrAccountUnavailable)
	}
	creds, err := q.credentials(ctx, account)
	if err != nil {
		return err
	}
	remote, err := q.svc.Questions.GetQuestion(ctx, creds, job.ResourceID)
	if err != nil {
		return q.apiErr(ctx, account, err)
	}
	if remote.SellerID != account.MLUserID {
		q.metrics.AddMercadoLivreEvent("questions", "not_owned")
		return nil
	}
	if !mldomain.ValidNumericID(remote.FromUserID) {
		return fmt.Errorf("%w: question %s has no asker", mldomain.ErrInvalidResource, remote.ID)
	}
	contact, err := q.contacts.FindOrCreateByUserID(ctx, mldomain.FindOrCreateContactInput{
		WorkspaceID: account.WorkspaceID, AccountID: account.ID, MLUserID: remote.FromUserID,
	})
	if err != nil {
		return err
	}
	refreshNickname(ctx, q.svc.Users, q.contacts, q.now(), creds, contact)
	question, err := mldomain.QuestionFromRemote(account, remote, contact.ID, q.now())
	if err != nil {
		return err
	}
	created, err := q.questions.Upsert(ctx, question)
	if err != nil {
		return err
	}
	if !mldomain.IsRuleEligible(question, created) {
		q.metrics.AddMercadoLivreEvent("questions", "mirrored")
		return nil
	}
	q.metrics.AddMercadoLivreEvent("questions", "first_seen")
	return q.runRules(ctx, account, question, contact)
}

func (q *Questions) runRules(ctx context.Context, account *mldomain.Account, question *mldomain.Question, asker *mldomain.Contact) error {
	err := q.rules.Evaluate(ctx, shared.EntryTypeMercadoLivre, account.ID, question.QuestionID, ca.Subject{
		ContainerID: question.ItemID,
		AuthorName:  asker.DisplayName(),
		Text:        question.Text,
	}, q.actions)
	if err == nil {
		return nil
	}
	if ca.Retryable(err) {
		if forgetErr := q.questions.Forget(ctx, account.ID, question.QuestionID); forgetErr != nil {
			log.Printf("[mercadolivre][questions] question %s stays mirrored, its rules will not run again: %v", question.QuestionID, forgetErr)
		}
		return err
	}
	q.metrics.AddMercadoLivreEvent("questions", "rule_failed")
	log.Printf("[mercadolivre][questions] rules of question %s failed: %v", question.QuestionID, err)
	return nil
}

type ownedQuestion struct {
	account *mldomain.Account
	creds   mldomain.Credentials
	mirror  *mldomain.Question
	remote  *mldomain.RemoteQuestion
}

func (q *Questions) owned(ctx context.Context, workspaceID, accountID, questionID string) (*ownedQuestion, error) {
	account, err := q.accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	if !mldomain.ValidNumericID(questionID) {
		return nil, mldomain.ErrQuestionNotOwned
	}
	mirror, err := q.questions.FindByQuestionID(ctx, account.ID, questionID)
	if errors.Is(err, mldomain.ErrQuestionNotFound) {
		return nil, mldomain.ErrQuestionNotOwned
	}
	if err != nil {
		return nil, err
	}
	if err := mirror.OwnedBy(account.ID); err != nil {
		return nil, err
	}
	if account.Status != mldomain.StatusConnected {
		return nil, fmt.Errorf("%w: %s", ErrAccountUnavailable, account.Status)
	}
	creds, err := q.credentials(ctx, account)
	if err != nil {
		return nil, err
	}
	remote, err := q.svc.Questions.GetQuestion(ctx, creds, questionID)
	if err != nil {
		return nil, q.apiErr(ctx, account, err)
	}
	if remote.SellerID != account.MLUserID || remote.ItemID != mirror.ItemID {
		return nil, mldomain.ErrQuestionNotOwned
	}
	return &ownedQuestion{account: account, creds: creds, mirror: mirror, remote: remote}, nil
}

type AnswerInput struct {
	WorkspaceID string
	AccountID   string
	QuestionID  string
	Text        string
	Source      mldomain.AnswerSource
	UserID      *string
	RuleID      *string
}

func (in AnswerInput) validSource() bool {
	switch in.Source {
	case mldomain.AnsweredByOperator:
		return in.UserID != nil && strings.TrimSpace(*in.UserID) != ""
	case mldomain.AnsweredByRule:
		return in.RuleID != nil && strings.TrimSpace(*in.RuleID) != ""
	}
	return false
}

func (q *Questions) Answer(ctx context.Context, in AnswerInput) (*mldomain.Question, error) {
	if !in.validSource() {
		return nil, errAnswerSource
	}
	if err := mldomain.ValidateAnswerText(in.Text); err != nil {
		return nil, err
	}
	account, err := q.accounts.FindByIDInWorkspace(ctx, in.WorkspaceID, in.AccountID)
	if err != nil {
		return nil, err
	}
	if !account.CanAnswer() {
		return nil, fmt.Errorf("%w: answers", ErrAccountUnavailable)
	}
	target, err := q.owned(ctx, in.WorkspaceID, in.AccountID, in.QuestionID)
	if err != nil {
		return nil, err
	}
	if target.remote.Status != mldomain.QuestionUnanswered {
		q.refreshMirror(ctx, target)
		return nil, fmt.Errorf("%w: %s", mldomain.ErrQuestionNotOpen, target.remote.Status)
	}
	answered, err := q.svc.Questions.Answer(ctx, target.creds, in.QuestionID, in.Text)
	if err != nil {
		q.metrics.AddMercadoLivreEvent("answer", "failed")
		return nil, q.apiErr(ctx, target.account, err)
	}
	record := mldomain.AnswerRecord{
		Text: in.Text, Status: mldomain.AnswerActive, AnsweredBy: in.Source,
		AnsweredByUserID: in.UserID, RuleID: in.RuleID, AnsweredAt: q.now(),
	}
	if answered != nil && answered.Answer != nil && answered.Answer.Status != "" {
		record.Status = answered.Answer.Status
	}
	if err := q.questions.RecordAnswer(ctx, target.account.ID, in.QuestionID, record); err != nil {
		log.Printf("[mercadolivre][questions] answer to %s was sent but not recorded: %v", in.QuestionID, err)
	}
	q.metrics.AddMercadoLivreEvent("answer", string(in.Source))
	return q.questions.FindByQuestionID(ctx, target.account.ID, in.QuestionID)
}

func (q *Questions) refreshMirror(ctx context.Context, target *ownedQuestion) {
	fresh, err := mldomain.QuestionFromRemote(target.account, target.remote, target.mirror.ContactID, q.now())
	if err == nil {
		_, err = q.questions.Upsert(ctx, fresh)
	}
	if err != nil {
		log.Printf("[mercadolivre][questions] mirror of %s not refreshed: %v", target.mirror.QuestionID, err)
	}
}

func (q *Questions) Delete(ctx context.Context, workspaceID, accountID, questionID string) error {
	return q.delete(ctx, workspaceID, accountID, questionID, "operator")
}

func (q *Questions) delete(ctx context.Context, workspaceID, accountID, questionID, source string) error {
	target, err := q.owned(ctx, workspaceID, accountID, questionID)
	if err != nil {
		return err
	}
	if err := q.svc.Questions.DeleteQuestion(ctx, target.creds, questionID); err != nil {
		return q.apiErr(ctx, target.account, err)
	}
	if err := q.questions.SetStatus(ctx, target.account.ID, questionID, mldomain.QuestionDeleted); err != nil {
		log.Printf("[mercadolivre][questions] deleted question %s still mirrored as %s: %v", questionID, target.mirror.Status, err)
	}
	q.metrics.AddMercadoLivreEvent("delete", source)
	return nil
}

func (q *Questions) BlockAsker(ctx context.Context, workspaceID, accountID, questionID string) error {
	target, err := q.owned(ctx, workspaceID, accountID, questionID)
	if err != nil {
		return err
	}
	if err := q.svc.Questions.BlockAsker(ctx, target.creds, target.remote.FromUserID); err != nil {
		return q.apiErr(ctx, target.account, err)
	}
	q.metrics.AddMercadoLivreEvent("questions", "asker_blocked")
	at := q.now()
	q.mirrorBlock(ctx, target, &at)
	return nil
}

func (q *Questions) mirrorBlock(ctx context.Context, target *ownedQuestion, at *time.Time) {
	if err := q.contacts.SetQuestionsBlocked(ctx, target.mirror.ContactID, at); err != nil {
		q.metrics.AddMercadoLivreEvent("questions", "block_not_mirrored")
		log.Printf("[mercadolivre][questions] block of the asker of %s confirmed but not mirrored (the daily blacklist sync converges it): %v", target.mirror.QuestionID, err)
	}
}

func (q *Questions) UnblockAsker(ctx context.Context, workspaceID, accountID, questionID string) error {
	target, err := q.owned(ctx, workspaceID, accountID, questionID)
	if err != nil {
		return err
	}
	if err := q.svc.Questions.UnblockAsker(ctx, target.creds, target.remote.FromUserID); err != nil {
		return q.apiErr(ctx, target.account, err)
	}
	q.metrics.AddMercadoLivreEvent("questions", "asker_unblocked")
	q.mirrorBlock(ctx, target, nil)
	return nil
}

func (q *Questions) ResponseTime(ctx context.Context, workspaceID, accountID string) (*mldomain.ResponseTime, error) {
	account, err := q.accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	return cache.Remember(ctx, q.memo, "mercadolivre:response_time:"+account.ID, responseTimeTTL, func(ctx context.Context) (*mldomain.ResponseTime, error) {
		creds, err := q.credentials(ctx, account)
		if err != nil {
			return nil, err
		}
		rt, err := q.svc.Questions.ResponseTime(ctx, creds)
		if err != nil {
			return nil, q.apiErr(ctx, account, err)
		}
		return rt, nil
	})
}

type QuestionsQuery struct {
	WorkspaceID string
	AccountID   string
	ItemID      string
	Status      *mldomain.QuestionStatus
	Cursor      string
	Limit       int
}

type QuestionView struct {
	*mldomain.Question
	Asker   *mldomain.Contact
	Listing *ListingView
}

type QuestionPage struct {
	Items      []QuestionView
	NextCursor string
}

func (q *Questions) List(ctx context.Context, in QuestionsQuery) (*QuestionPage, error) {
	if in.ItemID != "" && !mldomain.ValidItemID(in.ItemID) {
		return nil, fmt.Errorf("%w: item %q", mldomain.ErrInvalidResource, in.ItemID)
	}
	after, err := mldomain.ParseQuestionCursor(in.Cursor)
	if err != nil {
		return nil, err
	}
	account, err := q.accounts.FindByIDInWorkspace(ctx, in.WorkspaceID, in.AccountID)
	if err != nil {
		return nil, err
	}
	list, err := q.questions.List(ctx, mldomain.ListQuestionsInput{AccountID: account.ID, ItemID: in.ItemID, Status: in.Status, After: after, Limit: in.Limit})
	if err != nil {
		return nil, err
	}
	views, err := q.views(ctx, account.ID, list.Items)
	if err != nil {
		return nil, err
	}
	page := &QuestionPage{Items: views}
	if list.Next != nil {
		page.NextCursor = list.Next.Encode()
	}
	return page, nil
}

func (q *Questions) Get(ctx context.Context, workspaceID, accountID, questionID string) (*QuestionView, error) {
	account, err := q.accounts.FindByIDInWorkspace(ctx, workspaceID, accountID)
	if err != nil {
		return nil, err
	}
	if !mldomain.ValidNumericID(questionID) {
		return nil, mldomain.ErrQuestionNotFound
	}
	question, err := q.questions.FindByQuestionID(ctx, account.ID, questionID)
	if err != nil {
		return nil, err
	}
	if err := question.OwnedBy(account.ID); err != nil {
		return nil, err
	}
	views, err := q.views(ctx, account.ID, []*mldomain.Question{question})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (q *Questions) views(ctx context.Context, accountID string, questions []*mldomain.Question) ([]QuestionView, error) {
	contactIDs := make([]string, 0, len(questions))
	itemIDs := make([]string, 0, len(questions))
	for _, item := range questions {
		contactIDs = append(contactIDs, item.ContactID)
		itemIDs = append(itemIDs, item.ItemID)
	}
	contacts, err := q.contacts.FindByIDs(ctx, contactIDs)
	if err != nil {
		return nil, err
	}
	askers := make(map[string]*mldomain.Contact, len(contacts))
	for _, c := range contacts {
		askers[c.ID] = c
	}
	listings, err := q.listings.views(ctx, accountID, itemIDs)
	if err != nil {
		return nil, err
	}
	out := make([]QuestionView, 0, len(questions))
	for _, item := range questions {
		view := QuestionView{Question: item, Asker: askers[item.ContactID]}
		if listing, ok := listings[item.ItemID]; ok {
			view.Listing = &listing
		}
		out = append(out, view)
	}
	return out, nil
}

const unansweredMaxPages = 20

type UnansweredQuestionsReconciler struct {
	api       mldomain.QuestionService
	questions mldomain.QuestionRepository
	publisher *JobPublisher
}

var _ AccountReconciler = (*UnansweredQuestionsReconciler)(nil)

func NewUnansweredQuestionsReconciler(api mldomain.QuestionService, questions mldomain.QuestionRepository, publisher *JobPublisher) *UnansweredQuestionsReconciler {
	if api == nil || questions == nil || publisher == nil {
		panic("mercadolivre: the questions reconciler requires the question service, the mirror and the job publisher")
	}
	return &UnansweredQuestionsReconciler{api: api, questions: questions, publisher: publisher}
}

func (u *UnansweredQuestionsReconciler) Reconcile(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, marker string) (int, error) {
	if !account.CanReadQuestionsNow() {
		return 0, nil
	}
	published := 0
	for page := 0; page < unansweredMaxPages; page++ {
		offset := page * mldomain.MaxQuestionSearchLimit
		found, err := u.api.SearchQuestions(ctx, creds, mldomain.QuestionSearch{Status: mldomain.QuestionUnanswered, Offset: offset, Limit: mldomain.MaxQuestionSearchLimit})
		if err != nil {
			return published, err
		}
		n, err := u.publishUnknown(ctx, account, found.Questions, marker)
		published += n
		if err != nil {
			return published, err
		}
		if len(found.Questions) < mldomain.MaxQuestionSearchLimit || offset+len(found.Questions) >= found.Total {
			return published, nil
		}
	}
	return published, nil
}

func (u *UnansweredQuestionsReconciler) publishUnknown(ctx context.Context, account *mldomain.Account, remote []*mldomain.RemoteQuestion, marker string) (int, error) {
	ids := make([]string, 0, len(remote))
	for _, r := range remote {
		ids = append(ids, r.ID)
	}
	mirrored, err := u.questions.FindByQuestionIDs(ctx, account.ID, ids)
	if err != nil {
		return 0, err
	}
	open := make(map[string]bool, len(mirrored))
	for _, m := range mirrored {
		open[m.QuestionID] = m.Unanswered()
	}
	published := 0
	for _, r := range remote {
		if open[r.ID] {
			continue
		}
		job, err := mldomain.QuestionJob(account.MLUserID, r.ID, marker)
		if err != nil {
			log.Printf("[mercadolivre][reconcile] question %q of %s skipped: %v", r.ID, account.ID, err)
			continue
		}
		if err := u.publisher.Publish(job); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}

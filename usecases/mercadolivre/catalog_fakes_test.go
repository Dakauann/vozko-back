package mercadolivre

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	ca "vozko/domain/commentautomation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
)

type fakeListings struct {
	mu          sync.Mutex
	rows        map[string]*mldomain.Listing
	claimedAt   map[string]time.Time
	released    int
	claimErr    error
	upsertCalls int
}

func newFakeListings() *fakeListings {
	return &fakeListings{rows: map[string]*mldomain.Listing{}, claimedAt: map[string]time.Time{}}
}

func listingKey(accountID, itemID string) string { return accountID + "|" + itemID }

func (f *fakeListings) UpsertMany(_ context.Context, listings []*mldomain.Listing) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertCalls++
	for _, l := range listings {
		key := listingKey(l.AccountID, l.ItemID)
		row, ok := f.rows[key]
		if !ok {
			cp := *l
			cp.ID = "lst-" + l.ItemID
			cp.ThumbnailKey, cp.ThumbnailSource = "", ""
			f.rows[key] = &cp
			continue
		}
		row.Title, row.Permalink, row.Status, row.SyncedAt = l.Title, l.Permalink, l.Status, l.SyncedAt
	}
	return nil
}

func (f *fakeListings) SetThumbnail(_ context.Context, accountID, itemID, key, source string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[listingKey(accountID, itemID)]
	if !ok {
		return mldomain.ErrListingNotFound
	}
	row.ThumbnailKey, row.ThumbnailSource = key, source
	return nil
}

func (f *fakeListings) FindByItemID(_ context.Context, accountID, itemID string) (*mldomain.Listing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[listingKey(accountID, itemID)]; ok {
		cp := *row
		return &cp, nil
	}
	return nil, mldomain.ErrListingNotFound
}

func (f *fakeListings) FindByItemIDs(ctx context.Context, accountID string, itemIDs []string) ([]*mldomain.Listing, error) {
	var out []*mldomain.Listing
	for _, id := range itemIDs {
		if l, err := f.FindByItemID(ctx, accountID, id); err == nil {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeListings) ListByAccount(_ context.Context, in mldomain.ListListingsInput) (*mldomain.ListingList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &mldomain.ListingList{}
	for _, row := range f.rows {
		if row.AccountID == in.AccountID {
			cp := *row
			out.Items = append(out.Items, &cp)
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].ItemID < out.Items[j].ItemID })
	return out, nil
}

func (f *fakeListings) ClaimSync(_ context.Context, accountID string, now, before time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.claimErr != nil {
		return false, f.claimErr
	}
	if at, ok := f.claimedAt[accountID]; ok && !at.Before(before) {
		return false, nil
	}
	f.claimedAt[accountID] = now
	return true, nil
}

func (f *fakeListings) ReleaseSync(_ context.Context, accountID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released++
	delete(f.claimedAt, accountID)
	return nil
}

type fakeQuestionRepo struct {
	mu       sync.Mutex
	rows     map[string]*mldomain.Question
	answers  []mldomain.AnswerRecord
	statuses []mldomain.QuestionStatus
	forgot   []string
}

func newFakeQuestionRepo() *fakeQuestionRepo {
	return &fakeQuestionRepo{rows: map[string]*mldomain.Question{}}
}

func (f *fakeQuestionRepo) Upsert(_ context.Context, q *mldomain.Question) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := q.AccountID + "|" + q.QuestionID
	row, ok := f.rows[key]
	if !ok {
		cp := *q
		cp.ID = "q-" + q.QuestionID
		f.rows[key] = &cp
		q.ID = cp.ID
		return true, nil
	}
	answeredBy := row.AnsweredBy
	cp := *q
	cp.ID = row.ID
	if answeredBy != "" {
		cp.AnsweredBy = answeredBy
	}
	f.rows[key] = &cp
	q.ID = row.ID
	return false, nil
}

func (f *fakeQuestionRepo) put(q *mldomain.Question) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *q
	if cp.ID == "" {
		cp.ID = "q-" + q.QuestionID
	}
	f.rows[q.AccountID+"|"+q.QuestionID] = &cp
}

func (f *fakeQuestionRepo) get(accountID, questionID string) *mldomain.Question {
	f.mu.Lock()
	defer f.mu.Unlock()
	if row, ok := f.rows[accountID+"|"+questionID]; ok {
		cp := *row
		return &cp
	}
	return nil
}

func (f *fakeQuestionRepo) FindByQuestionID(_ context.Context, accountID, questionID string) (*mldomain.Question, error) {
	if q := f.get(accountID, questionID); q != nil {
		return q, nil
	}
	return nil, mldomain.ErrQuestionNotFound
}

func (f *fakeQuestionRepo) FindByQuestionIDs(_ context.Context, accountID string, ids []string) ([]*mldomain.Question, error) {
	var out []*mldomain.Question
	for _, id := range ids {
		if q := f.get(accountID, id); q != nil {
			out = append(out, q)
		}
	}
	return out, nil
}

func (f *fakeQuestionRepo) Forget(_ context.Context, accountID, questionID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgot = append(f.forgot, questionID)
	delete(f.rows, accountID+"|"+questionID)
	return nil
}

func (f *fakeQuestionRepo) List(_ context.Context, in mldomain.ListQuestionsInput) (*mldomain.QuestionList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &mldomain.QuestionList{}
	for _, row := range f.rows {
		if row.AccountID != in.AccountID || in.ItemID != "" && row.ItemID != in.ItemID || in.Status != nil && row.Status != *in.Status {
			continue
		}
		cp := *row
		out.Items = append(out.Items, &cp)
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].QuestionID < out.Items[j].QuestionID })
	if len(out.Items) > 0 && in.Limit > 0 && len(out.Items) > in.Limit {
		out.Items = out.Items[:in.Limit]
		next := mldomain.CursorOf(out.Items[len(out.Items)-1])
		out.Next = &next
	}
	return out, nil
}

func (f *fakeQuestionRepo) RecordAnswer(_ context.Context, accountID, questionID string, a mldomain.AnswerRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[accountID+"|"+questionID]
	if !ok {
		return mldomain.ErrQuestionNotFound
	}
	f.answers = append(f.answers, a)
	at := a.AnsweredAt
	row.Status, row.AnswerText, row.AnswerStatus, row.AnsweredBy = mldomain.QuestionAnswered, a.Text, a.Status, a.AnsweredBy
	row.AnsweredByUserID, row.RuleID, row.AnsweredAt = a.AnsweredByUserID, a.RuleID, &at
	return nil
}

func (f *fakeQuestionRepo) SetStatus(_ context.Context, accountID, questionID string, status mldomain.QuestionStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[accountID+"|"+questionID]
	if !ok {
		return mldomain.ErrQuestionNotFound
	}
	f.statuses = append(f.statuses, status)
	row.Status = status
	return nil
}

type fakeItems struct {
	mu       sync.Mutex
	pages    [][]string
	items    map[string]*mldomain.RemoteItem
	searches []mldomain.ItemSearch
	bulks    [][]string
	ctxs     []context.Context
	err      error
}

func (f *fakeItems) SearchSellerItems(ctx context.Context, _ mldomain.Credentials, s mldomain.ItemSearch) (*mldomain.ItemIDPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches = append(f.searches, s)
	f.ctxs = append(f.ctxs, ctx)
	if f.err != nil {
		return nil, f.err
	}
	idx := 0
	if s.ScrollID != "" {
		idx = int(s.ScrollID[len("scroll-")] - '0')
	}
	if idx >= len(f.pages) {
		return &mldomain.ItemIDPage{}, nil
	}
	return &mldomain.ItemIDPage{IDs: f.pages[idx], ScrollID: "scroll-" + string(rune('0'+idx+1))}, nil
}

func (f *fakeItems) GetItems(_ context.Context, _ mldomain.Credentials, ids []string) ([]*mldomain.RemoteItem, []mldomain.ItemFailure, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bulks = append(f.bulks, ids)
	if f.err != nil {
		return nil, nil, f.err
	}
	if len(ids) > mldomain.MaxItemsPerBatch {
		return nil, nil, errors.New("bulk too large")
	}
	var out []*mldomain.RemoteItem
	var failed []mldomain.ItemFailure
	for _, id := range ids {
		if it, ok := f.items[id]; ok {
			cp := *it
			out = append(out, &cp)
			continue
		}
		failed = append(failed, mldomain.ItemFailure{ID: id, StatusCode: 404})
	}
	return out, failed, nil
}

type fakeImages struct {
	mu      sync.Mutex
	stored  map[string]string
	failFor string
}

func (f *fakeImages) Rehost(_ context.Context, keyBase, url string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if url == f.failFor {
		return "", errors.New("cdn down")
	}
	if f.stored == nil {
		f.stored = map[string]string{}
	}
	f.stored[keyBase+".jpg"] = url
	return keyBase + ".jpg", nil
}

func (f *fakeImages) URL(key string) string {
	if key == "" {
		return ""
	}
	return "https://cdn.example/" + key
}

type fakeQuestionAPI struct {
	mu          sync.Mutex
	questions   map[string]*mldomain.RemoteQuestion
	getErr      error
	gets        []string
	blockErr    error
	answered    []string
	answerErr   error
	deleted     []string
	blocked     []string
	unblocked   []string
	responses   int
	searchPages [][]*mldomain.RemoteQuestion
	searches    []mldomain.QuestionSearch
}

func (f *fakeQuestionAPI) GetQuestion(_ context.Context, _ mldomain.Credentials, id string) (*mldomain.RemoteQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, id)
	if f.getErr != nil {
		return nil, f.getErr
	}
	q, ok := f.questions[id]
	if !ok {
		return nil, &mlErr{msg: "question", notFound: true}
	}
	cp := *q
	return &cp, nil
}

func (f *fakeQuestionAPI) SearchQuestions(_ context.Context, _ mldomain.Credentials, s mldomain.QuestionSearch) (*mldomain.QuestionPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searches = append(f.searches, s)
	idx := s.Offset / max(s.Limit, 1)
	total := 0
	for _, p := range f.searchPages {
		total += len(p)
	}
	if idx >= len(f.searchPages) {
		return &mldomain.QuestionPage{Total: total}, nil
	}
	return &mldomain.QuestionPage{Questions: f.searchPages[idx], Total: total, Offset: s.Offset, Limit: s.Limit}, nil
}

func (f *fakeQuestionAPI) Answer(_ context.Context, _ mldomain.Credentials, id, text string) (*mldomain.RemoteQuestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.answerErr != nil {
		return nil, f.answerErr
	}
	f.answered = append(f.answered, id+"|"+text)
	q := f.questions[id]
	q.Status = mldomain.QuestionAnswered
	q.Answer = &mldomain.RemoteAnswer{Text: text, Status: mldomain.AnswerActive, CreatedAt: testNow}
	cp := *q
	return &cp, nil
}

func (f *fakeQuestionAPI) DeleteQuestion(_ context.Context, _ mldomain.Credentials, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeQuestionAPI) BlockAsker(_ context.Context, _ mldomain.Credentials, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blockErr != nil {
		return f.blockErr
	}
	f.blocked = append(f.blocked, id)
	return nil
}

func (f *fakeQuestionAPI) UnblockAsker(_ context.Context, _ mldomain.Credentials, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.blockErr != nil {
		return f.blockErr
	}
	f.unblocked = append(f.unblocked, id)
	return nil
}

func (f *fakeQuestionAPI) QuestionBlacklist(context.Context, mldomain.Credentials, int, int) (*mldomain.BlacklistPage, error) {
	return nil, errors.New("not used")
}

func (f *fakeQuestionAPI) ResponseTime(context.Context, mldomain.Credentials) (*mldomain.ResponseTime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses++
	return &mldomain.ResponseTime{Available: true, Total: &mldomain.ResponseTimeSlot{Minutes: 42}}, nil
}

type fakeRules struct {
	rules   []*ca.Rule
	listErr error
}

func (f *fakeRules) Create(context.Context, *ca.Rule) error       { return nil }
func (f *fakeRules) Update(context.Context, *ca.Rule) error       { return nil }
func (f *fakeRules) Delete(context.Context, string, string) error { return nil }
func (f *fakeRules) FindByID(context.Context, string, string) (*ca.Rule, error) {
	return nil, ca.ErrRuleNotFound
}
func (f *fakeRules) ListByAccount(context.Context, string, shared.EntryType, string) ([]*ca.Rule, error) {
	return f.rules, nil
}
func (f *fakeRules) ListCandidates(_ context.Context, source shared.EntryType, accountID, containerID string) ([]*ca.Rule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*ca.Rule
	for _, r := range f.rules {
		if r.Source == source && r.AccountID == accountID && (r.ContainerID == "" || r.ContainerID == containerID) {
			out = append(out, r)
		}
	}
	return out, nil
}

type fakeClaims struct {
	mu      sync.Mutex
	claimed map[string]bool
	err     error
}

func (f *fakeClaims) Claim(_ context.Context, source shared.EntryType, commentID, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if f.claimed == nil {
		f.claimed = map[string]bool{}
	}
	key := string(source) + "|" + commentID
	if f.claimed[key] {
		return false, nil
	}
	f.claimed[key] = true
	return true, nil
}

func (f *fakeClaims) PurgeOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

type fakeMemo struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (f *fakeMemo) Remember(ctx context.Context, key string, _ time.Duration, compute func(context.Context) ([]byte, error)) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if v, ok := f.values[key]; ok {
		return v, nil
	}
	v, err := compute(ctx)
	if err != nil {
		return nil, err
	}
	if f.values == nil {
		f.values = map[string][]byte{}
	}
	f.values[key] = v
	return v, nil
}

func joined(values []string) string { return strings.Join(values, ",") }

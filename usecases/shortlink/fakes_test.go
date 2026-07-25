package shortlink_usecase

import (
	"context"
	"sync"
	"time"

	"vozko/domain/messaging"
	"vozko/domain/shared"
	"vozko/domain/shortlink"
)

type fakeShortLinkRepo struct {
	CreateFn     func(ctx context.Context, l *shortlink.ShortLink) error
	UpdateFn     func(ctx context.Context, l *shortlink.ShortLink) error
	SoftDeleteFn func(ctx context.Context, ws, id string) error
	FindByIDFn   func(ctx context.Context, ws, id string) (*shortlink.ShortLink, error)
	FindByCodeFn func(ctx context.Context, code string) (*shortlink.ShortLink, error)
	CodeExistsFn func(ctx context.Context, code string) (bool, error)
	ListFn       func(ctx context.Context, ws string, dep *string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.ShortLink], error)
	CountFn      func(ctx context.Context, ws string) (int, error)
	SumFn        func(ctx context.Context, ws string) (int64, error)
	ApplyClickFn func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error
}

func (f *fakeShortLinkRepo) Create(ctx context.Context, l *shortlink.ShortLink) error {
	if f.CreateFn != nil {
		return f.CreateFn(ctx, l)
	}
	return nil
}
func (f *fakeShortLinkRepo) Update(ctx context.Context, l *shortlink.ShortLink) error {
	if f.UpdateFn != nil {
		return f.UpdateFn(ctx, l)
	}
	return nil
}
func (f *fakeShortLinkRepo) SoftDelete(ctx context.Context, ws, id string) error {
	if f.SoftDeleteFn != nil {
		return f.SoftDeleteFn(ctx, ws, id)
	}
	return nil
}
func (f *fakeShortLinkRepo) FindByID(ctx context.Context, ws, id string) (*shortlink.ShortLink, error) {
	if f.FindByIDFn != nil {
		return f.FindByIDFn(ctx, ws, id)
	}
	return nil, shortlink.ErrShortLinkNotFound
}
func (f *fakeShortLinkRepo) FindByCode(ctx context.Context, code string) (*shortlink.ShortLink, error) {
	if f.FindByCodeFn != nil {
		return f.FindByCodeFn(ctx, code)
	}
	return nil, shortlink.ErrShortLinkNotFound
}
func (f *fakeShortLinkRepo) CodeExists(ctx context.Context, code string) (bool, error) {
	if f.CodeExistsFn != nil {
		return f.CodeExistsFn(ctx, code)
	}
	return false, nil
}
func (f *fakeShortLinkRepo) ListByWorkspace(ctx context.Context, ws string, dep *string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.ShortLink], error) {
	if f.ListFn != nil {
		return f.ListFn(ctx, ws, dep, opts)
	}
	return shared.NewPaginatedResult([]*shortlink.ShortLink{}, opts, 0), nil
}
func (f *fakeShortLinkRepo) CountByWorkspace(ctx context.Context, ws string) (int, error) {
	if f.CountFn != nil {
		return f.CountFn(ctx, ws)
	}
	return 0, nil
}
func (f *fakeShortLinkRepo) SumClicksByWorkspace(ctx context.Context, ws string) (int64, error) {
	if f.SumFn != nil {
		return f.SumFn(ctx, ws)
	}
	return 0, nil
}
func (f *fakeShortLinkRepo) ApplyClick(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
	if f.ApplyClickFn != nil {
		return f.ApplyClickFn(ctx, id, uniqueDelta, occurredAt)
	}
	return nil
}

type fakeClickRepo struct {
	RecordFn       func(ctx context.Context, c *shortlink.Click) (bool, error)
	DailyFn        func(ctx context.Context, deltas []shortlink.DailyStatDelta) error
	AnalyticsFn    func(ctx context.Context, in shortlink.AnalyticsInput) (*shortlink.Analytics, error)
	RecentFn       func(ctx context.Context, ws, id string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.Click], error)
	PurgeClicksFn  func(ctx context.Context, cutoff time.Time) (int64, error)
	PurgeDailyFn   func(ctx context.Context, cutoff time.Time) (int64, error)
}

func (f *fakeClickRepo) RecordClick(ctx context.Context, c *shortlink.Click) (bool, error) {
	if f.RecordFn != nil {
		return f.RecordFn(ctx, c)
	}
	return true, nil
}
func (f *fakeClickRepo) ApplyDailyStats(ctx context.Context, deltas []shortlink.DailyStatDelta) error {
	if f.DailyFn != nil {
		return f.DailyFn(ctx, deltas)
	}
	return nil
}
func (f *fakeClickRepo) Analytics(ctx context.Context, in shortlink.AnalyticsInput) (*shortlink.Analytics, error) {
	if f.AnalyticsFn != nil {
		return f.AnalyticsFn(ctx, in)
	}
	return &shortlink.Analytics{}, nil
}
func (f *fakeClickRepo) RecentClicks(ctx context.Context, ws, id string, opts shared.Pagination) (*shared.PaginatedResult[*shortlink.Click], error) {
	if f.RecentFn != nil {
		return f.RecentFn(ctx, ws, id, opts)
	}
	return shared.NewPaginatedResult([]*shortlink.Click{}, opts, 0), nil
}
func (f *fakeClickRepo) PurgeClicksBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if f.PurgeClicksFn != nil {
		return f.PurgeClicksFn(ctx, cutoff)
	}
	return 0, nil
}
func (f *fakeClickRepo) PurgeDailyStatsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	if f.PurgeDailyFn != nil {
		return f.PurgeDailyFn(ctx, cutoff)
	}
	return 0, nil
}

type fakePasswordSvc struct {
	HashFn   func(plain string) (string, error)
	VerifyFn func(hash, plain string) error
}

func (f *fakePasswordSvc) Hash(plain string) (string, error) {
	if f.HashFn != nil {
		return f.HashFn(plain)
	}
	return "hash:" + plain, nil
}
func (f *fakePasswordSvc) Verify(hash, plain string) error {
	if f.VerifyFn != nil {
		return f.VerifyFn(hash, plain)
	}
	return nil
}

type fakeHostGuard struct {
	blocked bool
}

func (f fakeHostGuard) ResolvesToBlocked(host string) bool { return f.blocked }

type fakeScanner struct {
	verdict shortlink.ThreatVerdict
	err     error
}

func (f fakeScanner) Scan(ctx context.Context, rawURL string) (shortlink.ThreatVerdict, error) {
	return f.verdict, f.err
}

type fakeUA struct {
	info    shortlink.DeviceInfo
	panicOn bool
}

func (f fakeUA) Parse(userAgent string) shortlink.DeviceInfo {
	if f.panicOn {
		panic("ua boom")
	}
	return f.info
}

type fakeGeo struct {
	info shortlink.GeoInfo
}

func (f fakeGeo) Resolve(hints shortlink.GeoHints) shortlink.GeoInfo { return f.info }

type fakeQR struct {
	data []byte
	err  error
}

func (f fakeQR) Generate(content string, size int) ([]byte, error) { return f.data, f.err }

type fakeQueuePub struct {
	mu        sync.Mutex
	published [][]byte
	err       error
}

func (f *fakeQueuePub) Publish(topic string, msg []byte) error {
	if f.err != nil {
		return f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, msg)
	return nil
}
func (f *fakeQueuePub) PublishWithDelay(topic string, msg []byte, delay time.Duration) error {
	return f.Publish(topic, msg)
}
func (f *fakeQueuePub) ValidateConnection() error { return nil }
func (f *fakeQueuePub) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.published)
}

type fakeQueueSub struct {
	err     error
	topic   string
	handler func([]byte, messaging.MessageAck)
}

func (f *fakeQueueSub) Subscribe(topic string, handler func([]byte, messaging.MessageAck)) error {
	f.topic = topic
	f.handler = handler
	return f.err
}
func (f *fakeQueueSub) DeleteQueue(topic string) error         { return nil }
func (f *fakeQueueSub) ValidateConnection() error              { return nil }
func (f *fakeQueueSub) GetQueueLength(topic string) (int, error) { return 0, nil }

type fakeAck struct {
	acked   bool
	nacked  bool
	requeue bool
	ackErr  error
	done    chan struct{}
}

func newFakeAck() *fakeAck { return &fakeAck{done: make(chan struct{}, 1)} }

func (a *fakeAck) Ack() error {
	a.acked = true
	a.signal()
	return a.ackErr
}
func (a *fakeAck) Nack(requeue bool) error {
	a.nacked = true
	a.requeue = requeue
	a.signal()
	return nil
}
func (a *fakeAck) DeliveryCount() int { return 1 }
func (a *fakeAck) signal() {
	select {
	case a.done <- struct{}{}:
	default:
	}
}
func (a *fakeAck) wait(t interface{ Fatalf(string, ...any) }) {
	select {
	case <-a.done:
	case <-time.After(2 * time.Second):
		t.Fatalf("ack/nack never fired")
	}
}

type fakeSharedState struct {
	mu       sync.Mutex
	strs     map[string]string
	getErr   error
	setErr   error
	setNXErr error
	delErr   error
}

func newFakeSharedState() *fakeSharedState {
	return &fakeSharedState{strs: map[string]string{}}
}

func (f *fakeSharedState) SetNX(key, value string, ttl time.Duration) (bool, error) {
	if f.setNXErr != nil {
		return false, f.setNXErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.strs[key]; ok {
		return false, nil
	}
	f.strs[key] = value
	return true, nil
}
func (f *fakeSharedState) SetString(key, value string, ttl time.Duration) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.strs[key] = value
	return nil
}
func (f *fakeSharedState) GetString(key string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.strs[key], nil
}
func (f *fakeSharedState) Del(keys ...string) error {
	if f.delErr != nil {
		return f.delErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range keys {
		delete(f.strs, k)
	}
	return nil
}
func (f *fakeSharedState) Exists(key string) (bool, error)                 { return false, nil }
func (f *fakeSharedState) Incr(key string) (int64, error)                  { return 0, nil }
func (f *fakeSharedState) Decr(key string) (int64, error)                  { return 0, nil }
func (f *fakeSharedState) IncrWithTTL(key string, ttl time.Duration) (int64, error) { return 0, nil }
func (f *fakeSharedState) TryIncr(key string, max int64) (bool, error)     { return true, nil }
func (f *fakeSharedState) SAdd(key string, members ...string) error        { return nil }
func (f *fakeSharedState) SRem(key string, members ...string) error        { return nil }
func (f *fakeSharedState) SMembers(key string) ([]string, error)           { return nil, nil }
func (f *fakeSharedState) Publish(channel string, data []byte) error       { return nil }
func (f *fakeSharedState) Subscribe(ctx context.Context, channel string, handler func(data []byte)) {
}
func (f *fakeSharedState) HSet(key, field, value string) error             { return nil }
func (f *fakeSharedState) HDel(key, field string) error                    { return nil }
func (f *fakeSharedState) HGetAll(key string) (map[string]string, error)   { return nil, nil }
func (f *fakeSharedState) HIncrBy(key, field string, incr int64) (int64, error) { return 0, nil }
func (f *fakeSharedState) IncrBy(key string, amount int64) (int64, error)  { return 0, nil }
func (f *fakeSharedState) DecrBy(key string, amount int64) (int64, error)  { return 0, nil }
func (f *fakeSharedState) TryIncrBy(key string, delta, max int64) (bool, error) { return true, nil }
func (f *fakeSharedState) Expire(key string, ttl time.Duration) (bool, error)   { return true, nil }

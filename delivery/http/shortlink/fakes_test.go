package shortlink

import (
	"context"

	"vozko/domain/shared"
	shortlinkdomain "vozko/domain/shortlink"
)

type fakeCreate struct {
	link *shortlinkdomain.ShortLink
	err  error
}

func (f fakeCreate) Execute(ctx context.Context, in shortlinkdomain.CreateShortLinkInput) (*shortlinkdomain.ShortLink, error) {
	return f.link, f.err
}

type fakeUpdate struct {
	link *shortlinkdomain.ShortLink
	err  error
}

func (f fakeUpdate) Execute(ctx context.Context, ws, id string, in shortlinkdomain.UpdateShortLinkInput) (*shortlinkdomain.ShortLink, error) {
	return f.link, f.err
}

type fakeGet struct {
	link *shortlinkdomain.ShortLink
	err  error
}

func (f fakeGet) Execute(ctx context.Context, ws, id string) (*shortlinkdomain.ShortLink, error) {
	return f.link, f.err
}

type fakeList struct {
	res *shared.PaginatedResult[*shortlinkdomain.ShortLink]
	err error
}

func (f fakeList) Execute(ctx context.Context, ws string, dep *string, page, pageSize int) (*shared.PaginatedResult[*shortlinkdomain.ShortLink], error) {
	return f.res, f.err
}

type fakeDelete struct {
	err error
}

func (f fakeDelete) Execute(ctx context.Context, ws, id string) error { return f.err }

type fakeStats struct {
	stats *shortlinkdomain.WorkspaceClickStats
	err   error
}

func (f fakeStats) Execute(ctx context.Context, ws string) (*shortlinkdomain.WorkspaceClickStats, error) {
	return f.stats, f.err
}

type fakeResolve struct {
	resolved *shortlinkdomain.ResolvedLink
	err      error
}

func (f fakeResolve) Execute(ctx context.Context, code string) (*shortlinkdomain.ResolvedLink, error) {
	return f.resolved, f.err
}

type fakeUnlock struct {
	resolved *shortlinkdomain.ResolvedLink
	err      error
}

func (f fakeUnlock) Execute(ctx context.Context, code, password string) (*shortlinkdomain.ResolvedLink, error) {
	return f.resolved, f.err
}

type fakePublish struct {
	called bool
	err    error
}

func (f *fakePublish) Execute(ctx context.Context, msg shortlinkdomain.ClickMessage) error {
	f.called = true
	return f.err
}

type fakeAnalytics struct {
	analytics *shortlinkdomain.Analytics
	err       error
}

func (f fakeAnalytics) Execute(ctx context.Context, in shortlinkdomain.AnalyticsInput) (*shortlinkdomain.Analytics, error) {
	return f.analytics, f.err
}

type fakeRecent struct {
	res *shared.PaginatedResult[*shortlinkdomain.Click]
	err error
}

func (f fakeRecent) Execute(ctx context.Context, ws, id string, page, pageSize int) (*shared.PaginatedResult[*shortlinkdomain.Click], error) {
	return f.res, f.err
}

type fakeQR struct {
	data []byte
	err  error
}

func (f fakeQR) Execute(ctx context.Context, ws, id string, size int) ([]byte, error) {
	return f.data, f.err
}

type handlerDeps struct {
	create    shortlinkdomain.CreateShortLinkUseCase
	update    shortlinkdomain.UpdateShortLinkUseCase
	get       shortlinkdomain.GetShortLinkUseCase
	list      shortlinkdomain.ListShortLinksUseCase
	del       shortlinkdomain.DeleteShortLinkUseCase
	stats     shortlinkdomain.GetWorkspaceStatsUseCase
	resolve   shortlinkdomain.ResolveShortLinkUseCase
	unlock    shortlinkdomain.UnlockShortLinkUseCase
	publish   shortlinkdomain.PublishClickUseCase
	analytics shortlinkdomain.GetAnalyticsUseCase
	recent    shortlinkdomain.ListRecentClicksUseCase
	qr        shortlinkdomain.GenerateQRUseCase
}

func defaultDeps() handlerDeps {
	link := &shortlinkdomain.ShortLink{ID: "id-1", Code: "abc123", WorkspaceID: "ws-1"}
	return handlerDeps{
		create:    fakeCreate{link: link},
		update:    fakeUpdate{link: link},
		get:       fakeGet{link: link},
		list:      fakeList{res: shared.NewPaginatedResult([]*shortlinkdomain.ShortLink{link}, shared.Pagination{Page: 1, PageSize: 20}, 1)},
		del:       fakeDelete{},
		stats:     fakeStats{stats: &shortlinkdomain.WorkspaceClickStats{TotalLinks: 1, TotalClicks: 9}},
		resolve:   fakeResolve{resolved: &shortlinkdomain.ResolvedLink{State: shortlinkdomain.ResolveOK, TargetURL: "https://x.com", RedirectType: shortlinkdomain.RedirectTemporary}},
		unlock:    fakeUnlock{resolved: &shortlinkdomain.ResolvedLink{State: shortlinkdomain.ResolveOK, TargetURL: "https://x.com", RedirectType: shortlinkdomain.RedirectTemporary}},
		publish:   &fakePublish{},
		analytics: fakeAnalytics{analytics: &shortlinkdomain.Analytics{TotalClicks: 9}},
		recent:    fakeRecent{res: shared.NewPaginatedResult([]*shortlinkdomain.Click{{ID: "c"}}, shared.Pagination{Page: 1, PageSize: 20}, 1)},
		qr:        fakeQR{data: []byte{0x89, 'P', 'N', 'G'}},
	}
}

func (d handlerDeps) build() *ShortLinkHandler {
	return NewShortLinkHandler(d.create, d.update, d.get, d.list, d.del, d.stats, d.resolve, d.unlock, d.publish, d.analytics, d.recent, d.qr, "https://vx.co/r")
}

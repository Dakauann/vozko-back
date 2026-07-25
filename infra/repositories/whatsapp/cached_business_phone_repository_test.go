package whatsapp_repository

import (
	"sync"
	"sync/atomic"
	"testing"

	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
)

type fakeBPhoneRepo struct {
	mu     sync.Mutex
	byID   map[string]*businessphone.WhatsAppBusinessPhoneNumber
	byMeta map[string]string

	findByIDCalls   int64
	findByMetaCalls int64
	updateCalls     int64
	deleteCalls     int64
}

func newFakeBPhoneRepo() *fakeBPhoneRepo {
	return &fakeBPhoneRepo{
		byID:   make(map[string]*businessphone.WhatsAppBusinessPhoneNumber),
		byMeta: make(map[string]string),
	}
}

func (f *fakeBPhoneRepo) put(p *businessphone.WhatsAppBusinessPhoneNumber) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *p
	f.byID[p.ID] = &cp
	if p.MetaPhoneNumberID != "" {
		f.byMeta[p.MetaPhoneNumberID] = p.ID
	}
}

func (f *fakeBPhoneRepo) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	atomic.AddInt64(&f.findByIDCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeBPhoneRepo) FindByMetaPhoneNumberID(metaID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	atomic.AddInt64(&f.findByMetaCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byMeta[metaID]
	if !ok {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	p := f.byID[id]
	cp := *p
	return &cp, nil
}

func (f *fakeBPhoneRepo) Create(p *businessphone.WhatsAppBusinessPhoneNumber) error {
	f.put(p)
	return nil
}

func (f *fakeBPhoneRepo) Update(id string, p *businessphone.WhatsAppBusinessPhoneNumber) error {
	atomic.AddInt64(&f.updateCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.byID[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}

	if p.MetaPhoneNumberID != "" && p.MetaPhoneNumberID != existing.MetaPhoneNumberID {
		delete(f.byMeta, existing.MetaPhoneNumberID)
		existing.MetaPhoneNumberID = p.MetaPhoneNumberID
		f.byMeta[p.MetaPhoneNumberID] = id
	}
	if p.VerifiedName != "" {
		existing.VerifiedName = p.VerifiedName
	}
	return nil
}

func (f *fakeBPhoneRepo) Delete(id string) error {
	atomic.AddInt64(&f.deleteCalls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	delete(f.byMeta, p.MetaPhoneNumberID)
	delete(f.byID, id)
	return nil
}

func (f *fakeBPhoneRepo) FindByMetaPhoneNumberIDUnscoped(metaID string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return f.FindByMetaPhoneNumberID(metaID)
}
func (f *fakeBPhoneRepo) FindByDisplayPhoneNumber(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (f *fakeBPhoneRepo) FindByWABAId(string) ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (f *fakeBPhoneRepo) List(businessphone.ListInput) (*shared.PaginatedResult[*businessphone.WhatsAppBusinessPhoneNumber], error) {
	return nil, nil
}
func (f *fakeBPhoneRepo) ListAll() ([]*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return nil, nil
}
func (f *fakeBPhoneRepo) BatchUpdate(ps []*businessphone.WhatsAppBusinessPhoneNumber) error {
	for _, p := range ps {
		f.put(p)
	}
	return nil
}
func (f *fakeBPhoneRepo) UpdateStatus(id string, s businessphone.Status) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	p.Status = s
	return nil
}
func (f *fakeBPhoneRepo) UpdateBusinessProfile(id string, prof businessphone.BusinessProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	p.BusinessProfile = prof
	return nil
}
func (f *fakeBPhoneRepo) SyncFromMeta(p *businessphone.WhatsAppBusinessPhoneNumber) error {
	f.put(p)
	return nil
}
func (f *fakeBPhoneRepo) ClearOwner(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	p.OwnerWorkspaceID = ""
	p.OwnerAssignedBy = ""
	p.OwnerAssignedAt = nil
	return nil
}
func (f *fakeBPhoneRepo) ClearAccessToken(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.byID[id]
	if !ok {
		return businessphone.ErrPhoneNumberNotFound
	}
	p.AccessToken = ""
	return nil
}
func (f *fakeBPhoneRepo) Restore(id string) error { return nil }

func samplePhone(id, metaID string) *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                id,
		MetaPhoneNumberID: metaID,
		VerifiedName:      "Acme",
		Status:            businessphone.Status("CONNECTED"),
	}
}

func TestCachedBPhone_FindByMetaPhoneNumberID_CachesAfterFirstRead(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	for i := 0; i < 5; i++ {
		got, err := cached.FindByMetaPhoneNumberID("meta-1")
		if err != nil || got == nil || got.ID != "phone-1" {
			t.Fatalf("iter %d: unexpected result %+v err=%v", i, got, err)
		}
	}
	if c := atomic.LoadInt64(&inner.findByMetaCalls); c != 1 {
		t.Fatalf("expected exactly 1 inner FindByMetaPhoneNumberID call, got %d", c)
	}
}

func TestCachedBPhone_FindByID_CachesAfterFirstRead(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	for i := 0; i < 5; i++ {
		got, err := cached.FindByID("phone-1")
		if err != nil || got == nil || got.MetaPhoneNumberID != "meta-1" {
			t.Fatalf("iter %d: unexpected result %+v err=%v", i, got, err)
		}
	}
	if c := atomic.LoadInt64(&inner.findByIDCalls); c != 1 {
		t.Fatalf("expected exactly 1 inner FindByID call, got %d", c)
	}
}

func TestCachedBPhone_FirstReadByMeta_AlsoCachesByID(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	if _, err := cached.FindByMetaPhoneNumberID("meta-1"); err != nil {
		t.Fatalf("warm: %v", err)
	}

	beforeID := atomic.LoadInt64(&inner.findByIDCalls)
	if _, err := cached.FindByID("phone-1"); err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got := atomic.LoadInt64(&inner.findByIDCalls); got != beforeID {
		t.Fatalf("FindByID should be served from cache, but inner was hit (%d -> %d)", beforeID, got)
	}
}

func TestCachedBPhone_Update_InvalidatesBothKeys(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	_, _ = cached.FindByMetaPhoneNumberID("meta-1")
	_, _ = cached.FindByID("phone-1")

	if err := cached.Update("phone-1", &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-1",
		MetaPhoneNumberID: "meta-1",
		VerifiedName:      "Acme Updated",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	beforeID := atomic.LoadInt64(&inner.findByIDCalls)

	got2, err := cached.FindByID("phone-1")
	if err != nil || got2 == nil || got2.VerifiedName != "Acme Updated" {
		t.Fatalf("post-update id: %+v err=%v", got2, err)
	}
	if afterID := atomic.LoadInt64(&inner.findByIDCalls); afterID == beforeID {
		t.Fatalf("expected inner FindByID after invalidation, count unchanged at %d", afterID)
	}

	got, err := cached.FindByMetaPhoneNumberID("meta-1")
	if err != nil || got == nil || got.VerifiedName != "Acme Updated" {
		t.Fatalf("post-update meta: %+v err=%v", got, err)
	}
}

func TestCachedBPhone_Update_MetaPhoneIDChange_InvalidatesOldMetaKey(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-old"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	_, _ = cached.FindByMetaPhoneNumberID("meta-old")

	if err := cached.Update("phone-1", &businessphone.WhatsAppBusinessPhoneNumber{
		ID:                "phone-1",
		MetaPhoneNumberID: "meta-new",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := cached.FindByMetaPhoneNumberID("meta-old")
	if got != nil {
		t.Fatalf("stale read on old meta key returned: %+v", got)
	}

	got2, err := cached.FindByMetaPhoneNumberID("meta-new")
	if err != nil || got2 == nil || got2.ID != "phone-1" {
		t.Fatalf("new meta key: %+v err=%v", got2, err)
	}
}

func TestCachedBPhone_Delete_InvalidatesBothKeys(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	_, _ = cached.FindByMetaPhoneNumberID("meta-1")
	_, _ = cached.FindByID("phone-1")

	if err := cached.Delete("phone-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if got, _ := cached.FindByMetaPhoneNumberID("meta-1"); got != nil {
		t.Fatalf("expected nil after delete by meta, got %+v", got)
	}
	if got, _ := cached.FindByID("phone-1"); got != nil {
		t.Fatalf("expected nil after delete by id, got %+v", got)
	}
}

func TestCachedBPhone_UpdateStatus_InvalidatesBoth(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	_, _ = cached.FindByMetaPhoneNumberID("meta-1")

	if err := cached.UpdateStatus("phone-1", businessphone.Status("DISCONNECTED")); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, err := cached.FindByMetaPhoneNumberID("meta-1")
	if err != nil || got == nil {
		t.Fatalf("post-status: %+v err=%v", got, err)
	}
	if string(got.Status) != "DISCONNECTED" {
		t.Fatalf("expected DISCONNECTED, got %s", got.Status)
	}
}

func TestCachedBPhone_Create_InvalidatesPrePopulatedKey(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	if got, _ := cached.FindByMetaPhoneNumberID("meta-2"); got != nil {
		t.Fatalf("preflight not nil: %+v", got)
	}

	if err := cached.Create(samplePhone("phone-2", "meta-2")); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := cached.FindByMetaPhoneNumberID("meta-2")
	if err != nil || got == nil || got.ID != "phone-2" {
		t.Fatalf("post-create: %+v err=%v", got, err)
	}
}

func TestCachedBPhone_NotFound_IsNotCached(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	for i := 0; i < 3; i++ {
		if got, _ := cached.FindByMetaPhoneNumberID("missing"); got != nil {
			t.Fatalf("iter %d unexpected hit: %+v", i, got)
		}
	}
	if c := atomic.LoadInt64(&inner.findByMetaCalls); c != 3 {
		t.Fatalf("expected 3 inner calls (not-found should not be cached), got %d", c)
	}
}

func TestCachedBPhone_NilSharedFallsThrough(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, nil)

	for i := 0; i < 4; i++ {
		got, err := cached.FindByMetaPhoneNumberID("meta-1")
		if err != nil || got == nil {
			t.Fatalf("iter %d err=%v got=%+v", i, err, got)
		}
	}

	if c := atomic.LoadInt64(&inner.findByMetaCalls); c != 4 {
		t.Fatalf("expected 4 inner calls without cache, got %d", c)
	}
}

func TestCachedBPhone_EmptyIDsPassThrough(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	if _, err := cached.FindByID(""); err == nil {

		t.Logf("FindByID with empty id returned no error (acceptable)")
	}
	if _, err := cached.FindByMetaPhoneNumberID(""); err == nil {
		t.Logf("FindByMetaPhoneNumberID with empty id returned no error (acceptable)")
	}
}

func TestCachedBPhone_ConcurrentReadsRaceSafe(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	inner.put(samplePhone("phone-1", "meta-1"))
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	const N = 32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = cached.FindByMetaPhoneNumberID("meta-1")
				_, _ = cached.FindByID("phone-1")
			}
		}()
	}
	wg.Wait()
}

func TestCachedBPhone_AccessTokenSurvivesCacheRoundTrip(t *testing.T) {
	t.Parallel()
	inner := newFakeBPhoneRepo()
	phone := samplePhone("phone-1", "meta-1")
	phone.AccessToken = "EAARQfx8secret"
	phone.WABAId = "waba-1"
	inner.put(phone)
	cached := NewCachedBusinessPhoneRepository(inner, newFakeSharedState())

	if _, err := cached.FindByID("phone-1"); err != nil {
		t.Fatalf("warmup FindByID: %v", err)
	}

	got, err := cached.FindByID("phone-1")
	if err != nil || got == nil {
		t.Fatalf("FindByID after warmup: %+v err=%v", got, err)
	}
	if got.AccessToken != "EAARQfx8secret" {
		t.Fatalf("AccessToken lost across cache round-trip: got %q", got.AccessToken)
	}
	if got.WABAId != "waba-1" {
		t.Fatalf("WABAId lost across cache round-trip: got %q", got.WABAId)
	}

	gotByMeta, err := cached.FindByMetaPhoneNumberID("meta-1")
	if err != nil || gotByMeta == nil {
		t.Fatalf("FindByMetaPhoneNumberID: %+v err=%v", gotByMeta, err)
	}
	if gotByMeta.AccessToken != "EAARQfx8secret" {
		t.Fatalf("AccessToken lost on meta lookup: got %q", gotByMeta.AccessToken)
	}

	if c := atomic.LoadInt64(&inner.findByIDCalls); c != 1 {
		t.Fatalf("expected exactly 1 inner FindByID call (cache should serve the second), got %d", c)
	}
}

func (f *fakeBPhoneRepo) UpdateCallsEnabled(string, bool) error { return nil }

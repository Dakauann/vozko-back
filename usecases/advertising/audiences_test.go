package advertising

import (
	"context"
	"errors"
	"slices"
	"testing"

	ads "vozko/domain/advertising"
	"vozko/domain/crmfilter"
)

type fakeCustomers struct{ customers []ads.Customer }

func (f fakeCustomers) Customers(context.Context, string, crmfilter.Filter, int) ([]ads.Customer, error) {
	return f.customers, nil
}

type fakeFiles struct{ data []byte }

func (f fakeFiles) Bytes(context.Context, string, string) ([]byte, error) { return f.data, nil }

type fakeSaved struct{ byID map[string]*ads.SavedAudience }

func (f *fakeSaved) Create(_ context.Context, s *ads.SavedAudience) error {
	s.ID = "saved-1"
	f.byID[s.ID] = s
	return nil
}
func (f *fakeSaved) Update(_ context.Context, s *ads.SavedAudience) error {
	f.byID[s.ID] = s
	return nil
}
func (f *fakeSaved) Delete(context.Context, string, string) error { return nil }
func (f *fakeSaved) Find(_ context.Context, ws, id string) (*ads.SavedAudience, error) {
	s, ok := f.byID[id]
	if !ok || s.WorkspaceID != ws {
		return nil, ads.ErrSavedAudienceNotFound
	}
	return s, nil
}
func (f *fakeSaved) List(context.Context, string) ([]*ads.SavedAudience, error) { return nil, nil }

func audienceUseCase(w *world, customers []ads.Customer, file []byte) *AudienceUseCase {
	uc := NewAudienceUseCase(w.sync, w.gateway, fakeCustomers{customers: customers}, fakeFiles{data: file}, &fakeSaved{byID: map[string]*ads.SavedAudience{}})
	uc.sessionID = func() int64 { return 42 }
	return uc
}

func TestCRMCustomerListIsHashedUploadedAndCounted(t *testing.T) {
	w := newWorld()
	uc := audienceUseCase(w, []ads.Customer{{ads.MatchPhone: "5511988887777", ads.MatchFirstName: "Ana"}, {ads.MatchPhone: "12"}}, nil)
	got, err := uc.CreateCustomerList(context.Background(), "ws-1", ads.CustomerListDraft{AdAccountID: "acc-1", Name: "Clientes", Source: ads.SourceCRM})
	if err != nil {
		t.Fatal(err)
	}
	if got.Matched != 1 || got.Skipped != 1 || len(w.gateway.batches) != 1 || w.gateway.batches[0].Rows[0][0] != ads.SHA256Hex("5511988887777") {
		t.Fatalf("result %+v batches %+v", got, w.gateway.batches)
	}
}

func TestCSVCustomerListUsesTheMappedColumnsAndSkipsTheHeader(t *testing.T) {
	w := newWorld()
	csv := []byte("nome;email;telefone\nAna;ana@x.com;(11) 98888-7777\n")
	uc := audienceUseCase(w, nil, csv)
	got, err := uc.CreateCustomerList(context.Background(), "ws-1", ads.CustomerListDraft{
		AdAccountID: "acc-1", Name: "Arquivo", Source: ads.SourceFile, FileMediaID: "m", SkipHeader: true,
		Columns: []ads.MatchKey{ads.MatchFirstName, ads.MatchEmail, ads.MatchPhone},
	})
	if err != nil || got.Matched != 1 {
		t.Fatalf("result %+v err %v", got, err)
	}
	if b := w.gateway.batches[0]; b.Rows[0][2] != ads.SHA256Hex("5511988887777") || b.Rows[0][1] != ads.SHA256Hex("ana@x.com") {
		t.Fatalf("batch %+v", b)
	}
}

func TestNoCustomerListWithoutAcceptedTerms(t *testing.T) {
	w := newWorld()
	w.gateway.termsOK = false
	_, err := audienceUseCase(w, []ads.Customer{{ads.MatchPhone: "5511988887777"}}, nil).
		CreateCustomerList(context.Background(), "ws-1", ads.CustomerListDraft{AdAccountID: "acc-1", Name: "x", Source: ads.SourceCRM})
	if !errors.Is(err, ads.ErrAudienceTermsNotAccepted) || slices.Contains(w.gateway.calls, "create_audience") {
		t.Fatalf("err %v calls %v", err, w.gateway.calls)
	}
}

func TestPartialUploadRemovesTheAudience(t *testing.T) {
	w := newWorld()
	w.gateway.failOn, w.gateway.failWith = "add_customers", &ads.RemoteError{Kind: ads.FailureRejected, Code: 2650}
	_, err := audienceUseCase(w, []ads.Customer{{ads.MatchPhone: "5511988887777"}}, nil).
		CreateCustomerList(context.Background(), "ws-1", ads.CustomerListDraft{AdAccountID: "acc-1", Name: "x", Source: ads.SourceCRM})
	if err == nil || !slices.Contains(w.gateway.calls, "delete_audience:aud-new") {
		t.Fatalf("err %v calls %v", err, w.gateway.calls)
	}
}

func TestLookalikeNeedsAnOriginFromTheSameAccount(t *testing.T) {
	w := newWorld()
	uc := audienceUseCase(w, nil, nil)
	_, err := uc.CreateLookalike(context.Background(), "ws-1", ads.LookalikeDraft{AdAccountID: "acc-1", Name: "Parecidos", OriginAudienceID: "other", Percent: 1})
	requireIssue(t, err, "originAudienceId", "not_available")
	w.gateway.audiences = []ads.Audience{{MetaID: "aud-1"}}
	if got, err := uc.CreateLookalike(context.Background(), "ws-1", ads.LookalikeDraft{AdAccountID: "acc-1", Name: "Parecidos", OriginAudienceID: "aud-1", Percent: 2}); err != nil || got.MetaID != "lal-new" {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestSavedAudiencesAreWorkspaceScoped(t *testing.T) {
	w := newWorld()
	uc := audienceUseCase(w, nil, nil)
	created, err := uc.Save(context.Background(), "ws-1", "u-1", ads.SavedAudience{Name: "SP", Targeting: ads.Targeting{Locations: []ads.GeoLocation{{Kind: ads.LocationCountry, Key: "BR"}}}})
	if err != nil || created.CreatedBy != "u-1" {
		t.Fatalf("created %+v err %v", created, err)
	}
	created.Name = "SP 2"
	if _, err := uc.Save(context.Background(), "ws-2", "u-2", *created); !errors.Is(err, ads.ErrSavedAudienceNotFound) {
		t.Fatalf("cross workspace update: %v", err)
	}
}

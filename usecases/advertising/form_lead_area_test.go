package advertising

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/address"
	ads "vozko/domain/advertising"
	"vozko/domain/lead"
	"vozko/domain/shared"
	lead_usecase "vozko/usecases/lead"
)

type fakeLeadProfiles struct {
	updates []lead_usecase.ProfileUpdate
	err     error
	errs    []error
}

func (f *fakeLeadProfiles) Update(_ context.Context, in lead_usecase.ProfileUpdate) (lead_usecase.ProfileResult, error) {
	f.updates = append(f.updates, in)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return lead_usecase.ProfileResult{LeadID: in.LeadID}, err
	}
	return lead_usecase.ProfileResult{LeadID: in.LeadID}, f.err
}

func areaFormWorld(profiles leadProfiles) (*world, *FormsUseCase, *fakeFormLeads) {
	w := newWorld()
	forms := &fakeTrackedForms{byID: map[string]*ads.TrackedForm{"f-1": {MetaID: "f-1", WorkspaceID: "ws-1", AdAccountID: "acc-1", PageID: "page-1"}}}
	leads := &fakeFormLeads{saved: map[string]*ads.FormLead{}, linked: map[string]string{}}
	return w, NewFormsUseCase(w.sync, w.gateway, forms, leads, &fakeCRM{}, profiles), leads
}

func TestAFormLeadCarriesItsCityStateAndZipIntoAPendingAddress(t *testing.T) {
	profiles := &fakeLeadProfiles{}
	w, uc, leads := areaFormWorld(profiles)
	w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{
		"full_name": "Ana", "phone_number": "+55 11 98888-7777", "city": "São Paulo", "state": "SP", "zip_code": "01310-100",
	}}}

	imported, err := uc.Sync(context.Background(), "ws-1", "f-1")
	if err != nil || imported != 1 {
		t.Fatalf("imported %d err %v", imported, err)
	}
	want := []lead_usecase.ProfileUpdate{{
		WorkspaceID: "ws-1", LeadID: "lead-5511988887777", Actor: actor.SystemID, Source: lead.ProfileFromForm,
		Profile: lead.Profile{Address: address.Postal{ZipCode: "01310-100", City: "São Paulo", State: "SP"}},
	}}
	if !reflect.DeepEqual(profiles.updates, want) {
		t.Fatalf("updates = %+v", profiles.updates)
	}
	if leads.linked["l-1"] != "lead-5511988887777" {
		t.Fatalf("linked %v", leads.linked)
	}
}

func TestAFormLeadWithoutAreaAnswersWritesNoAddress(t *testing.T) {
	profiles := &fakeLeadProfiles{}
	w, uc, _ := areaFormWorld(profiles)
	w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{"full_name": "Ana", "phone_number": "+55 11 98888-7777"}}}
	if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); err != nil {
		t.Fatal(err)
	}
	if len(profiles.updates) != 0 {
		t.Fatalf("updates = %+v", profiles.updates)
	}
}

func TestARefusedFormAddressNeverStopsTheImport(t *testing.T) {
	profiles := &fakeLeadProfiles{err: lead.ErrProfileCEPUnknown}
	w, uc, leads := areaFormWorld(profiles)
	w.gateway.leads = []ads.FormLead{
		{MetaID: "l-1", Answers: map[string]string{"phone_number": "+55 11 98888-7777", "zip_code": "99999-999"}},
		{MetaID: "l-2", Answers: map[string]string{"phone_number": "+55 11 97777-6666", "city": "Santos", "state": "SP"}},
	}
	imported, err := uc.Sync(context.Background(), "ws-1", "f-1")
	if err != nil || imported != 2 || len(leads.linked) != 2 {
		t.Fatalf("imported %d err %v linked %v", imported, err, leads.linked)
	}
}

func TestAFormAddressThatFailsForAnotherReasonIsReported(t *testing.T) {
	w, uc, leads := areaFormWorld(&fakeLeadProfiles{err: errors.New("database down")})
	w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{"phone_number": "+55 11 98888-7777", "city": "Santos", "state": "SP"}}}
	if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); err == nil {
		t.Fatal("an internal failure must be reported")
	}
	if leads.linked["l-1"] != "lead-5511988887777" {
		t.Fatalf("the lead is linked before its address is written, got %v", leads.linked)
	}
}

func TestAFormAddressWithoutTheLeadProfilesIsRefused(t *testing.T) {
	w, uc, _ := areaFormWorld(nil)
	w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{"phone_number": "+55 11 98888-7777", "city": "Santos", "state": "SP"}}}
	if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); !errors.Is(err, errFormAddressUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestAFormCEPThatCannotBeCheckedKeepsTheCityAndState(t *testing.T) {
	profiles := &fakeLeadProfiles{errs: []error{fmt.Errorf("%w: viacep down", lead.ErrProfileCEPUnchecked)}}
	w, uc, _ := areaFormWorld(profiles)
	w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{"phone_number": "+55 11 98888-7777", "city": "Santos", "state": "SP", "zip_code": "11010-000"}}}
	if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); err != nil {
		t.Fatal(err)
	}
	if len(profiles.updates) != 2 || profiles.updates[1].Profile.Address != (address.Postal{City: "Santos", State: "SP"}) {
		t.Fatalf("updates = %+v, want a second write without the CEP", profiles.updates)
	}
}

func TestAFormAddressThatIsOnlyAnUncheckedCEPIsReported(t *testing.T) {
	profiles := &fakeLeadProfiles{err: fmt.Errorf("%w: viacep down", lead.ErrProfileCEPUnchecked)}
	w, uc, _ := areaFormWorld(profiles)
	w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{"phone_number": "+55 11 98888-7777", "zip_code": "11010-000"}}}
	if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); !errors.Is(err, lead.ErrProfileCEPUnchecked) {
		t.Fatalf("err = %v, the address cannot be kept and must be reported", err)
	}
}

func TestAFormAddressLostToRacesOrAMissingLeadIsReported(t *testing.T) {
	for _, cause := range []error{shared.ErrVersionConflict, lead.ErrLeadNotFound} {
		w, uc, _ := areaFormWorld(&fakeLeadProfiles{err: cause})
		w.gateway.leads = []ads.FormLead{{MetaID: "l-1", Answers: map[string]string{"phone_number": "+55 11 98888-7777", "city": "Santos", "state": "SP"}}}
		if _, err := uc.Sync(context.Background(), "ws-1", "f-1"); !errors.Is(err, cause) {
			t.Fatalf("err = %v, want %v reported", err, cause)
		}
	}
}

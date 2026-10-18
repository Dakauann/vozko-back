package smartcampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
)

type templateSource map[string]*template.Template

func (s templateSource) FindByID(id string) (*template.Template, error) {
	if t, ok := s[id]; ok {
		return t, nil
	}
	return nil, errBroken
}

type phoneSource map[string]*businessphone.WhatsAppBusinessPhoneNumber

func (s phoneSource) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if p, ok := s[id]; ok {
		return p, nil
	}
	return nil, errBroken
}

type timeline map[string]map[quality.Signal]quality.Change

func (t timeline) LatestBySignal(_ quality.Subject, externalID string) (map[quality.Signal]quality.Change, error) {
	return t[externalID], nil
}

type probe struct {
	info  businessphone.MetaPhoneNumberInfo
	err   error
	calls int
}

func (p *probe) RefreshPhone(_ context.Context, _ *businessphone.WhatsAppBusinessPhoneNumber) (businessphone.MetaPhoneNumberInfo, error) {
	p.calls++
	return p.info, p.err
}

var changedAt = time.Date(2026, 10, 16, 12, 56, 0, 0, time.UTC)

func metaPhone() *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone", MetaPhoneNumberID: "m1", WABAId: "waba", AccessToken: "t", Provider: businessphone.ProviderMeta,
		Status: businessphone.StatusConnected, QualityRating: businessphone.QualityRatingGreen}
}

func TestTemplateHealthCombinesTheRowAndItsTimeline(t *testing.T) {
	reader := NewHealthReader(
		templateSource{"tpl": {ID: "tpl", ExternalID: "x1", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryUtility}},
		phoneSource{}, timeline{"x1": {
			quality.SignalQuality: {Current: "YELLOW", OccurredAt: changedAt},
			quality.SignalStatus:  {Current: "APPROVED", OccurredAt: changedAt.Add(-time.Hour)},
		}}, &probe{})

	state, err := reader.Template(ctx, "tpl")

	if err != nil || state.Quality != businessphone.QualityRatingYellow || !state.QualityAt.Equal(changedAt) || state.Status != template.TemplateStatusApproved || !state.StatusAt.Equal(changedAt.Add(-time.Hour)) {
		t.Fatalf("template state = %+v, %v", state, err)
	}
}

func TestATemplateWithoutQualityHistoryIsUnknownNotGreen(t *testing.T) {
	reader := NewHealthReader(templateSource{"tpl": {ID: "tpl", ExternalID: "x1", Status: template.TemplateStatusApproved}}, phoneSource{}, timeline{}, &probe{})
	state, err := reader.Template(ctx, "tpl")
	if err != nil || state.Quality != businessphone.QualityRatingUnknown {
		t.Fatalf("template state = %+v, %v", state, err)
	}
}

func TestAMissingTemplateIsAnError(t *testing.T) {
	reader := NewHealthReader(templateSource{}, phoneSource{}, timeline{}, &probe{})
	if _, err := reader.Template(ctx, "gone"); err == nil {
		t.Fatal("a template that cannot be read must not look healthy")
	}
}

func TestAStoredPhoneReadNeverCallsMeta(t *testing.T) {
	p := &probe{}
	reader := NewHealthReader(templateSource{}, phoneSource{"phone": metaPhone()}, timeline{"m1": {quality.SignalStatus: {Current: "CONNECTED", OccurredAt: changedAt}}}, p)

	state, err := reader.Phone(ctx, "phone", false)

	if err != nil || p.calls != 0 || state.Status != businessphone.StatusConnected || !state.ChangedAt.Equal(changedAt) {
		t.Fatalf("phone state = %+v, %v, meta calls %d", state, err, p.calls)
	}
}

func TestAFreshPhoneReadUsesWhatMetaSays(t *testing.T) {
	p := &probe{info: businessphone.MetaPhoneNumberInfo{ID: "m1", Status: "flagged", QualityRating: "yellow"}}
	reader := NewHealthReader(templateSource{}, phoneSource{"phone": metaPhone()}, timeline{}, p)

	state, err := reader.Phone(ctx, "phone", true)

	if err != nil || p.calls != 1 || state.Status != businessphone.StatusFlagged || state.Quality != businessphone.QualityRatingYellow {
		t.Fatalf("phone state = %+v, %v", state, err)
	}
}

func TestAFailedFreshReadIsAnError(t *testing.T) {
	p := &probe{err: errBroken}
	reader := NewHealthReader(templateSource{}, phoneSource{"phone": metaPhone()}, timeline{}, p)
	if _, err := reader.Phone(ctx, "phone", true); !errors.Is(err, errBroken) {
		t.Fatalf("fresh read = %v, want the probe failure", err)
	}
}

func TestANumberMetaCannotBeAskedAboutKeepsItsStoredState(t *testing.T) {
	dialog := metaPhone()
	dialog.Provider = businessphone.ProviderDialog360
	p := &probe{}
	reader := NewHealthReader(templateSource{}, phoneSource{"phone": dialog}, timeline{}, p)

	state, err := reader.Phone(ctx, "phone", true)

	if err != nil || p.calls != 0 || state.Status != businessphone.StatusConnected {
		t.Fatalf("phone state = %+v, %v, meta calls %d", state, err, p.calls)
	}
}

func TestATemplateIsFlaggedWhileMetaLastSaidFlagged(t *testing.T) {
	flagged := func(latest string) bool {
		reader := NewHealthReader(
			templateSource{"tpl": {ID: "tpl", ExternalID: "x1", Status: template.TemplateStatusApproved}},
			phoneSource{}, timeline{"x1": {quality.SignalStatus: {Current: latest, OccurredAt: changedAt}}}, &probe{})
		state, err := reader.Template(ctx, "tpl")
		if err != nil {
			t.Fatalf("template state: %v", err)
		}
		return state.Flagged
	}

	if !flagged("FLAGGED") {
		t.Fatal("a template whose last status event is FLAGGED must read as flagged")
	}
	if flagged("REINSTATED") || flagged("APPROVED") {
		t.Fatal("a later status event clears the flag")
	}
}

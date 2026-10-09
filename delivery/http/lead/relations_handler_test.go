package lead

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vozko/domain/address"
	"vozko/domain/conversation"
	"vozko/domain/geo"
	leaddomain "vozko/domain/lead"
	"vozko/domain/shared"
	lead_usecase "vozko/usecases/lead"
)

const relativeLeadID = "1d2c3b4a-5f6e-4d7c-8b9a-0f1e2d3c4b5a"

func (s *stubCommands) AddRelative(_ context.Context, _ lead_usecase.Actor, _ string, in lead_usecase.AddRelativeInput) (lead_usecase.RelativeResult, error) {
	s.relative = in
	anchor, err := s.answer("relative", nil)
	if err != nil {
		return lead_usecase.RelativeResult{}, err
	}
	return lead_usecase.RelativeResult{
		Lead:       anchor,
		Relative:   &leaddomain.Lead{ID: relativeLeadID, Name: in.Relative.Name, Version: 1},
		Relation:   leaddomain.Relation{ID: "r-1", LeadID: anchor.ID, OtherLeadID: relativeLeadID, Kind: leaddomain.KindChild},
		Duplicates: s.duplicates,
	}, nil
}

func (s *stubCommands) LinkRelation(_ context.Context, _ lead_usecase.Actor, id, otherID string, kind leaddomain.RelationKind) (lead_usecase.RelationResult, error) {
	s.linked, s.kind = [2]string{id, otherID}, kind
	l, err := s.answer("link", nil)
	if err != nil {
		return lead_usecase.RelationResult{}, err
	}
	return lead_usecase.RelationResult{Lead: l, Relation: leaddomain.Relation{ID: "r-2", LeadID: otherID, OtherLeadID: id, Kind: leaddomain.KindReferred}}, nil
}

func (s *stubCommands) RemoveRelation(_ context.Context, _ lead_usecase.Actor, relationID string) (leaddomain.Relation, error) {
	s.removed = relationID
	_, err := s.answer("unlink", nil)
	return leaddomain.Relation{ID: relationID}, err
}

func TestCreateTakesPhonesAndAddressesAndAnswersTheDuplicateWarnings(t *testing.T) {
	cmds := &stubCommands{
		result: &leaddomain.Lead{ID: routeLeadID, Name: "Maria", Version: 1,
			Phones:    []leaddomain.ContactPhone{{ID: "p-1", Number: "551133334444", Label: leaddomain.PhoneLandline}},
			Addresses: []leaddomain.Address{{ID: "a-1", Label: leaddomain.AddressHome, Primary: true, Postal: address.Postal{ZipCode: "01310100", City: "São Paulo", State: "SP"}, GeoStatus: leaddomain.GeoPending}}},
		duplicates: []lead_usecase.DuplicateWarning{
			{LeadID: relativeLeadID, Reasons: []leaddomain.DuplicateReason{leaddomain.DuplicateSharedPhone}, Lead: &leaddomain.Lead{ID: relativeLeadID, Name: "João", Number: "5511912345678"}},
			{LeadID: "hidden", Reasons: []leaddomain.DuplicateReason{leaddomain.DuplicateSameNameAndAddress}},
		},
	}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads", nil, map[string]any{
		"name":      "Maria",
		"phones":    []map[string]any{{"number": "1133334444", "label": "landline"}},
		"addresses": []map[string]any{{"label": "home", "zipCode": "01310-100", "city": "São Paulo", "state": "SP"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if len(cmds.draft.Phones) != 1 || cmds.draft.Phones[0].Label != leaddomain.PhoneLandline || len(cmds.draft.Addresses) != 1 || cmds.draft.Addresses[0].Postal.ZipCode != "01310-100" {
		t.Fatalf("draft = %+v", cmds.draft)
	}
	var out CreateLeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != routeLeadID || len(out.Phones) != 1 || out.Phones[0].Label != "landline" || len(out.Addresses) != 1 || out.Addresses[0].GeoStatus != "pending" {
		t.Fatalf("record = %+v", out.LeadRecordResponse)
	}
	if len(out.Duplicates) != 2 || out.Duplicates[0].Name != "João" || out.Duplicates[0].Reasons[0] != "shared_phone" || out.Duplicates[1].Name != "" {
		t.Fatalf("duplicates = %+v", out.Duplicates)
	}
}

func TestUpdateTakesTheNumberPhonesAndAddressesOfTheAggregate(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{
		"number":    "5521998765432",
		"phones":    []map[string]any{},
		"addresses": []map[string]any{{"id": "a-1", "label": "home", "primary": true, "zipCode": "01310100", "keepPosition": true}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	e := cmds.edit
	if e.Number == nil || *e.Number != "5521998765432" || e.Phones == nil || len(*e.Phones) != 0 || e.Addresses == nil {
		t.Fatalf("edit = %+v", e)
	}
	if a := (*e.Addresses)[0]; a.ID != "a-1" || !a.Primary || !a.KeepFix {
		t.Fatalf("address = %+v", a)
	}
	if e.Name != nil {
		t.Fatal("fields that were not sent stay nil")
	}
}

func TestTheClientNeverWritesAPosition(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{
		"addresses": []map[string]any{{"label": "home", "zipCode": "01310100", "latitude": -23.5, "geoStatus": "located"}},
	})
	if rec.Code != http.StatusBadRequest || len(cmds.calls) != 0 {
		t.Fatalf("status = %d calls = %v: geo columns are server owned", rec.Code, cmds.calls)
	}
}

func TestAddRelativeCreatesTheRelativeWithItsRelation(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Name: "Maria", RelativesCount: 1, Version: 3}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/relatives", nil, map[string]any{
		"kind":               "child",
		"relative":           map[string]any{"name": "Pedro", "phones": []map[string]any{{"number": "5511912345678", "label": "mobile"}}},
		"copyPrimaryAddress": true,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	in := cmds.relative
	if in.Kind != leaddomain.KindChild || in.Relative.Name != "Pedro" || len(in.Relative.Phones) != 1 || !in.CopyPrimaryAddress {
		t.Fatalf("input = %+v", in)
	}
	var out AddRelativeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Lead.RelativesCount != 1 || out.Relative.ID != relativeLeadID || out.Relation.ID != "r-1" || out.Relation.RelativeID != relativeLeadID ||
		out.Relation.Kind != "child" || out.Relation.Dimension != "family" {
		t.Fatalf("body = %+v", out)
	}
}

func TestAddRelativeRefusesABodyWithoutKindOrRelative(t *testing.T) {
	for name, body := range map[string]any{
		"no kind":        map[string]any{"relative": map[string]any{"name": "Pedro"}},
		"no relative":    map[string]any{"kind": "child"},
		"an unknown key": map[string]any{"kind": "child", "relative": map[string]any{"name": "Pedro"}, "copy": true},
	} {
		t.Run(name, func(t *testing.T) {
			cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID}}
			rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/relatives", nil, body)
			if rec.Code != http.StatusBadRequest || len(cmds.calls) != 0 {
				t.Fatalf("status = %d calls = %v", rec.Code, cmds.calls)
			}
		})
	}
}

func TestLinkRelationAnswersTheRelationSeenFromTheLead(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 3}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/relations", nil, map[string]any{"otherLeadId": relativeLeadID, "kind": "referred_by"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if cmds.linked != [2]string{routeLeadID, relativeLeadID} || cmds.kind != leaddomain.KindReferredBy {
		t.Fatalf("linked = %v %s", cmds.linked, cmds.kind)
	}
	var out LinkRelationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Relation.LeadID != routeLeadID || out.Relation.RelativeID != relativeLeadID || out.Relation.Kind != "referred_by" || out.Relation.Dimension != "referral" || out.Lead.Version != 3 {
		t.Fatalf("body = %+v", out)
	}
}

func TestRemoveRelation(t *testing.T) {
	cmds := &stubCommands{}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodDelete, "/lead-relations/"+relativeLeadID, nil, nil)
	if rec.Code != http.StatusNoContent || cmds.removed != relativeLeadID {
		t.Fatalf("status = %d removed = %q", rec.Code, cmds.removed)
	}
	if rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodDelete, "/lead-relations/not-a-uuid", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("a non uuid id must not reach the handler, status = %d", rec.Code)
	}
}

func TestCollectionErrorsMapToStatusCodeAndDetails(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		status   int
		code     string
		expected map[string]string
	}{
		{"a bad phone", &leaddomain.ItemError{Field: leaddomain.FieldPhones, Index: 1, Err: leaddomain.ErrPhoneRepeatsIdentity}, http.StatusBadRequest, "lead_phone_repeats_identity", map[string]string{"field": "phones", "index": "1"}},
		{"a bad address", &leaddomain.ItemError{Field: leaddomain.FieldAddresses, Index: 0, Err: address.InvalidFieldError{Field: address.FieldZipCode, Rule: address.RuleFormat}}, http.StatusBadRequest, "lead_address_invalid", map[string]string{"field": "addresses", "index": "0", "addressField": "zipCode", "rule": "format"}},
		{"two primaries", leaddomain.ErrAddressPrimary, http.StatusBadRequest, "lead_address_primary", nil},
		{"a taken identity names its lead", &leaddomain.IdentityTaken{LeadID: relativeLeadID}, http.StatusConflict, "lead_identity_taken", map[string]string{"leadId": relativeLeadID}},
		{"an identity in use", leaddomain.ErrIdentityInUse, http.StatusConflict, "lead_identity_in_use", nil},
		{"a relation that exists", leaddomain.ErrRelationExists, http.StatusConflict, "lead_relation_exists", nil},
		{"a relative that is not there", leaddomain.ErrRelativeNotFound, http.StatusNotFound, "lead_relative_not_found", nil},
		{"a relation that is not there", leaddomain.ErrRelationNotFound, http.StatusNotFound, "lead_relation_not_found", nil},
		{"a self relation", leaddomain.ErrRelationSelf, http.StatusBadRequest, "lead_relation_self", nil},
		{"no primary address to copy", leaddomain.ErrNoPrimaryAddress, http.StatusBadRequest, "lead_no_primary_address", nil},
		{"a stale aggregate never answers 500", leaddomain.ErrAggregateNotLoaded, http.StatusInternalServerError, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, routedHandler(&stubCommands{err: tc.err}, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/relations", nil, map[string]any{"otherLeadId": relativeLeadID, "kind": "sibling"})
			var out struct {
				Code     string            `json:"code"`
				Expected map[string]string `json:"expected"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if rec.Code != tc.status || out.Code != tc.code {
				t.Fatalf("status = %d code = %q, want %d %q", rec.Code, out.Code, tc.status, tc.code)
			}
			for k, v := range tc.expected {
				if out.Expected[k] != v {
					t.Fatalf("details = %v, want %s=%s", out.Expected, k, v)
				}
			}
		})
	}
}

func TestTheDetailCarriesPhonesAddressesAndTheFamilyCounts(t *testing.T) {
	fixed := geo.Fix{Point: geo.Point{Lat: -23.56, Lng: -46.65}, Precision: geo.PrecisionPostalCode, Source: geo.SourceReference}
	history := &stubHistory{detail: &lead_usecase.LeadDetail{
		Lead: &leaddomain.Lead{ID: routeLeadID, Version: 2, RelativesCount: 3, ReferredCount: 1200,
			Phones:    []leaddomain.ContactPhone{{ID: "p-1", Number: "551133334444", Label: leaddomain.PhoneLandline}},
			Addresses: []leaddomain.Address{{ID: "a-1", Label: leaddomain.AddressHome, Primary: true, Postal: address.Postal{ZipCode: "01310100", District: "Bela Vista", City: "São Paulo", State: "SP"}, Fix: &fixed, GeoStatus: leaddomain.GeoApproximate}}},
	}}
	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/leads/"+routeLeadID, nil, nil)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if _, listed := out["relatives"]; listed {
		t.Fatal("the detail no longer lists the family; the family tab pages it")
	}
	var detail LeadDetailResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if len(detail.Phones) != 1 || len(detail.Addresses) != 1 || detail.Addresses[0].Precision != "postal_code" || detail.RelativesCount != 3 || detail.ReferredCount != 1200 {
		t.Fatalf("detail = %+v", detail.LeadRecordResponse)
	}
}

func TestTheRelativesPageReadsItsQueryAndAnswersTheCursor(t *testing.T) {
	history := &stubHistory{relatives: leaddomain.RelativesPage{
		Relatives: []leaddomain.Relative{{Relation: leaddomain.Relation{ID: "r-1", LeadID: relativeLeadID, OtherLeadID: routeLeadID, Kind: leaddomain.KindReferred},
			Lead: &leaddomain.Lead{ID: relativeLeadID, Name: "Joana Lima", Number: "5511912345678"}}},
		Next: "next-page",
	}}
	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/leads/"+routeLeadID+"/relatives?dimension=referral&after=abc&limit=20", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	want := leaddomain.RelativesQuery{LeadID: routeLeadID, Dimension: leaddomain.DimensionReferral, After: "abc", Limit: 20}
	if history.relativesQuery != want {
		t.Fatalf("query = %+v, want %+v", history.relativesQuery, want)
	}
	var out RelativesPageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Next != "next-page" || len(out.Items) != 1 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if item := out.Items[0]; item.Kind != "referred_by" || item.Dimension != "referral" || item.LeadID != relativeLeadID || item.Name != "Joana Lima" {
		t.Fatalf("item = %+v, want the kind seen from this lead", item)
	}

	if rec := send(t, routedHandler(&stubCommands{}, &stubHistory{}), http.MethodGet, "/leads/"+routeLeadID+"/relatives?limit=many", nil, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("a limit that is not a number = %d", rec.Code)
	}
	refused := &stubHistory{err: leaddomain.ErrRelativesQueryInvalid}
	rec = send(t, routedHandler(&stubCommands{}, refused), http.MethodGet, "/leads/"+routeLeadID+"/relatives?dimension=friends", nil, nil)
	if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("lead_relatives_query_invalid")) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestLinkingAMalformedLeadAnswersRelativeNotFound(t *testing.T) {
	cmds := &stubCommands{err: leaddomain.ErrRelativeNotFound}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/relations", nil, map[string]any{"otherLeadId": "abc", "kind": "sibling"})
	if rec.Code != http.StatusNotFound || !bytes.Contains(rec.Body.Bytes(), []byte("lead_relative_not_found")) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestSetDistrictIsAFieldScopedCommand(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 5,
		Addresses: []leaddomain.Address{{ID: "a-1", Label: leaddomain.AddressHome, Primary: true, Postal: address.Postal{District: "Jardim Paulista", City: "São Paulo", State: "SP"}, GeoStatus: leaddomain.GeoPending}}}}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/district", nil, map[string]any{"district": "Jardim Paulista", "city": "São Paulo", "state": "SP"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if cmds.area != (leaddomain.Area{District: "Jardim Paulista", City: "São Paulo", State: "SP"}) || cmds.expected != nil {
		t.Fatalf("area = %+v expected = %v", cmds.area, cmds.expected)
	}
	var out LeadRecordResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Version != 5 || out.Addresses[0].District != "Jardim Paulista" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	for _, body := range []map[string]any{{"district": "Centro"}, {"district": "Centro", "city": "São Paulo", "state": "SP", "street": "x"}} {
		refused := &stubCommands{}
		if rec := send(t, routedHandler(refused, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/district", nil, body); rec.Code != http.StatusBadRequest || len(refused.calls) != 0 {
			t.Fatalf("body %v = %d", body, rec.Code)
		}
	}
}

func TestTheEntryLeadCardIsReadThroughTheConversation(t *testing.T) {
	history := &stubHistory{card: leaddomain.Card{LeadID: routeLeadID, Version: 4, Name: "Ana", Owner: "u-1", RelativesCount: 2,
		Area: &leaddomain.Area{District: "Bela Vista", City: "São Paulo", State: "SP"}, CustomFields: map[string]any{"interesse": "alto"}}}
	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/entries/"+relativeLeadID+"/lead?entryType=instagram", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if history.cardEntry != relativeLeadID+"/instagram" {
		t.Fatalf("entry = %q", history.cardEntry)
	}
	var out LeadCardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.LeadID != routeLeadID || out.Area == nil || out.Area.District != "Bela Vista" || out.RelativesCount != 2 || out.CustomFields["interesse"] != "alto" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	for err, code := range map[error]string{lead_usecase.ErrConversationNotVisible: "conversation_not_found", leaddomain.ErrLeadNotFound: "lead_not_found"} {
		rec := send(t, routedHandler(&stubCommands{}, &stubHistory{err: err}), http.MethodGet, "/entries/"+relativeLeadID+"/lead", nil, nil)
		if rec.Code != http.StatusNotFound || !bytes.Contains(rec.Body.Bytes(), []byte(code)) {
			t.Fatalf("%v: status = %d body = %s", err, rec.Code, rec.Body.String())
		}
	}
}

func (s *stubCommands) SetArea(_ context.Context, _ lead_usecase.Actor, _ string, area leaddomain.Area) (*leaddomain.Lead, error) {
	s.area = area
	return s.answer("district", nil)
}

func (s *stubHistory) Relatives(_ context.Context, _ conversation.Viewer, q leaddomain.RelativesQuery) (leaddomain.RelativesPage, error) {
	s.relativesQuery = q
	return s.relatives, s.err
}

func (s *stubHistory) EntryLead(_ context.Context, _ conversation.Viewer, entryID string, entryType shared.EntryType) (leaddomain.Card, error) {
	s.cardEntry = entryID + "/" + string(entryType)
	return s.card, s.err
}

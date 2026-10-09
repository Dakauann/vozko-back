package lead

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"vozko/domain/customfield"
	leaddomain "vozko/domain/lead"
	lead_usecase "vozko/usecases/lead"
)

func (s *stubCommands) Anonymize(_ context.Context, _ lead_usecase.Actor, id string) (leaddomain.Erasure, error) {
	s.calls = append(s.calls, "anonymize:"+id)
	if s.err != nil {
		return leaddomain.Erasure{}, s.err
	}
	return leaddomain.Erasure{LeadID: id, Version: 6, At: time.Date(2026, time.October, 8, 12, 0, 0, 0, time.UTC),
		Rows: map[leaddomain.ErasureTarget]int64{leaddomain.ErasureRecord: 1, leaddomain.ErasureCallNumbers: 2}}, nil
}

type codedError struct {
	Code     string            `json:"code"`
	Expected map[string]string `json:"expected"`
}

func TestCustomFieldsTravelInCreateAndUpdate(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4, CustomFields: map[string]any{"interesse": "alto"}}}
	handler := routedHandler(cmds, &stubHistory{})

	rec := send(t, handler, http.MethodPost, "/leads", nil, map[string]any{"name": "Ana", "customFields": map[string]any{"interesse": "alto"}})
	if rec.Code != http.StatusCreated || !reflect.DeepEqual(cmds.draft.CustomFields, map[string]any{"interesse": "alto"}) {
		t.Fatalf("status = %d draft = %+v", rec.Code, cmds.draft.CustomFields)
	}

	rec = send(t, handler, http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"},
		map[string]any{"customFields": map[string]any{"interesse": "baixo", "notas": nil}})
	want := map[string]any{"interesse": "baixo", "notas": nil}
	if rec.Code != http.StatusOK || !reflect.DeepEqual(cmds.edit.CustomFields, want) {
		t.Fatalf("status = %d edit = %+v body %s", rec.Code, cmds.edit.CustomFields, rec.Body.String())
	}
	var out LeadRecordResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.CustomFields["interesse"] != "alto" {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAnUpdateWithoutCustomFieldsLeavesThemAlone(t *testing.T) {
	cmds := &stubCommands{result: &leaddomain.Lead{ID: routeLeadID, Version: 4}}
	send(t, routedHandler(cmds, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID, map[string]string{"If-Match": "3"}, map[string]any{"nickname": "Aninha"})
	if cmds.edit.CustomFields != nil {
		t.Fatalf("custom fields = %v; absent must mean untouched", cmds.edit.CustomFields)
	}
}

func TestSensitiveAndAddressRefusalsMapToStatusAndCode(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		key    string
	}{
		{"addresses without the address permission", leaddomain.ErrAddressesForbidden, http.StatusForbidden, "lead_addresses_forbidden", ""},
		{"a sensitive field without permission", &customfield.ValueError{Key: "classificacao", Err: customfield.ErrValueForbidden}, http.StatusForbidden, "custom_field_sensitive_forbidden", "classificacao"},
		{"an option the field does not have", &customfield.ValueError{Key: "interesse", Err: customfield.ErrValueNotInOptions}, http.StatusBadRequest, "custom_field_value_not_in_options", "interesse"},
		{"an unknown field", &customfield.ValueError{Key: "cpf", Err: customfield.ErrUnknownKey}, http.StatusBadRequest, "custom_field_unknown_key", "cpf"},
		{"a missing required field", &customfield.ValueError{Key: "interesse", Err: customfield.ErrValueRequired}, http.StatusBadRequest, "custom_field_value_required", "interesse"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := send(t, routedHandler(&stubCommands{err: tc.err}, &stubHistory{}), http.MethodPut, "/leads/"+routeLeadID,
				map[string]string{"If-Match": "3"}, map[string]any{"nickname": "x"})
			var out codedError
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if rec.Code != tc.status || out.Code != tc.code || out.Expected["key"] != tc.key {
				t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAnonymizeAnswersWhatWasErased(t *testing.T) {
	cmds := &stubCommands{}
	rec := send(t, routedHandler(cmds, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/anonymize", nil, nil)
	if rec.Code != http.StatusOK || !reflect.DeepEqual(cmds.calls, []string{"anonymize:" + routeLeadID}) {
		t.Fatalf("status = %d calls = %v", rec.Code, cmds.calls)
	}
	var out AnonymizeLeadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.LeadID != routeLeadID || out.Version != 6 || out.AnonymizedAt != "2026-10-08T12:00:00Z" || out.Erased["lead_record"] != 1 || out.Erased["calls"] != 2 {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestAnonymizeRefusals(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leaddomain.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{leaddomain.ErrLeadNotFound, http.StatusNotFound, "lead_not_found"},
	}
	for _, tc := range cases {
		rec := send(t, routedHandler(&stubCommands{err: tc.err}, &stubHistory{}), http.MethodPost, "/leads/"+routeLeadID+"/anonymize", nil, nil)
		var out codedError
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != tc.status || out.Code != tc.code {
			t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
}

func TestADetailTheViewerMayNotReadIsForbidden(t *testing.T) {
	rec := send(t, routedHandler(&stubCommands{}, &stubHistory{err: leaddomain.ErrLeadForbidden}), http.MethodGet, "/leads/"+routeLeadID, nil, nil)
	var out codedError
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if rec.Code != http.StatusForbidden || out.Code != "forbidden" {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

package lead

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	leaddomain "vozko/domain/lead"
	"vozko/domain/leadarea"
)

func filterRefusalCases() []struct {
	name   string
	err    error
	status int
	code   string
} {
	return []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"a deleted area", fmt.Errorf("%w: %s", leadarea.ErrNotFound, geoAreaID), http.StatusBadRequest, "area_not_found"},
		{"too many areas", leadarea.ErrTooManyAreas, http.StatusBadRequest, "area_too_many"},
		{"a negated area", leadarea.ErrOperatorUnsupported, http.StatusBadRequest, "area_operator_unsupported"},
		{"an area without the address permission", leadarea.ErrAddressesRequired, http.StatusForbidden, "lead_addresses_forbidden"},
		{"a sensitive filter", &customfield.FilterError{Key: "classificacao", Err: customfield.ErrFilterSensitive}, http.StatusForbidden, "custom_field_filter_sensitive_forbidden"},
		{"an unknown custom field", fmt.Errorf("%w: %w", leaddomain.ErrLeadFilterInvalid, &customfield.FilterError{Key: "partido", Err: customfield.ErrFilterUnknownKey}), http.StatusBadRequest, "custom_field_filter_unknown_key"},
		{"a search without a usable word", fmt.Errorf("%w: %w", leaddomain.ErrLeadFilterInvalid, leaddomain.ErrLeadSearchTooShort), http.StatusBadRequest, "lead_search_too_short"},
		{"an invalid filter", leaddomain.ErrLeadFilterInvalid, http.StatusBadRequest, "lead_filter_invalid"},
		{"an unbound predicate", crmfilter.ErrNotApplicable, http.StatusBadRequest, "lead_filter_invalid"},
	}
}

func TestEveryLeadFilterPathRefusesAnAreaTheSameWay(t *testing.T) {
	for _, tc := range filterRefusalCases() {
		t.Run(tc.name, func(t *testing.T) {
			paths := map[string]http.Handler{
				"/leads":                  listHandler(&stubPages{err: tc.err}),
				"/leads/sections/summary": sectionsHandler(&stubSections{err: tc.err}),
				"/leads/sections/places":  sectionsHandler(&stubSections{err: tc.err}),
				"/leads/map/summary":      geographyRouter(&stubMap{err: tc.err}, &stubAreas{}),
			}
			for path, router := range paths {
				rec := send(t, router, http.MethodGet, path, nil, nil)
				if rec.Code != tc.status || errorCodeOf(t, rec.Body.Bytes()) != tc.code {
					t.Fatalf("%s = %d %s, want %d %s", path, rec.Code, rec.Body.String(), tc.status, tc.code)
				}
			}
		})
	}
}

func TestAFilterRefusalNeverHidesAServerError(t *testing.T) {
	if _, _, refused := filterRefusal(errors.New("db down")); refused {
		t.Fatal("an unknown error is a server error, not a refusal")
	}
	rec := send(t, listHandler(&stubPages{err: errors.New("db down")}), http.MethodGet, "/leads", nil, nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "db down") {
		t.Fatalf("a server error = %d %s", rec.Code, rec.Body.String())
	}
}

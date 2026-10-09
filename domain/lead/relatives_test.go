package lead

import (
	"errors"
	"testing"
	"time"
)

func TestRelativesQueryNormalize(t *testing.T) {
	at := time.Date(2026, 10, 8, 12, 30, 0, 123456789, time.UTC)
	cursor := RelativesCursor{CreatedAt: at, RelationID: "6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f"}.Encode()
	cases := []struct {
		name    string
		in      RelativesQuery
		want    RelativesQuery
		after   *RelativesCursor
		wantErr error
	}{
		{"the first page of every dimension", RelativesQuery{LeadID: " l-1 "}, RelativesQuery{LeadID: "l-1", Limit: DefaultRelativesPage}, nil, nil},
		{"a page is never larger than the maximum", RelativesQuery{LeadID: "l-1", Limit: 5000}, RelativesQuery{LeadID: "l-1", Limit: MaxRelativesPage}, nil, nil},
		{"one dimension", RelativesQuery{LeadID: "l-1", Dimension: DimensionReferral, Limit: 10}, RelativesQuery{LeadID: "l-1", Dimension: DimensionReferral, Limit: 10}, nil, nil},
		{"the next page", RelativesQuery{LeadID: "l-1", After: cursor}, RelativesQuery{LeadID: "l-1", After: cursor, Limit: DefaultRelativesPage}, &RelativesCursor{CreatedAt: at, RelationID: "6f1c2d3e-4b5a-4c6d-8e7f-9a0b1c2d3e4f"}, nil},
		{"no lead", RelativesQuery{}, RelativesQuery{}, nil, ErrLeadRequired},
		{"an unknown dimension", RelativesQuery{LeadID: "l-1", Dimension: "friends"}, RelativesQuery{}, nil, ErrRelativesQueryInvalid},
		{"a cursor that was not ours", RelativesQuery{LeadID: "l-1", After: "abc"}, RelativesQuery{}, nil, ErrRelativesQueryInvalid},
		{"a cursor with a broken relation id", RelativesQuery{LeadID: "l-1", After: RelativesCursor{CreatedAt: at, RelationID: "x"}.Encode()}, RelativesQuery{}, nil, ErrRelativesQueryInvalid},
		{"a negative page", RelativesQuery{LeadID: "l-1", Limit: -1}, RelativesQuery{}, nil, ErrRelativesQueryInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.in.Normalize()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Normalize error = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got != tc.want {
				t.Fatalf("Normalize = %+v, want %+v", got, tc.want)
			}
			after, ok, err := got.Cursor()
			if err != nil || ok != (tc.after != nil) {
				t.Fatalf("Cursor = %+v, %v, %v", after, ok, err)
			}
			if tc.after != nil && (!after.CreatedAt.Equal(tc.after.CreatedAt) || after.RelationID != tc.after.RelationID) {
				t.Fatalf("Cursor = %+v, want %+v", after, tc.after)
			}
		})
	}
}

func TestRelativeKindIsSeenFromTheLeadThePageBelongsTo(t *testing.T) {
	r := Relative{Relation: Relation{ID: "r-1", LeadID: "mae", OtherLeadID: "filho", Kind: KindChild}, Lead: &Lead{ID: "filho"}}
	if got := r.KindFrom("mae"); got != KindChild {
		t.Fatalf("seen from the mother = %s", got)
	}
	if got := r.KindFrom("filho"); got != KindParent {
		t.Fatalf("seen from the child = %s", got)
	}
}

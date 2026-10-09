package calllist

import "testing"

func TestPagesAreBoundedWhateverTheCallerAsks(t *testing.T) {
	cases := []struct {
		in       ListQuery
		page     int
		pageSize int
	}{
		{ListQuery{}, 1, DefaultPageSize},
		{ListQuery{Page: -3, PageSize: -1}, 1, DefaultPageSize},
		{ListQuery{Page: 4, PageSize: 10_000}, 4, MaxPageSize},
	}
	for _, tc := range cases {
		got := tc.in.Normalized()
		if got.Page != tc.page || got.PageSize != tc.pageSize {
			t.Errorf("Normalized(%+v) = page %d size %d", tc.in, got.Page, got.PageSize)
		}
	}
	items := []struct {
		in    ItemQuery
		after int
		limit int
	}{
		{ItemQuery{}, 0, DefaultItemPage},
		{ItemQuery{AfterPosition: -5, Limit: 9_999}, 0, MaxItemPage},
		{ItemQuery{AfterPosition: 40, Limit: 10}, 40, 10},
	}
	for _, tc := range items {
		got := tc.in.Normalized()
		if got.AfterPosition != tc.after || got.Limit != tc.limit {
			t.Errorf("Normalized(%+v) = after %d limit %d", tc.in, got.AfterPosition, got.Limit)
		}
	}
}

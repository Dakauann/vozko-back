package shared

import "testing"

func TestPaginationWithinACustomCap(t *testing.T) {
	cases := []struct {
		name       string
		in         Pagination
		max        int
		wantSize   int
		wantOffset int
	}{
		{"a list that allows 200 keeps 200", Pagination{Page: 2, PageSize: 200}, 200, 200, 200},
		{"above the custom cap clamps to it", Pagination{Page: 1, PageSize: 500}, 200, 200, 0},
		{"no size falls back to the default", Pagination{Page: 3}, 200, DefaultPageSize, 2 * DefaultPageSize},
		{"the shared cap still clamps the default lists", Pagination{Page: 1, PageSize: 200}, MaxPageSize, MaxPageSize, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizePaginationWithin(tc.in, tc.max)
			if got.PageSize != tc.wantSize {
				t.Fatalf("PageSize = %d, want %d", got.PageSize, tc.wantSize)
			}
			if off := tc.in.OffsetWithin(tc.max); off != tc.wantOffset {
				t.Fatalf("OffsetWithin() = %d, want %d", off, tc.wantOffset)
			}
		})
	}
	page := NewPaginatedResultWithin([]int{1}, Pagination{Page: 1, PageSize: 200}, 401, 200)
	if page.PageSize != 200 || page.TotalPages != 3 {
		t.Fatalf("page = %+v, want size 200 and 3 pages", page)
	}
	if NormalizePagination(Pagination{PageSize: 200}).PageSize != MaxPageSize {
		t.Fatal("the default normalization keeps the shared cap")
	}
}

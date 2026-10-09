package geocoding

import (
	"math"
	"testing"

	"vozko/domain/leadmap"
)

func TestPlatformQueryNormalize(t *testing.T) {
	long := ""
	for range PlatformSearchMax + 20 {
		long += "a"
	}
	tests := []struct {
		name string
		in   PlatformQuery
		want PlatformQuery
	}{
		{"defaults", PlatformQuery{}, PlatformQuery{Page: 1, PageSize: PlatformDefaultPageSize}},
		{"a negative page starts at one", PlatformQuery{Page: -3, PageSize: 10}, PlatformQuery{Page: 1, PageSize: 10}},
		{"an oversized page is capped", PlatformQuery{Page: 2, PageSize: 5000}, PlatformQuery{Page: 2, PageSize: PlatformMaxPageSize}},
		{"the search is trimmed", PlatformQuery{Page: 1, PageSize: 10, Search: "  escola  "}, PlatformQuery{Page: 1, PageSize: 10, Search: "escola"}},
		{"a long search is cut", PlatformQuery{Page: 1, PageSize: 10, Search: long}, PlatformQuery{Page: 1, PageSize: 10, Search: long[:PlatformSearchMax]}},
		{"a huge page is capped", PlatformQuery{Page: math.MaxInt, PageSize: PlatformMaxPageSize}, PlatformQuery{Page: PlatformMaxPage, PageSize: PlatformMaxPageSize}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.Normalize(); got != tt.want {
				t.Fatalf("Normalize() = %+v, want %+v", got, tt.want)
			}
		})
	}
	if offset := (PlatformQuery{Page: 3, PageSize: 20}).Normalize().Offset(); offset != 40 {
		t.Fatalf("Offset() = %d, want 40", offset)
	}
	if offset := (PlatformQuery{Page: math.MaxInt, PageSize: math.MaxInt}).Normalize().Offset(); offset < 0 || offset > math.MaxInt32 {
		t.Fatalf("Offset() of the largest page = %d, want a positive offset Postgres accepts", offset)
	}
}

func TestPlatformPageTotalPages(t *testing.T) {
	tests := []struct {
		total int64
		size  int
		want  int
	}{{0, 20, 0}, {1, 20, 1}, {20, 20, 1}, {21, 20, 2}, {5, 0, 0}}
	for _, tt := range tests {
		if got := (PlatformPage{TotalItems: tt.total, PageSize: tt.size}).TotalPages(); got != tt.want {
			t.Fatalf("TotalPages(%d, %d) = %d, want %d", tt.total, tt.size, got, tt.want)
		}
	}
}

func TestCoverageShares(t *testing.T) {
	tests := []struct {
		name    string
		summary leadmap.Summary
		address float64
		onMap   float64
	}{
		{"no leads", leadmap.Summary{}, 0, 0},
		{"half with an address, a quarter on the map", leadmap.Summary{Total: 8, WithoutAddress: 4, OnMap: 2, Approximate: 2}, 0.5, 0.25},
		{"everyone placed", leadmap.Summary{Total: 3, OnMap: 3}, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Coverage{Summary: tt.summary}
			if c.AddressShare() != tt.address || c.MapShare() != tt.onMap {
				t.Fatalf("shares = %v, %v, want %v, %v", c.AddressShare(), c.MapShare(), tt.address, tt.onMap)
			}
		})
	}
}

func TestOnlyAnIdentifiedPlatformAdminReadsEveryWorkspace(t *testing.T) {
	tests := []struct {
		name   string
		caller Caller
		want   bool
	}{
		{"a platform admin", Caller{UserID: "u-1", PlatformAdmin: true}, true},
		{"a workspace member", Caller{UserID: "u-1"}, false},
		{"an anonymous platform flag", Caller{UserID: " ", PlatformAdmin: true}, false},
		{"nobody", Caller{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.caller.CanReadEveryWorkspace(); got != tt.want {
				t.Fatalf("CanReadEveryWorkspace() = %v, want %v", got, tt.want)
			}
		})
	}
}

package workspace_department

import (
	"testing"
)

func TestDepartmentFilter_ShouldFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter *DepartmentFilter
		want   bool
	}{
		{
			name:   "nil filter does not filter",
			filter: nil,
			want:   false,
		},
		{
			name:   "owner bypasses filtering",
			filter: &DepartmentFilter{IsOwnerOrAdmin: true, DepartmentIDs: []string{"d1"}},
			want:   false,
		},
		{
			name:   "member with departments should filter",
			filter: &DepartmentFilter{DepartmentIDs: []string{"d1"}},
			want:   true,
		},
		{
			name:   "member with no departments in department-aware workspace should filter",
			filter: &DepartmentFilter{WorkspaceHasDepartments: true},
			want:   true,
		},
		{
			name:   "member with no departments and no workspace departments does not filter",
			filter: &DepartmentFilter{DepartmentIDs: nil},
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.filter.ShouldFilter(); got != tt.want {
				t.Errorf("ShouldFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDepartmentFilter_EffectiveDepartmentIDs(t *testing.T) {
	d1 := "dept-1"

	tests := []struct {
		name   string
		filter *DepartmentFilter
		want   []string
	}{
		{
			name:   "nil filter returns nil",
			filter: nil,
			want:   nil,
		},
		{
			name:   "no selection returns all user departments",
			filter: &DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}},
			want:   []string{"d1", "d2"},
		},
		{
			name:   "selected department narrows to one",
			filter: &DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}, SelectedDepartmentID: &d1},
			want:   []string{"dept-1"},
		},
		{
			name:   "empty selected department returns all",
			filter: &DepartmentFilter{DepartmentIDs: []string{"d1"}, SelectedDepartmentID: strPtr("")},
			want:   []string{"d1"},
		},
		{
			name:   "workspace without departments returns nil",
			filter: &DepartmentFilter{},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.EffectiveDepartmentIDs()
			if len(got) != len(tt.want) {
				t.Fatalf("EffectiveDepartmentIDs() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("EffectiveDepartmentIDs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDepartmentFilter_DepartmentIDForCreation(t *testing.T) {
	d1 := "dept-1"

	tests := []struct {
		name    string
		filter  *DepartmentFilter
		want    string
		wantErr error
	}{
		{
			name:   "nil filter returns empty",
			filter: nil,
			want:   "",
		},
		{
			name:   "owner without selection creates at workspace level",
			filter: &DepartmentFilter{IsOwnerOrAdmin: true, DepartmentIDs: []string{"d1", "d2"}},
			want:   "",
		},
		{
			name:   "owner with selection creates in that department",
			filter: &DepartmentFilter{IsOwnerOrAdmin: true, SelectedDepartmentID: &d1},
			want:   "dept-1",
		},
		{
			name:   "member with zero departments creates at workspace level when workspace has no departments",
			filter: &DepartmentFilter{DepartmentIDs: nil},
			want:   "",
		},
		{
			name:    "member with zero departments in department-aware workspace is denied",
			filter:  &DepartmentFilter{WorkspaceHasDepartments: true},
			wantErr: ErrDepartmentAccessDenied,
		},
		{
			name:   "member with one department auto-selects",
			filter: &DepartmentFilter{DepartmentIDs: []string{"dept-1"}},
			want:   "dept-1",
		},
		{
			name:    "member with multiple departments must select",
			filter:  &DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}},
			wantErr: ErrDepartmentRequired,
		},
		{
			name:   "member with multiple departments and selection uses it",
			filter: &DepartmentFilter{DepartmentIDs: []string{"d1", "d2"}, SelectedDepartmentID: &d1},
			want:   "dept-1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.filter.DepartmentIDForCreation()
			if tt.wantErr != nil {
				if err != tt.wantErr {
					t.Fatalf("DepartmentIDForCreation() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("DepartmentIDForCreation() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("DepartmentIDForCreation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

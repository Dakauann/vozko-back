package lead

import "testing"

func TestNumberIndexFindsAHolderByEitherNinthDigitFormat(t *testing.T) {
	index := map[string]string{}
	IndexByNumber(index, "5511987654321", "lead-1")
	IndexByNumber(index, "551187654321", "lead-2")
	cases := []struct {
		number string
		want   string
		found  bool
	}{
		{"5511987654321", "lead-1", true},
		{"551187654321", "lead-2", true},
		{"5511912345678", "", false},
	}
	for _, tc := range cases {
		got, ok := FindByNumber(index, tc.number)
		if got != tc.want || ok != tc.found {
			t.Errorf("FindByNumber(%s) = %q, %v; want %q, %v", tc.number, got, ok, tc.want, tc.found)
		}
	}
	other := map[string]string{}
	IndexByNumber(other, "5511987654321", "lead-1")
	if got, ok := FindByNumber(other, "551187654321"); !ok || got != "lead-1" {
		t.Fatalf("the other format finds the holder, got %q %v", got, ok)
	}
}

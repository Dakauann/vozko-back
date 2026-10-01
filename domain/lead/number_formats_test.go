package lead

import (
	"slices"
	"testing"
)

func TestANumberMatchesWithAndWithoutTheMobileNine(t *testing.T) {
	if got := NumberFormats("5584994409684"); !slices.Equal(got, []string{"5584994409684", "558494409684"}) {
		t.Fatalf("NumberFormats(13 digits) = %v", got)
	}
	if got := NumberFormats("558494409684"); !slices.Equal(got, []string{"558494409684", "5584994409684"}) {
		t.Fatalf("NumberFormats(12 digits) = %v", got)
	}
	if got := NumberFormats("551130000000"); !slices.Equal(got, []string{"551130000000"}) {
		t.Fatalf("NumberFormats(landline) = %v", got)
	}
	if got := NumberFormats("100"); len(got) != 0 {
		t.Fatalf("NumberFormats(extension) = %v, want none", got)
	}
}

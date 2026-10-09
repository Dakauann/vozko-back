package media

import "testing"

func TestAStudioProxyIsAWorkingCopyKeptOutOfTheLibrary(t *testing.T) {
	if MediaTypeStudioProxy.Listed() {
		t.Fatal("an editing proxy must not appear in the library")
	}
	for _, kind := range []MediaType{MediaTypeProductImage, MediaTypeProductVideo, MediaTypeAudio, MediaTypeDocument} {
		if !kind.Listed() {
			t.Fatalf("%s must stay in the library", kind)
		}
	}
}

func TestALeadImportSheetIsKeptOutOfTheLibrary(t *testing.T) {
	if MediaTypeLeadImport.Listed() {
		t.Fatal("a sheet uploaded for a lead import holds personal data and must not appear in the library")
	}
}

func TestOnlyALeadImportSheetIsPrivate(t *testing.T) {
	cases := []struct {
		kind    MediaType
		private bool
	}{
		{MediaTypeLeadImport, true},
		{MediaTypeStudioProxy, false},
		{MediaTypeProductImage, false},
		{MediaTypeProductVideo, false},
		{MediaTypeAudio, false},
		{MediaTypeDocument, false},
	}
	for _, c := range cases {
		if got := c.kind.Private(); got != c.private {
			t.Fatalf("%s: Private() = %v, want %v", c.kind, got, c.private)
		}
	}
}

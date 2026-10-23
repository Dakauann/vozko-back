package media

import "testing"

func TestOnlyMessageKindsOfTheLibraryAreSendable(t *testing.T) {
	sendable := []MediaType{
		MediaTypeProductImage, MediaTypeProductVideo, MediaTypeAudio, MediaTypeSticker,
		MediaTypeDocument, MediaTypeDocumentPdf, MediaTypeDocumentDoc,
	}
	for _, kind := range sendable {
		if !kind.Sendable() {
			t.Errorf("%s must be sendable to a contact", kind)
		}
	}
	for _, kind := range []MediaType{MediaTypeHtml5, MediaTypeStudioProxy, MediaTypeLeadImport, MediaTypeLut, MediaTypeLottie, "", "unknown"} {
		if kind.Sendable() {
			t.Errorf("%q must not be sendable to a contact", kind)
		}
	}
}

func TestLibraryMediaIsSendableOnlyInsideItsOwnWorkspace(t *testing.T) {
	photo := &Media{ID: "m-1", WorkspaceID: "ws-1", URL: "https://cdn/p.jpg", Type: MediaTypeProductImage}
	if !photo.SendableFor("ws-1") {
		t.Fatal("a photo of the workspace library must be sendable in that workspace")
	}
	cases := map[string]struct {
		media     *Media
		workspace string
	}{
		"another workspace": {photo, "ws-2"},
		"no workspace":      {photo, "  "},
		"no row":            {nil, "ws-1"},
		"row without owner": {&Media{ID: "m-1", URL: "https://cdn/p.jpg", Type: MediaTypeProductImage}, "ws-1"},
		"no url":            {&Media{ID: "m-1", WorkspaceID: "ws-1", URL: " ", Type: MediaTypeProductImage}, "ws-1"},
		"private kind":      {&Media{ID: "m-1", WorkspaceID: "ws-1", URL: "https://cdn/l.csv", Type: MediaTypeLeadImport}, "ws-1"},
		"unknown kind":      {&Media{ID: "m-1", WorkspaceID: "ws-1", URL: "https://cdn/x", Type: "mystery"}, "ws-1"},
	}
	for name, c := range cases {
		if c.media.SendableFor(c.workspace) {
			t.Errorf("%s: must not be sendable", name)
		}
	}
}

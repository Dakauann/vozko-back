package workflow

import (
	"reflect"
	"testing"
)

func TestAnInboundMessageCarriesTheKeysBranchesRead(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		kind      string
		mediaType string
		selection *OptionSelection
		want      map[string]interface{}
	}{
		{"typed text", "oi", "text", "", nil, map[string]interface{}{"message": "oi", "message_type": "text"}},
		{"a tapped list row", "Graduação", "text", "", &OptionSelection{ID: "grad", Title: "Graduação", Kind: "list"},
			map[string]interface{}{"message": "Graduação", "message_type": "text", "selected_option_id": "grad", "selected_option_title": "Graduação", "selected_option_type": "list"}},
		{"a photo with caption", "minha foto", "media", "image", nil, map[string]interface{}{"message": "minha foto", "message_type": "media", "media_type": "image"}},
		{"no kind known", "oi", "", "", nil, map[string]interface{}{"message": "oi"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := InboundMessageData(tc.text, tc.kind, tc.mediaType, tc.selection)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("data = %v, want %v", got, tc.want)
			}
		})
	}
}

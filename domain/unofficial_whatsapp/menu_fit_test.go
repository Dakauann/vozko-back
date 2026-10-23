package unofficial_whatsapp

import "testing"

func TestAMenuFitsTheStyleWhatsAppCanRender(t *testing.T) {
	cases := []struct {
		name      string
		style     string
		count     int
		wantStyle string
		wantShown int
	}{
		{"few buttons stay buttons", InteractiveStyleButtons, 2, InteractiveStyleButtons, 2},
		{"an unknown style is buttons", "carousel", 3, InteractiveStyleButtons, 3},
		{"more than three buttons become a list", InteractiveStyleButtons, 4, InteractiveStyleList, 4},
		{"a list keeps its style", InteractiveStyleList, 2, InteractiveStyleList, 2},
		{"a list shows at most its limit", InteractiveStyleList, MaxListOptions + 3, InteractiveStyleList, MaxListOptions},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			style, shown := FitMenu(tc.style, tc.count)
			if style != tc.wantStyle || shown != tc.wantShown {
				t.Fatalf("FitMenu = %q %d, want %q %d", style, shown, tc.wantStyle, tc.wantShown)
			}
		})
	}
}

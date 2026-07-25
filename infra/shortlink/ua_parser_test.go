package shortlink

import "testing"

func TestUAParse(t *testing.T) {
	r := NewUAResolver()

	tests := []struct {
		name    string
		ua      string
		device  string
		os      string
		browser string
		bot     bool
	}{
		{"empty", "", "unknown", "", "", false},
		{"googlebot", "Mozilla/5.0 (compatible; Googlebot/2.1)", "bot", "", "bot", true},
		{"windows chrome", "Mozilla/5.0 (Windows NT 10.0; Win64) AppleWebKit Chrome/120 Safari/537", "desktop", "Windows", "Chrome", false},
		{"windows edge", "Mozilla/5.0 (Windows NT 10.0) Chrome/120 Safari/537 Edg/120", "desktop", "Windows", "Edge", false},
		{"opera", "Mozilla/5.0 (Windows NT 10.0) Chrome/120 OPR/100", "desktop", "Windows", "Opera", false},
		{"samsung", "Mozilla/5.0 (Linux; Android 13) SamsungBrowser/20 Chrome/120 Mobile", "mobile", "Android", "Samsung Internet", false},
		{"firefox linux", "Mozilla/5.0 (X11; Linux x86_64) Gecko Firefox/120", "desktop", "Linux", "Firefox", false},
		{"chrome android mobile", "Mozilla/5.0 (Linux; Android 13; Pixel) AppleWebKit Chrome/120 Mobile Safari/537", "mobile", "Android", "Chrome", false},
		{"iphone safari", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Version/17 Mobile Safari/604", "mobile", "iOS", "Safari", false},
		{"ipad tablet", "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) Version/17 Safari/604", "tablet", "iOS", "Safari", false},
		{"android tablet keyword", "Mozilla/5.0 (Linux; Android 13; Tablet) AppleWebKit Chrome/120", "tablet", "Android", "Chrome", false},
		{"macos safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Version/17 Safari/605", "desktop", "macOS", "Safari", false},
		{"chromeos", "Mozilla/5.0 (X11; CrOS x86_64 14541) Chrome/120 Safari/537", "desktop", "ChromeOS", "Chrome", false},
		{"fxios", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) FxiOS/120 Mobile", "mobile", "iOS", "Firefox", false},
		{"unknown agent", "SomeRandomAgent/1.0", "desktop", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := r.Parse(tt.ua)
			if info.DeviceType != tt.device || info.OS != tt.os || info.Browser != tt.browser || info.IsBot != tt.bot {
				t.Fatalf("Parse(%q) = %+v want {%s %s %s bot=%v}", tt.ua, info, tt.device, tt.os, tt.browser, tt.bot)
			}
		})
	}
}

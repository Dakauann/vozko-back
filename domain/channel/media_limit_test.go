package channel

import "testing"

func TestAMediaLimitComparesTheMediaTypeWithoutItsParameters(t *testing.T) {
	limit := MediaLimit{MaxBytes: 1 << 20, MIMETypes: []string{"audio/ogg", "image/jpeg"}}
	cases := []struct {
		name    string
		mime    string
		allowed bool
	}{
		{"exact type", "audio/ogg", true},
		{"codec parameter from a browser recorder", "audio/ogg; codecs=opus", true},
		{"parameter without a space", "audio/ogg;codecs=opus", true},
		{"upper case type", "IMAGE/JPEG", true},
		{"surrounding spaces", "  image/jpeg  ", true},
		{"type not listed", "audio/webm;codecs=opus", false},
		{"malformed type", "audio/ogg;;=", false},
		{"no subtype", "audio", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := limit.Allows(c.mime); got != c.allowed {
				t.Fatalf("Allows(%q) = %v, want %v", c.mime, got, c.allowed)
			}
		})
	}
}

func TestAMediaLimitWithoutTypesAcceptsAnyWellFormedType(t *testing.T) {
	limit := MediaLimit{MaxBytes: 1 << 20}
	if !limit.Allows("application/zip") {
		t.Fatal("a limit without a type list must accept any well formed type")
	}
}

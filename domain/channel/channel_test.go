package channel

import (
	"testing"
)

func TestMediaLimit_Allows(t *testing.T) {
	limit := MediaLimit{MaxBytes: 100, MIMETypes: []string{"image/png", "image/jpeg"}}

	if !limit.Allows("image/png") {
		t.Error("image/png should be allowed")
	}
	if limit.Allows("image/gif") {
		t.Error("image/gif should not be allowed")
	}
	if limit.Allows("") {
		t.Error("an empty mime type should not match")
	}
}

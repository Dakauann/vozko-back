package notification_usecase

import (
	"testing"

	"vozko/brand"
	"vozko/domain/notification"
)

func TestEveryCategoryHasAnEmailLabel(t *testing.T) {
	for _, c := range notification.Categories() {
		if categoryLabels[c] == "" {
			t.Errorf("category %q has no label, so its emails and digest lines show a blank category", c)
		}
	}
}

func TestDefaultActionLabel_FollowsTheBrandGender(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Acme", Gender: brand.Feminine})
	t.Cleanup(func() { brand.SetForTest(brand.Brand{}) })

	copy := emailComposer{dashboardURL: "https://acme.example"}.compose(notification.Record{Draft: notification.Draft{ActionURL: "/dashboard"}}, "Loja")
	if got := copy.Placeholders["ActionLabel"]; got != "Abrir na Acme" {
		t.Fatalf("ActionLabel = %q, want the feminine contraction", got)
	}

	brand.SetForTest(brand.Brand{Name: "Acme", Gender: brand.Masculine})
	copy = emailComposer{dashboardURL: "https://acme.example"}.compose(notification.Record{Draft: notification.Draft{ActionURL: "/dashboard"}}, "Loja")
	if got := copy.Placeholders["ActionLabel"]; got != "Abrir no Acme" {
		t.Fatalf("ActionLabel = %q, want the masculine contraction", got)
	}
}

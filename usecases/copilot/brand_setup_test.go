package copilot_usecase

import (
	"os"
	"testing"

	"vozko/brand"
)

// TestMain installs a deterministic brand for the whole package so tests that
// build the copilot system prompt (which reads the active brand) do not require
// BRAND_* env vars. Production resolves the brand via brand.MustLoad at boot.
func TestMain(m *testing.M) {
	brand.SetForTest(brand.Brand{Key: "test", Name: "TestBrand"})
	os.Exit(m.Run())
}

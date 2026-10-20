package copilot_usecase

import (
	"os"
	"testing"

	"vozko/brand"
)

func TestMain(m *testing.M) {
	brand.SetForTest(brand.Brand{Key: "test", Name: "TestBrand", Gender: brand.Feminine})
	os.Exit(m.Run())
}

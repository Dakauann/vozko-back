package copilot_usecase

import (
	"os"
	"testing"

	"vozko/brand"
)

func TestMain(m *testing.M) {
	brand.SetForTest(brand.Brand{Key: "test", Name: "TestBrand"})
	os.Exit(m.Run())
}

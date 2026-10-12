package studiotools

import (
	"reflect"
	"strings"
	"testing"

	"vozko/usecases/copilot/copilottools"
)

func TestImportLottieIsAVideoOperationThatNeedsAMedia(t *testing.T) {
	allowed := enumOf(t, studioVideoOperation{}, "Op")
	if !copilottools.Contains(allowed, "import_lottie") {
		t.Fatal("import_lottie is not offered to the video editor")
	}
	if err := checkOperations([]studioVideoOperation{{Op: "import_lottie", MediaID: "m-1"}}, allowed); err != nil {
		t.Fatalf("a Lottie import was refused: %v", err)
	}
	err := checkOperations([]studioVideoOperation{{Op: "import_lottie"}}, allowed)
	if err == nil || !strings.Contains(err.Error(), "media_id") {
		t.Fatalf("an import without media_id passed: %v", err)
	}
	if copilottools.Contains(enumOf(t, studioImageOperation{}, "Op"), "import_lottie") {
		t.Fatal("the image editor must not offer import_lottie")
	}
}

func enumOf(t *testing.T, value any, field string) []string {
	t.Helper()
	f, ok := reflect.TypeOf(value).FieldByName(field)
	if !ok {
		t.Fatalf("no field %s", field)
	}
	return copilottools.EnumValues(f)
}

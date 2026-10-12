package media_usecase

import (
	"strings"
	"testing"

	"vozko/domain/media"
)

func cubeFile(rows int) []byte {
	return []byte("LUT_3D_SIZE 2\n" + strings.Repeat("0.5 0.5 0.5\n", rows))
}

func TestLutUploadsMustBeValidCubeFiles(t *testing.T) {
	if err := lutIssue("look.cube", cubeFile(8)); err != nil {
		t.Fatalf("a valid cube refused: %v", err)
	}
	if err := lutIssue("look.CUBE", cubeFile(8)); err != nil {
		t.Fatalf("an upper case extension refused: %v", err)
	}
	if lutIssue("look.3dl", cubeFile(8)) == nil {
		t.Fatal("a LUT without the .cube extension was accepted")
	}
	if lutIssue("look.cube", cubeFile(7)) == nil {
		t.Fatal("a cube with missing rows was accepted")
	}
	uc := &UploadMediaUseCase{}
	if _, err := uc.UploadMedia("ws", cubeFile(3), "broken.cube", media.MediaTypeLut, "broken"); err == nil {
		t.Fatal("the upload accepted a broken LUT before reaching storage")
	}
}

func TestStoredMediaCannotSkipTheLutCheck(t *testing.T) {
	if validUploadType(media.MediaTypeLut) {
		t.Fatal("stored media registration would accept a LUT without reading the file")
	}
}

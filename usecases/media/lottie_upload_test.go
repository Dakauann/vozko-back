package media_usecase

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"vozko/domain/media"
)

func TestLottieUploadsMustBeLottieAnimations(t *testing.T) {
	json, err := os.ReadFile("../../domain/studio/testdata/lottie/static-shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("../../domain/studio/testdata/lottie/static-shapes.lottie")
	if err != nil {
		t.Fatal(err)
	}
	if err := uploadIssue("logo.json", json, media.MediaTypeLottie); err != nil {
		t.Fatalf("a Lottie JSON was refused: %v", err)
	}
	if err := uploadIssue("logo.lottie", archive, media.MediaTypeLottie); err != nil {
		t.Fatalf("a dotLottie file was refused: %v", err)
	}
	if uploadIssue("logo.json", []byte(`{"hello":"world"}`), media.MediaTypeLottie) == nil {
		t.Fatal("a JSON that is not an animation was accepted")
	}
	uc := &UploadMediaUseCase{}
	if _, err := uc.UploadMedia("ws", []byte("{"), "broken.json", media.MediaTypeLottie, "broken"); err == nil {
		t.Fatal("the upload accepted a broken Lottie before reaching storage")
	}
	oversized := append([]byte(`{"fr":30,"ip":0,"op":30,"w":100,"h":100,"layers":[]`), bytes.Repeat([]byte(" "), 25<<20)...)
	if _, err := uc.UploadMedia("ws", oversized, "huge.json", media.MediaTypeLottie, "huge"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("an oversized file was read as an animation before the size check: %v", err)
	}
}

func TestStoredMediaCannotSkipTheLottieCheck(t *testing.T) {
	if validUploadType(media.MediaTypeLottie) {
		t.Fatal("stored media registration would accept a Lottie without reading the file")
	}
}

func TestLottieMediaIsListedInTheLibrary(t *testing.T) {
	if !media.MediaTypeLottie.Listed() || media.MediaTypeLottie.Private() {
		t.Fatal("imported Lottie files must show in the library")
	}
}

package mediaprobe

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"sync"
	"testing"

	"vozko/domain/conversation"
)

func gradient(width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / width), G: uint8(y * 255 / height), B: 160, A: 255})
		}
	}
	return img
}

func encodedJPEG(t testing.TB, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, gradient(width, height), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodedPNG(t testing.TB, width, height int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, gradient(width, height)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func withOrientation(jpegData []byte, orientation uint16) []byte {
	tiff := new(bytes.Buffer)
	tiff.WriteString("MM")
	_ = binary.Write(tiff, binary.BigEndian, uint16(42))
	_ = binary.Write(tiff, binary.BigEndian, uint32(8))
	_ = binary.Write(tiff, binary.BigEndian, uint16(1))
	_ = binary.Write(tiff, binary.BigEndian, uint16(0x0112))
	_ = binary.Write(tiff, binary.BigEndian, uint16(3))
	_ = binary.Write(tiff, binary.BigEndian, uint32(1))
	_ = binary.Write(tiff, binary.BigEndian, orientation)
	_ = binary.Write(tiff, binary.BigEndian, uint16(0))
	_ = binary.Write(tiff, binary.BigEndian, uint32(0))
	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	segment := []byte{0xFF, 0xE1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	out := append([]byte{}, jpegData[:2]...)
	out = append(out, segment...)
	out = append(out, payload...)
	return append(out, jpegData[2:]...)
}

func TestAPhotoGetsItsSizeAndABlurredPreview(t *testing.T) {
	layout := New().Inspect(encodedJPEG(t, 1600, 1200), conversation.MediaTypeImage)
	if layout.Width != 1600 || layout.Height != 1200 {
		t.Fatalf("layout = %+v", layout)
	}
	hash, err := base64.StdEncoding.DecodeString(layout.Thumbhash)
	if err != nil || len(hash) == 0 || len(layout.Thumbhash) > 64 {
		t.Fatalf("thumbhash %q must be short base64: %v", layout.Thumbhash, err)
	}
}

func TestAPortraitPhotoFromAPhoneIsMeasuredTheWayItIsShown(t *testing.T) {
	layout := New().Inspect(withOrientation(encodedJPEG(t, 400, 300), 6), conversation.MediaTypeImage)
	if layout.Width != 300 || layout.Height != 400 {
		t.Fatalf("a photo rotated by its EXIF tag must be measured upright, got %+v", layout)
	}
}

func TestAStickerInWebPIsMeasured(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker.webp")
	if err != nil {
		t.Fatal(err)
	}
	layout := New().Inspect(data, conversation.MediaTypeSticker)
	if !layout.Known() || layout.Thumbhash == "" {
		t.Fatalf("layout = %+v", layout)
	}
}

func TestOnlyPicturesAreInspected(t *testing.T) {
	inspector := New()
	for _, kind := range []conversation.MediaType{conversation.MediaTypeAudio, conversation.MediaTypeDocument, conversation.MediaTypeVideo} {
		if layout := inspector.Inspect(encodedPNG(t, 10, 10), kind); layout != (conversation.MediaLayout{}) {
			t.Fatalf("%s: layout = %+v", kind, layout)
		}
	}
}

func TestBrokenOrHostileFilesYieldNoLayout(t *testing.T) {
	inspector := New()
	for name, data := range map[string][]byte{
		"empty":     nil,
		"text":      []byte("not an image at all"),
		"truncated": encodedJPEG(t, 800, 600)[:300],
	} {
		if layout := inspector.Inspect(data, conversation.MediaTypeImage); layout.Thumbhash != "" {
			t.Fatalf("%s: layout = %+v", name, layout)
		}
	}
}

func pngHeaderOnly(width, height uint32) []byte {
	ihdr := new(bytes.Buffer)
	_ = binary.Write(ihdr, binary.BigEndian, width)
	_ = binary.Write(ihdr, binary.BigEndian, height)
	ihdr.Write([]byte{8, 2, 0, 0, 0})
	chunk := append([]byte("IHDR"), ihdr.Bytes()...)
	out := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	out = binary.BigEndian.AppendUint32(out, uint32(ihdr.Len()))
	out = append(out, chunk...)
	return binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
}

func TestAHugePictureIsMeasuredWithoutDecodingItsPixels(t *testing.T) {
	data := pngHeaderOnly(20000, 15000)
	layout := New().Inspect(data, conversation.MediaTypeImage)
	if layout.Width != 20000 || layout.Height != 15000 || layout.Thumbhash != "" {
		t.Fatalf("layout = %+v", layout)
	}
}

func TestInspectionIsSafeUnderConcurrentLoad(t *testing.T) {
	inspector := New()
	data := encodedJPEG(t, 1280, 960)
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if layout := inspector.Inspect(data, conversation.MediaTypeImage); layout.Thumbhash == "" {
				t.Error("missing thumbhash under load")
			}
		}()
	}
	wg.Wait()
}

func BenchmarkInspectTypicalChatPhoto(b *testing.B) {
	benchmarkInspect(b, encodedJPEG(b, 1600, 1200))
}

func BenchmarkInspectTwelveMegapixelPhoto(b *testing.B) {
	benchmarkInspect(b, encodedJPEG(b, 4000, 3000))
}

func benchmarkInspect(b *testing.B, data []byte) {
	inspector := New()
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			inspector.Inspect(data, conversation.MediaTypeImage)
		}
	})
}

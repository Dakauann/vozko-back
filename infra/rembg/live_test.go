package rembg

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"vozko/domain/mediagen"
)

func TestTheRealServiceCutsTheSubjectOut(t *testing.T) {
	base := os.Getenv("REMBG_TEST_URL")
	if base == "" {
		t.Skip("REMBG_TEST_URL not set")
	}
	photo := image.NewRGBA(image.Rect(0, 0, 320, 320))
	for y := 0; y < 320; y++ {
		for x := 0; x < 320; x++ {
			photo.Set(x, y, color.RGBA{230, 230, 230, 255})
			if (x-160)*(x-160)+(y-160)*(y-160) < 90*90 {
				photo.Set(x, y, color.RGBA{200, 30, 30, 255})
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, photo); err != nil {
		t.Fatal(err)
	}
	media := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(encoded.Bytes()) }))
	defer media.Close()
	client, err := New(base, &http.Client{Timeout: 5 * time.Minute}, media.Client())
	if err != nil {
		t.Fatal(err)
	}
	out, err := client.Generate(context.Background(), mediagen.Request{}, []mediagen.Source{{MediaID: "m", URL: media.URL}})
	if err != nil {
		t.Fatal(err)
	}
	cut, err := png.Decode(bytes.NewReader(out.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, cornerAlpha := cut.At(5, 5).RGBA()
	_, _, _, centerAlpha := cut.At(160, 160).RGBA()
	if cornerAlpha > 0x2000 || centerAlpha < 0xd000 {
		t.Fatalf("background alpha %x subject alpha %x", cornerAlpha, centerAlpha)
	}
}

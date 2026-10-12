package adpreview

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	ads "vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

func pngWithTextAtTheBottom(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: 240, G: 236, B: 230, A: 255})
		}
	}
	for y := h * 85 / 100; y < h*92/100; y++ {
		for x := w / 10; x < w*9/10; x++ {
			if (x/9)%2 == 0 && (y/9)%2 == 0 {
				img.Set(x, y, color.NRGBA{A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodePanel(t *testing.T, url string) image.Image {
	t.Helper()
	payload, ok := strings.CutPrefix(url, "data:image/jpeg;base64,")
	if !ok {
		t.Fatalf("not a jpeg data url: %.40s", url)
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func vertical(t *testing.T) ads.PlacementFrame {
	t.Helper()
	f, ok := ads.PlacementFrameByID("vertical")
	if !ok {
		t.Fatal("no vertical frame")
	}
	return f
}

func TestTheDetailMapFindsTextWhereItIs(t *testing.T) {
	picture, err := NewPainter().Open(pngWithTextAtTheBottom(t, 1080, 1920))
	if err != nil {
		t.Fatal(err)
	}
	if picture.Size() != (ads.PixelSize{Width: 1080, Height: 1920}) {
		t.Fatalf("size %+v", picture.Size())
	}
	layout, err := vertical(t).Layout(1080, 1920)
	if err != nil {
		t.Fatal(err)
	}
	d := layout.Detail(picture.Detail())
	if d.UnderUI < 0.9 || d.Bounds == nil || d.Bounds.Y0 < 0.8 {
		t.Fatalf("text drawn at 85 to 92 percent of the height must be found there, got %+v %+v", d, d.Bounds)
	}
}

func TestAPanelIsAReadableJPEGShapedLikeThePlacement(t *testing.T) {
	picture, err := NewPainter().Open(pngWithTextAtTheBottom(t, 1080, 1080))
	if err != nil {
		t.Fatal(err)
	}
	frame := vertical(t)
	layout, _ := frame.Layout(1080, 1080)
	url, err := picture.Panel(frame, layout, adsuc.PanelLabels{Title: "Stories e Reels", Top: "perfil", Bottom: "legenda e botão"})
	if err != nil {
		t.Fatal(err)
	}
	img := decodePanel(t, url)
	b := img.Bounds()
	if max(b.Dx(), b.Dy()) > maxPanelSide || b.Dx() <= 0 {
		t.Fatalf("panel %v must fit the vision budget", b)
	}
	ratio := float64(b.Dx()) / float64(b.Dy())
	if ratio < 0.54 || ratio > 0.59 {
		t.Fatalf("a story panel must be 9:16, got %v", ratio)
	}
	r, g, bl, _ := img.At(b.Dx()/2, b.Dy()*95/100).RGBA()
	if r <= g || r <= bl {
		t.Fatalf("the caption band must be tinted, got %d %d %d", r>>8, g>>8, bl>>8)
	}
}

func TestSmallPlacementsGetSmallPanels(t *testing.T) {
	picture, err := NewPainter().Open(pngWithTextAtTheBottom(t, 1080, 1080))
	if err != nil {
		t.Fatal(err)
	}
	frame, _ := ads.PlacementFrameByID("thumbnail")
	layout, _ := frame.Layout(1080, 1080)
	url, err := picture.Panel(frame, layout, adsuc.PanelLabels{Title: "Miniaturas"})
	if err != nil {
		t.Fatal(err)
	}
	if b := decodePanel(t, url).Bounds(); b.Dx() != frame.DisplayWidth*3/2 {
		t.Fatalf("a thumbnail must be drawn near its real size, got %v", b)
	}
}

func TestTransparentImagesAreFlattenedOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 700, 700))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	picture, err := NewPainter().Open(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	frame, _ := ads.PlacementFrameByID("square")
	layout, _ := frame.Layout(700, 700)
	url, err := picture.Panel(frame, layout, adsuc.PanelLabels{})
	if err != nil {
		t.Fatal(err)
	}
	panel := decodePanel(t, url)
	if r, _, _, _ := panel.At(panel.Bounds().Dx()/2, panel.Bounds().Dy()/2).RGBA(); r>>8 < 240 {
		t.Fatalf("transparent pixels must read as white, got %d", r>>8)
	}
}

func TestUnreadableOrHugeFilesAreRefused(t *testing.T) {
	if _, err := NewPainter().Open([]byte("not an image")); err == nil {
		t.Fatal("garbage must be refused")
	}
	small := Painter{maxPixels: 100}
	if _, err := small.Open(pngWithTextAtTheBottom(t, 20, 20)); err == nil {
		t.Fatal("an image past the pixel ceiling must be refused before decoding")
	}
}

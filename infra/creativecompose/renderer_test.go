package creativecompose

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vozko/domain/creativecompose"
	"vozko/infra/browser"
)

func pngURL(t *testing.T, width, height int, fill color.Color) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}

func customerLayout(template creativecompose.Template) creativecompose.Layout {
	return creativecompose.Layout{
		Template: template, Eyebrow: "Clareamento", Headline: "Sorriso mais claro", Highlight: "em uma sessão.",
		Subline: "Avaliação gratuita e parcelamento no cartão.", ImageMediaID: "photo", LogoMediaID: "logo",
		Callouts: []string{"Resultado na primeira sessão"}, CallToAction: "Agende pelo WhatsApp", WhatsAppIcon: true, Footnote: "clinicaviva.com.br",
	}
}

func TestTheCreativePageCarriesTheTextsEscapedAndTheImagesIntact(t *testing.T) {
	layout := customerLayout(creativecompose.TemplateFeed)
	layout.Subline = "Avaliação <b>grátis</b>"
	html, err := NewRenderer(nil).HTML(layout, creativecompose.Images{Image: "https://cdn/photo.jpg", Logo: "https://cdn/logo.png"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Sorriso mais claro", "em uma sessão.", "Avaliação &lt;b&gt;grátis&lt;/b&gt;", "Resultado na primeira sessão", `src="https://cdn/photo.jpg"`, `src="https://cdn/logo.png"`, `url("data:font/woff2;base64,`} {
		if !strings.Contains(html, want) {
			t.Fatalf("page is missing %q", want)
		}
	}
	if strings.Contains(html, "ZgotmplZ") {
		t.Fatal("an image or font address was sanitized away")
	}
}

func TestTheCreativeWithoutALogoLeavesNoBrokenImage(t *testing.T) {
	html, err := NewRenderer(nil).HTML(customerLayout(creativecompose.TemplateCard), creativecompose.Images{Image: "https://cdn/photo.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(html, "<img") != 1 {
		t.Fatal("a missing logo must not leave an empty image")
	}
}

func TestTheCreativeRendersAnyImageShapeAtTheTemplateSize(t *testing.T) {
	printer := browser.NewRenderer()
	if !printer.Available() {
		t.Skip("no chromium on this machine")
	}
	out := os.Getenv("CREATIVE_SAMPLES")
	wide := pngURL(t, 1600, 900, color.RGBA{R: 230, G: 240, B: 236, A: 255})
	tall := pngURL(t, 720, 1560, color.RGBA{R: 240, G: 230, B: 236, A: 255})
	logo := pngURL(t, 200, 80, color.White)
	for _, c := range []struct {
		template creativecompose.Template
		image    string
	}{{creativecompose.TemplateFeed, wide}, {creativecompose.TemplateCard, tall}, {creativecompose.TemplateStory, tall}} {
		shot, err := NewRenderer(printer).Render(context.Background(), customerLayout(c.template), creativecompose.Images{Image: c.image, Logo: logo})
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(shot))
		if err != nil {
			t.Fatal(err)
		}
		w, h, _ := c.template.Size()
		if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
			t.Fatalf("%s: %v", c.template, img.Bounds())
		}
		if out != "" {
			_ = os.WriteFile(filepath.Join(out, string(c.template)+".png"), shot, 0o644)
		}
	}
}

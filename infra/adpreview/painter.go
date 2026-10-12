package adpreview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"sync"

	_ "image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	_ "golang.org/x/image/webp"

	ads "vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

const (
	defaultMaxPixels = 60_000_000
	analysisSide     = 512
	cellPixels       = 8
	edgeThreshold    = 160
	maxPanelSide     = 800
	panelScale       = 1.5
	jpegQuality      = 85
	bandAlpha        = 120
	outlinePixels    = 2
	labelPadding     = 4
	largeLabelSize   = 14
	smallLabelSize   = 10
	smallPanelWidth  = 300
	dataURLPrefix    = "data:image/jpeg;base64,"
	backgroundShade  = 75
)

var (
	ErrUnreadable = errors.New("adpreview: the image could not be decoded")
	ErrTooLarge   = errors.New("adpreview: the image has too many pixels to preview")

	bandColor    = color.NRGBA{R: 200, G: 30, B: 40, A: bandAlpha}
	outlineColor = color.NRGBA{R: 255, G: 212, B: 0, A: 255}
	pillColor    = color.NRGBA{R: 15, G: 15, B: 20, A: 200}
	white        = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

	parsedFont = sync.OnceValues(func() (*opentype.Font, error) { return opentype.Parse(gobold.TTF) })
)

type Painter struct{ maxPixels int }

func NewPainter() Painter { return Painter{maxPixels: defaultMaxPixels} }

type picture struct {
	img     *image.RGBA
	size    ads.PixelSize
	detail  ads.DetailMap
	average color.RGBA
}

func (p Painter) Open(data []byte) (adsuc.PlacementPicture, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, ErrUnreadable
	}
	if config.Width*config.Height > p.maxPixels {
		return nil, ErrTooLarge
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrUnreadable
	}
	flat := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(flat, flat.Bounds(), image.NewUniform(white), image.Point{}, draw.Src)
	draw.Draw(flat, flat.Bounds(), decoded, decoded.Bounds().Min, draw.Over)
	gray := analysisImage(flat)
	return &picture{
		img: flat, size: ads.PixelSize{Width: config.Width, Height: config.Height},
		detail: detailMap(gray), average: averageColor(flat),
	}, nil
}

func (p *picture) Size() ads.PixelSize   { return p.size }
func (p *picture) Detail() ads.DetailMap { return p.detail }

func analysisImage(src *image.RGBA) *image.Gray {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	scale := math.Min(1, float64(analysisSide)/float64(max(w, h)))
	gw, gh := max(1, int(math.Round(float64(w)*scale))), max(1, int(math.Round(float64(h)*scale)))
	gray := image.NewGray(image.Rect(0, 0, gw, gh))
	draw.ApproxBiLinear.Scale(gray, gray.Bounds(), src, src.Bounds(), draw.Src, nil)
	return gray
}

func sobel(g *image.Gray, x, y int) int {
	at := func(dx, dy int) int {
		px := min(max(x+dx, 0), g.Rect.Dx()-1)
		py := min(max(y+dy, 0), g.Rect.Dy()-1)
		return int(g.Pix[py*g.Stride+px])
	}
	gx := at(1, -1) + 2*at(1, 0) + at(1, 1) - at(-1, -1) - 2*at(-1, 0) - at(-1, 1)
	gy := at(-1, 1) + 2*at(0, 1) + at(1, 1) - at(-1, -1) - 2*at(0, -1) - at(1, -1)
	return abs(gx) + abs(gy)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func detailMap(g *image.Gray) ads.DetailMap {
	w, h := g.Rect.Dx(), g.Rect.Dy()
	cols, rows := max(1, (w+cellPixels-1)/cellPixels), max(1, (h+cellPixels-1)/cellPixels)
	cells := make([]float64, cols*rows)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if sobel(g, x, y) >= edgeThreshold {
				cells[(y*rows/h)*cols+x*cols/w]++
			}
		}
	}
	return ads.DetailMap{Cols: cols, Rows: rows, Cells: cells}
}

func averageColor(img *image.RGBA) color.RGBA {
	var r, g, b, n uint64
	step := max(1, int(math.Sqrt(float64(img.Rect.Dx()*img.Rect.Dy())/4096)))
	for y := 0; y < img.Rect.Dy(); y += step {
		for x := 0; x < img.Rect.Dx(); x += step {
			i := y*img.Stride + x*4
			r, g, b, n = r+uint64(img.Pix[i]), g+uint64(img.Pix[i+1]), b+uint64(img.Pix[i+2]), n+1
		}
	}
	return color.RGBA{R: uint8(r * backgroundShade / n / 100), G: uint8(g * backgroundShade / n / 100), B: uint8(b * backgroundShade / n / 100), A: 255}
}

func panelSize(frame ads.PlacementFrame, layout ads.FrameLayout) (int, int) {
	w := float64(frame.DisplayWidth) * panelScale
	h := w / layout.Ratio
	if longest := math.Max(w, h); longest > maxPanelSide {
		w, h = w*maxPanelSide/longest, h*maxPanelSide/longest
	}
	return max(1, int(math.Round(w))), max(1, int(math.Round(h)))
}

func pixelRect(r ads.FrameRect, w, h int) image.Rectangle {
	return image.Rect(int(math.Round(r.X0*float64(w))), int(math.Round(r.Y0*float64(h))), int(math.Round(r.X1*float64(w))), int(math.Round(r.Y1*float64(h))))
}

func (p *picture) Panel(frame ads.PlacementFrame, layout ads.FrameLayout, labels adsuc.PanelLabels) (string, error) {
	w, h := panelSize(frame, layout)
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(p.average), image.Point{}, draw.Src)
	draw.BiLinear.Scale(canvas, pixelRect(layout.Image, w, h), p.img, pixelRect(layout.Crop, p.size.Width, p.size.Height), draw.Over, nil)
	face, err := labelFace(w)
	if err != nil {
		return "", err
	}
	defer face.Close()
	if frame.Safe != nil {
		safe := pixelRect(layout.Safe, w, h)
		tint(canvas, image.Rect(0, 0, w, safe.Min.Y))
		tint(canvas, image.Rect(0, safe.Max.Y, w, h))
		tint(canvas, image.Rect(0, safe.Min.Y, safe.Min.X, safe.Max.Y))
		tint(canvas, image.Rect(safe.Max.X, safe.Min.Y, w, safe.Max.Y))
		outline(canvas, safe)
		centerLabel(canvas, face, labels.Top, image.Rect(0, 0, w, safe.Min.Y))
		centerLabel(canvas, face, labels.Bottom, image.Rect(0, safe.Max.Y, w, h))
	}
	pill(canvas, face, labels.Title)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, canvas, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return "", err
	}
	return dataURLPrefix + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func labelFace(panelWidth int) (font.Face, error) {
	parsed, err := parsedFont()
	if err != nil {
		return nil, err
	}
	size := float64(largeLabelSize)
	if panelWidth < smallPanelWidth {
		size = smallLabelSize
	}
	return opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
}

func tint(canvas *image.RGBA, r image.Rectangle) {
	draw.Draw(canvas, r.Intersect(canvas.Bounds()), image.NewUniform(bandColor), image.Point{}, draw.Over)
}

func outline(canvas *image.RGBA, r image.Rectangle) {
	fill := image.NewUniform(outlineColor)
	for _, edge := range []image.Rectangle{
		image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+outlinePixels),
		image.Rect(r.Min.X, r.Max.Y-outlinePixels, r.Max.X, r.Max.Y),
		image.Rect(r.Min.X, r.Min.Y, r.Min.X+outlinePixels, r.Max.Y),
		image.Rect(r.Max.X-outlinePixels, r.Min.Y, r.Max.X, r.Max.Y),
	} {
		draw.Draw(canvas, edge.Intersect(canvas.Bounds()), fill, image.Point{}, draw.Src)
	}
}

func textBox(face font.Face, text string) (int, int, int) {
	width := font.MeasureString(face, text).Ceil()
	metrics := face.Metrics()
	return width, metrics.Ascent.Ceil(), (metrics.Ascent + metrics.Descent).Ceil()
}

func write(canvas *image.RGBA, face font.Face, text string, x, baseline int) {
	d := font.Drawer{Dst: canvas, Src: image.NewUniform(white), Face: face, Dot: fixed.P(x, baseline)}
	d.DrawString(text)
}

func centerLabel(canvas *image.RGBA, face font.Face, text string, area image.Rectangle) {
	if text == "" || area.Empty() {
		return
	}
	width, ascent, height := textBox(face, text)
	if height+2*labelPadding > area.Dy() {
		return
	}
	x := area.Min.X + max(labelPadding, (area.Dx()-width)/2)
	top := area.Min.Y + (area.Dy()-height)/2
	write(canvas, face, text, x, top+ascent)
}

func pill(canvas *image.RGBA, face font.Face, text string) {
	if text == "" {
		return
	}
	width, ascent, height := textBox(face, text)
	box := image.Rect(labelPadding, labelPadding, labelPadding+width+2*labelPadding, labelPadding+height+2*labelPadding)
	draw.Draw(canvas, box.Intersect(canvas.Bounds()), image.NewUniform(pillColor), image.Point{}, draw.Over)
	write(canvas, face, text, box.Min.X+labelPadding, box.Min.Y+labelPadding+ascent)
}

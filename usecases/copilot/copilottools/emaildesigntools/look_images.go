package emaildesigntools

import (
	"bytes"
	"context"
	"encoding/base64"
	"html"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	_ "image/gif"

	"github.com/disintegration/imaging"

	"vozko/domain/emaildesign"
	"vozko/domain/media"
)

const (
	imageShown      = "shown"
	imageExternal   = "external"
	imageUnreadable = "unreadable"
	imageSkipped    = "skipped"

	maxInlinedBytes     = 2 << 20
	maxInlinedWidth     = 1200
	maxDecodedPixels    = 40_000_000
	heavyImageBytes     = 1 << 20
	retinaDensity       = 2
	inlinedJPEGQuality  = 80
	codeLowResolution   = "image_low_resolution"
	codeHeavyImageBytes = "image_heavy_file"
)

type lookImage struct {
	BlockID    string `json:"block_id"`
	State      string `json:"state"`
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	Bytes      int    `json:"bytes,omitempty"`
	ShownWidth int    `json:"shown_width"`
}

type imageBlock struct {
	id  string
	src string
}

func imageBlocks(blocks []emaildesign.Block) []imageBlock {
	var out []imageBlock
	for _, b := range blocks {
		switch {
		case b.Kind == emaildesign.KindImage && b.Image != nil && strings.TrimSpace(b.Image.Src) != "":
			out = append(out, imageBlock{id: b.ID, src: b.Image.Src})
		case b.Kind == emaildesign.KindColumns && b.Columns != nil:
			for _, column := range b.Columns.Columns {
				out = append(out, imageBlocks(column.Blocks)...)
			}
		}
	}
	return out
}

type inlined struct {
	dataURL string
	report  lookImage
}

func (d Deps) inlineImages(ctx context.Context, doc emaildesign.Document, page string, budget int) (string, []lookImage) {
	widths := emaildesign.ImageWidths(doc)
	done := map[string]inlined{}
	reports := make([]lookImage, 0)
	used := 0
	for _, block := range imageBlocks(doc.Blocks) {
		shown := widths[block.id]
		got, seen := done[block.src]
		if !seen {
			got = d.inlineOne(ctx, block.src, shown)
			if got.dataURL != "" && used+len(got.dataURL) > budget {
				got = inlined{report: lookImage{State: imageSkipped, Width: got.report.Width, Height: got.report.Height, Bytes: got.report.Bytes}}
			}
			if got.dataURL != "" {
				used += len(got.dataURL)
				page = strings.ReplaceAll(page, `src="`+html.EscapeString(block.src)+`"`, `src="`+got.dataURL+`"`)
			}
			done[block.src] = got
		}
		report := got.report
		report.BlockID, report.ShownWidth = block.id, shown
		reports = append(reports, report)
	}
	return page, reports
}

func (d Deps) inlineOne(ctx context.Context, src string, shown int) inlined {
	if d.Files == nil {
		return inlined{report: lookImage{State: imageExternal}}
	}
	key, ours := d.Files.KeyFromURL(src)
	if !ours {
		return inlined{report: lookImage{State: imageExternal}}
	}
	data, _, err := d.Files.DownloadFile(ctx, key)
	if err != nil || len(data) == 0 || len(data) > media.MaxReadBytes {
		return inlined{report: lookImage{State: imageUnreadable}}
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width*config.Height > maxDecodedPixels {
		return inlined{report: lookImage{State: imageUnreadable, Bytes: len(data)}}
	}
	report := lookImage{State: imageShown, Width: config.Width, Height: config.Height, Bytes: len(data)}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return inlined{report: lookImage{State: imageUnreadable, Bytes: len(data)}}
	}
	target := min(config.Width, max(shown*retinaDensity, 1), maxInlinedWidth)
	if target < config.Width {
		decoded = imaging.Resize(decoded, target, 0, imaging.Lanczos)
	}
	var buf bytes.Buffer
	mime := "image/jpeg"
	if format == "png" || format == "gif" {
		mime = "image/png"
		err = png.Encode(&buf, decoded)
	} else {
		err = jpeg.Encode(&buf, decoded, &jpeg.Options{Quality: inlinedJPEGQuality})
	}
	if err != nil {
		return inlined{report: lookImage{State: imageUnreadable, Bytes: len(data)}}
	}
	return inlined{dataURL: "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), report: report}
}

func imageChecks(reports []lookImage) []emaildesign.Check {
	checks := make([]emaildesign.Check, 0)
	for _, r := range reports {
		if r.State != imageShown {
			continue
		}
		if r.ShownWidth > 0 && r.Width < r.ShownWidth*retinaDensity {
			checks = append(checks, emaildesign.Check{Code: codeLowResolution, BlockID: r.BlockID})
		}
		if r.Bytes > heavyImageBytes {
			checks = append(checks, emaildesign.Check{Code: codeHeavyImageBytes, BlockID: r.BlockID})
		}
	}
	return checks
}

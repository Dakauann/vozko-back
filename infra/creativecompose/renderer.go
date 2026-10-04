package creativecompose

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"

	"vozko/domain/creativecompose"
)

//go:embed assets/fonts
var fonts embed.FS

func fontURL(file string) (template.URL, error) {
	raw, err := fonts.ReadFile("assets/fonts/" + file)
	if err != nil {
		return "", fmt.Errorf("creativecompose: read font %s: %w", file, err)
	}
	return template.URL("data:font/woff2;base64," + base64.StdEncoding.EncodeToString(raw)), nil
}

type Screenshotter interface {
	Screenshot(ctx context.Context, html string, width, height int) ([]byte, error)
}

type Renderer struct {
	browser Screenshotter
}

func NewRenderer(browser Screenshotter) *Renderer {
	return &Renderer{browser: browser}
}

type spacing struct {
	Pad      int
	TopPad   int
	Title    int
	Subline  int
	LogoSize int
}

var spacings = map[creativecompose.Template]spacing{
	creativecompose.TemplateFeed:  {Pad: 72, TopPad: 56, Title: 70, Subline: 30, LogoSize: 88},
	creativecompose.TemplateCard:  {Pad: 70, TopPad: 52, Title: 62, Subline: 28, LogoSize: 64},
	creativecompose.TemplateStory: {Pad: 80, TopPad: 250, Title: 78, Subline: 34, LogoSize: 96},
}

type page struct {
	Width, Height int
	Space         spacing
	Layout        creativecompose.Layout
	Image         template.URL
	Logo          template.URL
	Oxanium       template.URL
	Inter         template.URL
}

func (r *Renderer) HTML(layout creativecompose.Layout, images creativecompose.Images) (string, error) {
	width, height, err := layout.Template.Size()
	if err != nil {
		return "", err
	}
	oxanium, err := fontURL("oxanium-latin.woff2")
	if err != nil {
		return "", err
	}
	inter, err := fontURL("inter.woff2")
	if err != nil {
		return "", err
	}
	data := page{
		Width: width, Height: height, Space: spacings[layout.Template], Layout: layout,
		Image: template.URL(images.Image), Logo: template.URL(images.Logo), Oxanium: oxanium, Inter: inter,
	}
	var out bytes.Buffer
	if err := pageTemplate.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func (r *Renderer) Render(ctx context.Context, layout creativecompose.Layout, images creativecompose.Images) ([]byte, error) {
	html, err := r.HTML(layout, images)
	if err != nil {
		return nil, err
	}
	width, height, err := layout.Template.Size()
	if err != nil {
		return nil, err
	}
	return r.browser.Screenshot(ctx, html, width, height)
}

var pageTemplate = template.Must(template.New("creative").Parse(`<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8">
<style>
@font-face { font-family: "Oxanium"; src: url("{{.Oxanium}}") format("woff2"); font-weight: 200 800; }
@font-face { font-family: "Inter"; src: url("{{.Inter}}") format("woff2"); font-weight: 100 900; }
:root { --bg: #0c1311; --ink: #f2f6f4; --muted: #a9b8b2; --primary: #00c28f; }
* { box-sizing: border-box; margin: 0; padding: 0; }
html, body { width: {{.Width}}px; height: {{.Height}}px; overflow: hidden; background: var(--bg); }
body {
  font-family: "Inter", sans-serif; color: var(--ink);
  display: flex; flex-direction: column; gap: 28px;
  padding: {{.Space.TopPad}}px {{.Space.Pad}}px {{.Space.Pad}}px;
  background-image: radial-gradient(rgba(255,255,255,.07) 1.5px, transparent 1.5px); background-size: 36px 36px;
}
.head { display: flex; align-items: center; justify-content: space-between; min-height: {{.Space.LogoSize}}px; }
.head img { height: {{.Space.LogoSize}}px; width: auto; }
.eyebrow { font-family: "Oxanium", sans-serif; font-weight: 600; font-size: 26px; color: var(--primary); letter-spacing: .04em; }
h1 { font-family: "Oxanium", sans-serif; font-weight: 700; font-size: {{.Space.Title}}px; line-height: 1.05; }
h1 em { font-style: normal; color: var(--primary); display: block; }
.sub { font-size: {{.Space.Subline}}px; line-height: 1.38; color: var(--muted); max-width: 920px; }
.stage { position: relative; flex: 1; min-height: 0; display: flex; align-items: flex-start; justify-content: center; }
.stage img { display: block; max-width: 100%; max-height: 100%; width: auto; height: auto; border-radius: 18px;
  border: 1px solid rgba(255,255,255,.14); box-shadow: 0 30px 80px rgba(0,0,0,.55); }
.fade { position: absolute; left: -{{.Space.Pad}}px; right: -{{.Space.Pad}}px; bottom: 0; height: 30%;
  background: linear-gradient(to bottom, rgba(12,19,17,0), var(--bg) 92%); }
.callouts { position: absolute; left: -{{.Space.Pad}}px; bottom: 24px; display: flex; flex-direction: column; align-items: flex-start; gap: 14px; padding-left: {{.Space.Pad}}px; }
.callout { display: flex; align-items: center; gap: 12px; padding: 14px 24px 14px 18px; border-radius: 999px; background: #fff; color: #0e1412;
  font-weight: 600; font-size: 26px; box-shadow: 0 14px 34px rgba(0,0,0,.35); white-space: nowrap; }
.callout i { width: 12px; height: 12px; border-radius: 50%; background: var(--primary); }
.foot { display: flex; align-items: center; justify-content: space-between; gap: 24px; }
.cta { display: inline-flex; align-items: center; gap: 14px; background: var(--primary); color: #04110c; font-weight: 600; font-size: 30px; padding: 22px 34px; border-radius: 14px; }
.cta svg { width: 34px; height: 34px; }
.note { font-family: "Oxanium", sans-serif; font-weight: 600; font-size: 28px; color: var(--muted); }
</style></head>
<body>
<div class="head">
  {{if .Logo}}<img src="{{.Logo}}" alt="">{{else}}<span></span>{{end}}
  {{if .Layout.Eyebrow}}<span class="eyebrow">{{.Layout.Eyebrow}}</span>{{end}}
</div>
<h1>{{.Layout.Headline}}{{if .Layout.Highlight}}<em>{{.Layout.Highlight}}</em>{{end}}</h1>
{{if .Layout.Subline}}<p class="sub">{{.Layout.Subline}}</p>{{end}}
<div class="stage">
  <img src="{{.Image}}" alt="">
  <div class="fade"></div>
  {{if .Layout.Callouts}}<div class="callouts">{{range .Layout.Callouts}}<span class="callout"><i></i>{{.}}</span>{{end}}</div>{{end}}
</div>
{{if or .Layout.CallToAction .Layout.Footnote}}
<div class="foot">
  {{if .Layout.CallToAction}}<span class="cta">{{if .Layout.WhatsAppIcon}}<svg viewBox="0 0 24 24" aria-hidden="true"><path fill="currentColor" d="M17.47 14.38c-.3-.15-1.76-.87-2.03-.97-.27-.1-.47-.15-.67.15-.2.3-.77.97-.94 1.16-.17.2-.35.22-.64.07-.3-.15-1.26-.46-2.39-1.47-.88-.79-1.48-1.76-1.65-2.06-.17-.3-.02-.46.13-.6.13-.14.3-.35.45-.52.15-.17.2-.3.3-.5.1-.2.05-.37-.03-.52-.07-.15-.67-1.61-.92-2.2-.24-.58-.49-.5-.67-.51h-.57c-.2 0-.52.07-.79.37-.27.3-1.04 1.02-1.04 2.48s1.07 2.88 1.21 3.07c.15.2 2.1 3.2 5.08 4.49.71.31 1.26.49 1.7.63.71.23 1.36.2 1.87.12.57-.09 1.76-.72 2.01-1.41.25-.7.25-1.29.17-1.41-.07-.13-.27-.2-.57-.35M12.05 21.79h-.01a9.87 9.87 0 0 1-5.03-1.38l-.36-.21-3.74.98 1-3.65-.24-.37a9.86 9.86 0 0 1-1.51-5.26c0-5.45 4.44-9.88 9.9-9.88 2.64 0 5.12 1.03 6.99 2.9a9.82 9.82 0 0 1 2.89 6.99c0 5.45-4.44 9.88-9.89 9.88m8.41-18.3A11.81 11.81 0 0 0 12.05 0C5.5 0 .16 5.34.16 11.89c0 2.1.55 4.14 1.59 5.95L.06 24l6.3-1.65a11.88 11.88 0 0 0 5.68 1.45h.01c6.55 0 11.89-5.34 11.89-11.9 0-3.18-1.24-6.16-3.48-8.41"/></svg>{{end}}{{.Layout.CallToAction}}</span>{{else}}<span></span>{{end}}
  {{if .Layout.Footnote}}<span class="note">{{.Layout.Footnote}}</span>{{end}}
</div>
{{end}}
<script>
(function () {
  var images = Array.prototype.slice.call(document.images);
  var waits = [document.fonts.ready].concat(images.map(function (img) { return img.decode(); }));
  Promise.all(waits).then(function () { window.__PAGE_READY__ = true; }, function () { window.__PAGE_FAILED__ = true; });
})();
</script>
</body></html>`))

package emaildesign

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const DefaultLang = "pt-BR"

const (
	mobileBreakpoint    = 600
	defaultDividerColor = "#e5e7eb"
	buttonHeight        = 44
	buttonMinWidth      = 120
	preheaderPadding    = 80
)

var (
	ErrUnsubscribeURLInvalid     = errors.New("emaildesign: unsubscribe url must be an https url")
	ErrUnsubscribeLabelMissing   = errors.New("emaildesign: unsubscribe label is required")
	ErrViewInBrowserURLInvalid   = errors.New("emaildesign: view in browser url must be an https url")
	ErrViewInBrowserLabelMissing = errors.New("emaildesign: view in browser label is required")
	ErrLangInvalid               = errors.New("emaildesign: lang is not a valid language tag")
)

var langPattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

type RenderInput struct {
	Personalization
	Subject            string
	UnsubscribeURL     string
	UnsubscribeLabel   string
	InertUnsubscribe   bool
	ViewInBrowserURL   string
	ViewInBrowserLabel string
	UTM                *UTM
	Lang               string
	Transactional      bool
	Annotate           bool
	Draft              bool
}

type Rendered struct {
	HTML string
	Text string
}

func (in RenderInput) lang() (string, error) {
	if err := in.unsubscribeProblem(); err != nil {
		return "", err
	}
	if in.ViewInBrowserURL != "" {
		if !isHTTPSWithHost(in.ViewInBrowserURL) {
			return "", ErrViewInBrowserURLInvalid
		}
		if strings.TrimSpace(in.ViewInBrowserLabel) == "" {
			return "", ErrViewInBrowserLabelMissing
		}
	}
	if in.Lang == "" {
		return DefaultLang, nil
	}
	if !langPattern.MatchString(in.Lang) {
		return "", ErrLangInvalid
	}
	return in.Lang, nil
}

func (in RenderInput) unsubscribeProblem() error {
	if in.Transactional {
		return nil
	}
	if !in.InertUnsubscribe && !isHTTPSWithHost(in.UnsubscribeURL) {
		return ErrUnsubscribeURLInvalid
	}
	if strings.TrimSpace(in.UnsubscribeLabel) == "" {
		return ErrUnsubscribeLabelMissing
	}
	return nil
}

func (in RenderInput) unsubscribeHref() string {
	if in.InertUnsubscribe {
		return "#"
	}
	return in.UnsubscribeURL
}

func (in RenderInput) documentIssues(doc Document) []Issue {
	issues := Validate(doc)
	if in.Draft {
		return StructuralIssues(issues)
	}
	if !in.Transactional {
		return issues
	}
	kept := issues[:0:0]
	for _, issue := range issues {
		if issue.Code != issueFooterMissing {
			kept = append(kept, issue)
		}
	}
	return kept
}

func RenderSubject(subject string, p Personalization) (string, error) {
	if issues := ValidateSubject(subject); len(issues) > 0 {
		return "", &ValidationError{Kind: ErrInvalidSubject, Issues: issues}
	}
	return expander{person: p}.expand(subject, identity, singleLine)
}

func Render(doc Document, in RenderInput) (Rendered, error) {
	if issues := in.documentIssues(doc); len(issues) > 0 {
		return Rendered{}, &ValidationError{Kind: ErrInvalidDocument, Issues: issues}
	}
	lang, err := in.lang()
	if err != nil {
		return Rendered{}, err
	}
	title := ""
	if in.Subject != "" {
		if title, err = RenderSubject(in.Subject, in.Personalization); err != nil {
			return Rendered{}, err
		}
	}
	settings := doc.Settings.resolved()
	font, _ := lookupFontStack(settings.FontFamily)
	r := &renderer{settings: settings, font: font, exp: expander{person: in.Personalization}, utm: in.UTM, in: in}
	out := Rendered{HTML: r.document(doc, lang, title), Text: r.plainDocument(doc)}
	if r.err != nil {
		return Rendered{}, r.err
	}
	return out, nil
}

type renderer struct {
	settings Settings
	font     string
	exp      expander
	utm      *UTM
	in       RenderInput
	err      error
}

func (r *renderer) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *renderer) markup(s string) string {
	out, err := r.exp.expand(s, html.EscapeString, html.EscapeString)
	switch {
	case err != nil && r.in.Draft:
		return html.EscapeString(s)
	case err != nil:
		r.fail(err)
	}
	return out
}

func (r *renderer) plain(s string) string {
	out, err := r.exp.expand(s, identity, identity)
	switch {
	case err != nil && r.in.Draft:
		return s
	case err != nil:
		r.fail(err)
	}
	return out
}

func (r *renderer) link(href string) string {
	out, err := r.exp.expand(href, identity, url.QueryEscape)
	switch {
	case err != nil && r.in.Draft:
		return ""
	case err != nil:
		r.fail(err)
		return ""
	}
	if code := checkLink(out); code != linkOK {
		if code == linkMissing && r.in.Draft {
			return ""
		}
		r.fail(fmt.Errorf("%w: %s", ErrInvalidDocument, code))
		return ""
	}
	if r.utm != nil {
		return ApplyUTM(out, *r.utm)
	}
	return out
}

type box struct {
	vertical   int
	horizontal int
}

func paddingBox(p Padding, insideColumns bool) box {
	if p == "" {
		p = PaddingMedium
		if insideColumns {
			p = PaddingSmall
		}
	}
	switch p {
	case PaddingSmall:
		return box{vertical: 8, horizontal: 20}
	case PaddingLarge:
		return box{vertical: 32, horizontal: 40}
	}
	return box{vertical: 16, horizontal: 30}
}

func px(n int) string {
	return strconv.Itoa(n) + "px"
}

func alignOr(a, fallback Align) Align {
	if a == "" {
		return fallback
	}
	return a
}

func colorOr(c, fallback string) string {
	if c == "" {
		return fallback
	}
	return normalizeColor(c)
}

func (r *renderer) document(doc Document, lang, title string) string {
	s := r.settings
	attrLang := html.EscapeString(lang)
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Transitional//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd">` + "\n")
	b.WriteString(`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" lang="` + attrLang + `" xml:lang="` + attrLang + `">` + "\n")
	b.WriteString("<head>\n")
	b.WriteString(`<meta http-equiv="Content-Type" content="text/html; charset=UTF-8" />` + "\n")
	b.WriteString(`<meta name="viewport" content="width=device-width, initial-scale=1.0" />` + "\n")
	b.WriteString(`<meta http-equiv="X-UA-Compatible" content="IE=edge" />` + "\n")
	b.WriteString(`<meta name="x-apple-disable-message-reformatting" />` + "\n")
	b.WriteString(`<meta name="format-detection" content="telephone=no, date=no, address=no, email=no, url=no" />` + "\n")
	b.WriteString(`<meta name="color-scheme" content="light" />` + "\n")
	b.WriteString(`<meta name="supported-color-schemes" content="light" />` + "\n")
	b.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	b.WriteString(`<!--[if mso]><noscript><xml><o:OfficeDocumentSettings><o:AllowPNG/><o:PixelsPerInch>96</o:PixelsPerInch></o:OfficeDocumentSettings></xml></noscript><![endif]-->` + "\n")
	b.WriteString(`<style type="text/css">` + "\n")
	b.WriteString(":root{color-scheme:light;supported-color-schemes:light;}\n")
	b.WriteString("@media only screen and (max-width: " + px(mobileBreakpoint) + "){\n")
	b.WriteString(".container{width:100% !important;max-width:100% !important;}\n")
	b.WriteString(".column{display:block !important;width:100% !important;max-width:100% !important;}\n")
	b.WriteString(".fluid{width:100% !important;max-width:100% !important;height:auto !important;}\n")
	b.WriteString(".block{padding-left:16px !important;padding-right:16px !important;}\n")
	b.WriteString("}\n</style>\n</head>\n")
	b.WriteString(`<body style="margin:0;padding:0;width:100%;background-color:` + s.BackgroundColor + `;-webkit-text-size-adjust:100%;-ms-text-size-adjust:100%;">` + "\n")
	if strings.TrimSpace(s.Preheader) != "" {
		b.WriteString(`<span class="preheader" style="display:none !important;visibility:hidden;mso-hide:all;font-size:1px;line-height:1px;max-height:0;max-width:0;opacity:0;overflow:hidden;color:transparent;">`)
		b.WriteString(r.markup(s.Preheader))
		b.WriteString(strings.Repeat("&zwnj;&nbsp;", preheaderPadding))
		b.WriteString("</span>\n")
	}
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" bgcolor="` + s.BackgroundColor + `" style="width:100%;background-color:` + s.BackgroundColor + `;">` + "\n")
	b.WriteString(`<tr><td align="center" style="padding:24px 0;">` + "\n")
	b.WriteString(`<!--[if mso]><table role="presentation" width="` + strconv.Itoa(s.Width) + `" align="center" cellpadding="0" cellspacing="0" border="0"><tr><td><![endif]-->` + "\n")
	b.WriteString(`<table role="presentation" class="container" width="100%" cellpadding="0" cellspacing="0" border="0" style="width:100%;max-width:` + px(s.Width) + `;margin:0 auto;">` + "\n")
	if r.in.ViewInBrowserURL != "" {
		b.WriteString(`<tr><td align="center" style="padding:0 16px 12px 16px;font-family:` + r.font + `;font-size:12px;line-height:18px;color:` + s.TextColor + `;">`)
		b.WriteString(`<a href="` + html.EscapeString(r.in.ViewInBrowserURL) + `" target="_blank" style="color:` + s.TextColor + `;text-decoration:underline;">` + html.EscapeString(r.in.ViewInBrowserLabel) + `</a>`)
		b.WriteString("</td></tr>\n")
	}
	b.WriteString(`<tr><td bgcolor="` + s.ContentBackgroundColor + `" style="background-color:` + s.ContentBackgroundColor + `;">` + "\n")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">` + "\n")
	for _, block := range doc.Blocks {
		b.WriteString(r.block(block, s.Width, false))
		b.WriteString("\n")
	}
	b.WriteString("</table>\n</td></tr>\n</table>\n")
	b.WriteString(`<!--[if mso]></td></tr></table><![endif]-->` + "\n")
	b.WriteString("</td></tr>\n</table>\n</body>\n</html>\n")
	return b.String()
}

func (r *renderer) block(block Block, available int, insideColumns bool) string {
	if block.Kind == KindSpacer {
		return r.spacer(block)
	}
	pad := paddingBox(block.Padding, insideColumns)
	inner := available - 2*pad.horizontal
	var content string
	var align Align
	switch block.Kind {
	case KindHeading:
		content = r.heading(*block.Heading)
	case KindText:
		content = r.text(*block.Text)
	case KindButton:
		align = alignOr(block.Button.Align, AlignCenter)
		content = r.button(*block.Button, align, inner)
	case KindImage:
		align = alignOr(block.Image.Align, AlignCenter)
		content = r.image(*block.Image, align, inner)
	case KindDivider:
		content = r.divider(*block.Divider)
	case KindColumns:
		content = r.columns(*block.Columns, inner)
	case KindSocial:
		content = r.social(*block.Social)
	case KindFooter:
		content = r.footer(*block.Footer)
	}
	style := "padding:" + px(pad.vertical) + " " + px(pad.horizontal) + ";"
	attrs := ""
	if block.BackgroundColor != "" {
		bg := normalizeColor(block.BackgroundColor)
		style += "background-color:" + bg + ";"
		attrs += ` bgcolor="` + bg + `"`
	}
	if block.Kind == KindColumns {
		style += "font-size:0;"
	}
	if align != "" {
		attrs += ` align="` + string(align) + `"`
	}
	attrs += r.annotation(block)
	class := ""
	if !insideColumns {
		class = ` class="block"`
	}
	return "<tr><td" + class + attrs + ` style="` + style + `">` + content + "</td></tr>"
}

func (r *renderer) annotation(block Block) string {
	if !r.in.Annotate {
		return ""
	}
	return ` data-block-id="` + html.EscapeString(block.ID) + `"`
}

func ImageWidths(doc Document) map[string]int {
	widths := map[string]int{}
	var walk func(blocks []Block, available int, insideColumns bool)
	walk = func(blocks []Block, available int, insideColumns bool) {
		for _, block := range blocks {
			inner := available - 2*paddingBox(block.Padding, insideColumns).horizontal
			switch {
			case block.Kind == KindImage && block.Image != nil:
				widths[block.ID] = inner * block.Image.Width / 100
			case block.Kind == KindColumns && block.Columns != nil:
				shares := columnWidths(block.Columns.Ratio, inner)
				for i, column := range block.Columns.Columns {
					if i < len(shares) {
						walk(column.Blocks, shares[i], true)
					}
				}
			}
		}
	}
	walk(doc.Blocks, doc.Settings.resolved().Width, false)
	return widths
}

func (r *renderer) spacer(block Block) string {
	h := px(block.Spacer.Height)
	style := "height:" + h + ";line-height:" + h + ";font-size:1px;mso-line-height-rule:exactly;"
	attrs := ` height="` + strconv.Itoa(block.Spacer.Height) + `"` + r.annotation(block)
	if block.BackgroundColor != "" {
		bg := normalizeColor(block.BackgroundColor)
		style += "background-color:" + bg + ";"
		attrs += ` bgcolor="` + bg + `"`
	}
	return "<tr><td" + attrs + ` style="` + style + `">&nbsp;</td></tr>`
}

func (r *renderer) textStyle(size, lineHeight int, color string, align Align) string {
	return "font-family:" + r.font + ";font-size:" + px(size) + ";line-height:" + px(lineHeight) + ";color:" + color + ";text-align:" + string(align) + ";mso-line-height-rule:exactly;"
}

var headingSizes = map[int][2]int{1: {28, 36}, 2: {22, 30}, 3: {18, 26}}

func (r *renderer) heading(h Heading) string {
	size := headingSizes[h.Level]
	tag := "h" + strconv.Itoa(h.Level)
	style := "margin:0;font-weight:700;" + r.textStyle(size[0], size[1], colorOr(h.Color, r.settings.TextColor), alignOr(h.Align, AlignLeft))
	return "<" + tag + ` style="` + style + `">` + r.markup(h.Text) + "</" + tag + ">"
}

func (r *renderer) text(t Text) string {
	align := alignOr(t.Align, AlignLeft)
	var b strings.Builder
	for i, p := range t.Paragraphs {
		margin := "margin:0 0 16px 0;"
		if i == len(t.Paragraphs)-1 {
			margin = "margin:0;"
		}
		b.WriteString(`<p style="` + margin + r.textStyle(16, 26, r.settings.TextColor, align) + `">`)
		if len(p.Runs) == 0 {
			b.WriteString("&nbsp;")
		}
		for _, run := range p.Runs {
			b.WriteString(r.run(run))
		}
		b.WriteString("</p>")
	}
	return b.String()
}

func (r *renderer) run(run Run) string {
	out := strings.ReplaceAll(r.markup(run.Text), "\n", "<br />")
	if run.Underline {
		out = `<span style="text-decoration:underline;">` + out + "</span>"
	}
	if run.Italic {
		out = "<em>" + out + "</em>"
	}
	if run.Bold {
		out = "<strong>" + out + "</strong>"
	}
	if run.Link != nil {
		out = `<a href="` + html.EscapeString(r.link(run.Link.Href)) + `" target="_blank" style="color:` + r.settings.LinkColor + `;text-decoration:underline;">` + out + "</a>"
	}
	return out
}

func (r *renderer) button(btn Button, align Align, inner int) string {
	bg := colorOr(btn.Color, r.settings.ButtonColor)
	fg := colorOr(btn.TextColor, r.settings.ButtonTextColor)
	href := html.EscapeString(r.link(btn.Href))
	label := r.markup(btn.Label)
	width := inner
	if !btn.FullWidth {
		width = min(max(utf8.RuneCountInString(r.plain(btn.Label))*9+56, buttonMinWidth), inner)
	}
	tableAttrs := ""
	tableStyle := "border-collapse:separate;"
	anchorDisplay := "display:inline-block;"
	switch {
	case btn.FullWidth:
		tableAttrs = ` width="100%"`
		tableStyle += "width:100%;"
		anchorDisplay = "display:block;"
	case align == AlignCenter:
		tableStyle += "margin:0 auto;"
	case align == AlignRight:
		tableStyle += "margin:0 0 0 auto;"
	}
	var b strings.Builder
	b.WriteString(`<table role="presentation" cellpadding="0" cellspacing="0" border="0" align="` + string(align) + `"` + tableAttrs + ` style="` + tableStyle + `"><tr>`)
	b.WriteString(`<td align="center" bgcolor="` + bg + `" style="border-radius:6px;background-color:` + bg + `;">`)
	b.WriteString(`<!--[if mso]><v:roundrect xmlns:v="urn:schemas-microsoft-com:vml" xmlns:w="urn:schemas-microsoft-com:office:word" href="` + href + `" style="height:` + px(buttonHeight) + `;v-text-anchor:middle;width:` + px(width) + `;" arcsize="14%" stroke="f" fillcolor="` + bg + `"><w:anchorlock/>`)
	b.WriteString(`<center style="color:` + fg + `;font-family:` + r.font + `;font-size:16px;font-weight:bold;">` + label + `</center></v:roundrect><![endif]-->`)
	b.WriteString(`<!--[if !mso]><!--><a href="` + href + `" target="_blank" style="` + anchorDisplay + `padding:12px 28px;font-family:` + r.font + `;font-size:16px;font-weight:700;line-height:20px;color:` + fg + `;text-decoration:none;text-align:center;border-radius:6px;background-color:` + bg + `;">` + label + `</a><!--<![endif]-->`)
	b.WriteString("</td></tr></table>")
	return b.String()
}

func (r *renderer) image(img Image, align Align, inner int) string {
	width := inner * img.Width / 100
	margin := "margin:0 auto;"
	switch align {
	case AlignLeft:
		margin = "margin:0;"
	case AlignRight:
		margin = "margin:0 0 0 auto;"
	}
	tag := `<img src="` + html.EscapeString(img.Src) + `" alt="` + r.markup(img.Alt) + `" width="` + strconv.Itoa(width) + `" class="fluid" style="display:block;width:100%;max-width:` + px(width) + `;height:auto;border:0;outline:none;text-decoration:none;` + margin + `" />`
	if img.Href == "" {
		return tag
	}
	return `<a href="` + html.EscapeString(r.link(img.Href)) + `" target="_blank" style="text-decoration:none;">` + tag + "</a>"
}

func (r *renderer) divider(d Divider) string {
	color := colorOr(d.Color, defaultDividerColor)
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr><td style="border-top:` + px(d.Thickness) + ` solid ` + color + `;font-size:1px;line-height:1px;mso-line-height-rule:exactly;">&nbsp;</td></tr></table>`
}

var columnFractions = map[ColumnRatio][]int{
	Ratio5050:   {1, 1},
	Ratio3367:   {1, 2},
	Ratio6733:   {2, 1},
	Ratio333333: {1, 1, 1},
}

func columnWidths(ratio ColumnRatio, inner int) []int {
	fractions := columnFractions[ratio]
	total := 0
	for _, f := range fractions {
		total += f
	}
	widths := make([]int, len(fractions))
	used := 0
	for i, f := range fractions {
		if i == len(fractions)-1 {
			widths[i] = inner - used
			break
		}
		widths[i] = inner * f / total
		used += widths[i]
	}
	return widths
}

func (r *renderer) columns(c Columns, inner int) string {
	widths := columnWidths(c.Ratio, inner)
	var b strings.Builder
	b.WriteString(`<!--[if mso]><table role="presentation" width="` + strconv.Itoa(inner) + `" cellpadding="0" cellspacing="0" border="0"><tr>`)
	for i, column := range c.Columns {
		w := strconv.Itoa(widths[i])
		if i == 0 {
			b.WriteString(`<td width="` + w + `" valign="top" style="width:` + w + `px;"><![endif]-->`)
		} else {
			b.WriteString(`<!--[if mso]></td><td width="` + w + `" valign="top" style="width:` + w + `px;"><![endif]-->`)
		}
		b.WriteString(`<div class="column" style="display:inline-block;width:100%;max-width:` + w + `px;vertical-align:top;">`)
		b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">`)
		if len(column.Blocks) == 0 {
			b.WriteString(`<tr><td style="font-size:1px;line-height:1px;">&nbsp;</td></tr>`)
		}
		for _, child := range column.Blocks {
			b.WriteString(r.block(child, widths[i], true))
		}
		b.WriteString("</table></div>")
	}
	b.WriteString(`<!--[if mso]></td></tr></table><![endif]-->`)
	return b.String()
}

func (r *renderer) social(s Social) string {
	align := alignOr(s.Align, AlignCenter)
	parts := make([]string, 0, len(s.Links))
	for _, l := range s.Links {
		href := html.EscapeString(r.link(l.URL))
		parts = append(parts, `<a href="`+href+`" target="_blank" style="color:`+r.settings.LinkColor+`;text-decoration:none;font-weight:700;">`+r.markup(l.displayLabel())+`</a>`)
	}
	return `<p style="margin:0;` + r.textStyle(14, 22, r.settings.TextColor, align) + `">` + strings.Join(parts, "&nbsp;&nbsp;&middot;&nbsp;&nbsp;") + "</p>"
}

func (r *renderer) footer(f Footer) string {
	style := r.textStyle(12, 18, r.settings.TextColor, AlignCenter)
	var b strings.Builder
	if strings.TrimSpace(f.Text) != "" {
		b.WriteString(`<p style="margin:0 0 8px 0;` + style + `">` + strings.ReplaceAll(r.markup(f.Text), "\n", "<br />") + "</p>")
	}
	b.WriteString(`<p style="margin:0 0 8px 0;` + style + `">` + r.markup(f.CompanyName) + "<br />" + r.markup(f.Address) + "</p>")
	if r.in.Transactional {
		return b.String()
	}
	b.WriteString(`<p style="margin:0;` + style + `"><a href="` + html.EscapeString(r.in.unsubscribeHref()) + `" target="_blank" style="color:` + r.settings.LinkColor + `;text-decoration:underline;">` + html.EscapeString(r.in.UnsubscribeLabel) + "</a></p>")
	return b.String()
}

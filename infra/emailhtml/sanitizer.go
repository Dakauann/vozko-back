package emailhtml

import (
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"

	em "vozko/domain/emailinbox"
)

var (
	remoteImage = regexp.MustCompile(`(?i)<img[^>]*\ssrc="https?:`)
	dataImage   = regexp.MustCompile(`^data:image/(png|gif|jpe?g|webp);base64,[A-Za-z0-9+/=\s]+$`)
	tags        = regexp.MustCompile(`(?s)<[^>]*>`)
)

var emailStyles = []string{
	"color", "background-color", "font-family", "font-size", "font-weight", "font-style", "line-height", "letter-spacing",
	"text-align", "text-decoration", "text-transform", "vertical-align", "white-space", "word-break",
	"width", "max-width", "min-width", "height", "max-height", "min-height",
	"margin", "margin-top", "margin-right", "margin-bottom", "margin-left",
	"padding", "padding-top", "padding-right", "padding-bottom", "padding-left",
	"border", "border-top", "border-right", "border-bottom", "border-left", "border-color", "border-style", "border-width",
	"border-radius", "border-collapse", "border-spacing", "display", "table-layout",
}

type Sanitizer struct {
	withImages    *bluemonday.Policy
	withoutRemote *bluemonday.Policy
}

var _ em.HTMLSanitizer = (*Sanitizer)(nil)

func New() *Sanitizer {
	return &Sanitizer{withImages: emailPolicy(true), withoutRemote: emailPolicy(false)}
}

func emailPolicy(remoteImages bool) *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("p", "br", "hr", "div", "span", "center", "font", "h1", "h2", "h3", "h4", "h5", "h6",
		"strong", "b", "em", "i", "u", "s", "small", "big", "sup", "sub", "blockquote", "pre", "code", "a", "img")
	p.AllowLists()
	p.AllowTables()
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor", "border", "cellpadding", "cellspacing", "colspan", "rowspan",
		"dir", "title").Globally()
	p.AllowAttrs("color", "face", "size").OnElements("font")
	p.AllowAttrs("alt").OnElements("img")
	p.AllowStyles(emailStyles...).Globally()
	p.AllowAttrs("href").OnElements("a")
	p.AllowURLSchemes("http", "https", "mailto")
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(false)
	p.RequireNoReferrerOnFullyQualifiedLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	if remoteImages {
		p.AllowAttrs("src").OnElements("img")
	} else {
		p.AllowAttrs("src").Matching(dataImage).OnElements("img")
	}
	p.AllowDataURIImages()
	return p
}

func (s *Sanitizer) Sanitize(html string, allowRemoteImages bool) (em.SanitizedHTML, error) {
	if strings.TrimSpace(html) == "" || len(html) > em.MaxStoredHTMLBytes {
		return em.SanitizedHTML{}, em.ErrHTMLUnsafe
	}
	full := s.withImages.Sanitize(html)
	if !shows(full) {
		return em.SanitizedHTML{}, em.ErrHTMLUnsafe
	}
	if allowRemoteImages || !remoteImage.MatchString(full) {
		return em.SanitizedHTML{HTML: full}, nil
	}
	return em.SanitizedHTML{HTML: s.withoutRemote.Sanitize(html), RemoteImagesBlocked: true}, nil
}

func shows(html string) bool {
	return strings.Contains(html, "<img") || strings.TrimSpace(tags.ReplaceAllString(html, "")) != ""
}

package studio

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const LayerQR LayerType = "qr"

type QRLinkKind string

const (
	QRLinkShort    QRLinkKind = "shortLink"
	QRLinkWhatsapp QRLinkKind = "whatsapp"
	QRLinkURL      QRLinkKind = "url"
	QRLinkText     QRLinkKind = "text"
)

const (
	MinQRQuietZone  = 2
	MaxQRQuietZone  = 16
	MinQRContrast   = 4
	MaxQRURLBytes   = 2048
	MinQRLogoScale  = 0.08
	MinQRLogoAspect = 0.1
	MaxQRLogoAspect = 10

	WhatsappLinkBase = "https://wa.me/"
)

var (
	qrPhonePattern  = regexp.MustCompile(`^[1-9]\d{7,14}$`)
	qrLinkIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	qrColorPattern  = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

var qrByteCapacity = map[string]int{"L": 2953, "M": 2331, "Q": 1663, "H": 1273}

var qrLogoMax = map[string]float64{"L": 0, "M": 0.2, "Q": 0.25, "H": 0.3}

func QRLinkKinds() []QRLinkKind {
	return []QRLinkKind{QRLinkShort, QRLinkWhatsapp, QRLinkURL, QRLinkText}
}

func QRLevels() []string { return []string{"L", "M", "Q", "H"} }

func QRLogoMax(ecc string) float64 { return qrLogoMax[ecc] }

type QRLink struct {
	Kind        QRLinkKind `json:"kind,omitempty"`
	ShortLinkID string     `json:"shortLinkId,omitempty"`
	URL         string     `json:"url,omitempty"`
	Phone       string     `json:"phone,omitempty"`
	Message     string     `json:"message,omitempty"`
	Text        string     `json:"text,omitempty"`
}

type QRLogo struct {
	AssetID string  `json:"assetId"`
	Scale   float64 `json:"scale"`
	Aspect  float64 `json:"aspect"`
}

type QRSpec struct {
	Link      *QRLink `json:"link"`
	ECC       string  `json:"ecc"`
	QuietZone int     `json:"quietZone"`
	Dark      string  `json:"dark"`
	Light     string  `json:"light"`
	Logo      *QRLogo `json:"logo,omitempty"`
}

var qrLayerFields = map[string]bool{
	"id": true, "type": true, "name": true, "transform": true, "hidden": true, "locked": true,
	"groupId": true, "qr": true, "layoutItem": true,
}

func (l Layer) qrIssue() string {
	if !l.setsOnly(qrLayerFields) {
		return CodeInvalid
	}
	if l.QR == nil {
		return CodeRequired
	}
	return l.QR.Issue()
}

func (s QRSpec) Issue() string {
	if s.Link == nil {
		return CodeRequired
	}
	if code := s.Link.issue(); code != "" {
		return code
	}
	switch {
	case !slices.Contains(QRLevels(), s.ECC):
		return CodeUnknown
	case s.QuietZone < MinQRQuietZone || s.QuietZone > MaxQRQuietZone:
		return CodeOutOfRange
	case !qrColorPattern.MatchString(s.Dark) || !qrColorPattern.MatchString(s.Light):
		return CodeInvalid
	case qrColorsUnreadable(s.Dark, s.Light):
		return CodeInvalid
	case len(s.Link.content()) > qrByteCapacity[s.ECC]:
		return CodeTooLarge
	}
	return s.Logo.issue(s.ECC)
}

func (k QRLinkKind) fields() map[string]bool {
	switch k {
	case QRLinkShort:
		return map[string]bool{"shortLinkId": true, "url": true}
	case QRLinkWhatsapp:
		return map[string]bool{"phone": true, "message": true}
	case QRLinkURL:
		return map[string]bool{"url": true}
	}
	return map[string]bool{"text": true}
}

func (l QRLink) strayField() bool {
	allowed := l.Kind.fields()
	values := map[string]string{"shortLinkId": l.ShortLinkID, "url": l.URL, "phone": l.Phone, "message": l.Message, "text": l.Text}
	for name, value := range values {
		if value != "" && !allowed[name] {
			return true
		}
	}
	return false
}

func (l QRLink) issue() string {
	if !slices.Contains(QRLinkKinds(), l.Kind) {
		return CodeUnknown
	}
	if l.strayField() {
		return CodeInvalid
	}
	switch l.Kind {
	case QRLinkShort:
		if !qrLinkIDPattern.MatchString(l.ShortLinkID) || !qrURL(l.URL) {
			return CodeInvalid
		}
	case QRLinkURL:
		if !qrURL(l.URL) {
			return CodeInvalid
		}
	case QRLinkWhatsapp:
		if !qrPhonePattern.MatchString(l.Phone) || hasControl(l.Message, true) {
			return CodeInvalid
		}
	case QRLinkText:
		if strings.TrimFunc(l.Text, jsSpace) == "" {
			return CodeRequired
		}
		if hasControl(l.Text, true) {
			return CodeInvalid
		}
	}
	return ""
}

func (l QRLink) content() string {
	switch l.Kind {
	case QRLinkWhatsapp:
		if l.Message == "" {
			return WhatsappLinkBase + l.Phone
		}
		return WhatsappLinkBase + l.Phone + "?text=" + encodeURIComponent(l.Message)
	case QRLinkText:
		return l.Text
	}
	return l.URL
}

func (logo *QRLogo) issue(ecc string) string {
	switch {
	case logo == nil:
		return ""
	case !validToken(logo.AssetID):
		return CodeInvalid
	case !within(logo.Aspect, MinQRLogoAspect, MaxQRLogoAspect):
		return CodeOutOfRange
	case !within(logo.Scale, MinQRLogoScale, qrLogoMax[ecc]):
		return CodeOutOfRange
	}
	return ""
}

func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func hasControl(text string, allowBreaks bool) bool {
	for _, r := range text {
		if (r < 0x20 || (r >= 0x7f && r <= 0x9f)) && !(allowBreaks && (r == '\n' || r == '\r' || r == '\t')) {
			return true
		}
	}
	return false
}

func qrURL(value string) bool {
	rest, ok := cutSchemeFold(value, "https://")
	if !ok {
		rest, ok = cutSchemeFold(value, "http://")
	}
	if !ok || rest == "" || strings.ContainsAny(rest[:1], "/?#") || strings.ContainsFunc(rest, jsSpace) {
		return false
	}
	return !hasControl(value, false) && len(value) <= MaxQRURLBytes
}

func cutSchemeFold(value, scheme string) (string, bool) {
	if len(value) < len(scheme) || !strings.EqualFold(value[:len(scheme)], scheme) {
		return "", false
	}
	return value[len(scheme):], true
}

func encodeURIComponent(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		b := text[i]
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || strings.IndexByte("-_.!~*'()", b) >= 0 {
			out.WriteByte(b)
			continue
		}
		out.WriteString("%" + strings.ToUpper(strconv.FormatUint(uint64(b)>>4, 16)+strconv.FormatUint(uint64(b)&15, 16)))
	}
	return out.String()
}

func srgbLinear(channel uint64) float64 {
	c := float64(channel) / 255
	if c <= 0.03928 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func luminance(hex string) float64 {
	n, _ := strconv.ParseUint(hex[1:7], 16, 32)
	return 0.2126*srgbLinear(n>>16&255) + 0.7152*srgbLinear(n>>8&255) + 0.0722*srgbLinear(n&255)
}

func contrastRatio(a, b float64) float64 {
	return (math.Max(a, b) + 0.05) / (math.Min(a, b) + 0.05)
}

func qrColorsUnreadable(dark, light string) bool {
	d, l := luminance(dark), luminance(light)
	return d >= l || contrastRatio(d, l) < MinQRContrast
}

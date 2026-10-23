package emaildesign

import (
	"net/url"
	"regexp"
	"strings"
)

const (
	DefaultWidth = 600
	MinWidth     = 480
	MaxWidth     = 720
)

var hexColorPattern = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

func validColor(c string) bool {
	return hexColorPattern.MatchString(c)
}

func normalizeColor(c string) string {
	c = strings.ToLower(c)
	if len(c) == 4 {
		return "#" + strings.Repeat(c[1:2], 2) + strings.Repeat(c[2:3], 2) + strings.Repeat(c[3:4], 2)
	}
	return c
}

type fontStack struct {
	id    string
	stack string
}

var fontStacks = []fontStack{
	{id: "inter", stack: "Inter, Arial, Helvetica, sans-serif"},
	{id: "arial", stack: "Arial, Helvetica, sans-serif"},
	{id: "georgia", stack: "Georgia, 'Times New Roman', Times, serif"},
	{id: "helvetica", stack: "Helvetica, Arial, sans-serif"},
	{id: "verdana", stack: "Verdana, Geneva, sans-serif"},
	{id: "trebuchet", stack: "'Trebuchet MS', Arial, Helvetica, sans-serif"},
}

func FontFamilies() []string {
	ids := make([]string, 0, len(fontStacks))
	for _, f := range fontStacks {
		ids = append(ids, f.id)
	}
	return ids
}

func lookupFontStack(id string) (string, bool) {
	for _, f := range fontStacks {
		if f.id == id {
			return f.stack, true
		}
	}
	return "", false
}

var defaultSettings = Settings{
	Width:                  DefaultWidth,
	BackgroundColor:        "#f4f4f5",
	ContentBackgroundColor: "#ffffff",
	TextColor:              "#1f2937",
	LinkColor:              "#2563eb",
	ButtonColor:            "#2563eb",
	ButtonTextColor:        "#ffffff",
	FontFamily:             "arial",
}

func DefaultSettings() Settings {
	return defaultSettings
}

func (s Settings) resolved() Settings {
	out := s
	if out.Width == 0 {
		out.Width = defaultSettings.Width
	}
	pick := func(value, fallback string) string {
		if value == "" {
			return fallback
		}
		return normalizeColor(value)
	}
	out.BackgroundColor = pick(s.BackgroundColor, defaultSettings.BackgroundColor)
	out.ContentBackgroundColor = pick(s.ContentBackgroundColor, defaultSettings.ContentBackgroundColor)
	out.TextColor = pick(s.TextColor, defaultSettings.TextColor)
	out.LinkColor = pick(s.LinkColor, defaultSettings.LinkColor)
	out.ButtonColor = pick(s.ButtonColor, defaultSettings.ButtonColor)
	out.ButtonTextColor = pick(s.ButtonTextColor, defaultSettings.ButtonTextColor)
	if out.FontFamily == "" {
		out.FontFamily = defaultSettings.FontFamily
	}
	return out
}

func validPadding(p Padding) bool {
	switch p {
	case "", PaddingSmall, PaddingMedium, PaddingLarge:
		return true
	}
	return false
}

func validAlign(a Align) bool {
	switch a {
	case "", AlignLeft, AlignCenter, AlignRight:
		return true
	}
	return false
}

func (r ColumnRatio) shares() []int {
	switch r {
	case Ratio5050:
		return []int{50, 50}
	case Ratio3367:
		return []int{33, 67}
	case Ratio6733:
		return []int{67, 33}
	case Ratio333333:
		return []int{33, 33, 33}
	}
	return nil
}

var socialNetworks = []struct {
	network SocialNetwork
	label   string
}{
	{network: NetworkFacebook, label: "Facebook"},
	{network: NetworkInstagram, label: "Instagram"},
	{network: NetworkLinkedIn, label: "LinkedIn"},
	{network: NetworkX, label: "X"},
	{network: NetworkYouTube, label: "YouTube"},
	{network: NetworkTikTok, label: "TikTok"},
	{network: NetworkWhatsApp, label: "WhatsApp"},
	{network: NetworkWebsite, label: ""},
}

func (n SocialNetwork) known() bool {
	for _, s := range socialNetworks {
		if s.network == n {
			return true
		}
	}
	return false
}

func (l SocialLink) displayLabel() string {
	if strings.TrimSpace(l.Label) != "" {
		return l.Label
	}
	for _, s := range socialNetworks {
		if s.network == l.Network && s.label != "" {
			return s.label
		}
	}
	if u, err := url.Parse(l.URL); err == nil && u.Host != "" {
		return u.Host
	}
	return l.URL
}

const (
	linkOK            = ""
	linkMissing       = "href_missing"
	linkSchemeInvalid = "link_scheme_invalid"
	linkHostMissing   = "link_host_missing"
)

func checkLink(href string) string {
	if strings.TrimSpace(href) == "" {
		return linkMissing
	}
	if strings.ContainsAny(href, " \t\r\n") {
		return linkSchemeInvalid
	}
	u, err := url.Parse(href)
	if err != nil {
		return linkSchemeInvalid
	}
	switch strings.ToLower(u.Scheme) {
	case "https", "http":
		if u.Host == "" {
			return linkHostMissing
		}
		return linkOK
	case "mailto", "tel":
		if u.Opaque == "" {
			return linkHostMissing
		}
		return linkOK
	}
	return linkSchemeInvalid
}

func isHTTPSWithHost(raw string) bool {
	if strings.ContainsAny(raw, " \t\r\n") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https") && u.Host != ""
}

package shortlink

import (
	"strings"

	"vozko/domain/shortlink"
)

type uaResolver struct{}

func NewUAResolver() shortlink.UAResolver {
	return &uaResolver{}
}

var botSignatures = []string{
	"bot", "crawler", "spider", "slurp", "mediapartners", "bingpreview",
	"facebookexternalhit", "whatsapp", "telegrambot", "curl", "wget",
	"python-requests", "go-http-client", "headlesschrome", "phantomjs",
	"monitor", "pingdom", "uptimerobot", "semrush", "ahrefs",
}

func (r *uaResolver) Parse(userAgent string) shortlink.DeviceInfo {
	ua := strings.ToLower(strings.TrimSpace(userAgent))
	if ua == "" {
		return shortlink.DeviceInfo{DeviceType: "unknown"}
	}

	if isBot(ua) {
		return shortlink.DeviceInfo{
			DeviceType: "bot",
			OS:         detectOS(ua),
			Browser:    "bot",
			IsBot:      true,
		}
	}

	return shortlink.DeviceInfo{
		DeviceType: detectDeviceType(ua),
		OS:         detectOS(ua),
		Browser:    detectBrowser(ua),
	}
}

func isBot(ua string) bool {
	for _, sig := range botSignatures {
		if strings.Contains(ua, sig) {
			return true
		}
	}
	return false
}

func detectDeviceType(ua string) string {
	if strings.Contains(ua, "ipad") || strings.Contains(ua, "tablet") {
		return "tablet"
	}
	if strings.Contains(ua, "mobi") || strings.Contains(ua, "iphone") || strings.Contains(ua, "android") {
		return "mobile"
	}
	return "desktop"
}

func detectOS(ua string) string {
	switch {
	case strings.Contains(ua, "windows"):
		return "Windows"
	case strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") || strings.Contains(ua, "ios"):
		return "iOS"
	case strings.Contains(ua, "mac os") || strings.Contains(ua, "macintosh"):
		return "macOS"
	case strings.Contains(ua, "android"):
		return "Android"
	case strings.Contains(ua, "cros"):
		return "ChromeOS"
	case strings.Contains(ua, "linux"):
		return "Linux"
	default:
		return ""
	}
}

func detectBrowser(ua string) string {
	switch {
	case strings.Contains(ua, "edg"):
		return "Edge"
	case strings.Contains(ua, "opr") || strings.Contains(ua, "opera"):
		return "Opera"
	case strings.Contains(ua, "samsungbrowser"):
		return "Samsung Internet"
	case strings.Contains(ua, "firefox") || strings.Contains(ua, "fxios"):
		return "Firefox"
	case strings.Contains(ua, "chrome") || strings.Contains(ua, "crios"):
		return "Chrome"
	case strings.Contains(ua, "safari"):
		return "Safari"
	default:
		return ""
	}
}

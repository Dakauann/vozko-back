package shortlink

import "context"

type GeoInfo struct {
	Country string
	Region  string
	City    string
	IsProxy bool
}

type GeoHints struct {
	IP      string
	Country string
	Region  string
	City    string
}

type GeoResolver interface {
	Resolve(hints GeoHints) GeoInfo
}

type DeviceInfo struct {
	DeviceType string
	OS         string
	Browser    string
	IsBot      bool
}

type UAResolver interface {
	Parse(userAgent string) DeviceInfo
}

type ThreatVerdict struct {
	Safe    bool
	Threats []string
}

type ThreatScanner interface {
	Scan(ctx context.Context, rawURL string) (ThreatVerdict, error)
}

type HostGuard interface {
	ResolvesToBlocked(host string) bool
}

type QRCodeService interface {
	Generate(content string, size int) ([]byte, error)
}

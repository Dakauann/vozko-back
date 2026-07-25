package shortlink

import (
	qrcode "github.com/skip2/go-qrcode"

	"vozko/domain/shortlink"
)

type qrGenerator struct{}

func NewQRGenerator() shortlink.QRCodeService {
	return &qrGenerator{}
}

func (g *qrGenerator) Generate(content string, size int) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, size)
}

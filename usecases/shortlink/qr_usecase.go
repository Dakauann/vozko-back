package shortlink_usecase

import (
	"context"

	"vozko/domain/shortlink"
)

const (
	minQRSize     = 128
	maxQRSize     = 1024
	defaultQRSize = 320
)

type generateQRUseCase struct {
	repo    shortlink.ShortLinkRepository
	qr      shortlink.QRCodeService
	baseURL string
}

func NewGenerateQRUseCase(repo shortlink.ShortLinkRepository, qr shortlink.QRCodeService, baseURL string) shortlink.GenerateQRUseCase {
	return &generateQRUseCase{repo: repo, qr: qr, baseURL: baseURL}
}

func (uc *generateQRUseCase) Execute(ctx context.Context, workspaceID, id string, size int) ([]byte, error) {
	link, err := uc.repo.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return uc.qr.Generate(shortlink.ShortURL(uc.baseURL, link.Code), clampQRSize(size))
}

func clampQRSize(size int) int {
	if size <= 0 {
		return defaultQRSize
	}
	if size < minQRSize {
		return minQRSize
	}
	if size > maxQRSize {
		return maxQRSize
	}
	return size
}

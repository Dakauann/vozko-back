package advertising

import (
	"context"
	"log"

	"github.com/google/uuid"

	ads "vozko/domain/advertising"
	"vozko/domain/media"
)

type FundsGate interface {
	Check(workspaceID string) error
}

type AIBilling interface {
	Publish(workspaceID, model string, promptTokens, completionTokens int, providerCostMicros int64)
}

type mediaUploader interface {
	UploadMedia(workspaceID string, data []byte, mediaName string, mediaType media.MediaType, description string) (media.Media, error)
}

type ImageUseCase struct {
	generator ads.ImageGenerator
	funds     FundsGate
	billing   AIBilling
	uploader  mediaUploader
	ceiling   int64
}

func NewImageUseCase(generator ads.ImageGenerator, funds FundsGate, billing AIBilling, uploader mediaUploader, unknownCostCeilingMicros int64) *ImageUseCase {
	return &ImageUseCase{generator: generator, funds: funds, billing: billing, uploader: uploader, ceiling: unknownCostCeilingMicros}
}

type GeneratedCreative struct {
	Media media.Media
	Model string
}

func (uc *ImageUseCase) Generate(ctx context.Context, req ads.ImageRequest) (*GeneratedCreative, error) {
	if err := uc.Check(req); err != nil {
		return nil, err
	}
	image, err := uc.generator.Generate(ctx, req)
	if err != nil {
		return nil, err
	}
	cost := image.ProviderCostMicros
	if cost <= 0 {
		log.Printf("[ads] image generation for workspace %s reported no cost; billing the ceiling of %d micros", req.WorkspaceID, uc.ceiling)
		cost = uc.ceiling
	}
	uc.billing.Publish(req.WorkspaceID, image.Model, 0, 0, cost)
	name := "ads/" + req.WorkspaceID + "/" + uuid.NewString() + ".jpg"
	stored, err := uc.uploader.UploadMedia(req.WorkspaceID, image.Bytes, name, media.MediaTypeProductImage, "Imagem gerada para anúncio")
	if err != nil {
		log.Printf("[ads] generated image for workspace %s was billed but could not be stored: %v", req.WorkspaceID, err)
		return nil, err
	}
	return &GeneratedCreative{Media: stored, Model: image.Model}, nil
}

func (uc *ImageUseCase) Check(req ads.ImageRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	return uc.funds.Check(req.WorkspaceID)
}

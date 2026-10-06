package container

import (
	"context"
	"log"
	"net/http"
	"time"

	mediagenhttp "vozko/delivery/http/mediagen"
	"vozko/domain/mediagen"
	"vozko/infra/ai/aibilling"
	"vozko/infra/ai/openrouter"
	"vozko/infra/ai/openroutergen"
	media_infra "vozko/infra/media"
	"vozko/infra/mediagenqueue"
	"vozko/infra/processingcharges"
	"vozko/infra/rembg"
	mediagen_repository "vozko/infra/repositories/mediagen"
	mediagen_usecase "vozko/usecases/mediagen"
	webhook_usecase "vozko/usecases/webhook"
)

var (
	patientCostChecks = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second, 30 * time.Second, 30 * time.Second, 30 * time.Second}
	lateCostCheck     = []time.Duration{0}
)

const (
	renderDownloadTimeout = 90 * time.Second
	rembgTimeout          = 2 * time.Minute
	captionsLanguage      = "pt"
)

type mediaGenerationBundle struct {
	Service   *mediagen_usecase.Service
	Consumers []*webhook_usecase.ConsumerRunner[mediagen.QueueMessage]
	Handler   *mediagenhttp.Handler
}

func (c *Container) initMediaGeneration() {
	for _, consumer := range c.mediaGeneration().Consumers {
		if err := consumer.Start(); err != nil {
			log.Fatalf("[media-generation] failed to start a consumer: %v", err)
		}
	}
	log.Printf("[media-generation] consumers started")
}

func (c *Container) mediaGeneration() *mediaGenerationBundle {
	if c.mediaGenerationBundle != nil {
		return c.mediaGenerationBundle
	}
	if c.useCases.chatFunds == nil || c.useCases.uploadMedia == nil || c.useCases.getMedia == nil || c.services.mediaGenerationPub == nil || c.services.mediaGenerationSub == nil {
		log.Fatalf("[media-generation] the funds gate, media upload, media lookup and generation queue must exist before media generation")
	}
	provider := openroutergen.Config{APIKey: c.cfg.OpenRouterAPIKey}
	images, err := openroutergen.NewImageGenerator(provider)
	if err != nil {
		log.Fatalf("[media-generation] image generator: %v", err)
	}
	audio, err := openroutergen.NewAudioGenerator(provider, adAudioEncoder{})
	if err != nil {
		log.Fatalf("[media-generation] audio generator: %v", err)
	}
	mediaClient := &http.Client{Timeout: renderDownloadTimeout}
	cutout, err := rembg.New(c.cfg.RembgURL, &http.Client{Timeout: rembgTimeout}, mediaClient)
	if err != nil {
		log.Fatalf("[media-generation] background removal: %v", err)
	}
	if c.services.whisperPool == nil {
		log.Fatalf("[media-generation] captions need the whisper pool")
	}
	service, err := mediagen_usecase.NewService(mediagen_usecase.Deps{
		Generators: map[mediagen.Kind]mediagen.Generator{
			mediagen.KindImage:    images,
			mediagen.KindMusic:    audio,
			mediagen.KindVoice:    audio,
			mediagen.KindVideo:    media_infra.NewVideoRenderer(mediaClient),
			mediagen.KindCutout:   cutout,
			mediagen.KindCaptions: media_infra.NewCaptionsGenerator(mediaClient, c.services.whisperPool, captionsLanguage),
			mediagen.KindDenoise:  media_infra.NewDenoiseGenerator(mediaClient),
		},
		Models:    openrouter.NewMediaModelCatalog(c.cfg.OpenRouterAPIKey),
		Jobs:      mediagen_repository.NewJobRepository(c.db),
		Queue:     mediagenqueue.NewPublisher(c.services.mediaGenerationPub),
		Funds:     c.useCases.chatFunds,
		Billing:   aibilling.NewPublisher(c.services.billingQueuePub),
		Uploader:  c.useCases.uploadMedia,
		Library:   c.useCases.getMedia,
		Costs:     openrouter.NewGenerationCostLookup(c.cfg.OpenRouterAPIKey, patientCostChecks),
		LateCosts: openrouter.NewGenerationCostLookup(c.cfg.OpenRouterAPIKey, lateCostCheck),
		Charges:   processingcharges.Unpriced{},
	})
	if err != nil {
		log.Fatalf("[media-generation] every dependency must be wired: %v", err)
	}
	c.mediaGenerationBundle = &mediaGenerationBundle{
		Service:   service,
		Consumers: mediagen_usecase.NewConsumers(c.services.mediaGenerationSub, c.services.mediaGenerationPub, c.redisProvider.SharedState(), service),
		Handler:   mediagenhttp.NewHandler(service),
	}
	return c.mediaGenerationBundle
}

type adAudioEncoder struct{}

func (adAudioEncoder) Encode(ctx context.Context, raw []byte) ([]byte, error) {
	return media_infra.EncodeAdAudio(ctx, raw)
}

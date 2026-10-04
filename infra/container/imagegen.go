package container

import (
	"log"

	imagegenhttp "vozko/delivery/http/imagegen"
	"vozko/domain/imagegen"
	"vozko/infra/ai/aibilling"
	"vozko/infra/ai/openrouter"
	"vozko/infra/imagegenqueue"
	"vozko/infra/openaiimage"
	imagegen_repository "vozko/infra/repositories/imagegen"
	imagegen_usecase "vozko/usecases/imagegen"
	webhook_usecase "vozko/usecases/webhook"
)

type imageGenerationBundle struct {
	Service  *imagegen_usecase.Service
	Consumer *webhook_usecase.ConsumerRunner[imagegen.QueueMessage]
	Handler  *imagegenhttp.Handler
}

func (c *Container) initImageGeneration() {
	if err := c.imageGeneration().Consumer.Start(); err != nil {
		log.Fatalf("[image-generation] failed to start the consumer: %v", err)
	}
	log.Printf("[image-generation] consumer started")
}

func (c *Container) imageGeneration() *imageGenerationBundle {
	if c.imageGenerationBundle != nil {
		return c.imageGenerationBundle
	}
	if c.useCases.chatFunds == nil || c.useCases.uploadMedia == nil || c.useCases.getMedia == nil || c.services.imageGenerationPub == nil || c.services.imageGenerationSub == nil {
		log.Fatalf("[image-generation] the funds gate, media upload, media lookup and image queue must exist before image generation")
	}
	generator, err := openaiimage.New(openaiimage.Config{APIKey: c.cfg.OpenRouterAPIKey})
	if err != nil {
		log.Fatalf("[image-generation] generator: %v", err)
	}
	service, err := imagegen_usecase.NewService(imagegen_usecase.Deps{
		Generator:         generator,
		Models:            openrouter.NewImageModelCatalog(c.cfg.OpenRouterAPIKey),
		Jobs:              imagegen_repository.NewJobRepository(c.db),
		Queue:             imagegenqueue.NewPublisher(c.services.imageGenerationPub),
		Funds:             c.useCases.chatFunds,
		Billing:           aibilling.NewPublisher(c.services.billingQueuePub),
		Uploader:          c.useCases.uploadMedia,
		References:        c.useCases.getMedia,
		CostCeilingMicros: int64(c.cfg.ImageGenerationCostCeilingMicros),
	})
	if err != nil {
		log.Fatalf("[image-generation] IMAGE_GENERATION_COST_CEILING_MICROS must be positive and every dependency wired: %v", err)
	}
	c.imageGenerationBundle = &imageGenerationBundle{
		Service:  service,
		Consumer: imagegen_usecase.NewConsumer(c.services.imageGenerationSub, c.services.imageGenerationPub, c.redisProvider.SharedState(), service),
		Handler:  imagegenhttp.NewHandler(service),
	}
	return c.imageGenerationBundle
}

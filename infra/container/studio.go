package container

import (
	"log"

	studiohttp "vozko/delivery/http/studio"
	"vozko/domain/copilot"
	studio_repository "vozko/infra/repositories/studio"
	copilottools "vozko/usecases/copilot/copilottools"
	media_usecase "vozko/usecases/media"
	studio_usecase "vozko/usecases/studio"
)

func (c *Container) studioService() *studio_usecase.Service {
	service, err := studio_usecase.NewService(studio_repository.New(c.db))
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	return service
}

func (c *Container) studioCapabilities() *studio_usecase.CapabilityService {
	service, err := studio_usecase.NewCapabilityService(studio_repository.NewCapabilities(c.db))
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	return service
}

func (c *Container) studioExports() *studio_usecase.ExportService {
	library := media_usecase.NewRegisterStoredMediaUseCase(c.repositories.media, c.s3)
	service, err := studio_usecase.NewExportService(studio_repository.New(c.db), c.s3, library)
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	return service
}

func (c *Container) studioHandler() *studiohttp.Handler {
	return studiohttp.NewHandler(c.studioService(), c.studioCapabilities(), c.studioExports())
}

func (c *Container) studioTools() []copilot.Tool {
	media := c.mediaGeneration().Service
	return copilottools.StudioTools(copilottools.StudioDeps{Projects: c.studioService(), Media: media, Jobs: media})
}

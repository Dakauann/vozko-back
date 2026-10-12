package container

import (
	"log"

	studiohttp "vozko/delivery/http/studio"
	"vozko/domain/copilot"
	media_domain "vozko/domain/media"
	"vozko/domain/studio/library"
	studio_repository "vozko/infra/repositories/studio"
	"vozko/usecases/copilot/copilottools/studiotools"
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

func (c *Container) studioPresets() *studio_usecase.PresetService {
	service, err := studio_usecase.NewPresetService(studio_repository.NewPresets(c.db))
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	return service
}

func (c *Container) studioHandler() *studiohttp.Handler {
	return studiohttp.NewHandler(c.studioService(), c.studioCapabilities(), c.studioExports(), c.studioPresets()).WithLibraryAssets(c.studioLibraryAssets())
}

type libraryAssetMedia struct {
	*media_usecase.RegisterStoredMediaUseCase
	media_domain.GetMediaUseCase
}

func (c *Container) studioLibraryAssets() *studio_usecase.LibraryAssetService {
	catalog, err := library.Default()
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	stored := libraryAssetMedia{media_usecase.NewRegisterStoredMediaUseCase(c.repositories.media, c.s3), c.useCases.getMedia}
	service, err := studio_usecase.NewLibraryAssetService(catalog, c.s3, stored)
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	return service
}

func (c *Container) studioTools() []copilot.Tool {
	media := c.mediaGeneration().Service
	deps := studiotools.StudioDeps{Projects: c.studioService(), Media: media, Jobs: media, Presets: c.studioPresets(), Links: c.studioShortLinks(), Library: c.useCases.getMedia}
	return append(studiotools.StudioTools(deps), studiotools.NewNewStudioProjectTool(deps))
}

func (c *Container) studioShortLinks() studiotools.StudioShortLinks {
	return studiotools.StudioShortLinks{Create: c.useCases.createShortLink, Get: c.useCases.getShortLink, Delete: c.useCases.deleteShortLink, BaseURL: c.cfg.ShortLinkBaseURL, Permissions: c.useCases.checkWsAccess}
}

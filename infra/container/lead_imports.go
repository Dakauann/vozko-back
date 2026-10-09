package container

import (
	"log"

	"vozko/domain/leadimport"
	media_domain "vozko/domain/media"
	"vozko/infra/leadimportfiles"
	actor_repository "vozko/infra/repositories/actor"
	lead_repository "vozko/infra/repositories/lead"
	lead_usecase "vozko/usecases/lead"
	media_usecase "vozko/usecases/media"
)

type leadImportBundle struct {
	service *lead_usecase.Import
}

func (c *Container) leadImports() *lead_usecase.Import {
	if c.leadImportBundle != nil {
		return c.leadImportBundle.service
	}
	c.leadImportBundle = &leadImportBundle{service: c.buildLeadImports()}
	return c.leadImportBundle.service
}

func (c *Container) buildLeadImports() *lead_usecase.Import {
	if c.s3 == nil || c.services.fileReader == nil || c.repositories.media == nil {
		log.Printf("[lead-import] disabled: object storage or the media library is not configured; the import routes answer 503")
		return nil
	}
	if c.services.conversationAuthImpl == nil || c.useCases.memberVisibility == nil {
		log.Fatalf("[lead-import] the authorizer and member visibility must be wired before lead imports")
	}
	if c.services.metrics == nil {
		log.Fatalf("[lead-import] metrics must be wired before lead imports")
	}
	store, err := lead_repository.NewImports(c.repositories.lead)
	if err != nil {
		log.Fatalf("[lead-import] %v", err)
	}
	sheets := media_usecase.NewReadMediaUseCase(media_usecase.NewGetMediaUseCase(c.repositories.media, media_domain.MediaTypeLeadImport), c.services.fileReader)
	files, err := leadimportfiles.New(c.s3, c.repositories.media, sheets)
	if err != nil {
		log.Fatalf("[lead-import] %v", err)
	}
	var seeder leadimport.Seeder
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled && c.unofficialWhatsApp.SeedInboxPublisher != nil {
		seeder = c.unofficialWhatsApp.SeedInboxPublisher
	}
	service, err := lead_usecase.NewImport(lead_usecase.ImportDeps{
		Jobs:        store,
		Writer:      store,
		Lookup:      store,
		Files:       files,
		Members:     actor_repository.NewMemberEmails(c.db),
		Permissions: c.services.conversationAuthImpl,
		Visibility:  c.useCases.memberVisibility,
		Definitions: c.repositories.customField,
		Seeder:      seeder,
		Metrics:     c.services.metrics,
	})
	if err != nil {
		log.Fatalf("[lead-import] %v", err)
	}
	return service
}

package container

import (
	"log"

	"vozko/infra/browser"
	composeinfra "vozko/infra/creativecompose"
	composeuc "vozko/usecases/creativecompose"
)

func (c *Container) creativeComposer() *composeuc.Service {
	if c.useCases.uploadMedia == nil || c.useCases.getMedia == nil {
		log.Fatalf("[creative-compose] media upload and media lookup must exist before the creative composer")
	}
	return composeuc.NewService(composeinfra.NewLibrary(), composeinfra.NewRenderer(browser.NewRenderer()), c.useCases.getMedia, c.useCases.uploadMedia)
}

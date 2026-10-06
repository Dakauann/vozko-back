package container

import (
	"log"

	studiohttp "vozko/delivery/http/studio"
	studio_repository "vozko/infra/repositories/studio"
	studio_usecase "vozko/usecases/studio"
)

func (c *Container) studioHandler() *studiohttp.Handler {
	service, err := studio_usecase.NewService(studio_repository.New(c.db), c.mediaGeneration().Service)
	if err != nil {
		log.Fatalf("[studio] %v", err)
	}
	return studiohttp.NewHandler(service)
}

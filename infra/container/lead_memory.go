package container

import (
	"log"

	lead_memory_domain "vozko/domain/lead_memory"
	lead_memory_usecase "vozko/usecases/lead_memory"
)

type leadMemoryUseCases struct {
	create lead_memory_domain.CreateUseCase
	update lead_memory_domain.UpdateUseCase
	delete lead_memory_domain.DeleteUseCase
	list   lead_memory_domain.ListUseCase
}

func (c *Container) buildLeadMemories() leadMemoryUseCases {
	repo := c.repositories.leadMemory
	events := c.services.crmTelemetryEmitter

	var built leadMemoryUseCases
	var err error

	if built.create, err = lead_memory_usecase.NewCreateUseCase(repo, events); err != nil {
		log.Fatalf("[container] lead memories: %v", err)
	}
	if built.update, err = lead_memory_usecase.NewUpdateUseCase(repo, events); err != nil {
		log.Fatalf("[container] lead memories: %v", err)
	}
	if built.delete, err = lead_memory_usecase.NewDeleteUseCase(repo, events); err != nil {
		log.Fatalf("[container] lead memories: %v", err)
	}
	if built.list, err = lead_memory_usecase.NewListUseCase(repo, c.repositories.agent, c.repositories.user); err != nil {
		log.Fatalf("[container] lead memories: %v", err)
	}

	return built
}

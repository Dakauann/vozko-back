package container

import (
	"log"

	lead_memory_domain "vozko/domain/lead_memory"
	lead_memory_usecase "vozko/usecases/lead_memory"
)

// leadMemoryUseCases is the feature's whole use-case surface, returned as a
// value and spread into the container's useCases literal (see
// scheduledMessageUseCases for why field-by-field assignment would nil-deref).
//
// The same four values feed three consumers: the HTTP handler (operator CRUD),
// the manage_lead_memory tool (agent writes) and the agentturn assembler
// (prompt injection). One write model, one read model: wiring them from one
// builder is what keeps that a fact rather than an aspiration.
type leadMemoryUseCases struct {
	create lead_memory_domain.CreateUseCase
	update lead_memory_domain.UpdateUseCase
	delete lead_memory_domain.DeleteUseCase
	list   lead_memory_domain.ListUseCase
}

// buildLeadMemories wires the lead-memory feature.
//
// crmTelemetryEmitter may still be nil here (telemetry is optional); the
// emitter is nil-safe by contract, so it is passed as-is rather than guarded.
// The agent and user repositories serve only actor-label resolution and are
// likewise optional to the constructor.
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

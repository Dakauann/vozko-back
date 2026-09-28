package container

import (
	"log"

	stage_repository "vozko/infra/repositories/stage"
)

func (c *Container) wireContainerPipelines() {
	registrar, ok := c.repositories.stage.(stage_repository.PipelineRegistrar)
	if !ok {
		log.Printf("[stage] the stage repository cannot register pipeline resolvers; channels fall back to the default funnel")
		return
	}

	for entryType, resolver := range stage_repository.ChannelPipelineResolvers(c.db) {
		registrar.SetContainerPipelineResolver(entryType, resolver)
	}
}

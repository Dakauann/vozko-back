package container

import "vozko/usecases/actornames"

func (c *Container) actorNames() actornames.Directory {
	directory := actornames.Directory{Users: c.repositories.user, Agents: c.repositories.agent}
	if workflows, ok := c.repositories.workflow.(actornames.WorkflowLookup); ok {
		directory.Workflows = workflows
	}
	return directory
}

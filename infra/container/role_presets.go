package container

import (
	"log"

	workspace_usecase "vozko/usecases/workspace"
)

func (c *Container) syncLinkedRoles() {
	report, err := workspace_usecase.NewSyncLinkedRolesUseCase(c.repositories.workspace, c.repositories.customRole).Execute()
	if err != nil {
		log.Fatalf("Failed to bring roles linked to a preset up to date: %v", err)
	}
	log.Printf("[workspace] roles linked to a preset: %d updated, %d detached from a retired preset", report.Updated, report.Detached)
}

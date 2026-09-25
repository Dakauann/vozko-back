package container

import (
	"context"

	igdomain "vozko/domain/instagram"
	"vozko/domain/rag"
	"vozko/domain/readiness"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	"vozko/domain/workspace"
	readiness_usecase "vozko/usecases/readiness"
)

var countOnly = shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 1}}

func (c *Container) workspaceReadiness() readiness.SnapshotUseCase {
	access := c.useCases.checkWsAccess
	probes := []readiness.Probe{
		readiness_usecase.NewOfficialWhatsAppProbe(readiness_usecase.OfficialWhatsAppDeps{
			Phones:       c.useCases.workspacePhones,
			Entitlements: c.useCases.getWorkspaceEntitlements,
			Gate:         c.useCases.phoneProvisioningGate,
			Access:       access,
		}),
		readiness_usecase.NewTemplatesProbe(readiness_usecase.TemplatesDeps{
			Templates: c.useCases.workspaceTemplates,
			Phones:    c.useCases.workspacePhones,
			Access:    access,
		}),
		readiness_usecase.NewCountProbe(readiness.KnowledgeBases, workspace.ResourceKnowledgeBases,
			c.repositories.ragKnowledgeBase.CountByWorkspace, rag.MaxKnowledgeBasesPerWorkspace, access),
	}
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Entitlements != nil {
		probes = append(probes, readiness_usecase.NewUnofficialWhatsAppProbe(c.unofficialWhatsApp.Entitlements, access))
	}
	if c.instagram != nil && c.instagram.Accounts != nil {
		accounts := c.instagram.Accounts
		probes = append(probes, readiness_usecase.NewCountProbe(readiness.Instagram, workspace.ResourceInstagramAccounts,
			func(ctx context.Context, workspaceID string) (int, error) {
				out, err := accounts.ListByWorkspace(ctx, igdomain.ListAccountsInput{WorkspaceID: workspaceID, Options: countOnly})
				if err != nil {
					return 0, err
				}
				return int(out.TotalItems), nil
			}, 0, access))
	}
	if c.telegram != nil && c.telegram.Accounts != nil {
		accounts := c.telegram.Accounts
		probes = append(probes, readiness_usecase.NewCountProbe(readiness.Telegram, workspace.ResourceTelegramAccounts,
			func(ctx context.Context, workspaceID string) (int, error) {
				out, err := accounts.ListByWorkspace(ctx, tgdomain.ListAccountsInput{WorkspaceID: workspaceID, Options: countOnly})
				if err != nil {
					return 0, err
				}
				return int(out.TotalItems), nil
			}, 0, access))
	}
	return readiness_usecase.NewSnapshotUseCase(readiness_usecase.SnapshotDeps{
		Subscription: c.useCases.ensureActiveWorkspaceSubscription,
		Balance:      c.services.cachedBalanceChecker,
		Probes:       probes,
	})
}

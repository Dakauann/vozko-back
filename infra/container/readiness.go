package container

import (
	"context"

	fbdomain "vozko/domain/facebook"
	igdomain "vozko/domain/instagram"
	"vozko/domain/rag"
	"vozko/domain/readiness"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
	wcdomain "vozko/domain/webchat"
	"vozko/domain/workspace"
	readiness_usecase "vozko/usecases/readiness"
)

var countOnly = shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 1}}

func workspaceTotal[In, T any](
	list func(context.Context, In) (*shared.PaginatedResult[T], error),
	input func(workspaceID string) In,
) func(context.Context, string) (int, error) {
	return func(ctx context.Context, workspaceID string) (int, error) {
		out, err := list(ctx, input(workspaceID))
		if err != nil {
			return 0, err
		}
		return int(out.TotalItems), nil
	}
}

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
		probes = append(probes, readiness_usecase.NewCountProbe(readiness.Instagram, workspace.ResourceInstagramAccounts,
			workspaceTotal(c.instagram.Accounts.ListByWorkspace, func(workspaceID string) igdomain.ListAccountsInput {
				return igdomain.ListAccountsInput{WorkspaceID: workspaceID, Options: countOnly}
			}), 0, access))
	}
	if c.telegram != nil && c.telegram.Accounts != nil {
		probes = append(probes, readiness_usecase.NewCountProbe(readiness.Telegram, workspace.ResourceTelegramAccounts,
			workspaceTotal(c.telegram.Accounts.ListByWorkspace, func(workspaceID string) tgdomain.ListAccountsInput {
				return tgdomain.ListAccountsInput{WorkspaceID: workspaceID, Options: countOnly}
			}), 0, access))
	}
	if c.webchat != nil {
		probes = append(probes, readiness_usecase.NewCountProbe(readiness.Webchat, workspace.ResourceWebchatWidgets,
			workspaceTotal(c.webchat.Widgets.ListByWorkspace, func(workspaceID string) wcdomain.ListWidgetsInput {
				return wcdomain.ListWidgetsInput{WorkspaceID: workspaceID, Options: countOnly}
			}), 0, access))
	}
	if c.facebook != nil && c.facebook.Pages != nil {
		probes = append(probes, readiness_usecase.NewCountProbe(readiness.Facebook, workspace.ResourceFacebookPages,
			workspaceTotal(c.facebook.Pages.ListByWorkspace, func(workspaceID string) fbdomain.ListPagesInput {
				return fbdomain.ListPagesInput{WorkspaceID: workspaceID, Options: countOnly}
			}), 0, access))
	}
	return readiness_usecase.NewSnapshotUseCase(readiness_usecase.SnapshotDeps{
		Subscription: c.useCases.ensureActiveWorkspaceSubscription,
		Balance:      c.services.cachedBalanceChecker,
		Probes:       probes,
	})
}

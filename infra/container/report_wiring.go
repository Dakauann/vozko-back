package container

import (
	"context"
	"log"
	"strings"

	lead_domain "vozko/domain/lead"
	opportunity_domain "vozko/domain/opportunity"
	report_domain "vozko/domain/report"
	"vozko/domain/shared"
	"vozko/domain/whatsappform"
	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/browser"
	whatsappform_repository "vozko/infra/repositories/whatsappform"
	report_usecase "vozko/usecases/report"
	report_renderers "vozko/usecases/report/renderers"
)

type reportObjectStore interface {
	UploadFile(key string, data []byte, contentType string) error
	DownloadFile(ctx context.Context, key string) ([]byte, string, error)
}

type s3ReportStorage struct {
	files reportObjectStore
}

func (s s3ReportStorage) Upload(key string, data []byte, contentType string) error {
	return s.files.UploadFile(key, data, contentType)
}

func (s s3ReportStorage) Download(ctx context.Context, key string) ([]byte, string, error) {
	return s.files.DownloadFile(ctx, key)
}

var _ report_domain.Storage = s3ReportStorage{}

type reportDepartmentScopes struct {
	members     dept.Members
	departments dept.MemberDepartments
}

func (s reportDepartmentScopes) For(_ context.Context, workspaceID, userID string, isAdmin bool) (*dept.DepartmentFilter, error) {
	return dept.RequesterScope(s.members, s.departments, workspaceID, userID, isAdmin)
}

type formReadScopes struct {
	deals opportunity_domain.PersonDealsUseCase
}

func (s formReadScopes) ReadScope(userID, workspaceID string, isAdmin bool) (whatsappform.ReadScope, bool) {
	scope, err := s.deals.Scope(shared.Person{UserID: userID, SystemAdmin: isAdmin}, workspaceID)
	if err != nil {
		return whatsappform.ReadScope{}, false
	}
	return whatsappform.ReadScope{Restrict: scope.Restrict, DepartmentIDs: scope.DepartmentIDs, AssigneeUserID: scope.AssigneeOverride}, true
}

type leadsByID interface {
	FindByIDs(workspaceID string, ids []string) ([]*lead_domain.Lead, error)
}

type formLeadNames struct {
	leads leadsByID
}

func (n formLeadNames) Names(_ context.Context, workspaceID string, ids []string) (map[string]string, error) {
	found, err := n.leads.FindByIDs(workspaceID, ids)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(found))
	for _, l := range found {
		if l != nil && l.WorkspaceID == workspaceID {
			names[l.ID] = l.RealName()
		}
	}
	return names, nil
}

func (c *Container) whatsAppFormResponsesRenderer() report_domain.Renderer {
	if c.db == nil || c.useCases.personDeals == nil || c.repositories.lead == nil || c.repositories.businessPhone == nil || c.services.conversationAuth == nil {
		log.Printf("[report] whatsapp form responses export is off: a dependency is not configured")
		return nil
	}
	return report_renderers.NewWhatsAppFormResponsesRenderer(report_renderers.WhatsAppFormResponsesDeps{
		Forms:       whatsappform_repository.NewFormStore(c.db),
		Versions:    whatsappform_repository.NewVersionStore(c.db),
		Responses:   whatsappform_repository.NewResponseStore(c.db),
		Scopes:      formReadScopes{deals: c.useCases.personDeals},
		LeadNames:   formLeadNames{leads: c.repositories.lead},
		Phones:      c.repositories.businessPhone,
		Locations:   c.attendanceScheduleResolver(),
		Permissions: c.services.conversationAuth,
	})
}

func (c *Container) buildReportRegistry() *report_domain.Registry {
	registry := report_domain.NewRegistry()

	if reason := c.reportPDFDisabledReason(); reason != "" {
		log.Printf("[report] PDF is disabled: %s", reason)
	}

	withPDF := func(kind report_domain.Kind, filename string, base report_domain.Renderer) {
		registry.Register(report_domain.NewFormatRouter(kind, base, c.pdfRendererFor(kind, filename)))
	}

	if c.useCases.getOverview != nil {
		withPDF(report_domain.KindAttendanceOverview, "atendimento",
			report_renderers.NewAttendanceRenderer(
				c.useCases.getOverview,
				report_renderers.ExportLabels(),
			))
	}
	if c.useCases.exportEntries != nil {
		withPDF(report_domain.KindConversationEntries, "conversas",
			report_renderers.NewConversationEntriesRenderer(c.useCases.exportEntries, reportDepartmentScopes{members: c.repositories.workspace, departments: c.repositories.workspaceDepartment}))
	}
	if c.services.opportunityIO != nil {
		withPDF(report_domain.KindOpportunities, "oportunidades",
			report_renderers.NewOpportunitiesRenderer(c.services.opportunityIO, c.useCases.personDeals))
	}
	registry.Register(c.leadExportRenderer())
	if c.useCases.exportWCDispatchReport != nil {
		registry.Register(report_renderers.NewWhatsAppDispatchRenderer(c.useCases.exportWCDispatchReport, reportDepartmentScopes{members: c.repositories.workspace, departments: c.repositories.workspaceDepartment}))
	}
	if c.services.transactionsExporter != nil {
		withPDF(report_domain.KindBalanceTransactions, "transacoes",
			report_renderers.NewBalanceTransactionsRenderer(c.services.transactionsExporter))
	}
	registry.Register(c.whatsAppFormResponsesRenderer())

	return registry
}

func (c *Container) pdfRendererFor(kind report_domain.Kind, filename string) report_domain.Renderer {
	printer := browser.NewRenderer()
	if !printer.Available() {
		return nil
	}
	if strings.TrimSpace(c.cfg.FrontendBaseURL) == "" || strings.TrimSpace(c.cfg.AuthJWTSecret) == "" {
		return nil
	}

	return report_renderers.NewPDFRenderer(
		kind, printer, c.cfg.FrontendBaseURL, c.cfg.AuthJWTSecret, filename,
	)
}

func (c *Container) reportPDFDisabledReason() string {
	if !browser.NewRenderer().Available() {
		return "no chromium executable was found; set CHROMIUM_PATH"
	}
	if strings.TrimSpace(c.cfg.FrontendBaseURL) == "" {
		return "FrontendBaseURL is not configured"
	}
	if strings.TrimSpace(c.cfg.AuthJWTSecret) == "" {
		return "no secret is available to sign print tokens"
	}
	return ""
}

func (c *Container) buildReportService() *report_usecase.Service {
	if c.repositories.report == nil || c.s3 == nil || c.services.reportQueuePub == nil {
		log.Printf("[report] disabled: the repository, object storage or queue is not configured")
		return nil
	}

	service := report_usecase.NewService(
		c.repositories.report,
		c.buildReportRegistry(),
		c.services.reportQueuePub,
		s3ReportStorage{files: c.s3},
	)
	service.SetPrintSecret(c.cfg.AuthJWTSecret)
	service.SetAccess(c.services.conversationAuth)
	return service
}

func (c *Container) startReportWorker() {
	if c.services.reportService == nil || c.services.reportQueueSub == nil {
		return
	}
	if err := report_usecase.NewWorker(c.services.reportService, c.services.reportQueueSub).Start(); err != nil {
		log.Printf("[report] the worker did not start, requested reports will stay queued: %v", err)
		return
	}
	log.Printf("[report] worker listening on %v", report_domain.QueueTopics())
}

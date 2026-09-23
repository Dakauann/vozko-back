package container

import (
	"context"
	"log"
	"strings"

	report_domain "vozko/domain/report"
	"vozko/infra/browser"
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
			report_renderers.NewConversationEntriesRenderer(c.useCases.exportEntries))
	}
	if c.services.opportunityIO != nil {
		withPDF(report_domain.KindOpportunities, "oportunidades",
			report_renderers.NewOpportunitiesRenderer(c.services.opportunityIO))
	}
	if c.services.transactionsExporter != nil {
		withPDF(report_domain.KindBalanceTransactions, "transacoes",
			report_renderers.NewBalanceTransactionsRenderer(c.services.transactionsExporter))
	}

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

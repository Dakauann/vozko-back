package container

import (
	"context"
	"log"

	report_domain "vozko/domain/report"
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

	if c.useCases.getOverview != nil {
		registry.Register(report_renderers.NewAttendanceRenderer(
			c.useCases.getOverview,
			report_renderers.ExportLabels(),
		))
	}
	if c.useCases.exportEntries != nil {
		registry.Register(report_renderers.NewConversationEntriesRenderer(c.useCases.exportEntries))
	}
	if c.services.opportunityIO != nil {
		registry.Register(report_renderers.NewOpportunitiesRenderer(c.services.opportunityIO))
	}
	if c.services.transactionsExporter != nil {
		registry.Register(report_renderers.NewBalanceTransactionsRenderer(c.services.transactionsExporter))
	}

	return registry
}

func (c *Container) buildReportService() *report_usecase.Service {
	if c.repositories.report == nil || c.s3 == nil || c.services.reportQueuePub == nil {
		log.Printf("[report] disabled: the repository, object storage or queue is not configured")
		return nil
	}

	return report_usecase.NewService(
		c.repositories.report,
		c.buildReportRegistry(),
		c.services.reportQueuePub,
		s3ReportStorage{files: c.s3},
	)
}

func (c *Container) startReportWorker() {
	if c.services.reportService == nil || c.services.reportQueueSub == nil {
		return
	}
	if err := report_usecase.NewWorker(c.services.reportService, c.services.reportQueueSub).Start(); err != nil {
		log.Printf("[report] the worker did not start, requested reports will stay queued: %v", err)
		return
	}
	log.Printf("[report] worker listening on %s", report_domain.QueueTopic)
}

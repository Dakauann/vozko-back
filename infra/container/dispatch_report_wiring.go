package container

import (
	"time"

	wc_usecase "vozko/usecases/whatsapp_campaign"
)

const dispatchReportSectionTTL = 60 * time.Second

func (c *Container) dispatchReportCaching() wc_usecase.ReportCaching {
	return wc_usecase.ReportCaching{
		Memo: c.analyticsMemo(),
		Gate: c.sharedAnalyticsGate(),
		TTL:  dispatchReportSectionTTL,
	}
}

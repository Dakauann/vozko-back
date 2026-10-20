package container

import (
	"log"

	"vozko/domain/campaign"
	"vozko/infra/repositories/campaignstamp"
	"vozko/usecases/campaignlive"
)

type campaignLive struct {
	progress campaign.ProgressUseCase
	timeline campaign.TimelineUseCase
}

func (c *Container) campaignLive(namespace string, counter campaignlive.Counter, table campaignstamp.EntryTable) campaignLive {
	progress, err := campaignlive.NewProgress(campaignlive.ProgressDeps{
		Namespace: namespace,
		Counter:   counter,
		Memo:      c.analyticsMemo(),
		Samples:   c.redisProvider.SharedState(),
	})
	if err != nil {
		log.Fatalf("[campaigns] the %s campaign progress cannot be built: %v", namespace, err)
	}
	timeline, err := campaignlive.NewTimeline(campaignlive.TimelineDeps{
		Namespace: namespace,
		Source:    campaignstamp.NewTimelineReader(c.db, table),
		Locator:   c.attendanceScheduleResolver(),
		Memo:      c.analyticsMemo(),
		Gate:      c.sharedAnalyticsGate(),
	})
	if err != nil {
		log.Fatalf("[campaigns] the %s campaign timeline cannot be built: %v", namespace, err)
	}
	return campaignLive{progress: progress, timeline: timeline}
}

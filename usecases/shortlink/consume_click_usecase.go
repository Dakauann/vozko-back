package shortlink_usecase

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/shortlink"
)

const clickConsumerConcurrency = 8

type consumeClickUseCase struct {
	queueSub      messaging.MessageQueueSub
	queuePub      messaging.MessageQueuePub
	clickRepo     shortlink.ClickRepository
	shortLinkRepo shortlink.ShortLinkRepository
	ua            shortlink.UAResolver
	geo           shortlink.GeoResolver
	shared        cache.SharedState
	ipHashSalt    string
	uniqueWindow  time.Duration
	semaphore     chan struct{}
}

func NewConsumeClickUseCase(
	queueSub messaging.MessageQueueSub,
	queuePub messaging.MessageQueuePub,
	clickRepo shortlink.ClickRepository,
	shortLinkRepo shortlink.ShortLinkRepository,
	ua shortlink.UAResolver,
	geo shortlink.GeoResolver,
	shared cache.SharedState,
	ipHashSalt string,
	uniqueWindow time.Duration,
) shortlink.ConsumeClickUseCase {
	return &consumeClickUseCase{
		queueSub:      queueSub,
		queuePub:      queuePub,
		clickRepo:     clickRepo,
		shortLinkRepo: shortLinkRepo,
		ua:            ua,
		geo:           geo,
		shared:        shared,
		ipHashSalt:    ipHashSalt,
		uniqueWindow:  uniqueWindow,
		semaphore:     make(chan struct{}, clickConsumerConcurrency),
	}
}

func (uc *consumeClickUseCase) Start() error {
	return uc.queueSub.Subscribe(shortlink.ClickTopic, func(payload []byte, ack messaging.MessageAck) {
		uc.handle(payload, ack)
	})
}

func (uc *consumeClickUseCase) handle(payload []byte, ack messaging.MessageAck) {
	var msg shortlink.ClickMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		log.Printf("[shortlink] consumer: invalid message, nacking without requeue: %v", err)
		_ = ack.Nack(false)
		return
	}

	uc.semaphore <- struct{}{}
	go func(m shortlink.ClickMessage) {
		defer func() { <-uc.semaphore }()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[shortlink] consumer: panic processing click %s: %v", m.ClickEventID, r)
				_ = ack.Nack(m.Attempt <= 1)
				return
			}
			if err := ack.Ack(); err != nil {
				log.Printf("[shortlink] consumer: failed to ack click %s: %v", m.ClickEventID, err)
			}
		}()

		if err := uc.process(m); err != nil {
			log.Printf("[shortlink] consumer: processing failed for click %s (attempt %d/%d): %v",
				m.ClickEventID, m.Attempt, shortlink.MaxClickProcessingAttempts, err)
			if m.Attempt < shortlink.MaxClickProcessingAttempts {
				uc.retryLater(m)
			}
		}
	}(msg)
}

func (uc *consumeClickUseCase) process(msg shortlink.ClickMessage) error {
	device := uc.ua.Parse(msg.UserAgent)
	geo := uc.geo.Resolve(shortlink.GeoHints{
		IP:      msg.IP,
		Country: msg.GeoCountry,
		Region:  msg.GeoRegion,
		City:    msg.GeoCity,
	})
	ipHash := HashIP(uc.ipHashSalt, msg.IP)

	click := &shortlink.Click{
		ID:            msg.ClickEventID,
		ShortLinkID:   msg.ShortLinkID,
		WorkspaceID:   msg.WorkspaceID,
		OccurredAt:    msg.OccurredAt,
		IPHash:        ipHash,
		Country:       geo.Country,
		Region:        geo.Region,
		City:          geo.City,
		DeviceType:    device.DeviceType,
		OS:            device.OS,
		Browser:       device.Browser,
		RefererDomain: RefererDomain(msg.Referer),
		UTMSource:     msg.UTMSource,
		UTMMedium:     msg.UTMMedium,
		UTMCampaign:   msg.UTMCampaign,
		IsBot:         device.IsBot,
		IsProxy:       geo.IsProxy,
		Language:      msg.Language,
	}

	isNew, err := uc.clickRepo.RecordClick(context.Background(), click)
	if err != nil {
		return err
	}
	if !isNew {
		return nil
	}

	uniqueDelta := uc.uniqueDelta(click.ShortLinkID, ipHash)

	if err := uc.clickRepo.ApplyDailyStats(context.Background(), buildDailyStatDeltas(click, uniqueDelta)); err != nil {
		return err
	}
	return uc.shortLinkRepo.ApplyClick(context.Background(), click.ShortLinkID, uniqueDelta, click.OccurredAt)
}

func (uc *consumeClickUseCase) uniqueDelta(shortLinkID, ipHash string) int64 {
	if ipHash == "" || uc.shared == nil {
		return 0
	}
	isNew, err := uc.shared.SetNX(uniqueVisitorKey(shortLinkID, ipHash), "1", uc.uniqueWindow)
	if err != nil || !isNew {
		return 0
	}
	return 1
}

func (uc *consumeClickUseCase) retryLater(msg shortlink.ClickMessage) {
	retry := msg
	retry.Attempt = msg.Attempt + 1
	payload, _ := json.Marshal(retry)
	if err := uc.queuePub.Publish(shortlink.ClickTopic, payload); err != nil {
		log.Printf("[shortlink] consumer: failed to publish retry for %s: %v", msg.ClickEventID, err)
	}
}

func buildDailyStatDeltas(click *shortlink.Click, uniqueDelta int64) []shortlink.DailyStatDelta {
	day := click.OccurredAt.UTC().Truncate(24 * time.Hour)
	deltas := []shortlink.DailyStatDelta{
		{
			ShortLinkID:  click.ShortLinkID,
			WorkspaceID:  click.WorkspaceID,
			Day:          day,
			Dimension:    shortlink.DimTotal,
			Clicks:       1,
			UniqueClicks: uniqueDelta,
		},
	}
	add := func(dim, val string) {
		if val == "" {
			return
		}
		deltas = append(deltas, shortlink.DailyStatDelta{
			ShortLinkID:    click.ShortLinkID,
			WorkspaceID:    click.WorkspaceID,
			Day:            day,
			Dimension:      dim,
			DimensionValue: val,
			Clicks:         1,
		})
	}
	add(shortlink.DimCountry, click.Country)
	add(shortlink.DimDevice, click.DeviceType)
	add(shortlink.DimReferer, click.RefererDomain)
	add(shortlink.DimBrowser, click.Browser)
	add(shortlink.DimOS, click.OS)
	return deltas
}

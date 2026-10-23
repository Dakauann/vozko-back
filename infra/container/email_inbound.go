package container

import (
	"log"
	"time"

	domainCache "vozko/domain/cache"
	"vozko/infra/ses"
)

const (
	emailReceiptRulesLockTTL  = time.Minute
	emailReceiptRulesLockPoll = 200 * time.Millisecond
)

func (c *Container) emailInboundRoutes(receipts ses.ReceiptAPI, topicARN, bucket string) *ses.InboundRoutes {
	lock, err := domainCache.NewOwnedLock(c.redisProvider.SharedState(), emailReceiptRulesLockTTL, emailReceiptRulesLockPoll)
	if err != nil {
		log.Fatalf("[email] receipt rules lock: %v", err)
	}
	routes, err := ses.NewInboundRoutes(receipts, lock, ses.InboundRoutesConfig{InboundTopicARN: topicARN, InboundBucket: bucket})
	if err != nil {
		log.Fatalf("[email] inbound routes: %v", err)
	}
	return routes
}

package emailsending

import "time"

const (
	InboundChunkBytes          int64 = 256 << 10
	InboundFloodThreshold            = 300
	InboundFloodWindow               = 10 * time.Minute
	InboundBlockDuration             = 7 * 24 * time.Hour
	InboundConditionsPerRule         = 100
	InboundRulesPerRuleSet           = 200
	InboundIPFiltersPerAccount       = 100
	replyLocalPart                   = "reply"
	unsubscribeLocalPart             = "unsubscribe"
)

type Verdict string

const (
	VerdictPass             Verdict = "PASS"
	VerdictFail             Verdict = "FAIL"
	VerdictGray             Verdict = "GRAY"
	VerdictProcessingFailed Verdict = "PROCESSING_FAILED"
)

type InboundVerdict struct {
	Spam  Verdict
	Virus Verdict
}

func (v InboundVerdict) Rejected() bool {
	return v.Spam == VerdictFail || v.Virus == VerdictFail
}

func BillableChunks(sizeBytes int64) int64 {
	if sizeBytes <= 0 {
		return 0
	}
	return sizeBytes / InboundChunkBytes
}

func Flooding(messagesInWindow int) bool {
	return messagesInWindow >= InboundFloodThreshold
}

func InboundPaused(balanceMicros int64) bool {
	return balanceMicros < 0
}

func InboundRecipients(replyDomain string) []string {
	return []string{ReplyRecipient(replyDomain), UnsubscribeRecipient(replyDomain)}
}

func ReplyRecipient(replyDomain string) string {
	return replyLocalPart + "@" + replyDomain
}

func UnsubscribeRecipient(replyDomain string) string {
	return unsubscribeLocalPart + "@" + replyDomain
}

func InboundReplyDomainsPerRule() int {
	return InboundConditionsPerRule / len(InboundRecipients("x"))
}

func InboundReplyDomainCapacity() int {
	return InboundReplyDomainsPerRule() * InboundRulesPerRuleSet
}

func (d SendingDomain) ReceivesReplies() bool {
	return !d.Retired || d.Kind.Shared()
}

func (d SendingDomain) RepliesPaused() bool {
	return d.RepliesPausedAt != nil
}

type SenderBlock struct {
	Name  string
	CIDR  string
	Until time.Time
}

func ExpiredSenderBlocks(blocks []SenderBlock, now time.Time) []SenderBlock {
	var expired []SenderBlock
	for _, b := range blocks {
		if !b.Until.After(now) {
			expired = append(expired, b)
		}
	}
	return expired
}

func SenderBlockToEvict(managed []SenderBlock, filtersInUse int) (SenderBlock, bool) {
	if filtersInUse < InboundIPFiltersPerAccount || len(managed) == 0 {
		return SenderBlock{}, false
	}
	soonest := managed[0]
	for _, b := range managed[1:] {
		if b.Until.Before(soonest.Until) {
			soonest = b
		}
	}
	return soonest, true
}

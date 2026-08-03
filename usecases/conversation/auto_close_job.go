package conversation_usecase

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"vozko/domain/conversation"
	wce "vozko/domain/whatsapp_campaign_entry"
)

// DefaultAutoCloseBatch is the max entries closed per channel per policy per tick.
// Keeps each cron run short and index-friendly (ORDER BY + LIMIT).
const DefaultAutoCloseBatch = 200

type autoCloseJob struct {
	whatsapp wce.Repository
	status   conversation.ConversationStatusUpdater
	batch    int
	// disabled when env CONVERSATION_AUTO_CLOSE_DISABLED is truthy.
	disabled bool
}

// NewAutoCloseJob wires idle + max-age closers. status must be the shared service.
//
// Policy A (customer_idle): agent/AI last, customer quiet, default on (24h).
// Policy C (max_age): last_message_at absolute inactivity, default on (7d).
// Industry: Intercom waiting-on-customer + LivePerson long inactivity hygiene.
func NewAutoCloseJob(whatsapp wce.Repository, status conversation.ConversationStatusUpdater) conversation.AutoCloseJob {
	batch := DefaultAutoCloseBatch
	if v := strings.TrimSpace(os.Getenv("CONVERSATION_AUTO_CLOSE_BATCH")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if n > 1000 {
				n = 1000
			}
			batch = n
		}
	}
	disabled := false
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CONVERSATION_AUTO_CLOSE_DISABLED"))) {
	case "1", "true", "yes", "on":
		disabled = true
	}
	return &autoCloseJob{
		whatsapp: whatsapp,
		status:   status,
		batch:    batch,
		disabled: disabled,
	}
}

func (j *autoCloseJob) ProcessIdleCloses() error {
	if j == nil || j.status == nil {
		return nil
	}
	if j.disabled {
		return nil
	}

	started := time.Now()
	idleClosed, idleFailed := j.finishBatch("customer_idle", conversation.CloseReasonCustomerIdle, j.collectIdle)
	maxClosed, maxFailed := j.finishBatch("max_age", conversation.CloseReasonMaxAge, j.collectMaxAge)
	failed := idleFailed + maxFailed

	if idleClosed > 0 || maxClosed > 0 || failed > 0 {
		log.Printf("[auto_close] idle=%d max_age=%d failed=%d batch=%d took=%s",
			idleClosed, maxClosed, failed, j.batch, time.Since(started).Round(time.Millisecond))
	}
	return nil
}

type entryRef struct {
	id  string
	typ string
}

func (j *autoCloseJob) collectIdle() ([]entryRef, error) {
	var out []entryRef
	if j.whatsapp != nil {
		cands, err := j.whatsapp.ListEligibleForAutoClose(j.batch)
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			out = append(out, entryRef{id: c.EntryID, typ: "whatsapp"})
		}
	}
	return out, nil
}

func (j *autoCloseJob) collectMaxAge() ([]entryRef, error) {
	var out []entryRef
	if j.whatsapp != nil {
		cands, err := j.whatsapp.ListEligibleForMaxAge(j.batch)
		if err != nil {
			return nil, err
		}
		for _, c := range cands {
			out = append(out, entryRef{id: c.EntryID, typ: "whatsapp"})
		}
	}
	return out, nil
}

func (j *autoCloseJob) finishBatch(
	name string,
	reason conversation.CloseReason,
	collect func() ([]entryRef, error),
) (closed, failed int) {
	refs, err := collect()
	if err != nil {
		log.Printf("[auto_close] %s list error: %v", name, err)
		return 0, 1
	}
	for _, ref := range refs {
		if err := j.status.Finish(ref.id, ref.typ, conversation.FinishOptions{
			Source: conversation.CloseSourceSystem,
			Reason: reason,
		}); err != nil {
			failed++
			log.Printf("[auto_close] %s finish %s %s: %v", name, ref.typ, ref.id, err)
			continue
		}
		closed++
	}
	return closed, failed
}

package unofficial_whatsapp_campaign

import (
	"errors"
	"testing"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignqueue"
)

const audioExitText = "Responda /sair para não receber mais"

func (h *harness) seedAudio(t *testing.T, exitText string) (campID, entryID string) {
	t.Helper()
	campID, entryID = h.seed(t)
	c, _ := h.campaigns.FindByID(campID)
	c.Message = uwc.MessageSpec{Kind: uwc.KindAudio, MediaID: "media-audio", ExitText: exitText}
	h.campaigns.put(c)
	return campID, entryID
}

func TestAnAudioIsFollowedByItsExitTextInTheSameChat(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seedAudio(t, audioExitText)

	if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done", got.Outcome)
	}

	if h.sender.count() != 2 {
		t.Fatalf("sends = %d, want the audio and then its exit text", h.sender.count())
	}
	audio, exit := h.sender.sent[0], h.sender.sent[1]
	if audio.MediaID != "media-audio" || exit.MediaID != "" || exit.Text != audioExitText {
		t.Fatalf("audio = %+v, exit = %+v", audio, exit)
	}
	if exit.EntryID != audio.EntryID || exit.CampaignID != campID || exit.EntryType != audio.EntryType {
		t.Fatalf("the exit text left the chat: audio %+v, exit %+v", audio, exit)
	}
	record := h.entries.sends[0]
	if record.MessageID != "msg-"+audio.EntryID+"-1" || record.ProviderMessageID != "prov-"+audio.EntryID+"-1" || record.ExitMessageID != "msg-"+audio.EntryID+"-2" {
		t.Fatalf("record = %+v", record)
	}
	if record.ErrorCode != 0 || h.entries.get(entryID).Status != campaign.SendStatusSent {
		t.Fatalf("record = %+v, status %s", record, h.entries.get(entryID).Status)
	}
	if len(h.spam.recorded) != 1 || len(h.metrics.recorded) != 1 {
		t.Fatalf("one entry counts once: cooldowns %d, metrics %d", len(h.spam.recorded), len(h.metrics.recorded))
	}
}

func TestAFailedExitTextKeepsTheAudioSentAndRecordsWhyOnTheEntry(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seedAudio(t, audioExitText)
	h.sender.textErr = errors.New("host down")

	if got := h.run(campID, entryID); got.Outcome != campaignqueue.OutcomeDone {
		t.Fatalf("outcome = %v, want Done so the audio is never sent twice", got.Outcome)
	}

	record := h.entries.sends[0]
	if record.ExitMessageID != "" || record.ErrorCode != errExitTextNotSent || record.ErrorMessage == "" {
		t.Fatalf("record = %+v", record)
	}
	if h.entries.get(entryID).Status != campaign.SendStatusSent {
		t.Fatalf("status = %s, want SENT since the audio arrived", h.entries.get(entryID).Status)
	}
}

func TestAnOldAudioWithoutAnExitTextSendsOnlyTheAudio(t *testing.T) {
	h := newHarness(t)
	campID, entryID := h.seedAudio(t, "")

	h.run(campID, entryID)

	if h.sender.count() != 1 || h.entries.sends[0].ExitMessageID != "" || h.entries.sends[0].ErrorCode != 0 {
		t.Fatalf("sends = %d, record = %+v", h.sender.count(), h.entries.sends[0])
	}
}

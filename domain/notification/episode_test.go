package notification

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var episodeStart = time.Date(2026, 10, 18, 17, 20, 0, 0, time.UTC)

func sentEpisode(steps EpisodeStep, lastSent time.Time) Episode {
	e := NewEpisode("ws-1", "meta_ads_funds:acc-1:funds_out", episodeStart)
	e.ID = "ep-1"
	e.StepsSent = steps
	if steps > 0 {
		e.LastSentAt = &lastSent
	}
	return e
}

func TestTheEscalationScheduleSendsAtOnceThenAfterADayThenThreeDaysLaterAndThenStops(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		name     string
		episode  Episode
		at       time.Time
		wantStep EpisodeStep
		wantDue  bool
	}{
		{"a new episode notifies at once", sentEpisode(0, time.Time{}), episodeStart, FirstNotice, true},
		{"the reminder waits a full day after the first notice", sentEpisode(FirstNotice, episodeStart), episodeStart.Add(day - time.Minute), 0, false},
		{"the reminder goes out one day after the first notice", sentEpisode(FirstNotice, episodeStart), episodeStart.Add(day), Reminder, true},
		{"the last notice waits three days after the reminder", sentEpisode(Reminder, episodeStart.Add(day)), episodeStart.Add(4*day - time.Minute), 0, false},
		{"the last notice goes out three days after the reminder", sentEpisode(Reminder, episodeStart.Add(day)), episodeStart.Add(4 * day), LastNotice, true},
		{"a late reminder pushes the last notice back so the spacing holds", sentEpisode(Reminder, episodeStart.Add(2*day)), episodeStart.Add(4 * day), 0, false},
		{"after the last notice the episode stays silent", sentEpisode(LastNotice, episodeStart.Add(4*day)), episodeStart.Add(30 * day), 0, false},
	}
	for _, c := range cases {
		step, due := c.episode.NextStep(c.at)
		if step != c.wantStep || due != c.wantDue {
			t.Errorf("%s: got step %d due %v, want step %d due %v", c.name, step, due, c.wantStep, c.wantDue)
		}
	}
}

func TestAnEpisodeSendsAtMostThreeDeliveries(t *testing.T) {
	if EpisodeSteps != 3 {
		t.Fatalf("the owner capped persistent alerts at three emails, got %d", EpisodeSteps)
	}
}

func TestAnEpisodeWithoutAPublishForTwoDaysIsTreatedAsClosed(t *testing.T) {
	e := sentEpisode(FirstNotice, episodeStart)
	e.LastSeenAt = episodeStart.Add(10 * time.Hour)
	if e.Quiet(e.LastSeenAt.Add(48*time.Hour - time.Second)) {
		t.Fatal("a publish within two days keeps the episode open")
	}
	if !e.Quiet(e.LastSeenAt.Add(48 * time.Hour)) {
		t.Fatal("two days without a publish means the producer stopped seeing the situation")
	}
}

func TestEachStepOfAnEpisodeHasItsOwnDedupKey(t *testing.T) {
	e := sentEpisode(0, time.Time{})
	if e.StepKey(FirstNotice) != "episode:ep-1:1" || e.StepKey(Reminder) == e.StepKey(FirstNotice) {
		t.Fatalf("step keys %q %q", e.StepKey(FirstNotice), e.StepKey(Reminder))
	}
}

func episodeDraft() Draft {
	d := validDraft()
	d.Title = "Os anúncios pararam: os fundos acabaram"
	d.Body = "Os fundos da conta de anúncios Loja na Meta acabaram."
	d.ActionURL = "/dashboard/advertising"
	d.EpisodeKey = "meta_ads_funds:acc-1:funds_out"
	d.EmailCopy = &EmailCopy{Template: "meta_ads_funds_alert.html", Subject: "Os anúncios pararam: os fundos acabaram - Marca", Placeholders: map[string]interface{}{"Message": "m"}}
	return d
}

func TestTheFirstNoticeStatesTheSituationAsTheProducerWroteIt(t *testing.T) {
	d := episodeDraft()
	got := d.ForEpisodeStep(FirstNotice, episodeStart, time.UTC)
	if got.Title != d.Title || got.Body != d.Body || got.EmailCopy.Subject != d.EmailCopy.Subject {
		t.Fatalf("first notice changed the copy: %+v", got)
	}
	if _, ok := got.EmailCopy.Placeholders[EpisodeNoticePlaceholder]; ok {
		t.Fatal("the first notice carries no reminder block")
	}
}

func TestTheReminderSaysItIsStillHappeningAndSinceWhen(t *testing.T) {
	d := episodeDraft()
	brt := time.FixedZone("BRT", -3*60*60)
	got := d.ForEpisodeStep(Reminder, episodeStart, brt)
	if !strings.HasPrefix(got.Title, "Lembrete: ") || !strings.HasPrefix(got.EmailCopy.Subject, "Lembrete: ") {
		t.Fatalf("title %q subject %q", got.Title, got.EmailCopy.Subject)
	}
	if !strings.Contains(got.Body, "18/10/2026 às 14:20") || !strings.Contains(got.Body, d.Body) {
		t.Fatalf("body %q must say since when, in the reader's time, and keep the situation", got.Body)
	}
	notice, _ := got.EmailCopy.Placeholders[EpisodeNoticePlaceholder].(string)
	if !strings.Contains(notice, "18/10/2026 às 14:20") || strings.Contains(strings.ToLower(notice), "último") {
		t.Fatalf("reminder notice %q", notice)
	}
	if got.EmailCopy.Placeholders["Message"] != "m" {
		t.Fatal("the producer placeholders stay")
	}
	if _, touched := d.EmailCopy.Placeholders[EpisodeNoticePlaceholder]; touched {
		t.Fatal("shaping a step must not change the producer's draft")
	}
}

func TestTheLastNoticeSaysNoMoreEmailsWillComeAndKeepsTheFix(t *testing.T) {
	d := episodeDraft()
	got := d.ForEpisodeStep(LastNotice, episodeStart, time.UTC)
	if !strings.HasPrefix(got.Title, "Último aviso: ") || !strings.HasPrefix(got.EmailCopy.Subject, "Último aviso: ") {
		t.Fatalf("title %q subject %q", got.Title, got.EmailCopy.Subject)
	}
	for _, text := range []string{got.Body, got.EmailCopy.Placeholders[EpisodeNoticePlaceholder].(string)} {
		if !strings.Contains(text, "não vamos enviar mais emails") {
			t.Errorf("%q must say no more emails will be sent until it is resolved", text)
		}
	}
	if got.ActionURL != d.ActionURL {
		t.Fatal("the last notice keeps the link to fix it")
	}
}

func TestStepTitlesStayWithinTheTitleLimit(t *testing.T) {
	d := episodeDraft()
	d.Title = strings.Repeat("a", TitleMaxRunes)
	d.Body = strings.Repeat("b", BodyMaxRunes)
	for _, step := range []EpisodeStep{Reminder, LastNotice} {
		got := d.ForEpisodeStep(step, episodeStart, time.UTC)
		got.EpisodeKey = ""
		if err := got.Validate(); err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
	}
}

func TestEveryStepCopyHasNoDashPunctuation(t *testing.T) {
	d := episodeDraft()
	for _, step := range []EpisodeStep{Reminder, LastNotice} {
		got := d.ForEpisodeStep(step, episodeStart, time.UTC)
		for _, text := range []string{got.Title, got.Body, got.EmailCopy.Subject, got.EmailCopy.Placeholders[EpisodeNoticePlaceholder].(string)} {
			if strings.ContainsAny(text, string([]rune{0x2013, 0x2014})) {
				t.Errorf("step %d copy %q", step, text)
			}
		}
	}
}

func TestAnEpisodeDraftCannotAlsoCarryADedupKey(t *testing.T) {
	d := episodeDraft()
	d.DedupKey = "meta_ads_funds:acc-1"
	if err := d.Normalize().Validate(); !errors.Is(err, ErrEpisodeDedupConflict) {
		t.Fatalf("err = %v", err)
	}
	d = episodeDraft()
	d.EpisodeKey = strings.Repeat("k", DedupKeyMaxRunes+1)
	if err := d.Normalize().Validate(); !errors.Is(err, ErrDedupKeyTooLong) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveKeysDropsBlanksAndDuplicates(t *testing.T) {
	got := EpisodeKeys([]string{" a ", "", "a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("keys = %v", got)
	}
}

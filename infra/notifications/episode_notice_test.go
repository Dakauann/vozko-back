package notification_service

import (
	"strings"
	"testing"
)

const episodeNoticeText = "Isto continua acontecendo desde 18/10/2026 às 09:00."

func withEpisodeStep(data map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{"EpisodeNotice": episodeNoticeText, "EpisodeTitle": "Lembrete", "EpisodeTone": "info"}
	for k, v := range data {
		out[k] = v
	}
	return out
}

func TestEveryEmailShowsTheReminderBlockOfAnAlertEpisodeAboveItsContent(t *testing.T) {
	loader := loaderForTests()
	for _, c := range everyTemplateWithItsSenderData() {
		out, err := loader.LoadTemplate(c.name, withEpisodeStep(c.data))
		if err != nil {
			t.Fatalf("%s: render error: %v", c.name, err)
		}
		block := strings.Index(out, "data-episode-notice")
		if block < 0 || !strings.Contains(out[block:], episodeNoticeText) || !strings.Contains(out[block:], "Lembrete") {
			t.Fatalf("%s: the reminder block is missing", c.name)
		}
		if body := strings.Index(out, `class="vx-pad" style="padding:32px`); body < 0 || block < body {
			t.Fatalf("%s: the reminder block must open the email body", c.name)
		}
	}
}

func TestAFirstNoticeEmailHasNoReminderBlock(t *testing.T) {
	loader := loaderForTests()
	for _, c := range everyTemplateWithItsSenderData() {
		out, err := loader.LoadTemplate(c.name, c.data)
		if err != nil {
			t.Fatalf("%s: render error: %v", c.name, err)
		}
		if strings.Contains(out, "data-episode-notice") {
			t.Fatalf("%s: a plain email shows the reminder block", c.name)
		}
	}
}

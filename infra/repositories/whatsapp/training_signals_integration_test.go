package whatsapp_repository

import (
	"context"
	"testing"
	"time"

	"vozko/domain/whatsapp/optout"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
	"vozko/infra/database/schema"
)

func TestTemplateVersionIntegration_ANewVersionOnlyWhenTheTextChanges(t *testing.T) {
	db := qualityIntegrationDB(t)
	if err := db.AutoMigrate(&schema.WhatsAppTemplate{}, &schema.WhatsAppTemplateVersion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewTemplateRepository(db)
	body := func(text string) []template.TemplateComponent {
		return []template.TemplateComponent{{Type: "BODY", Text: text}}
	}
	tmpl := &template.Template{ExternalID: "1234", WABAId: "waba-1", Name: "regua_nr", Language: "pt_BR",
		Category: template.TemplateCategoryUtility, Status: template.TemplateStatusPending, Components: body("Olá {{1}}")}

	if err := repo.Create(tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}
	tmpl.Status = template.TemplateStatusApproved
	if err := repo.Update(tmpl.ID, tmpl); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := repo.SyncFromExternal(tmpl); err != nil {
		t.Fatalf("sync: %v", err)
	}
	tmpl.Components = body("Olá {{1}}, sua fatura vence amanhã")
	if err := repo.Update(tmpl.ID, tmpl); err != nil {
		t.Fatalf("edit: %v", err)
	}

	var versions []schema.WhatsAppTemplateVersion
	db.Order("id").Find(&versions)
	if len(versions) != 2 {
		t.Fatalf("an approval or a sync without a text change is not a new version, got %d", len(versions))
	}
	if versions[0].Status != string(template.TemplateStatusPending) || versions[1].Components[0].Text != "Olá {{1}}, sua fatura vence amanhã" {
		t.Fatalf("versions %+v", versions)
	}
}

func TestTemplateVersionIntegration_ARefreshedMediaLinkIsNotANewVersion(t *testing.T) {
	db := qualityIntegrationDB(t)
	if err := db.AutoMigrate(&schema.WhatsAppTemplate{}, &schema.WhatsAppTemplateVersion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewTemplateRepository(db)
	header := func(link string) []template.TemplateComponent {
		return []template.TemplateComponent{
			{Type: "HEADER", Format: "IMAGE", Example: &template.TemplateExample{HeaderHandle: []string{link}}},
			{Type: "BODY", Text: "Olá {{1}}"},
		}
	}
	tmpl := &template.Template{ExternalID: "5678", WABAId: "waba-1", Name: "mensalidade", Language: "pt_BR",
		Category: template.TemplateCategoryUtility, Status: template.TemplateStatusApproved,
		Components: header("https://scontent.whatsapp.net/v/t61/123_n.jpg?_nc_gid=first&oh=a")}

	if err := repo.Create(tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}
	tmpl.Components = header("https://scontent.whatsapp.net/v/t61/123_n.jpg?_nc_gid=second&oh=b")
	if err := repo.SyncFromExternal(tmpl); err != nil {
		t.Fatalf("sync: %v", err)
	}
	tmpl.Components = header("https://scontent.whatsapp.net/v/t61/456_n.jpg?_nc_gid=third&oh=c")
	if err := repo.SyncFromExternal(tmpl); err != nil {
		t.Fatalf("new image: %v", err)
	}

	var versions int64
	db.Model(&schema.WhatsAppTemplateVersion{}).Where("external_id = ?", "5678").Count(&versions)
	if versions != 2 {
		t.Fatalf("only the new image is a new version, got %d", versions)
	}
}

func TestQualityHistoryIntegration_LatestValueWinsPerPhone(t *testing.T) {
	db := qualityIntegrationDB(t)
	repo := NewQualityChangeRepository(db)
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	phone := func(id, current string, offset time.Duration) quality.Change {
		return quality.Change{Subject: quality.SubjectPhone, ExternalID: id, Signal: quality.SignalQuality, Current: current, Source: quality.SourcePoll, OccurredAt: at.Add(offset)}
	}
	if err := repo.Record(phone("m1", "GREEN", 0), phone("m1", "RED", time.Hour), phone("m2", "YELLOW", 0)); err != nil {
		t.Fatal(err)
	}

	latest, err := repo.LatestValues(quality.SubjectPhone, quality.SignalQuality, []string{"m1", "m2", "m3"})

	if err != nil || latest["m1"] != "RED" || latest["m2"] != "YELLOW" || latest["m3"] != "" {
		t.Fatalf("latest %v, %v", latest, err)
	}
}

func TestSendingPhonesIntegration_OnlyPhonesThatSentSinceTheWindow(t *testing.T) {
	db := messageStatusDB(t)
	repo := NewMessageStatusRepository(db)
	since := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	for _, row := range []schema.WhatsAppTemplateHourlyStat{
		{TemplateExternalID: "t", PhoneNumberID: "recent", Hour: since.Add(24 * time.Hour), Status: "sent", Messages: 5},
		{TemplateExternalID: "t", PhoneNumberID: "old", Hour: since.Add(-time.Hour), Status: "sent", Messages: 5},
		{TemplateExternalID: "t", PhoneNumberID: "only-read", Hour: since.Add(time.Hour), Status: "read", Messages: 5},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	phones, err := repo.PhonesSendingSince(context.Background(), since)

	if err != nil || len(phones) != 1 || phones[0] != "recent" {
		t.Fatalf("phones %v, %v", phones, err)
	}
}

func TestOptOutIntegration_ARedeliveredWebhookIsStoredOnce(t *testing.T) {
	db := qualityIntegrationDB(t)
	if err := db.AutoMigrate(&schema.WhatsAppOptOutEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewOptOutRepository(db)
	event := optout.Event{PhoneNumberID: "1179270238614202", RecipientHash: optout.HashRecipient("5511999990001"), Category: "marketing_messages",
		Preference: optout.PreferenceStop, OccurredAt: time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)}

	for i := 0; i < 2; i++ {
		if err := repo.Record(event); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	var stored []schema.WhatsAppOptOutEvent
	db.Find(&stored)
	if len(stored) != 1 || stored[0].RecipientHash == "5511999990001" {
		t.Fatalf("stored %+v", stored)
	}
}

func TestTemplateListIntegration_ReadsTheQualityStoredOnTheTemplate(t *testing.T) {
	db := qualityIntegrationDB(t)
	if err := db.AutoMigrate(&schema.WhatsAppTemplate{}, &schema.WhatsAppTemplateVersion{}, &schema.WhatsAppBusinessAccount{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewTemplateRepository(db)
	created := map[string]*template.Template{}
	for _, ext := range []string{"rated", "unrated"} {
		tmpl := &template.Template{ExternalID: ext, WABAId: "waba-1", Name: ext, Language: "pt_BR",
			Category: template.TemplateCategoryUtility, Status: template.TemplateStatusApproved, Components: []template.TemplateComponent{{Type: "BODY", Text: "Oi"}}}
		if err := repo.Create(tmpl); err != nil {
			t.Fatalf("create %s: %v", ext, err)
		}
		created[ext] = tmpl
	}
	rated := created["rated"]
	rated.RateQuality("green")
	if err := repo.Update(rated.ID, rated); err != nil {
		t.Fatalf("rate: %v", err)
	}
	rated.RateQuality("RED")
	if err := repo.Update(rated.ID, rated); err != nil {
		t.Fatalf("re-rate: %v", err)
	}

	page, err := repo.List(template.ListInput{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]string{}
	for _, item := range page.Items {
		got[item.ExternalID] = item.Quality
	}
	if len(got) != 2 || got["rated"] != "RED" || got["unrated"] != "" {
		t.Fatalf("qualities %v", got)
	}
	var versions int64
	db.Model(&schema.WhatsAppTemplateVersion{}).Where("external_id = ?", "rated").Count(&versions)
	if versions != 1 {
		t.Fatalf("a new rating is not a new text version, got %d", versions)
	}
}

package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

const businessPhoneUUID = "5c4b3a29-1807-4f6e-9d5c-4b3a29180765"

type stubConversions struct {
	pixels    *stubPixels
	settings  advertising.ConversionSettings
	saved     *advertising.ConversionSettings
	connected string
}

func (c *stubConversions) Settings(context.Context, string) (*advertising.ConversionSettings, error) {
	s := c.settings
	return &s, nil
}
func (c *stubConversions) CheckSave(_ context.Context, _ string, s advertising.ConversionSettings) (*advertising.Pixel, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s.PixelID == "" {
		return nil, nil
	}
	for i := range c.pixels.pixels {
		if c.pixels.pixels[i].MetaID == s.PixelID && !c.pixels.pixels[i].Unavailable {
			return &c.pixels.pixels[i], nil
		}
	}
	return nil, advertising.FieldError("pixelId", "not_available")
}
func (c *stubConversions) CheckConnectDataset(_ context.Context, _, phoneID string) (*advertising.AdAccount, error) {
	if c.settings.AdAccountID == "" {
		return nil, advertising.FieldError("adAccountId", "required")
	}
	if phoneID != businessPhoneUUID {
		return nil, advertising.ErrBusinessPhoneNotFound
	}
	return &advertising.AdAccount{ID: c.settings.AdAccountID, Name: "Loja"}, nil
}
func (c *stubConversions) Save(_ context.Context, _ string, s advertising.ConversionSettings) (*advertising.ConversionSettings, error) {
	c.saved = &s
	return &s, nil
}
func (c *stubConversions) ConnectDataset(_ context.Context, _, phoneID string) (*advertising.ConversionSettings, error) {
	c.connected = phoneID
	s := c.settings
	s.DatasetID = "ds-1"
	return &s, nil
}
func (c *stubConversions) Recent(context.Context, string) ([]advertising.ConversionRecord, error) {
	return []advertising.ConversionRecord{
		{OpportunityID: "op-1", EventName: advertising.EventNameLead, Status: advertising.ConversionSent, UpdatedAt: adTestClock},
		{OpportunityID: "op-2", EventName: advertising.EventNamePurchase, Status: advertising.ConversionSkipped, Reason: "value_missing", UpdatedAt: adTestClock},
		{OpportunityID: "op-3", EventName: advertising.EventNameLead, Status: advertising.ConversionSent, UpdatedAt: adTestClock},
	}, nil
}

type stubPixels struct {
	pixels  []advertising.Pixel
	created string
}

func (p *stubPixels) Pixels(context.Context, string, string) ([]advertising.Pixel, error) {
	return p.pixels, nil
}
func (p *stubPixels) CreatePixel(_ context.Context, _, _, name string) (*advertising.Pixel, error) {
	p.created = name
	return &advertising.Pixel{MetaID: "556", Name: name}, nil
}

func TestSavingConversionsKeepsWhatWasNotAskedAndChecksThePixel(t *testing.T) {
	tools, s := growthTools()
	s.conversions.settings.DatasetID = "ds-1"
	tool := tools["save_ad_conversion_settings"]
	args := map[string]interface{}{"ad_account_id": adAccountUUID, "enabled": true, "send_leads": false, "pixel_id": "404"}
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("unknown pixel err %v", err)
	}
	args["pixel_id"] = "555"
	if fields := growthDescribe(tool, args); fields["sending"] != "ligado" || fields["pixel"] != "Pixel do site" {
		t.Fatalf("fields %+v", fields)
	}
	result := tool.Execute(context.Background(), adContext, args)
	saved := s.conversions.saved
	if result.Status != copilot.StatusOK || !saved.Enabled || saved.SendLeads || !saved.SendPurchases || saved.DatasetID != "ds-1" || saved.AdAccountID != adAccountUUID {
		t.Fatalf("result %+v saved %+v", result, saved)
	}
}

func TestTurningConversionsOnNeedsADatasetOrAPixel(t *testing.T) {
	tools, _ := growthTools()
	if err := growthValidate(tools["save_ad_conversion_settings"], map[string]interface{}{"ad_account_id": adAccountUUID, "enabled": true}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}

func TestConnectingTheDatasetNeedsTheAccountSavedFirst(t *testing.T) {
	tools, s := growthTools()
	tool := tools["connect_ad_dataset"]
	args := map[string]interface{}{"business_phone_id": businessPhoneUUID}
	if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("no account err %v", err)
	}
	s.conversions.settings.AdAccountID = adAccountUUID
	if err := growthValidate(tool, map[string]interface{}{"business_phone_id": "5511999"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("invented phone err %v", err)
	}
	if err := growthValidate(tool, map[string]interface{}{"business_phone_id": "7d6c5b4a-3928-4f17-8e6d-5c4b3a291807"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("a well formed number of no line must be refused before approval: %v", err)
	}
	if s.conversions.connected != "" {
		t.Fatal("nothing may be connected before approval")
	}
	result := tool.Execute(context.Background(), adContext, args)
	if result.Status != copilot.StatusOK || s.conversions.connected != businessPhoneUUID || result.Data.(map[string]interface{})["whatsapp_dataset_connected"] != true {
		t.Fatalf("result %+v", result)
	}
}

func TestRecentConversionsAreCountedByStatus(t *testing.T) {
	tools, _ := growthTools()
	result := tools["recent_ad_conversions"].Execute(context.Background(), adContext, nil)
	totals := result.Data.(map[string]interface{})["totals"].(map[string]int)
	if result.Status != copilot.StatusOK || totals["sent"] != 2 || totals["skipped"] != 1 {
		t.Fatalf("result %+v", result)
	}
}

func TestPixelsAreListedAndCreatedOnlyOnAKnownAccount(t *testing.T) {
	tools, s := growthTools()
	listed := tools["list_ad_pixels"].Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID})
	if pixels := listed.Data.(map[string]interface{})["pixels"].([]map[string]interface{}); pixels[0]["pixel_id"] != "555" || pixels[0]["available"] != true {
		t.Fatalf("listed %+v", listed)
	}
	tool := tools["create_ad_pixel"]
	if err := growthValidate(tool, map[string]interface{}{"ad_account_id": "act_1", "name": "Site"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	result := tool.Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID, "name": " Site "})
	if result.Status != copilot.StatusOK || s.pixels.created != "Site" {
		t.Fatalf("result %+v created %q", result, s.pixels.created)
	}
}

package whatsapp_campaign

import (
	"errors"
	"testing"
)

func TestValidateWorkflowVars_NoRequiredKeys(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{Number: "5511999999999"},
		},
	}
	if err := c.ValidateWorkflowVars(nil); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if err := c.ValidateWorkflowVars([]string{}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateWorkflowVars_AllPresent(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{
				Number:   "5511999999999",
				Metadata: map[string]interface{}{"city": "São Paulo", "name": "John"},
			},
			{
				Number:   "5511888888888",
				Metadata: map[string]interface{}{"city": "Rio", "name": "Jane"},
			},
		},
	}
	if err := c.ValidateWorkflowVars([]string{"city", "name"}); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateWorkflowVars_MissingKey(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{
				Number:   "5511999999999",
				Metadata: map[string]interface{}{"city": "São Paulo"},
			},
		},
	}
	err := c.ValidateWorkflowVars([]string{"city", "name"})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !errors.Is(err, ErrCampaignWorkflowVarsMissing) {
		t.Fatalf("expected ErrCampaignWorkflowVarsMissing, got %v", err)
	}
}

func TestValidateWorkflowVars_EmptyStringValue(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{
				Number:   "5511999999999",
				Metadata: map[string]interface{}{"city": "  ", "name": "John"},
			},
		},
	}
	err := c.ValidateWorkflowVars([]string{"city"})
	if err == nil {
		t.Fatal("expected error for empty string value")
	}
	if !errors.Is(err, ErrCampaignWorkflowVarsMissing) {
		t.Fatalf("expected ErrCampaignWorkflowVarsMissing, got %v", err)
	}
}

func TestValidateWorkflowVars_NilMetadata(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{Number: "5511999999999"},
		},
	}
	err := c.ValidateWorkflowVars([]string{"city"})
	if err == nil {
		t.Fatal("expected error for nil metadata")
	}
	if !errors.Is(err, ErrCampaignWorkflowVarsMissing) {
		t.Fatalf("expected ErrCampaignWorkflowVarsMissing, got %v", err)
	}
}

func TestValidateWorkflowVars_NonStringValue(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{
				Number:   "5511999999999",
				Metadata: map[string]interface{}{"age": 25},
			},
		},
	}

	if err := c.ValidateWorkflowVars([]string{"age"}); err != nil {
		t.Fatalf("expected no error for non-string metadata value, got %v", err)
	}
}

func TestValidateWorkflowVars_NoPhoneInputs(t *testing.T) {
	c := &Campaign{}

	if err := c.ValidateWorkflowVars([]string{"city"}); err != nil {
		t.Fatalf("expected no error with no phone inputs, got %v", err)
	}
}

func TestValidateWorkflowVars_SecondEntryMissing(t *testing.T) {
	c := &Campaign{
		PhoneInputs: []PhoneInput{
			{
				Number:   "5511999999999",
				Metadata: map[string]interface{}{"city": "SP"},
			},
			{
				Number:   "5511888888888",
				Metadata: map[string]interface{}{},
			},
		},
	}
	err := c.ValidateWorkflowVars([]string{"city"})
	if err == nil {
		t.Fatal("expected error for second entry missing key")
	}
	if !errors.Is(err, ErrCampaignWorkflowVarsMissing) {
		t.Fatalf("expected ErrCampaignWorkflowVarsMissing, got %v", err)
	}
}

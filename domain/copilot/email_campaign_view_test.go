package copilot

import (
	"errors"
	"testing"
)

const openCampaignID = "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f"

func TestTheEmailCampaignViewNamesTheOpenCampaign(t *testing.T) {
	view := View{Surface: SurfaceEmailCampaign, ProjectID: openCampaignID}
	if err := view.Validate(); err != nil {
		t.Fatal(err)
	}
	if !view.OnEmailCampaign() || view.OnEmailDesign() || view.OnStudio() || !view.OnEmailDocument() || !view.OnEditor() || !view.Focused() {
		t.Fatalf("got %+v", view)
	}
	for name, bad := range map[string]View{
		"no campaign":    {Surface: SurfaceEmailCampaign},
		"invented id":    {Surface: SurfaceEmailCampaign, ProjectID: "c1"},
		"a studio kind":  {Surface: SurfaceEmailCampaign, ProjectID: openCampaignID, ProjectKind: StudioImage},
		"a campaign tab": {Surface: SurfaceEmailCampaign, ProjectID: openCampaignID, CampaignID: "x"},
		"selected leads": {Surface: SurfaceEmailCampaign, ProjectID: openCampaignID, SelectedLeads: 2},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalidView) {
			t.Errorf("%s: want ErrInvalidView, got %v", name, err)
		}
	}
}

func TestEmailDocumentsAreTheDesignAndTheCampaign(t *testing.T) {
	if !(View{Surface: SurfaceEmailDesign, ProjectID: openDesignID}).OnEmailDocument() {
		t.Fatal("a design holds an email document")
	}
	for _, view := range []View{{}, {Surface: SurfaceAttendance}, {Surface: SurfaceLeads}, {Surface: SurfaceStudio, ProjectID: openDesignID, ProjectKind: StudioImage}} {
		if view.OnEmailDocument() {
			t.Fatalf("%q holds no email document", view.Surface)
		}
	}
}

func TestEmailSkillsServeTheCampaignContentToo(t *testing.T) {
	campaign := View{Surface: SurfaceEmailCampaign, ProjectID: openCampaignID}
	if !(Skill{Surface: SkillEmailWork}).OfferedOn(campaign) || (Skill{Surface: SkillPanelOnly}).OfferedOn(campaign) || (Skill{Surface: SkillStudioOnly}).OfferedOn(campaign) {
		t.Fatal("the campaign content offers the email craft and nothing meant only for the panel or the studio")
	}
}

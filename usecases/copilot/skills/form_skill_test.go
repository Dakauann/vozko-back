package skills

import (
	"fmt"
	"strings"
	"testing"

	wf "vozko/domain/whatsappform"
)

func TestTheFormSkillTeachesTheMetaLabelLimits(t *testing.T) {
	lib, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	skill, ok := lib.Find("formularios-whatsapp")
	if !ok {
		t.Fatal("formularios-whatsapp must ship")
	}
	for _, limit := range []int{wf.MaxTextInputLabel, wf.MaxChoiceGroupLabel, wf.MaxDropdownLabel, wf.MaxDateLabel, wf.MaxOptInLabel, wf.MaxOptionTitle, wf.MaxHelpLength} {
		if !strings.Contains(skill.Body, fmt.Sprint(limit)) {
			t.Errorf("the form skill must state the limit %d", limit)
		}
	}
	for _, starter := range wf.Starters() {
		if !strings.Contains(skill.Body, starter.Key) {
			t.Errorf("the form skill must name the starter %s", starter.Key)
		}
	}
	for _, step := range []string{"question", "consent_whatsapp", "sensitive", "create_whatsapp_form", "publish_whatsapp_form", "send_whatsapp_form", "send_template"} {
		if !strings.Contains(skill.Body, step) {
			t.Errorf("the form skill must cover %q", step)
		}
	}
}

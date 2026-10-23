package template

import (
	"reflect"
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/conversation"
	"vozko/domain/whatsappform"
)

const testLinkBase = "https://vz.co/r"

func buttonsTemplate() *Template {
	return &Template{
		ID: "tpl-1", Name: "oferta", Language: "pt_BR", Category: TemplateCategoryMarketing,
		Components: []TemplateComponent{
			{Type: "BODY", Text: "Oferta da semana"},
			{Type: "BUTTONS", Buttons: []TemplateButton{
				{Type: "QUICK_REPLY", Text: "Quero"},
				{Type: "QUICK_REPLY", Text: "Parar"},
				{Type: "URL", Text: "Ver loja", URL: choicestats.TrackedButtonURL(testLinkBase, "Ab3x")},
				{Type: "URL", Text: "Site", URL: "https://loja.com/promo"},
			}},
		},
	}
}

func TestATemplateOffersItsQuickRepliesAndUrlButtonsMarkingWhichAreTracked(t *testing.T) {
	yes, no := true, false
	got := buttonsTemplate().ChoiceOptions(testLinkBase)
	want := []choicestats.OfferedOption{
		{ID: "template_button:0", Tracked: &yes},
		{ID: "template_button:1", Tracked: &yes},
		{ID: "template_url:2", Tracked: &yes},
		{ID: "template_url:3", Tracked: &no},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %+v", got)
	}
	if shown := buttonsTemplate().ShownChoiceIDs(testLinkBase); !reflect.DeepEqual(shown, []string{"template_button:0", "template_button:1", "template_url:2"}) {
		t.Fatalf("shown = %v, an untracked url can never be clicked so it is not counted as shown", shown)
	}
	if shown := buttonsTemplate().ShownChoiceIDs(""); !reflect.DeepEqual(shown, []string{"template_button:0", "template_button:1"}) {
		t.Fatalf("without a link base nothing is tracked, shown = %v", shown)
	}
}

func urlParamsOf(t *testing.T, tracking ClickTracking) []conversation.TemplateButtonParam {
	t.Helper()
	out, err := buttonsTemplate().BuildSendInput(SendInputParams{To: "5511987654321", ClickTracking: tracking})
	if err != nil {
		t.Fatal(err)
	}
	var urls []conversation.TemplateButtonParam
	for _, b := range out.Buttons {
		if b.SubType == conversation.TemplateButtonSubTypeURL {
			urls = append(urls, b)
		}
	}
	return urls
}

func TestATrackedUrlButtonCarriesTheClickTokenAsItsSuffix(t *testing.T) {
	tokens, err := choicestats.NewClickTokens("server-secret")
	if err != nil {
		t.Fatal(err)
	}
	urls := urlParamsOf(t, ClickTracking{LinkBase: testLinkBase, PromptKey: "tpl:attempt-1", Tokens: tokens})
	if len(urls) != 1 || urls[0].Index != 2 {
		t.Fatalf("url params = %+v, want only the tracked button", urls)
	}
	if index, key, ok := tokens.Verify(urls[0].Text); !ok || index != 2 || key != "tpl:attempt-1" {
		t.Fatalf("suffix %q verifies as %d %q %v", urls[0].Text, index, key, ok)
	}
}

func TestATrackedUrlButtonThatCannotBeAttributedStillOpensButCreditsNothing(t *testing.T) {
	tokens, _ := choicestats.NewClickTokens("server-secret")
	for name, tracking := range map[string]ClickTracking{
		"no prompt":  {LinkBase: testLinkBase, Tokens: tokens},
		"no signing": {LinkBase: testLinkBase, PromptKey: "tpl:attempt-1"},
	} {
		urls := urlParamsOf(t, tracking)
		if len(urls) != 1 || urls[0].Text != choicestats.UnattributedClickSuffix {
			t.Fatalf("%s: url params = %+v, Meta needs a suffix and the link must still open", name, urls)
		}
		if _, _, ok := tokens.Verify(urls[0].Text); ok {
			t.Fatalf("%s: the suffix credits a click", name)
		}
	}
}

func TestATemplateSendCountsUnderTheSourceItCameFrom(t *testing.T) {
	cases := []struct {
		name string
		in   BilledSendInput
		want choicestats.Source
	}{
		{"an operator in the inbox", BilledSendInput{Origin: whatsappform.OriginInbox}, choicestats.Source{Kind: choicestats.SourceInbox, TemplateID: "tpl-1"}},
		{"a conversation start", BilledSendInput{Origin: whatsappform.OriginTemplateStart}, choicestats.Source{Kind: choicestats.SourceOutreach, TemplateID: "tpl-1"}},
		{"a scheduled template", BilledSendInput{Origin: whatsappform.OriginTemplateScheduled}, choicestats.Source{Kind: choicestats.SourceOutreach, TemplateID: "tpl-1"}},
		{"a workflow names its node", BilledSendInput{Origin: whatsappform.OriginWorkflow, ChoiceSource: &choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf", NodeID: "n", Version: 2}},
			choicestats.Source{Kind: choicestats.SourceWorkflow, ID: "wf", NodeID: "n", Version: 2, TemplateID: "tpl-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.ChoiceSourceFor("tpl-1"); got != tc.want {
				t.Fatalf("source = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTheTemplateReadListsItsTrackedUrlButtonsEvenWhenNone(t *testing.T) {
	if got := buttonsTemplate().TrackedURLButtons(testLinkBase); !reflect.DeepEqual(got, []int{2}) {
		t.Fatalf("tracked = %v", got)
	}
	plain := &Template{Components: []TemplateComponent{{Type: "BODY", Text: "oi"}}}
	if got := plain.TrackedURLButtons(testLinkBase); got == nil || len(got) != 0 {
		t.Fatalf("a template without tracked buttons answers an empty list, got %#v", got)
	}
}

package instagram

import (
	"strings"
	"testing"

	"vozko/domain/conversation"
	igdomain "vozko/domain/instagram"
)

func opts(pairs ...string) []conversation.InteractiveOption {
	out := make([]conversation.InteractiveOption, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, conversation.InteractiveOption{ID: pairs[i], Title: pairs[i+1]})
	}
	return out
}

func TestQuickRepliesCarryTheIDAsThePayload(t *testing.T) {
	out, dropped := quickReplyOptionsFor(opts("sim", "Sim", "nao", "Não"))

	if len(out) != 2 || len(dropped) != 0 {
		t.Fatalf("out=%d dropped=%d, want 2 and 0", len(out), len(dropped))
	}
	// Branching on the title would break the moment an author reworded a label.
	if out[0].Payload != "sim" || out[0].Title != "Sim" {
		t.Errorf("option = %+v, want the id as payload", out[0])
	}
}

// "A maximum of 13 quick replies are supported."
func TestQuickRepliesStopAtInstagramsCap(t *testing.T) {
	many := make([]conversation.InteractiveOption, 0, igdomain.MaxQuickReplies+4)
	for i := 0; i < igdomain.MaxQuickReplies+4; i++ {
		many = append(many, conversation.InteractiveOption{
			ID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Title: "x",
		})
	}
	out, dropped := quickReplyOptionsFor(many)

	if len(out) != igdomain.MaxQuickReplies {
		t.Errorf("out = %d, want %d", len(out), igdomain.MaxQuickReplies)
	}
	if len(dropped) != 4 {
		t.Errorf("dropped = %d, want the 4 beyond the cap", len(dropped))
	}
	if len(dropped) > 0 && !strings.Contains(dropped[0].reason, "13") {
		t.Errorf("reason %q should name the limit", dropped[0].reason)
	}
}

func TestQuickRepliesSkipAnOptionWithNoPayload(t *testing.T) {
	out, dropped := quickReplyOptionsFor(opts("", "Sem id"))
	if len(out) != 0 || len(dropped) != 1 {
		t.Errorf("out=%d dropped=%d, want 0 and 1", len(out), len(dropped))
	}
}

func TestQuickRepliesFallBackToTheIDAsLabel(t *testing.T) {
	out, _ := quickReplyOptionsFor(opts("sim", ""))
	if len(out) != 1 || out[0].Title != "sim" {
		t.Errorf("out = %+v, want the id used as the visible label", out)
	}
}

// Instagram has no header or footer slot; the author's words must survive.
func TestComposeInteractiveBodyKeepsHeaderAndFooter(t *testing.T) {
	body := composeInteractiveBody(conversation.SendInteractiveRequest{
		Header: "Atendimento",
		Body:   "Escolha",
		Footer: "Rodapé",
	})
	for _, want := range []string{"Atendimento", "Escolha", "Rodapé"} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q is missing %q", body, want)
		}
	}
}

func TestInteractiveLimitsComeFromTheDescriptor(t *testing.T) {
	a := &channelAdapter{caps: igdomain.Descriptor().Capabilities}

	limits := a.InteractiveLimits()
	if limits.MaxOptionsButtons != igdomain.MaxQuickReplies {
		t.Errorf("MaxOptionsButtons = %d, want %d", limits.MaxOptionsButtons, igdomain.MaxQuickReplies)
	}
	// Instagram has ONE mechanism, so both prompt styles resolve to the same cap
	// rather than the list style silently reporting zero.
	if limits.MaxOptionsList != igdomain.MaxQuickReplies {
		t.Errorf("MaxOptionsList = %d, want the same single mechanism", limits.MaxOptionsList)
	}
	if limits.SupportsOptionDescriptions {
		t.Error("quick replies have no description slot")
	}
}

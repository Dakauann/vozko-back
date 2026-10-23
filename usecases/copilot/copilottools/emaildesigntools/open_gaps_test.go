package emaildesigntools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
)

func withGap(r *rig) {
	r.editor.live.Blocks[2] = emaildesign.Block{ID: "cta", Kind: emaildesign.KindButton, Button: &emaildesign.Button{Label: "Agendar consulta"}}
}

func endsWithGaps(t *testing.T, res copilot.Result) []emaildesign.Gap {
	t.Helper()
	raw, err := json.Marshal(res.Data)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	at := strings.LastIndex(text, `"open_gaps":`)
	if at < 0 || !strings.HasSuffix(text, "]}") || strings.Contains(text[at:], `":{`) {
		t.Fatalf("the result must end with the open gaps, got %s", text)
	}
	var data struct {
		OpenGaps []emaildesign.Gap `json:"open_gaps"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data.OpenGaps
}

func TestEveryEditorResultEndsWithTheLiveOpenGaps(t *testing.T) {
	r := newRig()
	withGap(r)
	read := endsWithGaps(t, NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil))
	if len(read) != 1 || read[0].BlockID != "cta" || read[0].Code != "href_missing" || read[0].Text != "Agendar consulta" {
		t.Fatalf("got %+v", read)
	}
	look := endsWithGaps(t, NewEmailLookTool(r.deps).Execute(context.Background(), seeing(r), nil))
	if len(look) != 1 {
		t.Fatalf("got %+v", look)
	}
	edited := endsWithGaps(t, NewEmailEditTool(r.deps).Execute(context.Background(), r.cc, args(t, `{"operations":[{"op":"set","id":"cta","fields":{"href":"https://clinica.example.com/agenda"}}]}`)))
	if len(edited) != 0 {
		t.Fatalf("a filled gap is gone from the next result, got %+v", edited)
	}
}

func TestTheOpenGapsFollowHandEditsBetweenCalls(t *testing.T) {
	r := newRig()
	if gaps := endsWithGaps(t, NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil)); len(gaps) != 0 {
		t.Fatalf("got %+v", gaps)
	}
	withGap(r)
	if gaps := endsWithGaps(t, NewEmailReadTool(r.deps).Execute(context.Background(), r.cc, nil)); len(gaps) != 1 {
		t.Fatalf("the person's edit shows up at once, got %+v", gaps)
	}
}

func watch(r *rig) copilot.SurfaceWatch {
	return NewEmailReadTool(r.deps).(copilot.SurfaceWatch)
}

func TestTheViewSnapshotListsTheOpenGaps(t *testing.T) {
	r := newRig()
	withGap(r)
	snapshot := watch(r).ViewSnapshot(context.Background(), r.cc)
	if !strings.Contains(snapshot, "cta") || !strings.Contains(snapshot, "href_missing") || !strings.Contains(snapshot, "Agendar consulta") {
		t.Fatalf("got %q", snapshot)
	}
	clean := newRig()
	if got := watch(clean).ViewSnapshot(context.Background(), clean.cc); got == "" || strings.Contains(got, "href_missing") {
		t.Fatalf("a design without gaps says so, got %q", got)
	}
	blind := r.cc
	blind.Screen = nil
	if got := watch(r).ViewSnapshot(context.Background(), blind); got != "" {
		t.Fatalf("without the editor there is nothing to show, got %q", got)
	}
}

func TestTheClosingNoteAsksOnlyAboutGapsTheReplyLeftOut(t *testing.T) {
	r := newRig()
	if note := watch(r).ClosingNote(context.Background(), r.cc, "Pronto!"); note != "" {
		t.Fatalf("no gaps, no note, got %q", note)
	}
	withGap(r)
	note := watch(r).ClosingNote(context.Background(), r.cc, "Pronto, o e-mail está montado.")
	if !strings.Contains(note, "cta") || !strings.Contains(note, "href_missing") || !strings.Contains(strings.ToLower(note), "nunca invente") {
		t.Fatalf("got %q", note)
	}
	for _, reply := range []string{"Falta o link do botão de agendamento.", "Me passe a URL da agenda.", "O botão Agendar consulta ainda está sem destino."} {
		if note := watch(r).ClosingNote(context.Background(), r.cc, reply); note != "" {
			t.Fatalf("%q already tells the person, got %q", reply, note)
		}
	}
}

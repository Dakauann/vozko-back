package choicestats

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestAClickNamesItsPromptByKeyOrByTheProviderMessage(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		click Click
		want  error
	}{
		{"a workflow tap names the prompt key", Click{WorkspaceID: "ws", PromptKey: "wf:r:n#1", Option: "sim", At: at}, nil},
		{"a template tap names the replied message", Click{WorkspaceID: "ws", ProviderMessageID: "wamid.1", Option: "template_button:0", At: at}, nil},
		{"a click naming both is ambiguous", Click{WorkspaceID: "ws", PromptKey: "k", ProviderMessageID: "m", Option: "a", At: at}, ErrInvalidClick},
		{"a click naming neither is refused", Click{WorkspaceID: "ws", Option: "a", At: at}, ErrInvalidClick},
		{"a click without a workspace is refused", Click{PromptKey: "k", Option: "a", At: at}, ErrInvalidClick},
		{"a click without an option is refused", Click{WorkspaceID: "ws", PromptKey: "k", At: at}, ErrInvalidClick},
		{"a click without a time is refused", Click{WorkspaceID: "ws", PromptKey: "k", Option: "a"}, ErrInvalidClick},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.click.Record()
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	rec, err := Click{WorkspaceID: "ws", PromptKey: "k", Option: "sim", At: at}.Record()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec.CounterOptions, []string{"sim", OptionTotal}) || !rec.NotBefore.Equal(AttributionCutoff(at)) {
		t.Fatalf("record = %+v", rec)
	}
}

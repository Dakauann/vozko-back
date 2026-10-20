package conversation

import (
	"errors"
	"testing"

	"vozko/domain/shared"
)

type scriptedVisibility struct {
	visible bool
	err     error
}

func (s scriptedVisibility) CanView(shared.Person, string, string, string) (bool, error) {
	return s.visible, s.err
}

func (s scriptedVisibility) Scope(shared.Person, string) (VisibilityScope, error) {
	return VisibilityScope{}, s.err
}

func TestRequireVisibleFailsClosedOnEveryDoubt(t *testing.T) {
	down := errors.New("db down")
	cases := []struct {
		name       string
		visibility Visibility
		workspace  string
		wantErr    bool
		wantCause  error
	}{
		{"visible", scriptedVisibility{visible: true}, "ws", false, nil},
		{"hidden", scriptedVisibility{}, "ws", true, nil},
		{"unreadable keeps the cause", scriptedVisibility{err: down}, "ws", true, down},
		{"no rule wired", nil, "ws", true, nil},
		{"no workspace", scriptedVisibility{visible: true}, " ", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RequireVisible(tc.visibility, shared.Person{UserID: "u"}, tc.workspace, "entry", "whatsapp")
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if tc.wantErr && !errors.Is(err, ErrNotVisible) {
				t.Fatalf("err = %v, want ErrNotVisible", err)
			}
			if tc.wantCause != nil && !errors.Is(err, tc.wantCause) {
				t.Fatalf("err = %v, want the cause kept", err)
			}
		})
	}
}

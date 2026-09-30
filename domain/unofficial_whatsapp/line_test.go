package unofficial_whatsapp

import (
	"testing"
	"time"
)

func lineInstance(id string, status Status, dept *string) *Instance {
	return &Instance{ID: id, WorkspaceID: "ws-1", PhoneNumber: "5511965467700", Status: status, DepartmentID: dept}
}

func TestInheritsLine(t *testing.T) {
	sales, support := "sales", "support"
	successor := lineInstance("new", StatusConnected, nil)

	cases := []struct {
		name        string
		successor   *Instance
		predecessor *Instance
		want        bool
	}{
		{"a dead link of the same number hands over", successor, lineInstance("old", StatusDisconnected, nil), true},
		{"a hibernated link hands over", successor, lineInstance("old", StatusHibernated, nil), true},
		{"a live link of the same number keeps its chats", successor, lineInstance("old", StatusConnected, nil), false},
		{"a removed link hands over whatever status it last recorded", successor, func() *Instance {
			i := lineInstance("old", StatusConnected, nil)
			removed := time.Now()
			i.DeletedAt = &removed
			return i
		}(), true},
		{"a banned number is not inherited", successor, lineInstance("old", StatusBanned, nil), false},
		{"itself is not a predecessor", successor, successor, false},
		{"another workspace never hands over", successor, func() *Instance {
			i := lineInstance("old", StatusDisconnected, nil)
			i.WorkspaceID = "ws-2"
			return i
		}(), false},
		{"another number never hands over", successor, func() *Instance {
			i := lineInstance("old", StatusDisconnected, nil)
			i.PhoneNumber = "5511000000000"
			return i
		}(), false},
		{"an unknown number never matches", lineInstance("new", StatusConnected, nil), func() *Instance {
			i := lineInstance("old", StatusDisconnected, nil)
			i.PhoneNumber = ""
			return i
		}(), false},
		{"a disconnected successor inherits nothing", lineInstance("new", StatusDisconnected, nil), lineInstance("old", StatusDisconnected, nil), false},
		{"the same department hands over", lineInstance("new", StatusConnected, &sales), lineInstance("old", StatusDisconnected, &sales), true},
		{"another department keeps its chats", lineInstance("new", StatusConnected, &sales), lineInstance("old", StatusDisconnected, &support), false},
		{"a department number does not absorb a workspace-wide one", lineInstance("new", StatusConnected, &sales), lineInstance("old", StatusDisconnected, nil), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := InheritsLine(tc.successor, tc.predecessor); got != tc.want {
				t.Errorf("InheritsLine = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBareJIDDropsTheLinkedDevice(t *testing.T) {
	cases := map[string]string{
		"5511965467700:37@s.whatsapp.net": "5511965467700@s.whatsapp.net",
		"5511965467700@s.whatsapp.net":    "5511965467700@s.whatsapp.net",
		" 5511965467700:4@s.whatsapp.net": "5511965467700@s.whatsapp.net",
		"122630107566238:12@lid":          "122630107566238@lid",
		"":                                "",
		"5511965467700":                   "5511965467700",
	}
	for in, want := range cases {
		if got := BareJID(in); got != want {
			t.Errorf("BareJID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameLine(t *testing.T) {
	sales, support := "sales", "support"
	base := lineInstance("a", StatusConnected, &sales)

	if !SameLine(base, lineInstance("b", StatusDisconnected, &sales)) {
		t.Error("same workspace, number and department is one line")
	}
	if SameLine(base, lineInstance("b", StatusConnected, &support)) {
		t.Error("another department is another line")
	}
	other := lineInstance("b", StatusConnected, &sales)
	other.WorkspaceID = "ws-2"
	if SameLine(base, other) {
		t.Error("another workspace is never the same line")
	}
	if SameLine(base, base) {
		t.Error("an instance is not its own sibling")
	}
}

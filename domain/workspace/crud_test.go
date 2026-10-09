package workspace

import "testing"

func TestCRUDAction_MapsEachOperationToItsAction(t *testing.T) {
	cases := []struct {
		op   Operation
		want Action
	}{
		{OperationRead, ActionRead},
		{OperationCreate, ActionCreate},
		{OperationUpdate, ActionUpdate},
		{OperationDelete, ActionDelete},
	}
	for _, tc := range cases {
		got, ok := CRUDAction(tc.op)
		if !ok || got != tc.want {
			t.Errorf("CRUDAction(%q) = %q, %v; want %q", tc.op, got, ok, tc.want)
		}
	}
	if _, ok := CRUDAction("rename"); ok {
		t.Fatal("an unknown operation must not map to an action")
	}
}

func TestOperation_WritesAreEverythingButRead(t *testing.T) {
	if OperationRead.Writes() {
		t.Fatal("read must not count as a write")
	}
	for _, op := range []Operation{OperationCreate, OperationUpdate, OperationDelete} {
		if !op.Writes() {
			t.Errorf("%q must count as a write", op)
		}
	}
}

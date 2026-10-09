package copilot

import "testing"

func TestOnlyANoEditorRefusalIsADecline(t *testing.T) {
	if !(ScreenReply{Error: &ScreenError{Code: ScreenNoEditor}}).Declined() {
		t.Fatal("a tab without the editor declines")
	}
	if (ScreenReply{Error: &ScreenError{Code: "editor_failed"}}).Declined() || (ScreenReply{OK: true}).Declined() || (ScreenReply{}).Declined() {
		t.Fatal("an editor that answered, even with a failure, did not decline")
	}
}

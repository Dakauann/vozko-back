package copilot

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/tools"
)

func jpegDataURL(size int) string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(make([]byte, size))
}

func TestScreenRepliesAreDecodedStrictly(t *testing.T) {
	accepted := map[string]string{
		"success with data":        `{"ok":true,"data":{"tracks":[]}}`,
		"refusal with a reason":    `{"ok":false,"error":{"code":"clip_not_found","message":"o clipe c-1 não existe"}}`,
		"success with one capture": `{"ok":true,"images":["` + jpegDataURL(1024) + `"]}`,
	}
	for name, raw := range accepted {
		if _, err := DecodeScreenReply([]byte(raw)); err != nil {
			t.Fatalf("%s: DecodeScreenReply() = %v", name, err)
		}
	}
	refused := map[string]string{
		"not json":                 `ok`,
		"unknown field":            `{"ok":true,"script":"alert(1)"}`,
		"refusal without a reason": `{"ok":false}`,
		"success with an error":    `{"ok":true,"error":{"code":"x","message":"y"}}`,
		"prose in the code":        `{"ok":false,"error":{"code":"drop table","message":"y"}}`,
		"remote image url":         `{"ok":true,"images":["https://evil.example/x.jpg"]}`,
		"svg capture":              `{"ok":true,"images":["data:image/svg+xml;base64,PHN2Zz4="]}`,
		"broken base64":            `{"ok":true,"images":["data:image/png;base64,***"]}`,
		"two captures":             `{"ok":true,"images":["` + jpegDataURL(10) + `","` + jpegDataURL(10) + `"]}`,
		"capture too large":        `{"ok":true,"images":["` + jpegDataURL(MaxScreenImageBytes+1) + `"]}`,
		"trailing data":            `{"ok":true}{"ok":false}`,
	}
	for name, raw := range refused {
		if _, err := DecodeScreenReply([]byte(raw)); !errors.Is(err, ErrInvalidScreenReply) {
			t.Fatalf("%s: DecodeScreenReply() = %v, want ErrInvalidScreenReply", name, err)
		}
	}
}

func TestScreenReplyBodyIsCapped(t *testing.T) {
	raw := `{"ok":true,"data":"` + strings.Repeat("a", MaxScreenReplyBytes) + `"}`
	if _, err := DecodeScreenReply([]byte(raw)); !errors.Is(err, ErrInvalidScreenReply) {
		t.Fatalf("DecodeScreenReply() = %v, want ErrInvalidScreenReply", err)
	}
}

func TestEveryScreenCommandHasABoundedWait(t *testing.T) {
	for _, name := range ScreenCommandNames() {
		if d := name.Timeout(); d <= 0 || d > time.Minute {
			t.Fatalf("%s waits %v", name, d)
		}
	}
	if ScreenCommandName("click").Known() {
		t.Fatal("an unknown command must not be known")
	}
}

func TestScreenKeyBindsTheReplyToItsThread(t *testing.T) {
	if ScreenKey("thread-a", "cmd-1") == ScreenKey("thread-b", "cmd-1") {
		t.Fatal("a reply must only reach the command of its own thread")
	}
}

type plainTool struct{}

func (plainTool) Definition() tools.Definition { return tools.Definition{Name: "plain"} }
func (plainTool) Meta() Meta                   { return Meta{} }
func (plainTool) Execute(context.Context, Context, map[string]interface{}) Result {
	return Result{}
}

type studioOnlyTool struct{ plainTool }

func (studioOnlyTool) OfferedOn(view View) bool { return view.OnStudio() }

func TestScopedToolsAreOfferedOnlyOnTheirSurface(t *testing.T) {
	studio := View{Surface: SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: StudioVideo}
	if !(View{}).Offers(plainTool{}) {
		t.Fatal("an unscoped tool is offered outside focused surfaces")
	}
	if !studio.Offers(studioOnlyTool{}) {
		t.Fatal("a studio tool is offered in the studio")
	}
	if (View{}).Offers(studioOnlyTool{}) || (View{Surface: SurfaceAttendance}).Offers(studioOnlyTool{}) {
		t.Fatal("a studio tool must not be offered outside the studio")
	}
}

func TestTheStudioIsAFocusedSurface(t *testing.T) {
	studio := View{Surface: SurfaceStudio, ProjectID: "5f0c7c1e-1d2a-4b8e-9d11-3a2b1c0d9e8f", ProjectKind: StudioImage}
	if studio.Offers(plainTool{}) {
		t.Fatal("general tools stay out of the studio so the model only sees what it can use there")
	}
	if !(View{Surface: SurfaceAttendance}).Offers(plainTool{}) {
		t.Fatal("other surfaces keep every general tool")
	}
}

func TestARefusalCanListEveryFailingOperation(t *testing.T) {
	long := strings.Repeat("operação 1 (add_shape): motivo; ", 100)[:3000]
	if _, err := DecodeScreenReply([]byte(`{"ok":false,"error":{"code":"operation_refused","message":"` + long + `"}}`)); err != nil {
		t.Fatalf("a long refusal was refused: %v", err)
	}
	tooLong := strings.Repeat("a", MaxScreenMessageRunes+1)
	if _, err := DecodeScreenReply([]byte(`{"ok":false,"error":{"code":"operation_refused","message":"` + tooLong + `"}}`)); !errors.Is(err, ErrInvalidScreenReply) {
		t.Fatalf("DecodeScreenReply() = %v, want ErrInvalidScreenReply", err)
	}
}

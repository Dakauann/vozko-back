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
		"a claim from the body":    `{"ok":true,"claim":"5f1c1a9e-8f7a-4c1e-9d55-3c2b8f0e6a11"}`,
	}
	for name, raw := range refused {
		if _, err := DecodeScreenReply([]byte(raw)); !errors.Is(err, ErrInvalidScreenReply) {
			t.Fatalf("%s: DecodeScreenReply() = %v, want ErrInvalidScreenReply", name, err)
		}
	}
}

func TestScreenRepliesNameWhatTheEditorLookedAt(t *testing.T) {
	accepted := map[string]string{
		"a named artboard":     `{"ok":true,"target":{"kind":"artboard","artboardId":"ab-2","n":2,"name":"Capa"}}`,
		"an unnamed artboard":  `{"ok":true,"target":{"kind":"artboard","artboardId":"ab-2","n":2}}`,
		"every artboard":       `{"ok":true,"target":{"kind":"artboards"}}`,
		"a stretch of moments": `{"ok":true,"target":{"kind":"moments","fromMs":3000,"toMs":5000}}`,
		"a single moment":      `{"ok":true,"target":{"kind":"moments","fromMs":3000,"toMs":3000}}`,
		"a library media":      `{"ok":true,"target":{"kind":"media"}}`,
	}
	for name, raw := range accepted {
		reply, err := DecodeScreenReply([]byte(raw))
		if err != nil || reply.Target == nil {
			t.Fatalf("%s: DecodeScreenReply() = %+v, %v", name, reply, err)
		}
	}
	refused := map[string]string{
		"an unknown kind":           `{"ok":true,"target":{"kind":"desktop"}}`,
		"an artboard without id":    `{"ok":true,"target":{"kind":"artboard","n":1}}`,
		"an artboard without place": `{"ok":true,"target":{"kind":"artboard","artboardId":"ab-1"}}`,
		"a name too long":           `{"ok":true,"target":{"kind":"artboard","artboardId":"ab-1","n":1,"name":"` + strings.Repeat("a", 121) + `"}}`,
		"a control character":       `{"ok":true,"target":{"kind":"artboard","artboardId":"ab-1","n":1,"name":"a\nb"}}`,
		"moments without times":     `{"ok":true,"target":{"kind":"moments"}}`,
		"moments backwards":         `{"ok":true,"target":{"kind":"moments","fromMs":5000,"toMs":3000}}`,
		"moments past the video":    `{"ok":true,"target":{"kind":"moments","fromMs":0,"toMs":600001}}`,
		"times on an artboard":      `{"ok":true,"target":{"kind":"artboard","artboardId":"ab-1","n":1,"fromMs":0,"toMs":10}}`,
		"an artboard on moments":    `{"ok":true,"target":{"kind":"moments","fromMs":0,"toMs":10,"artboardId":"ab-1"}}`,
		"a project the editor sets": `{"ok":true,"target":{"kind":"artboards","projectId":"p-9"}}`,
		"a target on a refusal":     `{"ok":false,"error":{"code":"x","message":"y"},"target":{"kind":"artboards"}}`,
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

func TestOnlyCommandsThatChangeSomethingWaitForAClaim(t *testing.T) {
	changing := map[ScreenCommandName]bool{ScreenEdit: true, ScreenSavePreset: true, ScreenCreateProject: true}
	for _, name := range ScreenCommandNames() {
		if name.Commits() != changing[name] {
			t.Fatalf("%s.Commits() = %v, want %v", name, name.Commits(), changing[name])
		}
	}
}

func TestAClaimNamesTheTabThatAsked(t *testing.T) {
	claimant := "5f1c1a9e-8f7a-4c1e-9d55-3c2b8f0e6a11"
	reply, err := ScreenClaim(claimant)
	if err != nil || reply.Claim != claimant || !reply.OK || reply.Declined() {
		t.Fatalf("ScreenClaim() = %+v, %v", reply, err)
	}
	for _, bad := range []string{"", "tab-1", strings.Repeat("a", 36)} {
		if _, err := ScreenClaim(bad); !errors.Is(err, ErrInvalidScreenClaim) {
			t.Fatalf("ScreenClaim(%q) = %v, want ErrInvalidScreenClaim", bad, err)
		}
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

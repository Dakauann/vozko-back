package studio

import (
	"errors"
	"strings"
	"testing"
)

const (
	originThread  = "4b1f6c2e-8a3d-4f5e-9b7c-1d2e3f4a5b6c"
	originCommand = "0b9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11"
)

func TestAProjectWithoutAnOriginIsAManualCreate(t *testing.T) {
	p := &Project{}
	if err := p.SetOrigin(Origin{}); err != nil || !p.Origin.Empty() {
		t.Fatalf("manual create refused: %v %+v", err, p.Origin)
	}
}

func TestAnOriginKeepsTheChatThreadAndTheScreenCommand(t *testing.T) {
	p := &Project{}
	if err := p.SetOrigin(Origin{ThreadID: " " + originThread + " ", CommandID: originCommand}); err != nil {
		t.Fatal(err)
	}
	if p.Origin != (Origin{ThreadID: originThread, CommandID: originCommand}) || p.Origin.Empty() {
		t.Fatalf("origin %+v", p.Origin)
	}
}

func TestAnIncompleteOrMalformedOriginIsRefused(t *testing.T) {
	cases := map[string]struct {
		origin Origin
		code   string
	}{
		"thread without command": {Origin{ThreadID: originThread}, CodeRequired},
		"command without thread": {Origin{CommandID: originCommand}, CodeRequired},
		"command not an id":      {Origin{ThreadID: originThread, CommandID: "cmd-1"}, CodeInvalid},
		"thread too long":        {Origin{ThreadID: strings.Repeat("a", MaxOriginThreadRunes+1), CommandID: originCommand}, CodeInvalid},
		"thread with spaces":     {Origin{ThreadID: "a b", CommandID: originCommand}, CodeInvalid},
	}
	for name, tc := range cases {
		p := &Project{}
		err := p.SetOrigin(tc.origin)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || invalid.Codes()[FieldOrigin] != tc.code {
			t.Fatalf("%s: %v", name, err)
		}
		if !p.Origin.Empty() {
			t.Fatalf("%s: a refused origin was kept", name)
		}
	}
}

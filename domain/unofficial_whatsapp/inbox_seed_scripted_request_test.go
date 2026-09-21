package unofficial_whatsapp

import (
	"errors"
	"testing"
)

func scriptedTargets(n int) []SeedTarget {
	out := make([]SeedTarget, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, SeedTarget{Number: numberFor(i), Name: "Lead"})
	}
	return out
}

func numberFor(i int) string {
	digits := "551190000000"
	suffix := []byte{'0' + byte(i/1000%10), '0' + byte(i/100%10), '0' + byte(i/10%10), '0' + byte(i%10)}
	return digits + string(suffix)
}

func TestSeedRequestNormalizeNormalizesItsScript(t *testing.T) {
	req := SeedRequest{
		WorkspaceID: " ws-1 ",
		Targets:     []SeedTarget{{Number: "5511999999999"}},
		Script: &SeedScript{
			Bodies:      []string{"  Oi {{1}}  ", "  "},
			MaxMessages: 99,
			Context:     "  curso  ",
		},
	}
	req.Normalize()

	if req.Script == nil {
		t.Fatal("the script was dropped by Normalize")
	}
	if len(req.Script.Bodies) != 1 || req.Script.Bodies[0] != "Oi {{1}}" {
		t.Errorf("script bodies = %#v, want the blank one dropped and the other trimmed", req.Script.Bodies)
	}
	if req.Script.MaxMessages != ScriptMaxMessages {
		t.Errorf("script MaxMessages = %d, want it clamped to %d", req.Script.MaxMessages, ScriptMaxMessages)
	}
	if req.Script.Context != "curso" {
		t.Errorf("script context = %q, want it trimmed", req.Script.Context)
	}
}

func TestSeedRequestNormalizeDropsAnEmptyScript(t *testing.T) {
	req := SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []SeedTarget{{Number: "5511999999999"}},
		Script:      &SeedScript{Bodies: []string{"   ", ""}},
	}
	req.Normalize()
	if req.Script != nil {
		t.Fatalf("an all-blank script survived Normalize as %#v", req.Script)
	}
}

func TestSeedRequestValidateReturnsTheScriptsError(t *testing.T) {
	req := SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []SeedTarget{{Number: "5511999999999"}},
		Script:      &SeedScript{Bodies: []string{"Oi {{1}}", "Oi {{2}}"}, MaxMessages: 4},
	}
	if err := req.Validate(); !errors.Is(err, ErrScriptVariantMismatch) {
		t.Fatalf("Validate() = %v, want ErrScriptVariantMismatch", err)
	}
}

func TestSeedRequestValidateAcceptsNoScript(t *testing.T) {
	req := SeedRequest{WorkspaceID: "ws-1", Targets: []SeedTarget{{Number: "5511999999999"}}}
	if err := req.Validate(); err != nil {
		t.Fatalf("a request with no script failed validation: %v", err)
	}
}

func TestSplitScriptsOnlyTheFirstMaxScriptedTargets(t *testing.T) {
	total := MaxScriptedTargets + 137
	req := SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     scriptedTargets(total),
		Script:      &SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 4},
	}
	req.Normalize()
	if len(req.Targets) != total {
		t.Fatalf("normalize kept %d of %d targets; the fixture collides", len(req.Targets), total)
	}

	batches := req.Split()

	scripted, plain := 0, 0
	for _, batch := range batches {
		if batch.Script != nil {
			scripted += len(batch.Targets)
			if len(batch.Targets) > ScriptedSeedBatchSize {
				t.Errorf("a scripted batch carries %d targets, over the %d cap",
					len(batch.Targets), ScriptedSeedBatchSize)
			}
		} else {
			plain += len(batch.Targets)
			if len(batch.Targets) > SeedBatchSize {
				t.Errorf("a plain batch carries %d targets, over the %d cap",
					len(batch.Targets), SeedBatchSize)
			}
		}
	}

	if scripted != MaxScriptedTargets {
		t.Errorf("scripted %d targets, want exactly %d", scripted, MaxScriptedTargets)
	}
	if plain != total-MaxScriptedTargets {
		t.Errorf("plain %d targets, want %d", plain, total-MaxScriptedTargets)
	}
	if scripted+plain != total {
		t.Fatalf("split covers %d targets, want %d", scripted+plain, total)
	}
}

func TestSplitPublishesScriptedBatchesFirst(t *testing.T) {
	req := SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     scriptedTargets(MaxScriptedTargets + 10),
		Script:      &SeedScript{Bodies: []string{"Oi"}, MaxMessages: 4},
	}
	req.Normalize()

	seenPlain := false
	for i, batch := range req.Split() {
		if batch.Script == nil {
			seenPlain = true
			continue
		}
		if seenPlain {
			t.Fatalf("batch %d is scripted but a plain batch came before it", i)
		}
	}
}

func TestSplitCopiesTheScriptOntoEveryScriptedBatch(t *testing.T) {
	script := &SeedScript{Bodies: []string{"Oi {{1}}"}, MaxMessages: 6, Context: "curso"}
	req := SeedRequest{WorkspaceID: "ws-1", Targets: scriptedTargets(60), Script: script}
	req.Normalize()

	for i, batch := range req.Split() {
		if batch.Script == nil {
			t.Fatalf("batch %d lost the script", i)
		}
		if batch.Script.MaxMessages != 6 || len(batch.Script.Bodies) != 1 || batch.Script.Context != "curso" {
			t.Fatalf("batch %d carries %#v, want the request's script", i, batch.Script)
		}
		if batch.WorkspaceID != "ws-1" {
			t.Fatalf("batch %d workspace = %q", i, batch.WorkspaceID)
		}
	}
}

func TestSplitWithoutAScriptCarriesNone(t *testing.T) {
	req := SeedRequest{WorkspaceID: "ws-1", Targets: scriptedTargets(1200)}
	req.Normalize()
	batches := req.Split()
	if len(batches) == 0 {
		t.Fatal("no batches")
	}
	for i, batch := range batches {
		if batch.Script != nil {
			t.Fatalf("batch %d invented a script", i)
		}
		if len(batch.Targets) > SeedBatchSize {
			t.Fatalf("batch %d carries %d targets", i, len(batch.Targets))
		}
	}
}

func TestScriptedCountIsTheTruthNotTheRequest(t *testing.T) {
	cases := []struct {
		name    string
		targets int
		script  *SeedScript
		want    int
	}{
		{"under the cap", 30, &SeedScript{Bodies: []string{"Oi"}, MaxMessages: 4}, 30},
		{"exactly the cap", MaxScriptedTargets, &SeedScript{Bodies: []string{"Oi"}, MaxMessages: 4}, MaxScriptedTargets},
		{"over the cap", MaxScriptedTargets + 500, &SeedScript{Bodies: []string{"Oi"}, MaxMessages: 4}, MaxScriptedTargets},
		{"no script at all", 500, nil, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := SeedRequest{WorkspaceID: "ws-1", Targets: scriptedTargets(tc.targets), Script: tc.script}
			req.Normalize()
			if got := req.ScriptedCount(); got != tc.want {
				t.Fatalf("ScriptedCount() = %d, want %d", got, tc.want)
			}
		})
	}
}

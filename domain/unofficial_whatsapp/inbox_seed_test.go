package unofficial_whatsapp

import "testing"

func TestNormalizeSeedRequestDropsUnusableTargets(t *testing.T) {
	cases := map[string]struct {
		in   SeedTarget
		want bool
	}{
		"plain number":     {SeedTarget{Number: "5511999999999", Name: "Ana"}, true},
		"formatted number": {SeedTarget{Number: "+55 (11) 99999-9999"}, true},
		"empty":            {SeedTarget{Number: "   "}, false},
		"letters only":     {SeedTarget{Number: "CLIENTE"}, false},
		"too short":        {SeedTarget{Number: "1234"}, false},
		"group jid":        {SeedTarget{Number: "120363427766359853@g.us"}, false},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			req := SeedRequest{WorkspaceID: "ws", Targets: []SeedTarget{c.in}}
			req.Normalize()
			got := len(req.Targets) == 1
			if got != c.want {
				t.Fatalf("kept = %v, want %v (targets: %#v)", got, c.want, req.Targets)
			}
		})
	}
}

func TestNormalizeSeedRequestNormalizesAndDeduplicates(t *testing.T) {
	req := SeedRequest{
		WorkspaceID: "  ws  ",
		Targets: []SeedTarget{
			{Number: "+55 (11) 99999-9999", Name: "  Ana  "},
			{Number: "5511999999999", Name: "Ana Maria"},
			{Number: "5511888888888"},
		},
	}
	req.Normalize()

	if req.WorkspaceID != "ws" {
		t.Fatalf("WorkspaceID = %q, want %q", req.WorkspaceID, "ws")
	}
	if len(req.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2: %#v", len(req.Targets), req.Targets)
	}
	if req.Targets[0].Number != "5511999999999" {
		t.Errorf("Number = %q, want digits only", req.Targets[0].Number)
	}
	if req.Targets[0].Name != "Ana" {
		t.Errorf("Name = %q, want %q (first occurrence wins)", req.Targets[0].Name, "Ana")
	}
}

func TestSeedRequestValidate(t *testing.T) {
	cases := map[string]struct {
		req     SeedRequest
		wantErr error
	}{
		"ok": {
			SeedRequest{WorkspaceID: "ws", Targets: []SeedTarget{{Number: "5511999999999"}}},
			nil,
		},
		"no workspace": {
			SeedRequest{Targets: []SeedTarget{{Number: "5511999999999"}}},
			ErrWorkspaceIDRequired,
		},
		"no targets": {
			SeedRequest{WorkspaceID: "ws"},
			ErrSeedNoTargets,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if err := c.req.Validate(); err != c.wantErr {
				t.Fatalf("Validate() = %v, want %v", err, c.wantErr)
			}
		})
	}
}

func TestSplitSeedRequestCoversEveryTargetExactlyOnce(t *testing.T) {
	targets := make([]SeedTarget, 0, SeedBatchSize*2+7)
	for i := 0; i < SeedBatchSize*2+7; i++ {
		targets = append(targets, SeedTarget{Number: "5511999999999"})
	}
	req := SeedRequest{WorkspaceID: "ws", Targets: targets}

	batches := req.Split()
	if len(batches) != 3 {
		t.Fatalf("len(batches) = %d, want 3", len(batches))
	}

	total := 0
	for i, b := range batches {
		if b.WorkspaceID != "ws" {
			t.Errorf("batch %d lost the workspace", i)
		}
		if len(b.Targets) > SeedBatchSize {
			t.Errorf("batch %d has %d targets, over the %d cap", i, len(b.Targets), SeedBatchSize)
		}
		total += len(b.Targets)
	}
	if total != len(targets) {
		t.Fatalf("batches cover %d targets, want %d", total, len(targets))
	}
}

func TestSplitSeedRequestOfNothingIsNothing(t *testing.T) {
	req := SeedRequest{WorkspaceID: "ws"}
	if batches := req.Split(); len(batches) != 0 {
		t.Fatalf("len(batches) = %d, want 0", len(batches))
	}
}

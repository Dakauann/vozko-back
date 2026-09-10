package unofficial_whatsapp

import "testing"

// A seed request is built from a lead import, which is the one caller that can
// hand this channel a hundred thousand rows at once. Every case below exists
// because the alternative reached the queue, the provider or the inbox.

func TestNormalizeSeedRequestDropsUnusableTargets(t *testing.T) {
	cases := map[string]struct {
		in   SeedTarget
		want bool
	}{
		// The ordinary case: digits survive, name rides along.
		"plain number": {SeedTarget{Number: "5511999999999", Name: "Ana"}, true},
		// Punctuation is what a spreadsheet actually contains.
		"formatted number": {SeedTarget{Number: "+55 (11) 99999-9999"}, true},
		// Nothing to address. A blank row must never become a contact.
		"empty": {SeedTarget{Number: "   "}, false},
		// Not a number at all. NormalizePhone strips to digits, and a value that
		// leaves nothing behind is a mis-mapped column, not a person.
		"letters only": {SeedTarget{Number: "CLIENTE"}, false},
		// Too short to be an international number. Seeding it creates a contact
		// nobody can ever reach and an inbox row nobody can ever close.
		"too short": {SeedTarget{Number: "1234"}, false},
		// A group id is not a phone number. PhoneFromJID already refuses to
		// derive one; the seed path must refuse it at the boundary too, or a
		// group ends up as a person with a "+120363..." number.
		"group jid": {SeedTarget{Number: "120363427766359853@g.us"}, false},
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
			// The same person, written the way a second spreadsheet wrote them.
			// Seeding twice is not merely wasteful: the second pass would find
			// the conversation the first pass created and could write a second
			// placeholder into it.
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
	// First writing wins, matching PrepareImport: the earlier row is the one the
	// operator sees reported, so the two must agree on which one it was.
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
		// A seed with no workspace would resolve an instance from nowhere and
		// write rows into a tenant chosen at random.
		"no workspace": {
			SeedRequest{Targets: []SeedTarget{{Number: "5511999999999"}}},
			ErrWorkspaceIDRequired,
		},
		// Every row was rejected by Normalize. Publishing this wakes a consumer
		// to do nothing; the caller should learn it seeded zero instead.
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

// A lead import accepts MaxImportRows (100.000) rows, and one queue message
// carrying all of them is the message the broker refuses. The request is
// therefore split before it is published, and the split has to be exact:
// silently dropping the remainder is how an operator imports 100.000 leads and
// finds 5.000 in the inbox.
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

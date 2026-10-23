package emaildesign

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"testing"
)

const contractPath = "testdata/edit_contract.json"

var updateContract = flag.Bool("update-contract", false, "rewrite the expected results of testdata/edit_contract.json")

type contractRefusal struct {
	Index int    `json:"index"`
	Code  string `json:"code"`
}

type contractCase struct {
	Name       string            `json:"name"`
	Operations []json.RawMessage `json:"operations"`
	Created    []string          `json:"created,omitempty"`
	Final      *Document         `json:"final,omitempty"`
	Steps      []StepPatch       `json:"steps,omitempty"`
	Refusal    *contractRefusal  `json:"refusal,omitempty"`
}

type contract struct {
	Base  Document       `json:"base"`
	Cases []contractCase `json:"cases"`
}

func contractIDs() IDSource {
	n := 0
	return func(kind BlockKind, taken map[string]bool) string {
		for {
			n++
			id := string(kind) + "-" + string(rune('a'+n-1)) + "00000"
			if !taken[id] {
				return id
			}
		}
	}
}

func runContractCase(t *testing.T, base Document, c contractCase) contractCase {
	t.Helper()
	ops := make([]Operation, 0, len(c.Operations))
	for _, raw := range c.Operations {
		var o Operation
		if err := json.Unmarshal(raw, &o); err != nil {
			t.Fatal(err)
		}
		ops = append(ops, o)
	}
	out := contractCase{Name: c.Name, Operations: c.Operations}
	plan, err := Apply(base, ops, contractIDs())
	var opErr *OperationError
	switch {
	case errors.As(err, &opErr):
		out.Refusal = &contractRefusal{Index: opErr.Index, Code: opErr.Code}
	case err != nil:
		t.Fatal(err)
	default:
		final := plan.Final(base)
		out.Final = &final
		out.Steps = EncodeSteps(base, plan.Steps)
		out.Created = plan.Created
	}
	return out
}

func TestTheEditContractFixtureMatchesTheExecutor(t *testing.T) {
	raw, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture contract
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if issues := Validate(fixture.Base); len(issues) != 0 {
		t.Fatalf("the contract base must be valid: %+v", issues)
	}
	computed := contract{Base: fixture.Base}
	for _, c := range fixture.Cases {
		got := runContractCase(t, fixture.Base, c)
		computed.Cases = append(computed.Cases, got)
		if *updateContract {
			continue
		}
		want, _ := json.Marshal(c)
		have, _ := json.Marshal(got)
		if !bytes.Equal(want, have) {
			t.Errorf("%s differs\nwant %s\ngot  %s", c.Name, want, have)
		}
	}
	if *updateContract {
		encoded, err := json.MarshalIndent(computed, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(contractPath, append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

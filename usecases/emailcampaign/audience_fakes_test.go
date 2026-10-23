package emailcampaign_usecase

import (
	"context"
	"strings"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/sheet"
)

type selectionCounts struct{ w *world }

func (p selectionCounts) Resolve(ctx context.Context, s selection.Scope, sel selection.Selection, after string, limit int) ([]selection.Ref, error) {
	return p.w.selection.Resolve(ctx, s, sel, after, limit)
}

func (p selectionCounts) Selected(ctx context.Context, s selection.Scope, sel selection.Selection) (int, error) {
	return p.w.selection.Count(ctx, s, sel)
}

type sheetText struct{}

func (sheetText) Read(data []byte) ([]sheet.Row, error) { return sheet.Parse(data), nil }

type noPhones struct{}

func (noPhones) IdentityIDs(context.Context, string, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

type knownEmails struct{ w *world }

func (k knownEmails) IDsByEmails(_ context.Context, _ string, addresses []string) (map[string]string, error) {
	out := map[string]string{}
	for _, address := range addresses {
		for id, f := range k.w.facts {
			if strings.EqualFold(f.Email, address) {
				out[address] = id
			}
		}
	}
	return out, nil
}

type createdEmailLeads struct {
	w      *world
	inputs []lead.BulkEmailInput
}

func (c *createdEmailLeads) FindOrCreateManyByEmail(_ context.Context, _ string, inputs []lead.BulkEmailInput) (map[string]*lead.Lead, error) {
	c.inputs = append(c.inputs, inputs...)
	out := map[string]*lead.Lead{}
	for _, in := range inputs {
		id := ""
		for known, f := range c.w.facts {
			if strings.EqualFold(f.Email, in.Email) {
				id = known
			}
		}
		if id == "" {
			id = "lead-" + in.Email
			c.w.facts[id] = lead.EmailFacts{ID: id, Name: in.Name, Email: in.Email}
		}
		out[in.Email] = &lead.Lead{ID: id, Email: in.Email}
	}
	return out, nil
}

type noDefinitions struct{}

func (noDefinitions) ListByObject(string, customfield.ObjectType) ([]*customfield.Definition, error) {
	return nil, nil
}

type freeGate struct{}

func (freeGate) Acquire(context.Context) (func(), error) { return func() {}, nil }

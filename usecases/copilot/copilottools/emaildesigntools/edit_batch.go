package emaildesigntools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
	"vozko/usecases/copilot/copilottools/screencall"
)

const (
	fieldSubject   = "subject"
	fieldPreheader = "preheader"
	emailNotText   = "operação %d: %s precisa ser texto"
)

type editBatch struct {
	ops       []emaildesign.Operation
	positions []int
	requested int
	subject   *string
	preheader *string
}

func singleBatch(op emaildesign.Operation) editBatch {
	return editBatch{ops: []emaildesign.Operation{op}, positions: []int{0}, requested: 1}
}

func textField(fields map[string]json.RawMessage, key string, index int) (*string, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf(emailNotText, index+1, key)
	}
	return &value, nil
}

func splitEnvelope(ops []emaildesign.Operation) (editBatch, error) {
	b := editBatch{requested: len(ops)}
	for i, op := range ops {
		if op.Op == emaildesign.OpSettings {
			subject, err := textField(op.Fields, fieldSubject, i)
			if err != nil {
				return editBatch{}, err
			}
			preheader, err := textField(op.Fields, fieldPreheader, i)
			if err != nil {
				return editBatch{}, err
			}
			if preheader != nil {
				b.preheader = preheader
			}
			if subject != nil {
				b.subject = subject
				op.Fields = withoutField(op.Fields, fieldSubject)
				if len(op.Fields) == 0 {
					continue
				}
			}
		}
		b.ops = append(b.ops, op)
		b.positions = append(b.positions, i)
	}
	return b, nil
}

func withoutField(fields map[string]json.RawMessage, key string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(fields))
	for name, value := range fields {
		if name != key {
			out[name] = value
		}
	}
	return out
}

func (b editBatch) fits(h holder) error {
	if b.subject != nil && !h.carriesEnvelope() {
		return errors.New(emailSubjectNoCampaign)
	}
	return nil
}

func (b editBatch) request(step int) int {
	if step >= 0 && step < len(b.positions) {
		return b.positions[step]
	}
	return step
}

func (b editBatch) requests(steps []int) []int {
	out := make([]int, 0, len(steps))
	for _, step := range steps {
		out = append(out, b.request(step))
	}
	return out
}

func (b editBatch) renumber(err error) error {
	var opErr *emaildesign.OperationError
	if errors.As(err, &opErr) {
		opErr.Index = b.request(opErr.Index)
	}
	return err
}

func (b editBatch) envelope(base *envelope) *envelope {
	if base == nil {
		return nil
	}
	next := *base
	if b.subject != nil {
		next.Subject = *b.subject
	}
	if b.preheader != nil {
		next.Preheader = *b.preheader
	}
	return &next
}

func (l liveDesign) issues() []emaildesign.Issue {
	issues := emaildesign.Validate(l.shown())
	if l.Envelope != nil {
		issues = append(issues, emaildesign.ValidateSubject(l.Envelope.Subject)...)
	}
	return issues
}

func (d Deps) applyBatch(ctx context.Context, cc copilot.Context, h holder, b editBatch) (batchOutcome, error) {
	if err := b.fits(h); err != nil {
		return batchOutcome{}, err
	}
	live, err := readLive(ctx, cc)
	if err != nil {
		return batchOutcome{}, err
	}
	plan, err := emaildesign.Apply(live.Document, b.ops, d.ids())
	if err != nil {
		return batchOutcome{}, operationFailure(b.renumber(err))
	}
	planned := liveDesign{Document: plan.Final(live.Document), Envelope: b.envelope(live.Envelope)}
	if broken := emaildesign.StructuralIssues(addedIssues(live.issues(), planned.issues())); len(broken) > 0 {
		return batchOutcome{}, fmt.Errorf(emailBreaks, describeIssues(broken))
	}
	payload := editPayload{Base: live.Document, Steps: emaildesign.EncodeSteps(live.Document, plan.Steps)}
	if live.Envelope != nil {
		payload.Envelope = &envelopeEdit{Base: *live.Envelope, Next: *planned.Envelope}
	}
	reply, err := screencall.Run(ctx, cc, copilot.ScreenEdit, payload, editorWords)
	if err != nil {
		return batchOutcome{}, err
	}
	after, got, err := decodeEdit(reply.Data, live)
	if err != nil {
		return batchOutcome{}, err
	}
	facts, err := h.keep(ctx, cc, live, after)
	if err != nil {
		return batchOutcome{}, err
	}
	return batchOutcome{after: after, batch: b, plan: plan, conflicts: got.Conflicts, skipped: b.requests(got.Skipped), facts: facts}, nil
}

func decodeEdit(data []byte, live liveDesign) (liveDesign, editReply, error) {
	var got editReply
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		return liveDesign{}, editReply{}, errors.New(emailUnreadable)
	}
	doc, err := emaildesign.Decode(got.Document)
	if err != nil {
		return liveDesign{}, editReply{}, errors.New(emailUnreadable)
	}
	env, ok := envelopeOf(got.Subject, got.Preheader, live.Envelope != nil)
	if !ok {
		return liveDesign{}, editReply{}, errors.New(emailUnreadable)
	}
	return liveDesign{Document: doc, Selected: live.Selected, Name: live.Name, Envelope: env}, got, nil
}

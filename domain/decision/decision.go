package decision

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidQuestion = errors.New("decision: invalid question")
	ErrInvalidRequest  = errors.New("decision: invalid request")
	ErrInvalidAnswer   = errors.New("decision: the model returned an answer outside the question")
	ErrUnavailable     = errors.New("decision: the decision model is unavailable")
	ErrDisabled        = errors.New("decision: live decisions are turned off for this workspace")
)

const (
	MaxChoiceOptions = 255
	MaxScoreLevels   = 10
)

type Kind string

const (
	KindChoice Kind = "choice"
	KindYesNo  Kind = "noul"
	KindScore  Kind = "score"
)

type Option struct {
	Key         string
	Description string
}

type Question struct {
	Kind         Kind
	Instructions string
	Options      []Option
	Yes          string
	No           string
	Levels       []string
}

func Choice(instructions string, options ...Option) Question {
	return Question{Kind: KindChoice, Instructions: instructions, Options: options}
}

func YesNo(instructions, yes, no string) Question {
	return Question{Kind: KindYesNo, Instructions: instructions, Yes: yes, No: no}
}

func Score(instructions string, levels ...string) Question {
	return Question{Kind: KindScore, Instructions: instructions, Levels: levels}
}

func (q Question) Validate() error {
	if strings.TrimSpace(q.Instructions) == "" {
		return fmt.Errorf("%w: instructions are required", ErrInvalidQuestion)
	}
	switch q.Kind {
	case KindChoice:
		return validateOptions(q.Options)
	case KindYesNo:
		if strings.TrimSpace(q.Yes) == "" || strings.TrimSpace(q.No) == "" {
			return fmt.Errorf("%w: both yes and no need a meaning", ErrInvalidQuestion)
		}
		return nil
	case KindScore:
		if len(q.Levels) < 2 || len(q.Levels) > MaxScoreLevels {
			return fmt.Errorf("%w: a score needs 2 to %d levels", ErrInvalidQuestion, MaxScoreLevels)
		}
		for _, level := range q.Levels {
			if strings.TrimSpace(level) == "" {
				return fmt.Errorf("%w: every level needs a description", ErrInvalidQuestion)
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidQuestion, q.Kind)
	}
}

func validateOptions(options []Option) error {
	if len(options) < 2 || len(options) > MaxChoiceOptions {
		return fmt.Errorf("%w: a choice needs 2 to %d options", ErrInvalidQuestion, MaxChoiceOptions)
	}
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		key := strings.TrimSpace(option.Key)
		if key == "" || strings.TrimSpace(option.Description) == "" {
			return fmt.Errorf("%w: every option needs a key and a description", ErrInvalidQuestion)
		}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("%w: option %q appears twice", ErrInvalidQuestion, key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (q Question) hasOption(key string) bool {
	for _, option := range q.Options {
		if option.Key == key {
			return true
		}
	}
	return false
}

type Request struct {
	WorkspaceID string
	Purpose     string
	ReferenceID string
	State       any
	Questions   map[string]Question
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return fmt.Errorf("%w: workspace is required", ErrInvalidRequest)
	}
	if r.State == nil {
		return fmt.Errorf("%w: state is required", ErrInvalidRequest)
	}
	if len(r.Questions) == 0 {
		return fmt.Errorf("%w: at least one question is required", ErrInvalidRequest)
	}
	for id, question := range r.Questions {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: question ids cannot be blank", ErrInvalidRequest)
		}
		if err := question.Validate(); err != nil {
			return fmt.Errorf("%w: question %q: %v", ErrInvalidRequest, id, err)
		}
	}
	return nil
}

func (r Request) Accepts(result Result) error {
	for id, question := range r.Questions {
		answer, ok := result.Answers[id]
		if !ok {
			return fmt.Errorf("%w: %q was not answered", ErrInvalidAnswer, id)
		}
		if answer.Kind != question.Kind {
			return fmt.Errorf("%w: %q came back as %s", ErrInvalidAnswer, id, answer.Kind)
		}
		if err := answer.within(question); err != nil {
			return fmt.Errorf("%w: %q %v", ErrInvalidAnswer, id, err)
		}
	}
	return nil
}

type Answer struct {
	Kind          Kind
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
	Yes           float64
	Score         float64
}

func (a Answer) within(question Question) error {
	switch a.Kind {
	case KindChoice:
		if !question.hasOption(a.Choice) {
			return fmt.Errorf("chose %q, which is not an option", a.Choice)
		}
		return unitInterval("confidence", a.Confidence)
	case KindYesNo:
		return unitInterval("probability", a.Yes)
	case KindScore:
		if a.Score < 0 || a.Score > float64(len(question.Levels)-1) {
			return fmt.Errorf("scored %v outside 0..%d", a.Score, len(question.Levels)-1)
		}
		return unitInterval("confidence", a.Confidence)
	}
	return fmt.Errorf("unknown kind %q", a.Kind)
}

func unitInterval(name string, value float64) error {
	if value < 0 || value > 1 {
		return fmt.Errorf("%s %v outside 0..1", name, value)
	}
	return nil
}

func (a Answer) Chosen(min Threshold) (string, bool) {
	if a.Kind != KindChoice || a.Confidence < float64(min) {
		return "", false
	}
	return a.Choice, true
}

func (a Answer) Affirmed(min Threshold) bool {
	return a.Kind == KindYesNo && a.Yes >= float64(min)
}

func (a Answer) Denied(min Threshold) bool {
	return a.Kind == KindYesNo && 1-a.Yes >= float64(min)
}

func (a Answer) Certainty() float64 {
	if a.Kind == KindYesNo {
		return max(a.Yes, 1-a.Yes)
	}
	return a.Confidence
}

type Result struct {
	Model       string
	Answers     map[string]Answer
	InputTokens int
	CostMicros  int64
}

func (r Result) Answer(id string) (Answer, bool) {
	answer, ok := r.Answers[id]
	return answer, ok
}

type Model interface {
	Decide(ctx context.Context, request Request) (Result, error)
}

type Threshold float64

type Action string

const (
	ActionMoveStage        Action = "move_stage"
	ActionSkipMemoryReview Action = "skip_memory_review"
	ActionSkipDealReview   Action = "skip_deal_review"
)

func Actions() []Action {
	return []Action{
		ActionMoveStage, ActionSkipMemoryReview, ActionSkipDealReview,
	}
}

type Policy map[Action]Threshold

func DefaultPolicy() Policy {
	return Policy{
		ActionMoveStage:        0.80,
		ActionSkipMemoryReview: 0.80,
		ActionSkipDealReview:   0.80,
	}
}

func (p Policy) Threshold(action Action) (Threshold, bool) {
	threshold, ok := p[action]
	return threshold, ok
}

func (p Policy) Allows(action Action, certainty float64) bool {
	threshold, ok := p[action]
	return ok && certainty >= float64(threshold)
}

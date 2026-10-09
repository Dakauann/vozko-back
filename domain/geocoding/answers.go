package geocoding

import (
	"context"
	"strings"
	"time"

	"vozko/domain/geo"
)

type AnswerKind string

const (
	AnswerLocated   AnswerKind = "located"
	AnswerAmbiguous AnswerKind = "ambiguous"
	AnswerNotFound  AnswerKind = "not_found"
	AnswerRefused   AnswerKind = "refused"
)

func (k AnswerKind) Valid() bool {
	switch k {
	case AnswerLocated, AnswerAmbiguous, AnswerNotFound, AnswerRefused:
		return true
	}
	return false
}

type AnswerKey struct {
	WorkspaceID string
	Fingerprint string
}

func (k AnswerKey) complete() bool {
	return strings.TrimSpace(k.WorkspaceID) != "" && strings.TrimSpace(k.Fingerprint) != ""
}

func AnswerKeysOf(keys []AnswerKey) []AnswerKey {
	out := make([]AnswerKey, 0, len(keys))
	seen := make(map[AnswerKey]bool, len(keys))
	for _, k := range keys {
		if !k.complete() || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

type Answer struct {
	Key        AnswerKey
	Provider   Provider
	Kind       AnswerKind
	Fix        *geo.Fix
	ResolvedAt time.Time
}

func AnswerOf(provider Provider, key AnswerKey, outcome geo.Outcome, at time.Time) (Answer, bool) {
	answer := Answer{Key: key, Provider: provider, ResolvedAt: at}
	switch {
	case outcome.Kind == geo.OutcomeLocated:
		fix := outcome.Fix
		answer.Kind, answer.Fix = AnswerLocated, &fix
	case outcome.Kind == geo.OutcomeAmbiguous:
		answer.Kind = AnswerAmbiguous
	case outcome.Kind == geo.OutcomeNotFound:
		answer.Kind = AnswerNotFound
	case RefusalOf(outcome) == RefusalQuery:
		answer.Kind = AnswerRefused
	default:
		return Answer{}, false
	}
	return answer, answer.Valid()
}

func (a Answer) Valid() bool {
	if !a.Key.complete() || !a.Kind.Valid() {
		return false
	}
	if a.Kind != AnswerLocated {
		return a.Fix == nil
	}
	return a.Fix != nil && a.Fix.Validate() == nil
}

func (a Answer) Outcome() geo.Outcome {
	switch a.Kind {
	case AnswerLocated:
		if a.Fix == nil {
			return geo.Unavailable(geo.ReasonInvalidAnswer, 0)
		}
		return geo.Located(*a.Fix)
	case AnswerAmbiguous:
		return geo.Ambiguous()
	case AnswerNotFound:
		return geo.NotFound()
	case AnswerRefused:
		return geo.Unavailable(geo.ReasonQueryRefused, 0)
	}
	return geo.Unavailable(geo.ReasonInvalidAnswer, 0)
}

type AnswerCache interface {
	Answers(ctx context.Context, keys []AnswerKey) (map[AnswerKey]Answer, error)
	Remember(ctx context.Context, answer Answer) error
}

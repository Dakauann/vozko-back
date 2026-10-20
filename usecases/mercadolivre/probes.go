package mercadolivre

import (
	"context"
	"fmt"
	"slices"

	mldomain "vozko/domain/mercadolivre"
)

type probeOutcome int

const (
	probeGranted probeOutcome = iota
	probeDenied
)

func probeCapabilities(ctx context.Context, svc Services, creds mldomain.Credentials, scopes []string) (mldomain.AccountCapabilities, error) {
	questions, err := probe(svc.Errors, func() error {
		_, err := svc.Questions.SearchQuestions(ctx, creds, mldomain.QuestionSearch{Status: mldomain.QuestionUnanswered, Limit: 1})
		return err
	})
	if err != nil {
		return mldomain.AccountCapabilities{}, err
	}
	messages, err := probe(svc.Errors, func() error {
		_, err := svc.Messages.Unread(ctx, creds)
		return err
	})
	if err != nil {
		return mldomain.AccountCapabilities{}, err
	}
	writes := slices.Contains(scopes, mldomain.ScopeWrite)
	return mldomain.AccountCapabilities{
		CanReadQuestions:   questions == probeGranted,
		CanAnswerQuestions: questions == probeGranted && writes,
		CanReadMessages:    messages == probeGranted,
		CanSendMessages:    messages == probeGranted && writes,
	}, nil
}

func probe(errs mldomain.ErrorClassifier, call func() error) (probeOutcome, error) {
	err := call()
	switch {
	case err == nil:
		return probeGranted, nil
	case errs.IsPermission(err):
		return probeDenied, nil
	case errs.IsAuth(err):
		return probeDenied, err
	}
	return probeDenied, fmt.Errorf("%w: %v", ErrProbeInconclusive, err)
}

func requiredCapabilitiesGranted(caps mldomain.AccountCapabilities) bool {
	return caps.CanReadMessages && caps.CanSendMessages && caps.CanReadQuestions
}

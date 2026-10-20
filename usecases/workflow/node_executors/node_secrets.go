package node_executors

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/redact"
	"vozko/domain/workflow"
	"vozko/domain/workflowsecret"
	"vozko/usecases/workflow/scriptvm"
)

type nodeSecrets struct {
	values map[string]string
	scrub  redact.Redactor
}

func resolveNodeSecrets(ctx *workflow.NodeContext, resolver workflowsecret.Resolver, names []string) (nodeSecrets, error) {
	if len(names) == 0 {
		return nodeSecrets{scrub: redact.SecretsOnly()}, nil
	}
	if resolver == nil {
		return nodeSecrets{}, fmt.Errorf("%w: %s", workflowsecret.ErrUnavailable, strings.Join(names, ", "))
	}
	if ctx.Run == nil || strings.TrimSpace(ctx.Run.WorkspaceID) == "" {
		return nodeSecrets{}, workflowsecret.ErrWorkspaceRequired
	}
	callCtx, release := ctx.CallContext()
	defer release()
	values, err := resolver.Resolve(callCtx, ctx.Run.WorkspaceID, names)
	if err != nil {
		return nodeSecrets{}, err
	}
	revealed := make([]string, 0, len(values))
	for _, v := range values {
		revealed = append(revealed, v)
	}
	scrub := redact.SecretsOnly(revealed...)
	ctx.Trace.ScrubWith(scrub)
	return nodeSecrets{values: values, scrub: scrub}, nil
}

type scrubbedError struct {
	message string
	cause   error
}

func (e scrubbedError) Error() string { return e.message }

func (e scrubbedError) Unwrap() error { return e.cause }

func (s nodeSecrets) scrubError(err error) error {
	message := s.scrub.String(err.Error())
	if message == err.Error() {
		return err
	}
	return scrubbedError{message: message, cause: err}
}

func requireAllSecrets(names []string, secrets nodeSecrets) error {
	if missing := workflowsecret.MissingNames(names, secrets.values); len(missing) > 0 {
		return &workflowsecret.MissingError{Names: missing}
	}
	return nil
}

func secretFailure(err error) (*workflow.NodeResult, error) {
	if errors.Is(err, workflowsecret.ErrMissing) ||
		errors.Is(err, workflowsecret.ErrUnavailable) ||
		errors.Is(err, workflowsecret.ErrWorkspaceRequired) {
		return &workflow.NodeResult{Error: err.Error(), ErrorKind: workflow.ErrorKindConfig}, nil
	}
	return nil, err
}

type scrubbedState struct {
	state scriptvm.StateAPI
	scrub redact.Redactor
}

func (s scrubbedState) Lookup(path string) (interface{}, bool) { return s.state.Lookup(path) }

func (s scrubbedState) Set(key string, value interface{}) { s.state.Set(key, s.scrub.Value(value)) }

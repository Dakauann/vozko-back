package workflow_usecase

import "vozko/domain/workflow"

type interruptibleRuntime interface {
	InterruptScope(node *workflow.Node) func()
}

func openInterruptScope(runtime interface{}, node *workflow.Node) func() {
	if runtime == nil {
		return func() {}
	}
	scoped, ok := runtime.(interruptibleRuntime)
	if !ok || scoped == nil {
		return func() {}
	}
	release := scoped.InterruptScope(node)
	if release == nil {
		return func() {}
	}
	return release
}

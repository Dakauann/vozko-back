package workflow

type GatedStep struct {
	Result *NodeResult
	Pinned bool
}

type StepGate interface {
	BeforeStep(run *WorkflowRun, node *Node) (*GatedStep, error)
}

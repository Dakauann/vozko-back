package workflow_usecase

import (
	"testing"

	"vozko/domain/workflow"
	"vozko/usecases/workflow/node_executors"
)

var offeredVariableTypes = map[workflow.VariableType]bool{
	workflow.VariableTypeText: true, workflow.VariableTypeNumber: true, workflow.VariableTypeBoolean: true,
	workflow.VariableTypeObject: true, workflow.VariableTypeList: true, workflow.VariableTypeDate: true,
	workflow.VariableTypeUnknown: true,
}

func TestEveryNodeOutputThePickerOffersHasAPortugueseLabelAndADeclaredType(t *testing.T) {
	registry := NewNodeExecutorRegistry()
	RegisterDefaultExecutors(registry, ExecutorDeps{})
	definitions := append(registry.Catalog(), node_executors.NewSendCarrierExecutor(node_executors.CarrierNodeDeps{}).Definition())
	for _, def := range definitions {
		if def.Type.IsTrigger() {
			continue
		}
		for _, out := range def.OutputKeys {
			if workflow.IsDynamicOutputKey(out.Key) || out.RepeatsConfig {
				continue
			}
			if out.Label == "" || out.Label == out.Key || !offeredVariableTypes[out.Type] {
				t.Errorf("%s output %q has label %q and type %q", def.Type, out.Key, out.Label, out.Type)
			}
		}
	}
}

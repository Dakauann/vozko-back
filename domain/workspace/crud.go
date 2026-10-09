package workspace

type Operation string

const (
	OperationRead   Operation = "read"
	OperationCreate Operation = "create"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
)

var crudActions = map[Operation]Action{
	OperationRead:   ActionRead,
	OperationCreate: ActionCreate,
	OperationUpdate: ActionUpdate,
	OperationDelete: ActionDelete,
}

func CRUDAction(op Operation) (Action, bool) {
	action, ok := crudActions[op]
	return action, ok
}

func (op Operation) Writes() bool {
	_, known := crudActions[op]
	return known && op != OperationRead
}

package container

import (
	"testing"

	calllist_usecase "vozko/usecases/calls/calllist"
)

func TestTheCallSocketAndTheCallLifecycleShareTheOneCallListService(t *testing.T) {
	service := &calllist_usecase.Service{}
	c := &Container{callListBundle: &callListBundle{service: service}}
	items := c.callListItems()
	if items == nil {
		t.Fatal("the start use case and the lifecycle must receive the call list service, not nil")
	}
	if got, ok := items.(*calllist_usecase.Service); !ok || got != service || c.callLists() != service {
		t.Fatalf("callListItems = %T %p, want the cached service %p", items, items, service)
	}
}

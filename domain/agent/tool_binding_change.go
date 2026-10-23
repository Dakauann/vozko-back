package agent

import (
	"bytes"
	"encoding/json"
)

func BindingChanged(stored, proposed []ToolBinding, name string) bool {
	next, bound := bindingNamed(proposed, name)
	if !bound {
		return false
	}
	previous, wasBound := bindingNamed(stored, name)
	return !wasBound || !sameBinding(previous, next)
}

func bindingNamed(bindings []ToolBinding, name string) (ToolBinding, bool) {
	for _, b := range bindings {
		if b.Name == name {
			return b, true
		}
	}
	return ToolBinding{}, false
}

func sameBinding(a, b ToolBinding) bool {
	left, errLeft := json.Marshal(a)
	right, errRight := json.Marshal(b)
	return errLeft == nil && errRight == nil && bytes.Equal(left, right)
}

func BindingsAdded(stored, proposed []ToolBinding, name string) []ToolBinding {
	var added []ToolBinding
	for _, next := range proposed {
		if next.Name != name {
			continue
		}
		known := false
		for _, previous := range stored {
			if previous.Name == name && sameBinding(previous, next) {
				known = true
				break
			}
		}
		if !known {
			added = append(added, next)
		}
	}
	return added
}

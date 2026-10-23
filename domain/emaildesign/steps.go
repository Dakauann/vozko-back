package emaildesign

import (
	"bytes"
	"encoding/json"
	"reflect"
)

type StepPatch struct {
	Op       OperationKind    `json:"op"`
	Focus    string           `json:"focus,omitempty"`
	Settings *Settings        `json:"settings,omitempty"`
	Order    *[]string        `json:"order,omitempty"`
	Blocks   map[string]Block `json:"blocks,omitempty"`
}

func EncodeSteps(base Document, steps []Step) []StepPatch {
	patches := make([]StepPatch, 0, len(steps))
	previous := base
	for _, step := range steps {
		patches = append(patches, patchBetween(previous, step))
		previous = step.Document
	}
	return patches
}

func sameJSON(a, b interface{}) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(left, right)
}

func patchBetween(previous Document, step Step) StepPatch {
	patch := StepPatch{Op: step.Op, Focus: step.Focus}
	next := step.Document
	if !sameJSON(previous.Settings, next.Settings) {
		settings := next.Settings
		patch.Settings = &settings
	}
	before := make(map[string]Block, len(previous.Blocks))
	order := make([]string, 0, len(previous.Blocks))
	for _, b := range previous.Blocks {
		before[b.ID] = b
		order = append(order, b.ID)
	}
	nextOrder := make([]string, 0, len(next.Blocks))
	for _, b := range next.Blocks {
		nextOrder = append(nextOrder, b.ID)
		if old, ok := before[b.ID]; !ok || !sameJSON(old, b) {
			if patch.Blocks == nil {
				patch.Blocks = map[string]Block{}
			}
			patch.Blocks[b.ID] = b
		}
	}
	if !reflect.DeepEqual(order, nextOrder) {
		patch.Order = &nextOrder
	}
	return patch
}

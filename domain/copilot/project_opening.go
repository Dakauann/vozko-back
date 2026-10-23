package copilot

import (
	"bytes"
	"encoding/json"
	"errors"
)

var ErrInvalidCreatedProject = errors.New("copilot: the app did not return a usable studio project")

type ProjectOpening struct {
	View   View
	Opened bool
}

type createdProject struct {
	ProjectID string `json:"projectId"`
	Kind      string `json:"kind"`
	Opened    bool   `json:"opened"`
}

func OpeningOf(order StudioProjectOrder, data []byte) (ProjectOpening, error) {
	var got createdProject
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil || dec.More() || got.Kind != order.Kind {
		return ProjectOpening{}, ErrInvalidCreatedProject
	}
	view := View{Surface: SurfaceStudio, ProjectID: got.ProjectID, ProjectKind: StudioKind(got.Kind)}
	if view.Validate() != nil {
		return ProjectOpening{}, ErrInvalidCreatedProject
	}
	return ProjectOpening{View: view, Opened: got.Opened}, nil
}

const emailDesignKind = "email"

func EmailDesignOpeningOf(designID string, data []byte) (ProjectOpening, error) {
	var got createdProject
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil || dec.More() || got.Kind != emailDesignKind || got.ProjectID != designID {
		return ProjectOpening{}, ErrInvalidCreatedProject
	}
	opening := WaitingEmailDesign(designID)
	if opening.View.Validate() != nil {
		return ProjectOpening{}, ErrInvalidCreatedProject
	}
	opening.Opened = got.Opened
	return opening, nil
}

func WaitingEmailDesign(designID string) ProjectOpening {
	return ProjectOpening{View: View{Surface: SurfaceEmailDesign, ProjectID: designID}}
}

func (o ProjectOpening) Awaiting() *View {
	if o.Opened {
		return nil
	}
	view := o.View
	return &view
}

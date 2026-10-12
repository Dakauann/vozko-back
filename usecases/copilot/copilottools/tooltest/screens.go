package tooltest

import (
	"context"
	"encoding/json"

	"vozko/domain/copilot"
)

type ProjectScreen struct {
	ProjectID string
	Opened    bool
	Reply     *copilot.ScreenReply
	Err       error
	Commands  []copilot.ScreenCommand
}

func (s *ProjectScreen) Run(_ context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	s.Commands = append(s.Commands, cmd)
	if s.Err != nil {
		return copilot.ScreenReply{}, s.Err
	}
	if s.Reply != nil {
		return *s.Reply, nil
	}
	order, _ := cmd.Args.(copilot.StudioProjectOrder)
	data, err := json.Marshal(map[string]interface{}{"projectId": s.ProjectID, "kind": order.Kind, "opened": s.Opened})
	if err != nil {
		return copilot.ScreenReply{}, err
	}
	return copilot.ScreenReply{OK: true, Data: data}, nil
}

func OwnerWithScreen(view copilot.View, screen copilot.Screen) copilot.Context {
	cc := OwnerOn(view)
	cc.Screen = screen
	return cc
}

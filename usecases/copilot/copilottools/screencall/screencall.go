package screencall

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"vozko/domain/copilot"
)

const Interrupted = "a edição foi interrompida antes de o editor responder"

type Words struct {
	Silent      string
	Expired     string
	Unconfirmed string
	Absent      string
	Refused     string
}

type Failure struct {
	message   string
	unapplied bool
	silent    bool
}

func (e Failure) Error() string { return e.message }

func Run(ctx context.Context, cc copilot.Context, name copilot.ScreenCommandName, args interface{}, words Words) (copilot.ScreenReply, error) {
	reply, err := cc.Screen.Run(ctx, copilot.ScreenCommand{Name: name, Args: args})
	switch {
	case errors.Is(err, copilot.ErrScreenUnavailable) && name.Commits():
		return reply, Failure{message: words.Expired, unapplied: true, silent: true}
	case errors.Is(err, copilot.ErrScreenUnavailable):
		return reply, Failure{message: words.Silent, unapplied: true, silent: true}
	case errors.Is(err, copilot.ErrScreenUnconfirmed):
		return reply, Failure{message: words.Unconfirmed, silent: true}
	case err != nil && ctx.Err() != nil:
		return reply, errors.New(Interrupted)
	case err != nil:
		log.Printf("[copilot] screen %s could not reach the editor: %v", name, err)
		return reply, Failure{message: words.Silent, silent: true}
	case reply.Declined():
		log.Printf("[copilot] screen %s: only tabs that cannot run it answered", name)
		return reply, Failure{message: words.Absent, unapplied: true}
	case !reply.OK:
		return reply, Failure{message: RefusalMessage(words.Refused, reply), unapplied: true}
	}
	return reply, nil
}

func Result(cc copilot.Context, err error) copilot.Result {
	res := copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	var failure Failure
	if errors.As(err, &failure) && failure.silent && cc.View.OnEditor() {
		view := cc.View
		res.AwaitsScreen = &view
	}
	return res
}

func RefusalMessage(refused string, reply copilot.ScreenReply) string {
	message := refused + ": " + reply.Error.Message
	if len(reply.Data) == 0 {
		return message
	}
	return message + " Resultado de cada operação: " + string(reply.Data)
}

func DidNotApply(err error) bool {
	var failure Failure
	return errors.As(err, &failure) && failure.unapplied
}

func Data(reply copilot.ScreenReply) interface{} {
	if len(reply.Data) == 0 {
		return nil
	}
	return json.RawMessage(reply.Data)
}

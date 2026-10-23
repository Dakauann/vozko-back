package emaildesigntools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/screencall"
)

const (
	defaultStarter      = "blank"
	stateOpened         = "opened"
	stateWaiting        = "waiting"
	newDesignOpened     = "design criado e aberto: o editor de e-mail já está na tela do usuário e as ferramentas email_ entram na sua próxima chamada. Continue o trabalho agora, começando por email_read; não peça que ele abra nem que confirme nada"
	newDesignWaiting    = "design criado, mas o editor continua fechado: ele abre quando o usuário tocar em Abrir no editor no cartão. Diga isso uma vez e não diga que abriu. Quando ele abrir, o app avisa você e as ferramentas email_ chegam; nunca peça que ele avise que abriu"
	newDesignNameBad    = "name é obrigatório e vai até %d caracteres"
	newDesignStarterBad = "starter %q não existe; use um de: %s"
	newDesignFailed     = "o design não pôde ser criado agora; tente de novo"
)

var openingWords = screencall.Words{
	Silent:      "o app não confirmou a abertura a tempo",
	Expired:     "o app não respondeu a tempo",
	Unconfirmed: "o app começou a abrir o editor e não confirmou",
	Absent:      "nenhuma aba conseguiu abrir o editor",
	Refused:     "o app não abriu o editor",
}

type newEmailDesignArgs struct {
	Name    string `json:"name" req:"true" desc:"nome do design, como aparece na lista de modelos de e-mail"`
	Starter string `json:"starter,omitempty" desc:"ponto de partida; padrão blank"`
}

type designOpening struct {
	DesignID string `json:"designId"`
	Name     string `json:"name"`
}

type newEmailDesignTool struct{ deps Deps }

func NewNewEmailDesignTool(deps Deps) copilot.Tool { return &newEmailDesignTool{deps: deps} }

func (t *newEmailDesignTool) OfferedOn(view copilot.View) bool { return !view.Focused() }

func (t *newEmailDesignTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionCreate}
}

func starterKeys() []string {
	keys := []string{}
	for _, s := range emaildesign.StarterDocuments() {
		keys = append(keys, s.Key)
	}
	return keys
}

func (t *newEmailDesignTool) Definition() tools.Definition {
	def := copilottools.Definition("new_email_design",
		"Cria um design de e-mail marketing (salvo em E-mail, Modelos) a partir de um ponto de partida e mostra um cartão que leva o usuário direto ao editor dele. "+
			"O retorno diz o estado real: opened (o editor já abriu na tela dele e as ferramentas email_ entram na sua próxima chamada) ou waiting "+
			"(criado, esperando ele abrir pelo cartão; o app avisa quando ele abrir). No editor, ele edita à mão e você edita junto com email_edit, "+
			"vê com email_look e gera imagens com email_generate_image.",
		newEmailDesignArgs{})
	starter := def.Parameters["starter"]
	starter.Enum = starterKeys()
	def.Parameters["starter"] = starter
	return def
}

func starterDocument(key string) (emaildesign.Document, bool) {
	for _, s := range emaildesign.StarterDocuments() {
		if s.Key == key {
			return s.Document, true
		}
	}
	return emaildesign.Document{}, false
}

func (t *newEmailDesignTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a newEmailDesignArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return failure(err.Error())
	}
	key := strings.TrimSpace(a.Starter)
	if key == "" {
		key = defaultStarter
	}
	doc, ok := starterDocument(key)
	if !ok {
		return failure(fmt.Sprintf(newDesignStarterBad, key, strings.Join(starterKeys(), ", ")))
	}
	if t.deps.Designs == nil {
		return failure(newDesignFailed)
	}
	design, err := t.deps.Designs.CreateDesign(ctx, actorOf(cc), emailcampaign.DesignInput{Name: a.Name, Document: doc, Kind: emailcampaign.DesignTemplate})
	if err != nil {
		return failure(createFailure(err))
	}
	card, err := copilot.NewEmailDesignCard(design.ID, design.Name)
	if err != nil {
		return failure(newDesignFailed)
	}
	opening := t.open(ctx, cc, design)
	state, next := stateWaiting, newDesignWaiting
	if opening.Opened {
		state, next = stateOpened, newDesignOpened
	}
	return copilot.Result{Status: copilot.StatusOK, Card: card, Opening: &opening, Data: map[string]interface{}{"state": state, "design_id": design.ID, "next": next}}
}

func (t *newEmailDesignTool) open(ctx context.Context, cc copilot.Context, design *emailcampaign.Design) copilot.ProjectOpening {
	waiting := copilot.WaitingEmailDesign(design.ID)
	if cc.Screen == nil {
		return waiting
	}
	reply, err := screencall.Run(ctx, cc, copilot.ScreenOpenDesign, designOpening{DesignID: design.ID, Name: design.Name}, openingWords)
	if err != nil {
		log.Printf("[copilot] new_email_design %s stays closed: %v", design.ID, err)
		return waiting
	}
	opening, err := copilot.EmailDesignOpeningOf(design.ID, reply.Data)
	if err != nil {
		return waiting
	}
	return opening
}

func createFailure(err error) string {
	switch {
	case errors.Is(err, emailcampaign.ErrDesignNameRequired), errors.Is(err, emailcampaign.ErrNameTooLong):
		return fmt.Sprintf(newDesignNameBad, emailcampaign.MaxNameRunes)
	}
	log.Printf("[copilot] new_email_design could not create the design: %v", err)
	return newDesignFailed
}

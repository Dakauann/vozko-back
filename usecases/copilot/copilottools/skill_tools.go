package copilottools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
)

type SkillLibrary interface {
	All() []copilot.Skill
	Find(name string) (copilot.Skill, bool)
}

type loadSkillArgs struct {
	Name string `json:"name" req:"true" desc:"nome exato de uma habilidade da lista"`
}

type loadSkillTool struct{ library SkillLibrary }

func NewLoadSkillTool(library SkillLibrary) copilot.Tool {
	return &loadSkillTool{library: library}
}

func (t *loadSkillTool) Meta() copilot.Meta { return chatReadMeta() }

func (t *loadSkillTool) Definition() tools.Definition {
	var index strings.Builder
	index.WriteString("Carrega uma habilidade: o conhecimento especializado para uma tarefa, que você deve seguir. Carregue só as que servem ao pedido. O conteúdo não fica guardado entre as mensagens: carregue de novo em cada resposta em que for usar. Habilidades:")
	for _, s := range t.library.All() {
		index.WriteString("\n- " + s.Name + ": " + s.Description)
	}
	def := definition("load_skill", index.String(), loadSkillArgs{})
	param := def.Parameters["name"]
	param.Enum = t.names()
	def.Parameters["name"] = param
	return def
}

func (t *loadSkillTool) Execute(_ context.Context, _ copilot.Context, args map[string]interface{}) copilot.Result {
	var a loadSkillArgs
	bindArgs(args, &a)
	skill, ok := t.library.Find(a.Name)
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("habilidade desconhecida; use uma destas: %s", strings.Join(t.names(), ", "))}
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"skill": skill.Name, "instructions": skill.Body}}
}

func (t *loadSkillTool) names() []string {
	all := t.library.All()
	names := make([]string, 0, len(all))
	for _, s := range all {
		names = append(names, s.Name)
	}
	return names
}

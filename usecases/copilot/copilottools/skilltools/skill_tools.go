package skilltools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

type SkillLibrary interface {
	All() []copilot.Skill
	Find(name string) (copilot.Skill, bool)
}

type loadSkillArgs struct {
	Names []string `json:"names" req:"true" desc:"nomes exatos de habilidades da lista"`
}

type loadSkillTool struct{ library SkillLibrary }

func NewLoadSkillTool(library SkillLibrary) copilot.Tool {
	return &loadSkillTool{library: library}
}

func (t *loadSkillTool) Meta() copilot.Meta { return copilottools.ChatReadMeta() }

func (t *loadSkillTool) Definition() tools.Definition {
	return t.index(t.library.All())
}

func (t *loadSkillTool) DefinitionOn(view copilot.View) tools.Definition {
	return t.index(t.offered(view))
}

func (t *loadSkillTool) index(skills []copilot.Skill) tools.Definition {
	var index strings.Builder
	index.WriteString("Carrega habilidades: o conhecimento especializado para uma tarefa, que você deve seguir. Carregue só as que servem ao pedido; várias vão numa só chamada. O conteúdo fica na conversa até ela ser resumida; se a habilidade não estiver mais na conversa, carregue de novo. Habilidades:")
	for _, s := range skills {
		index.WriteString("\n- " + s.Name + ": " + s.Description)
	}
	def := copilottools.Definition("load_skill", index.String(), loadSkillArgs{})
	param := def.Parameters["names"]
	param.Items.Enum = skillNames(skills)
	def.Parameters["names"] = param
	return def
}

func (t *loadSkillTool) offered(view copilot.View) []copilot.Skill {
	var skills []copilot.Skill
	for _, s := range t.library.All() {
		if s.OfferedOn(view) {
			skills = append(skills, s)
		}
	}
	return skills
}

func (t *loadSkillTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	names := requestedNames(args)
	skills := make([]copilot.Skill, 0, len(names))
	unknown := 0
	for _, name := range names {
		skill, ok := t.library.Find(name)
		if !ok || !skill.OfferedOn(cc.View) {
			unknown++
			continue
		}
		skills = append(skills, skill)
	}
	if len(skills) == 0 || unknown > 0 {
		return copilot.Result{Status: copilot.StatusError, Message: fmt.Sprintf("habilidade desconhecida; nada foi carregado; use uma destas: %s", strings.Join(skillNames(t.offered(cc.View)), ", "))}
	}
	if len(skills) == 1 {
		skill := skills[0]
		return copilot.Result{
			Status:  copilot.StatusOK,
			Data:    map[string]interface{}{"skill": skill.Name, "instructions": skill.Body},
			Subject: &copilot.Subject{Kind: copilot.SubjectSkill, Key: skill.Name, Label: skill.Title()},
		}
	}
	loaded := make([]map[string]interface{}, 0, len(skills))
	keys := make([]string, 0, len(skills))
	titles := make([]string, 0, len(skills))
	for _, skill := range skills {
		loaded = append(loaded, map[string]interface{}{"skill": skill.Name, "instructions": skill.Body})
		keys = append(keys, skill.Name)
		titles = append(titles, skill.Title())
	}
	return copilot.Result{
		Status:  copilot.StatusOK,
		Data:    map[string]interface{}{"skills": loaded},
		Subject: &copilot.Subject{Kind: copilot.SubjectSkill, Key: strings.Join(keys, ","), Label: strings.Join(titles, ", ")},
	}
}

func requestedNames(args map[string]interface{}) []string {
	var raw []string
	switch v := args["names"].(type) {
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				raw = append(raw, s)
			}
		}
	case []string:
		raw = v
	case string:
		raw = []string{v}
	}
	if legacy, ok := args["name"].(string); ok {
		raw = append(raw, legacy)
	}
	seen := make(map[string]bool, len(raw))
	names := make([]string, 0, len(raw))
	for _, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func skillNames(skills []copilot.Skill) []string {
	list := make([]string, 0, len(skills))
	for _, s := range skills {
		list = append(list, s.Name)
	}
	return list
}

func (t *loadSkillTool) OfferedOn(copilot.View) bool { return true }

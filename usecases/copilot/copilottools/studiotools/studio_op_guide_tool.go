package studiotools

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

const studioOpGuideHint = "Os campos, os valores aceitos, as faixas e os exemplos de cada operação estão em studio_op_guide: antes do primeiro lote com uma família nesta resposta, chame studio_op_guide com as famílias ou as operações que vai usar, várias de uma vez. Famílias e operações: "

const studioOpGuideDescription = "Devolve a referência das operações de studio_edit_image ou studio_edit_video, conforme o projeto aberto: para cada família ou operação pedida, a explicação, os exemplos e cada campo com o tipo, os valores aceitos e as faixas. " +
	"Peça de uma vez todas as famílias que a edição vai usar, antes de editar com elas, e peça de novo em cada nova resposta ao usuário."

type studioOpGuideArgs struct {
	Names []string `json:"names" req:"true" desc:"famílias do índice de studio_edit_image ou studio_edit_video, operações ou campos, como [\"texto\", \"animate\"]"`
}

func compactEditDefinition(reference tools.Definition, intro, outro string, families []studioOpFamily, core []string) tools.Definition {
	operations := reference.Parameters["operations"]
	items := *operations.Items
	properties := make(map[string]tools.Parameter, len(items.Properties))
	for name, p := range items.Properties {
		properties[name] = bareParameter(p, slices.Contains(core, name))
	}
	items.Properties = properties
	operations.Items = &items
	return tools.Definition{
		Name:        reference.Name,
		Description: intro + studioOpGuideHint + studioFamilyIndex(families) + ". " + outro,
		Parameters:  map[string]tools.Parameter{"operations": operations},
		Required:    reference.Required,
	}
}

func bareParameter(p tools.Parameter, documented bool) tools.Parameter {
	bare := tools.Parameter{Type: p.Type, Items: bareItems(p.Items)}
	if documented {
		bare.Description, bare.Enum = p.Description, p.Enum
	}
	return bare
}

func bareItems(items *tools.ParameterItems) *tools.ParameterItems {
	if items == nil {
		return nil
	}
	bare := &tools.ParameterItems{Type: items.Type, Required: items.Required, Items: bareItems(items.Items)}
	if items.Properties != nil {
		bare.Properties = make(map[string]tools.Parameter, len(items.Properties))
		for name, p := range items.Properties {
			bare.Properties[name] = bareParameter(p, false)
		}
	}
	return bare
}

type studioOpReference struct {
	studioOpNames
	families []studioOpFamily
	sections map[string]string
	byOp     map[string]string
}

func referenceOf(edit *studioEditTool) studioOpReference {
	params := edit.Definition().Parameters["operations"].Items.Properties
	order := edit.operationOrder()
	ref := studioOpReference{studioOpNames: newStudioOpNames(edit.kind, edit.families, order, edit.core), families: edit.families, sections: map[string]string{}, byOp: map[string]string{}}
	for _, family := range edit.families {
		var b strings.Builder
		b.WriteString("## " + family.name + ": " + strings.Join(family.ops, ", ") + "\n")
		b.WriteString(strings.TrimSpace(family.guide) + "\nCampos:\n")
		for _, name := range family.fields(order) {
			if !slices.Contains(edit.core, name) {
				writeField(&b, "", name, params[name])
			}
		}
		ref.sections[family.name] = b.String()
		for _, op := range family.ops {
			ref.byOp[op] = family.name
		}
	}
	return ref
}

func writeField(b *strings.Builder, indent, name string, p tools.Parameter) {
	kind := p.Type
	if p.Type == "array" && p.Items != nil {
		kind = "lista de " + itemsKind(p.Items)
	}
	b.WriteString(indent + "- " + name + " (" + kind)
	if values := p.Enum; len(values) > 0 && !listedIn(p.Description, values) {
		b.WriteString("; valores: " + strings.Join(values, ", "))
	}
	if p.Items != nil && len(p.Items.Enum) > 0 {
		b.WriteString("; valores: " + strings.Join(p.Items.Enum, ", "))
	}
	b.WriteString(")")
	if p.Description != "" {
		b.WriteString(": " + p.Description)
	}
	b.WriteString("\n")
	if p.Items == nil {
		return
	}
	names := make([]string, 0, len(p.Items.Properties))
	for child := range p.Items.Properties {
		names = append(names, child)
	}
	sort.Strings(names)
	for _, child := range names {
		writeField(b, indent+"  ", child, p.Items.Properties[child])
	}
}

func itemsKind(items *tools.ParameterItems) string {
	if items.Type == "array" && items.Items != nil {
		return "lista de " + itemsKind(items.Items)
	}
	return items.Type
}

func listedIn(description string, values []string) bool {
	for _, value := range values {
		if !strings.Contains(description, value) {
			return false
		}
	}
	return true
}

func (r studioOpReference) familyNames() []string {
	names := make([]string, 0, len(r.families))
	for _, family := range r.families {
		names = append(names, family.name)
	}
	return names
}

func (r studioOpReference) guide(names []string) (string, map[string]string, error) {
	var picked []string
	resolved := map[string]string{}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		family, ok := r.resolve(name)
		if !ok {
			return "", nil, fmt.Errorf("%w: %q não é família, operação nem campo deste editor; a família mais próxima é %s; as famílias são: %s", copilottools.ErrInvalidArgs, name, r.closest(name), strings.Join(r.familyNames(), ", "))
		}
		if family != name && r.byOp[name] != family {
			resolved[name] = family
		}
		if !slices.Contains(picked, family) {
			picked = append(picked, family)
		}
	}
	if len(picked) == 0 {
		return "", nil, fmt.Errorf("%w: diga ao menos uma família: %s", copilottools.ErrInvalidArgs, strings.Join(r.familyNames(), ", "))
	}
	sections := make([]string, 0, len(picked))
	for _, family := range picked {
		sections = append(sections, r.sections[family])
	}
	return strings.Join(sections, "\n"), resolved, nil
}

type studioOpGuideTool struct {
	studioScope
	references func() map[copilot.StudioKind]studioOpReference
}

func NewStudioOpGuideTool() copilot.Tool {
	return &studioOpGuideTool{references: sync.OnceValue(func() map[copilot.StudioKind]studioOpReference {
		return map[copilot.StudioKind]studioOpReference{
			copilot.StudioImage: referenceOf(studioEditFor(copilot.StudioImage)),
			copilot.StudioVideo: referenceOf(studioEditFor(copilot.StudioVideo)),
		}
	})}
}

func studioEditFor(kind copilot.StudioKind) *studioEditTool {
	if kind == copilot.StudioImage {
		return NewStudioEditImageTool(StudioDeps{}).(*studioEditTool)
	}
	return NewStudioEditVideoTool(StudioDeps{}).(*studioEditTool)
}

func (t *studioOpGuideTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioOpGuideTool) Definition() tools.Definition {
	return copilottools.Definition("studio_op_guide", studioOpGuideDescription, studioOpGuideArgs{})
}

func (t *studioOpGuideTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	ref, ok := t.references()[cc.View.ProjectKind]
	if !cc.View.OnStudio() || !ok {
		return studioFailure(studioNotHere)
	}
	var a studioOpGuideArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	guide, resolved, err := ref.guide(a.Names)
	if err != nil {
		return studioFailure(err.Error())
	}
	data := map[string]interface{}{"editor": string(cc.View.ProjectKind), "guide": guide}
	if len(resolved) > 0 {
		data["resolved"] = resolved
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

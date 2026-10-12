package studiotools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/studio"
	"vozko/domain/studio/library"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
)

const studioPresetsBroken = "as predefinições de animação não puderam ser lidas; tente de novo mais tarde"

type StudioPresets interface {
	List(ctx context.Context, workspaceID string) ([]studio.AnimationPreset, error)
}

type studioPresetsArgs struct {
	Query string `json:"query,omitempty" desc:"palavras para filtrar pelo nome e pela descrição, no idioma da conversa ou em português, sem precisar de acento; vazio lista todas"`
}

type presetResultItem struct {
	PresetID      string   `json:"preset_id"`
	Name          string   `json:"name"`
	Description   string   `json:"description,omitempty"`
	BuiltIn       bool     `json:"built_in"`
	Scope         string   `json:"scope"`
	DurationMS    int64    `json:"duration_ms"`
	Keys          []string `json:"keys"`
	Behaviors     []string `json:"behaviors"`
	Entrance      string   `json:"entrance"`
	Exit          string   `json:"exit"`
	TextAnimators int      `json:"text_animators"`
	Vfx           []string `json:"vfx"`
	MotionBlur    bool     `json:"motion_blur"`
}

type presetResult struct {
	Total   int                `json:"total"`
	Presets []presetResultItem `json:"presets"`
}

type studioPresetsTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioAnimationPresetsTool(deps StudioDeps) copilot.Tool {
	return &studioPresetsTool{studioScope: studioScope{kind: copilot.StudioVideo}, deps: deps}
}

func (t *studioPresetsTool) Meta() copilot.Meta { return studioReadMeta() }

func (t *studioPresetsTool) Definition() tools.Definition {
	return copilottools.Definition("studio_animation_presets",
		"Lista as predefinições de animação do Estúdio, as mesmas que a pessoa vê no editor de vídeo: as prontas (built_in true) e as salvas no workspace, sem passar pelo editor. "+
			"Cada uma traz preset_id, name, description, scope (visual serve para qualquer clipe visual; text só para clipes de texto), duration_ms (a duração em que a animação foi feita), "+
			"keys (as propriedades com chaves), behaviors, entrance e exit (motion, transition, text ou vazio), text_animators, vfx e motion_blur. "+
			"Aplique com apply_animation_preset(preset_id, clip_id ou clip_ids, preset_timing?) em studio_edit_video; para guardar a animação de um clipe como predefinição nova, use studio_save_animation_preset.",
		studioPresetsArgs{})
}

func (t *studioPresetsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	if !cc.View.OnStudio() {
		return studioFailure(studioNotHere)
	}
	var a studioPresetsArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return studioFailure(err.Error())
	}
	if t.deps.Presets == nil {
		return studioFailure(studioPresetsBroken)
	}
	presets, err := t.deps.Presets.List(ctx, cc.WorkspaceID)
	if err != nil {
		log.Printf("[copilot] studio_animation_presets could not list: %v", err)
		return studioFailure(studioPresetsBroken)
	}
	words := strings.Fields(library.Fold(a.Query))
	result := presetResult{Presets: []presetResultItem{}}
	for _, p := range presets {
		summary, err := p.Summary()
		if err != nil {
			log.Printf("[copilot] studio_animation_presets skipped preset %s: %v", p.ID, err)
			continue
		}
		if !presetMatches(p, cc.Locale, words) {
			continue
		}
		result.Presets = append(result.Presets, presetItem(p, summary, cc.Locale))
	}
	result.Total = len(result.Presets)
	return copilot.Result{Status: copilot.StatusOK, Data: result}
}

func presetText(p studio.AnimationPreset, locale string) (string, string) {
	if !p.BuiltIn {
		return p.Name, p.Description
	}
	return library.Names(p.Names).In(locale), library.Names(p.Descriptions).In(locale)
}

func presetMatches(p studio.AnimationPreset, locale string, words []string) bool {
	if len(words) == 0 {
		return true
	}
	name, description := presetText(p, locale)
	haystack := library.Fold(strings.Join([]string{name, description, p.Names[library.DefaultLocale], p.Descriptions[library.DefaultLocale]}, " "))
	for _, word := range words {
		if !strings.Contains(haystack, word) {
			return false
		}
	}
	return true
}

func presetItem(p studio.AnimationPreset, s studio.PresetSummary, locale string) presetResultItem {
	name, description := presetText(p, locale)
	return presetResultItem{
		PresetID: p.ID, Name: name, Description: description, BuiltIn: p.BuiltIn,
		Scope: s.Scope, DurationMS: s.DurationMS, Keys: s.Keys, Behaviors: s.Behaviors, Entrance: s.Entrance, Exit: s.Exit,
		TextAnimators: s.TextAnimators, Vfx: s.Vfx, MotionBlur: s.MotionBlur,
	}
}

type studioSavePresetArgs struct {
	ClipID      string `json:"clip_id" req:"true" desc:"o clipe cuja animação vira predefinição, o id exato de studio_read"`
	Name        string `json:"name" req:"true" desc:"nome da predefinição, de 1 a 120 caracteres"`
	Description string `json:"description,omitempty" desc:"para que serve a predefinição, até 500 caracteres"`
}

func (a studioSavePresetArgs) clean() (studioSavePresetArgs, error) {
	clip := strings.TrimSpace(a.ClipID)
	if clip == "" {
		return a, fmt.Errorf("%w: clip_id é obrigatório; copie o id exato de studio_read", copilottools.ErrInvalidArgs)
	}
	name, description, err := studio.CleanPresetText(a.Name, a.Description)
	var invalid *studio.ValidationError
	if errors.As(err, &invalid) {
		codes := invalid.Codes()
		switch {
		case codes[studio.FieldName] == studio.CodeRequired:
			return a, fmt.Errorf("%w: name é obrigatório", copilottools.ErrInvalidArgs)
		case codes[studio.FieldName] == studio.CodeTooLarge:
			return a, fmt.Errorf("%w: name vai até %d caracteres", copilottools.ErrInvalidArgs, studio.MaxPresetNameRunes)
		case codes[studio.FieldDescription] == studio.CodeTooLarge:
			return a, fmt.Errorf("%w: description vai até %d caracteres", copilottools.ErrInvalidArgs, studio.MaxPresetDescriptionRunes)
		}
	}
	if err != nil {
		return a, fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	return studioSavePresetArgs{ClipID: clip, Name: name, Description: description}, nil
}

type studioSavePresetTool struct {
	studioScope
	deps StudioDeps
}

func NewStudioSaveAnimationPresetTool(deps StudioDeps) copilot.Tool {
	return &studioSavePresetTool{studioScope: studioScope{kind: copilot.StudioVideo}, deps: deps}
}

func (t *studioSavePresetTool) Meta() copilot.Meta { return studioEditMeta() }

func (t *studioSavePresetTool) LogsArguments() bool { return true }

func (t *studioSavePresetTool) Definition() tools.Definition {
	return copilottools.Definition("studio_save_animation_preset",
		"Salva a animação de um clipe do vídeo aberto como predefinição do workspace, pelo mesmo caminho que a pessoa usa no editor: chaves de movimento, opacidade e desfoque, "+
			"comportamentos, entrada e saída, animação de texto, efeitos e desfoque de movimento. As posições ficam relativas ao lugar do clipe, então a predefinição serve em qualquer clipe. "+
			"A resposta traz preset_id, name, scope, duration_ms, keys, behaviors, entrance, exit, text_animators, vfx, motion_blur e skipped (o que o clipe tinha e não entra numa predefinição). "+
			"Depois aplique com apply_animation_preset em studio_edit_video.",
		studioSavePresetArgs{})
}

func (t *studioSavePresetTool) arguments(args map[string]interface{}) (studioSavePresetArgs, error) {
	var a studioSavePresetArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return a, err
	}
	return a.clean()
}

func (t *studioSavePresetTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	a, err := t.arguments(args)
	if err != nil {
		return studioFailure(err.Error())
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return studioFailure(err.Error())
	}
	reply, err := runOnEditor(ctx, cc, copilot.ScreenSavePreset, map[string]interface{}{"clip_id": a.ClipID, "name": a.Name, "description": a.Description})
	if err != nil {
		return screenResult(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: screenData(reply)}
}

func (t *studioSavePresetTool) NeedsApproval(mode copilot.Mode) bool { return mode == copilot.ModeAsk }

func (t *studioSavePresetTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := t.arguments(args); err != nil {
		return err
	}
	if _, err := t.deps.openProject(ctx, cc); err != nil {
		return fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	return nil
}

func (t *studioSavePresetTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	name, _ := args["name"].(string)
	clip, _ := args["clip_id"].(string)
	return []copilot.Field{{Key: "name", Value: strings.TrimSpace(name)}, {Key: "clip_id", Value: strings.TrimSpace(clip)}}
}

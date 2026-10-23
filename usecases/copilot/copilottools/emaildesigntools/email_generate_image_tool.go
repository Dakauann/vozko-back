package emaildesigntools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
	"vozko/domain/mediagen"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/mediatools"
	"vozko/usecases/copilot/copilottools/screencall"
)

const (
	emailPlaceOne      = "use block_id (um bloco de imagem que já existe) ou at (um bloco novo), um dos dois"
	emailNotImageBlock = "o bloco %q não é um bloco de imagem; use at para criar um bloco novo"
	emailNoBlock       = "o bloco %q não existe; confira os ids com email_read"
	emailNotPublic     = "a imagem foi gerada e cobrada (media_id %s), mas não tem um endereço https público e não foi colocada no e-mail"
	emailPlaceLater    = " A imagem foi gerada e cobrada (media_id %s, media_url %s): coloque com email_edit usando esse media_id num bloco de imagem; nunca gere de novo."
	fieldPlace         = "block"
	placeNew           = "novo bloco de imagem"
	probeSource        = "https://example.com/probe.jpg"
)

type queuedGeneration interface {
	copilot.Tool
	copilot.ChoiceAsker
	copilot.Validator
	copilot.Describer
	GenerationRequest(ctx context.Context, cc copilot.Context, args map[string]interface{}) (mediagen.Request, error)
}

type emailImagePlacementArgs struct {
	BlockID string              `json:"block_id,omitempty" desc:"um bloco de imagem que já existe (id de email_read): a imagem nova entra no lugar da atual"`
	At      *emailPlacementArgs `json:"at,omitempty" desc:"cria um bloco de imagem novo aqui (after, before ou into com column)"`
	Alt     string              `json:"alt" req:"true" desc:"texto alternativo: o que a imagem mostra, para quem não vê imagens"`
	Href    string              `json:"href,omitempty" desc:"opcional: link ao tocar na imagem, https"`
	Width   *int                `json:"width,omitempty" desc:"opcional: % da largura do bloco, 10 a 100 (padrão 100 no bloco novo)"`
}

type emailGenerateImageTool struct {
	designScope
	deps  Deps
	inner queuedGeneration
}

func NewEmailGenerateImageTool(deps Deps) copilot.Tool {
	return &emailGenerateImageTool{deps: deps, inner: mediatools.NewGenerateImageTool(deps.Media).(queuedGeneration)}
}

func (t *emailGenerateImageTool) Meta() copilot.Meta { return t.inner.Meta() }

func (t *emailGenerateImageTool) AlsoRequires() []workspace.PermissionEntry {
	return []workspace.PermissionEntry{{Resource: workspace.ResourceEmailMarketing, Action: workspace.ActionUpdate}}
}

func (t *emailGenerateImageTool) NeedsApproval(mode copilot.Mode) bool {
	return mode != copilot.ModeFull
}

func (t *emailGenerateImageTool) Definition() tools.Definition {
	inner := t.inner.Definition()
	_, ceiling := mediagen.KindImage.ActiveLane()
	def := copilottools.Definition("email_generate_image",
		"Gera uma imagem com IA pelo mesmo módulo de generate_image e a coloca no e-mail aberto: num bloco de imagem que já existe (block_id) ou num bloco novo (at). "+
			"A imagem fica na biblioteca com um endereço https público e permanente, o que os apps de e-mail precisam para mostrá-la meses depois. "+
			"É cobrada do saldo pelo custo do provedor; conforme o modo do usuário, roda direto com o modelo recomendado ou pede aprovação (o usuário escolhe o modelo no cartão). "+
			"Espera a imagem ficar pronta e devolve media_id, media_url, o bloco e a review. "+
			fmt.Sprintf("Até %d gerações com IA rodam ao mesmo tempo no workspace.", ceiling),
		emailImagePlacementArgs{})
	for name, param := range inner.Parameters {
		def.Parameters[name] = param
	}
	def.Required = append(append([]string(nil), inner.Required...), def.Required...)
	return def
}

func (t *emailGenerateImageTool) Choices(ctx context.Context, cc copilot.Context, args map[string]interface{}) ([]copilot.ChoiceField, error) {
	choices, err := t.inner.Choices(ctx, cc, args)
	if err != nil {
		return nil, err
	}
	return mediatools.WithPreferredModels(ctx, t.deps.Media, mediagen.KindImage, choices), nil
}

func (t *emailGenerateImageTool) Preview(ctx context.Context, cc copilot.Context, args map[string]interface{}) *copilot.Preview {
	if previewer, ok := t.inner.(copilot.Previewer); ok {
		return previewer.Preview(ctx, cc, args)
	}
	return nil
}

func (t *emailGenerateImageTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	fields := t.inner.Describe(ctx, cc, args)
	var p emailImagePlacementArgs
	copilottools.BindArgs(args, &p)
	place := placeNew
	if strings.TrimSpace(p.BlockID) != "" {
		place = strings.TrimSpace(p.BlockID)
	}
	return append(fields, copilot.Field{Key: fieldPlace, Value: place})
}

func (t *emailGenerateImageTool) placement(args map[string]interface{}) (emailImagePlacementArgs, error) {
	var p emailImagePlacementArgs
	if err := copilottools.DecodeArgs(args, &p); err != nil {
		return p, fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	if (strings.TrimSpace(p.BlockID) == "") == (p.At == nil) {
		return p, fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, emailPlaceOne)
	}
	return p, nil
}

func (t *emailGenerateImageTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	if _, err := t.placement(args); err != nil {
		return err
	}
	if _, err := t.deps.openHolder(ctx, cc, forEditing); err != nil {
		return fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	return t.inner.Validate(ctx, cc, args)
}

func (p emailImagePlacementArgs) operation(src string) (emaildesign.Operation, error) {
	fields := map[string]interface{}{"src": src, "alt": p.Alt}
	if strings.TrimSpace(p.Href) != "" {
		fields["href"] = strings.TrimSpace(p.Href)
	}
	if p.Width != nil {
		fields["width"] = *p.Width
	}
	raw := map[string]json.RawMessage{}
	for key, value := range fields {
		encoded, err := json.Marshal(value)
		if err != nil {
			return emaildesign.Operation{}, err
		}
		raw[key] = encoded
	}
	if id := strings.TrimSpace(p.BlockID); id != "" {
		return emaildesign.Operation{Op: emaildesign.OpSet, ID: id, Fields: raw}, nil
	}
	return emaildesign.Operation{Op: emaildesign.OpAdd, Kind: emaildesign.KindImage, Fields: raw, At: &emaildesign.Placement{After: p.At.After, Before: p.At.Before, Into: p.At.Into, Column: p.At.Column}}, nil
}

func (t *emailGenerateImageTool) fits(doc emaildesign.Document, p emailImagePlacementArgs) error {
	if id := strings.TrimSpace(p.BlockID); id != "" {
		block, ok := emaildesign.FindBlock(doc.Blocks, id)
		switch {
		case !ok:
			return fmt.Errorf(emailNoBlock, id)
		case block.Kind != emaildesign.KindImage:
			return fmt.Errorf(emailNotImageBlock, id)
		}
	}
	op, err := p.operation(probeSource)
	if err != nil {
		return err
	}
	if _, err := emaildesign.Apply(doc, []emaildesign.Operation{op}, emaildesign.RandomIDs()); err != nil {
		return operationFailure(err)
	}
	return nil
}

func (t *emailGenerateImageTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	p, err := t.placement(args)
	if err != nil {
		return failure(err.Error())
	}
	h, err := t.deps.openHolder(ctx, cc, forEditing)
	if err != nil {
		return failure(err.Error())
	}
	live, err := readLive(ctx, cc)
	if err != nil {
		return screencall.Result(cc, err)
	}
	if err := t.fits(live.Document, p); err != nil {
		return failure(err.Error())
	}
	req, err := t.inner.GenerationRequest(ctx, cc, args)
	if err != nil {
		return failure(err.Error())
	}
	job, result, message := mediatools.AwaitGeneration(ctx, t.deps.Media, cc, "email_generate_image", req)
	if message != "" {
		return failure(message)
	}
	if hasCode(emaildesign.Validate(probeImage(result.MediaURL)), "image_src_not_https") {
		return failure(fmt.Sprintf(emailNotPublic, result.MediaID))
	}
	op, err := p.operation(result.MediaURL)
	if err != nil {
		return failure(err.Error())
	}
	outcome, err := t.deps.applyBatch(ctx, cc, h, singleBatch(op))
	if err != nil {
		res := screencall.Result(cc, err)
		res.Message += fmt.Sprintf(emailPlaceLater, result.MediaID, result.MediaURL)
		return res
	}
	blockID := strings.TrimSpace(p.BlockID)
	if blockID == "" && len(outcome.plan.Created) > 0 {
		blockID = outcome.plan.Created[0]
	}
	data := outcome.data()
	data["media_id"], data["media_url"], data["model"], data["block_id"] = result.MediaID, result.MediaURL, result.Model, blockID
	return copilot.Result{Status: copilot.StatusOK, Data: withOpenGaps(data, outcome.after), Media: &copilot.Media{Kind: copilot.MediaImage, URL: result.MediaURL, MediaID: result.MediaID, Alt: job.Prompt}}
}

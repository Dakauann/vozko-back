package emaildesigntools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/copilot"
	"vozko/domain/emaildesign"
	"vozko/domain/media"
	"vozko/domain/tools"
	"vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/screencall"
)

type emailSocialLinkArgs struct {
	Network string `json:"network" req:"true" enum:"facebook,instagram,linkedin,x,youtube,tiktok,whatsapp,website" desc:"rede"`
	URL     string `json:"url" req:"true" desc:"endereço https do perfil"`
	Label   string `json:"label,omitempty" desc:"rótulo; padrão o nome da rede"`
}

type emailFieldArgs struct {
	Text                   *string               `json:"text,omitempty" desc:"título e rodapé: texto simples; bloco de texto: parágrafos separados por linha em branco, **negrito**, *itálico*, __sublinhado__, [rótulo](https://link), \\* para um asterisco literal"`
	Level                  *int                  `json:"level,omitempty" desc:"título: 1 a 3"`
	Align                  *string               `json:"align,omitempty" enum:"left,center,right" desc:"alinhamento"`
	Color                  *string               `json:"color,omitempty" desc:"#rrggbb: cor do título, da divisória ou do fundo do botão"`
	Label                  *string               `json:"label,omitempty" desc:"rótulo do botão"`
	Href                   *string               `json:"href,omitempty" desc:"link do botão ou da imagem: https, mailto: ou tel:"`
	FullWidth              *bool                 `json:"full_width,omitempty" desc:"botão na largura toda"`
	TextColor              *string               `json:"text_color,omitempty" desc:"#rrggbb: texto do botão; em settings, o texto do e-mail"`
	Src                    *string               `json:"src,omitempty" desc:"imagem: endereço https público"`
	MediaID                *string               `json:"media_id,omitempty" desc:"imagem: media_id de uma imagem da biblioteca, de um anexo ou gerada; vira o src"`
	Alt                    *string               `json:"alt,omitempty" desc:"imagem: texto alternativo"`
	Width                  *int                  `json:"width,omitempty" desc:"imagem: % da largura do bloco (10 a 100); settings: largura do e-mail em px (480 a 720)"`
	Thickness              *int                  `json:"thickness,omitempty" desc:"divisória: 1 a 8 px"`
	Height                 *int                  `json:"height,omitempty" desc:"espaçador: 4 a 120 px"`
	Ratio                  *string               `json:"ratio,omitempty" enum:"50-50,33-67,67-33,33-33-33" desc:"colunas: proporção; com menos colunas, o conteúdo das que saem vai para a última"`
	Links                  []emailSocialLinkArgs `json:"links,omitempty" desc:"redes sociais: a lista inteira"`
	CompanyName            *string               `json:"company_name,omitempty" desc:"rodapé: empresa"`
	Address                *string               `json:"address,omitempty" desc:"rodapé: endereço físico"`
	Padding                *string               `json:"padding,omitempty" enum:"s,m,l" desc:"respiro do bloco"`
	BackgroundColor        *string               `json:"background_color,omitempty" desc:"#rrggbb: fundo do bloco; em settings, o fundo em volta do e-mail"`
	ContentBackgroundColor *string               `json:"content_background_color,omitempty" desc:"settings: #rrggbb do fundo do e-mail"`
	LinkColor              *string               `json:"link_color,omitempty" desc:"settings: #rrggbb dos links"`
	ButtonColor            *string               `json:"button_color,omitempty" desc:"settings: #rrggbb padrão dos botões"`
	ButtonTextColor        *string               `json:"button_text_color,omitempty" desc:"settings: #rrggbb padrão do texto dos botões"`
	FontFamily             *string               `json:"font_family,omitempty" desc:"settings: fonte"`
	Preheader              *string               `json:"preheader,omitempty" desc:"settings: prévia que aparece ao lado do assunto na caixa de entrada"`
	Subject                *string               `json:"subject,omitempty" desc:"settings, só numa campanha: o assunto, uma linha, aceita merge tags"`
}

type emailPlacementArgs struct {
	After  string `json:"after,omitempty" desc:"logo depois deste bloco"`
	Before string `json:"before,omitempty" desc:"logo antes deste bloco"`
	Into   string `json:"into,omitempty" desc:"no fim de uma coluna deste bloco de colunas"`
	Column int    `json:"column,omitempty" desc:"com into: a coluna, de 0 à esquerda"`
}

type emailOperationArgs struct {
	Op     string              `json:"op" req:"true" enum:"add,set,move,remove,duplicate,settings" desc:"add cria, set muda campos, move, remove, duplicate e settings muda o e-mail todo"`
	Ref    string              `json:"ref,omitempty" desc:"nome curto (letras minúsculas, números, - e _) para o bloco que add ou duplicate criam; as operações seguintes do lote usam @nome"`
	ID     string              `json:"id,omitempty" desc:"bloco alvo de set, move, remove e duplicate: id de email_read ou @ref"`
	Kind   string              `json:"kind,omitempty" enum:"heading,text,button,image,divider,spacer,columns,social,footer" desc:"add: tipo do bloco"`
	Fields *emailFieldArgs     `json:"fields,omitempty" desc:"add, set e settings: os campos que mudam; string vazia limpa um campo opcional"`
	At     *emailPlacementArgs `json:"at,omitempty" desc:"add e move: onde; um entre after, before e into; sem at, entra no fim, antes do rodapé"`
}

type emailEditArgs struct {
	Operations []emailOperationArgs `json:"operations" req:"true" desc:"as operações, aplicadas em ordem, todas ou nenhuma"`
}

const (
	emailNoOperations = "envie ao menos uma operação"
	emailMediaUnknown = "media_id %q não é uma imagem da biblioteca deste workspace"
	emailMediaUnsafe  = "a imagem %q não tem um endereço https público e não pode ir num e-mail"
	emailMediaAndSrc  = "use media_id ou src, um dos dois"
	emailBreaks       = "o lote deixaria o e-mail malformado, então nada foi aplicado: %s. Corrija essas operações e reenvie o lote inteiro"
	emailOpFailed     = "operação %d (%s): %s; nada foi aplicado. Corrija e reenvie o lote inteiro"
)

var operationWords = map[string]string{
	"operation_unknown":      "op não existe",
	"block_not_found":        "o bloco não existe; confira os ids com email_read",
	"id_required":            "falta o id do bloco",
	"ref_unknown":            "o @ref não foi criado antes neste lote",
	"ref_duplicate":          "esse ref já foi usado neste lote",
	"ref_invalid":            "o ref usa letras minúsculas, números, - e _, até 32",
	"kind_required":          "add precisa de kind",
	"kind_unknown":           "kind não existe",
	"footer_locked":          "o rodapé fica sempre por último e não sai, não se move nem se duplica",
	"footer_exists":          "o e-mail já tem rodapé",
	"position_invalid":       "use um só entre after, before e into; into recebe um bloco de colunas e column só vale com into",
	"column_invalid":         "essa coluna não existe",
	"not_allowed_in_columns": "colunas e rodapé não entram dentro de colunas",
	"too_many_blocks":        "o e-mail chegaria ao limite de blocos",
	"fields_required":        "set e settings precisam de fields",
	"field_not_for_kind":     "esse campo não existe para esse bloco",
	"field_invalid":          "o valor do campo tem o tipo errado",
	"text_markup_invalid":    "a marcação do texto está incompleta (negrito, itálico, sublinhado ou link sem fechar)",
}

type editPayload struct {
	Base     emaildesign.Document    `json:"base"`
	Steps    []emaildesign.StepPatch `json:"steps"`
	Envelope *envelopeEdit           `json:"envelope,omitempty"`
}

type editReply struct {
	Document  json.RawMessage `json:"document"`
	Conflicts []string        `json:"conflicts"`
	Skipped   []int           `json:"skipped"`
	Subject   *string         `json:"subject,omitempty"`
	Preheader *string         `json:"preheader,omitempty"`
}

type batchOutcome struct {
	after     liveDesign
	batch     editBatch
	plan      emaildesign.Plan
	conflicts []string
	skipped   []int
	facts     map[string]interface{}
}

type emailEditTool struct {
	designScope
	deps Deps
}

func NewEmailEditTool(deps Deps) copilot.Tool {
	return &emailEditTool{deps: deps}
}

func (t *emailEditTool) Meta() copilot.Meta { return editMeta() }

func (t *emailEditTool) NeedsApproval(mode copilot.Mode) bool { return mode == copilot.ModeAsk }

func (t *emailEditTool) Definition() tools.Definition {
	def := copilottools.Definition("email_edit",
		"Edita o e-mail aberto (um design em Modelos ou o conteúdo de uma campanha), ao vivo no editor do usuário, com um lote de operações aplicadas em ordem, todas ou nenhuma. "+
			"Blocos: heading (text, level, align, color), text (text em marcação curta, align), button (label, href, align, full_width, color, text_color), "+
			"image (src ou media_id, alt, href, width, align), divider (color, thickness), spacer (height), columns (ratio; filhos entram com at.into), "+
			"social (links, align) e footer (company_name, address, text; um só, sempre por último). Todo bloco aceita padding e background_color. "+
			"settings muda o e-mail todo: width, background_color, content_background_color, text_color, link_color, button_color, button_text_color, font_family e preheader; numa campanha, também subject. "+
			"Textos aceitam merge tags como {{first_name|cliente}}, {{name}}, {{email}}, {{company}} e {{field.chave}}. "+
			"Lacunas que dependem do usuário (link de um botão, imagem, dados do rodapé, assunto) entram no rascunho e voltam em open_gaps; só um e-mail malformado é recusado. "+
			"O retorno traz created (ref para id), conflicts (onde o usuário mudou o mesmo ponto e a versão dele ficou) e review do resultado; numa campanha, campaign_status e returned_to_draft quando uma campanha pronta voltou a rascunho.",
		emailEditArgs{})
	if ops, ok := def.Parameters["operations"]; ok && ops.Items != nil {
		if fields, ok := ops.Items.Properties["fields"]; ok && fields.Items != nil {
			if font, ok := fields.Items.Properties["font_family"]; ok {
				font.Enum = emaildesign.FontFamilies()
				fields.Items.Properties["font_family"] = font
			}
			ops.Items.Properties["fields"] = fields
		}
		def.Parameters["operations"] = ops
	}
	return def
}

func (t *emailEditTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	batch, err := t.deps.operations(cc, args)
	if err != nil {
		return err
	}
	h, err := t.deps.openHolder(ctx, cc, forEditing)
	if err != nil {
		return fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	if err := batch.fits(h); err != nil {
		return fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	return nil
}

func (t *emailEditTool) Describe(_ context.Context, _ copilot.Context, args map[string]interface{}) []copilot.Field {
	var a emailEditArgs
	copilottools.BindArgs(args, &a)
	names := make([]string, 0, len(a.Operations))
	for _, op := range a.Operations {
		names = append(names, op.Op)
	}
	return []copilot.Field{
		{Key: "operations", Value: strconv.Itoa(len(names))},
		{Key: "steps", Value: strings.Join(names, ", ")},
	}
}

func (t *emailEditTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	batch, err := t.deps.operations(cc, args)
	if err != nil {
		return failure(err.Error())
	}
	h, err := t.deps.openHolder(ctx, cc, forEditing)
	if err != nil {
		return failure(err.Error())
	}
	outcome, err := t.deps.applyBatch(ctx, cc, h, batch)
	if err != nil {
		return screencall.Result(cc, err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: withOpenGaps(outcome.data(), outcome.after)}
}

func (o batchOutcome) data() map[string]interface{} {
	data := map[string]interface{}{
		"applied": o.batch.requested,
		"created": o.plan.Refs,
		"review":  o.after.review(),
	}
	if len(o.plan.Created) > 0 {
		data["new_blocks"] = o.plan.Created
	}
	if len(o.conflicts) > 0 {
		data["conflicts"] = o.conflicts
	}
	if len(o.skipped) > 0 {
		data["skipped"] = o.skipped
	}
	return withFacts(data, o.facts)
}

func (d Deps) operations(cc copilot.Context, args map[string]interface{}) (editBatch, error) {
	var a emailEditArgs
	if err := copilottools.DecodeArgs(args, &a); err != nil {
		return editBatch{}, fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	if len(a.Operations) == 0 {
		return editBatch{}, fmt.Errorf("%w: %s", copilottools.ErrInvalidArgs, emailNoOperations)
	}
	ops := make([]emaildesign.Operation, 0, len(a.Operations))
	for i, raw := range a.Operations {
		op, err := d.operation(cc, raw)
		if err != nil {
			return editBatch{}, fmt.Errorf("%w: operação %d: %v", copilottools.ErrInvalidArgs, i+1, err)
		}
		ops = append(ops, op)
	}
	batch, err := splitEnvelope(ops)
	if err != nil {
		return editBatch{}, fmt.Errorf("%w: %v", copilottools.ErrInvalidArgs, err)
	}
	return batch, nil
}

func knownOperation(name string) bool {
	for _, kind := range emaildesign.OperationKinds() {
		if string(kind) == name {
			return true
		}
	}
	return false
}

func (d Deps) operation(cc copilot.Context, a emailOperationArgs) (emaildesign.Operation, error) {
	if !knownOperation(a.Op) {
		return emaildesign.Operation{}, fmt.Errorf("op %q não existe", a.Op)
	}
	op := emaildesign.Operation{Op: emaildesign.OperationKind(a.Op), Ref: strings.TrimSpace(a.Ref), ID: strings.TrimSpace(a.ID), Kind: emaildesign.BlockKind(strings.TrimSpace(a.Kind))}
	if a.At != nil {
		op.At = &emaildesign.Placement{After: a.At.After, Before: a.At.Before, Into: a.At.Into, Column: a.At.Column}
	}
	if a.Fields == nil {
		return op, nil
	}
	fields, err := d.fields(cc, *a.Fields)
	if err != nil {
		return emaildesign.Operation{}, err
	}
	op.Fields = fields
	return op, nil
}

func (d Deps) fields(cc copilot.Context, a emailFieldArgs) (map[string]json.RawMessage, error) {
	if a.MediaID != nil {
		if a.Src != nil {
			return nil, errors.New(emailMediaAndSrc)
		}
		src, err := d.mediaSource(cc.WorkspaceID, *a.MediaID)
		if err != nil {
			return nil, err
		}
		a.Src, a.MediaID = &src, nil
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (d Deps) mediaSource(workspaceID, mediaID string) (string, error) {
	id := strings.TrimSpace(mediaID)
	if _, err := uuid.Parse(id); err != nil || d.Library == nil {
		return "", fmt.Errorf(emailMediaUnknown, id)
	}
	m, err := d.Library.GetMedia(workspaceID, id)
	if err != nil || m == nil || m.Type != media.MediaTypeProductImage {
		return "", fmt.Errorf(emailMediaUnknown, id)
	}
	if issues := emaildesign.Validate(probeImage(m.URL)); hasCode(issues, "image_src_not_https") || hasCode(issues, "image_src_missing") {
		return "", fmt.Errorf(emailMediaUnsafe, id)
	}
	return m.URL, nil
}

func probeImage(src string) emaildesign.Document {
	return emaildesign.Document{Version: emaildesign.CurrentVersion, Blocks: []emaildesign.Block{
		{ID: "probe", Kind: emaildesign.KindImage, Image: &emaildesign.Image{Src: src, Width: emaildesign.MaxImageWidth}},
	}}
}

func hasCode(issues []emaildesign.Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}

func issueKey(issue emaildesign.Issue) string {
	return issue.Code + "|" + issue.BlockID + "|" + issue.Field
}

func addedIssues(before, after []emaildesign.Issue) []emaildesign.Issue {
	seen := map[string]bool{}
	for _, issue := range before {
		seen[issueKey(issue)] = true
	}
	var added []emaildesign.Issue
	for _, issue := range after {
		if !seen[issueKey(issue)] {
			added = append(added, issue)
		}
	}
	return added
}

func describeIssues(issues []emaildesign.Issue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		part := issue.Code
		if issue.BlockID != "" {
			part += " no bloco " + issue.BlockID
		}
		if issue.Field != "" {
			part += " (" + issue.Field + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}

func operationFailure(err error) error {
	var opErr *emaildesign.OperationError
	if !errors.As(err, &opErr) {
		return errors.New(emailUnreadable)
	}
	words := operationWords[opErr.Code]
	if words == "" {
		words = "a operação não pôde ser aplicada"
	}
	code := opErr.Code
	if opErr.Field != "" {
		code += ", campo " + opErr.Field
	}
	return fmt.Errorf(emailOpFailed, opErr.Index+1, code, words)
}

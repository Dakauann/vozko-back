package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/sip_trunk"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
)

const phoneLinePasswordKey = "password"

type phoneLineLister interface {
	Execute(ctx context.Context, workspaceID string) ([]*sip_trunk.SIPTrunk, error)
}

type phoneLineGetter interface {
	Execute(ctx context.Context, workspaceID, id string) (*sip_trunk.SIPTrunk, error)
}

type phoneLineCreator interface {
	Execute(ctx context.Context, input sip_trunk_usecase.CreateTrunkInput) (*sip_trunk.SIPTrunk, error)
}

type phoneLineUpdater interface {
	Execute(ctx context.Context, input sip_trunk_usecase.UpdateTrunkInput) (*sip_trunk.SIPTrunk, error)
}

type phoneLineDeleter interface {
	Execute(ctx context.Context, workspaceID, id string) error
}

type PhoneLineDeps struct {
	List   phoneLineLister
	Get    phoneLineGetter
	Create phoneLineCreator
	Update phoneLineUpdater
	Delete phoneLineDeleter
}

func phoneLinesMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceSIPTrunks, Action: action}
}

func (d PhoneLineDeps) line(ctx context.Context, cc copilot.Context, id string) (*sip_trunk.SIPTrunk, error) {
	lineID, err := knownID(id, "line_id", "list_phone_lines")
	if err != nil {
		return nil, err
	}
	return d.Get.Execute(ctx, cc.WorkspaceID, lineID)
}

func (d PhoneLineDeps) describeLine(ctx context.Context, cc copilot.Context, id string) copilot.Field {
	if line, err := d.line(ctx, cc, id); err == nil {
		return copilot.Field{Key: "phoneLine", Value: line.Name}
	}
	return copilot.Field{Key: "phoneLine", Value: "linha desconhecida"}
}

var lineDirections = map[sip_trunk.TrunkType]string{
	sip_trunk.TrunkTypeBidirectional: "faz e recebe ligações",
	sip_trunk.TrunkTypeOutbound:      "só faz ligações",
	sip_trunk.TrunkTypeInbound:       "só recebe ligações",
}

var lineStatuses = map[sip_trunk.RegistrationStatus]string{
	sip_trunk.RegistrationStatusRegistered:   "conectada à operadora",
	sip_trunk.RegistrationStatusRegistering:  "conectando à operadora",
	sip_trunk.RegistrationStatusFailed:       "a operadora recusou a conexão",
	sip_trunk.RegistrationStatusUnregistered: "desconectada",
}

func lineStatus(line *sip_trunk.SIPTrunk) string {
	switch {
	case !line.Enabled:
		return "desligada"
	case line.Settings.SkipRegistration:
		return "autenticada pelo endereço IP, sem conexão por senha"
	}
	if status, ok := lineStatuses[line.RegistrationStatus]; ok {
		return status
	}
	return "desconectada"
}

func phoneLineData(line *sip_trunk.SIPTrunk) map[string]interface{} {
	data := map[string]interface{}{
		"line_id":      line.ID,
		"name":         line.Name,
		"direction":    lineDirections[line.TrunkType],
		"host":         line.Host,
		"port":         line.Port,
		"transport":    line.Transport,
		"username":     line.Username,
		"enabled":      line.Enabled,
		"status":       lineStatus(line),
		"has_password": line.Password != "",
	}
	if line.LastError != "" {
		data["last_error"] = line.LastError
	}
	return data
}

type listPhoneLinesTool struct{ deps PhoneLineDeps }

func NewListPhoneLinesTool(deps PhoneLineDeps) copilot.Tool { return &listPhoneLinesTool{deps: deps} }

func (t *listPhoneLinesTool) Meta() copilot.Meta { return phoneLinesMeta(workspace.ActionRead, false) }

func (t *listPhoneLinesTool) Definition() tools.Definition {
	return definition("list_phone_lines",
		"Lista as linhas telefônicas do workspace (contas SIP da operadora): se fazem ou recebem ligações, se estão ligadas "+
			"e se a operadora aceitou a conexão agora, com o último erro quando houver. Nunca traz a senha.",
		struct{}{})
}

func (t *listPhoneLinesTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	lines, err := t.deps.List.Execute(ctx, cc.WorkspaceID)
	if err != nil {
		return phoneLineFailure("list_phone_lines", err)
	}
	out := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		out = append(out, phoneLineData(line))
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"lines": out}}
}

type createPhoneLineArgs struct {
	Name      string `json:"name" req:"true" desc:"nome da linha, como a equipe vai reconhecê-la"`
	Host      string `json:"host" req:"true" desc:"endereço do servidor SIP da operadora, sem porta nem sip:"`
	Port      int    `json:"port" desc:"porta do servidor; omita para a padrão 5060"`
	Domain    string `json:"domain" desc:"domínio SIP, só se a operadora pedir um diferente do endereço"`
	Transport string `json:"transport" enum:"UDP,TCP" desc:"transporte; omita para UDP"`
	Direction string `json:"direction" enum:"BIDIRECTIONAL,OUTBOUND,INBOUND" desc:"faz e recebe (BIDIRECTIONAL, padrão), só faz (OUTBOUND) ou só recebe (INBOUND)"`
	Username  string `json:"username" req:"true" desc:"usuário da conta SIP que a operadora forneceu"`
	Enabled   *bool  `json:"enabled" desc:"ligar a linha já ao criar e testar a conexão; padrão true"`
}

func (a createPhoneLineArgs) input(workspaceID, password string) sip_trunk_usecase.CreateTrunkInput {
	enabled := a.Enabled == nil || *a.Enabled
	return sip_trunk_usecase.CreateTrunkInput{
		WorkspaceID: workspaceID,
		Name:        a.Name,
		TrunkType:   sip_trunk.TrunkType(a.Direction),
		Host:        a.Host,
		Port:        a.Port,
		Domain:      a.Domain,
		Transport:   sip_trunk.Transport(a.Transport),
		Username:    a.Username,
		Password:    password,
		Enabled:     enabled,
	}
}

type createPhoneLineTool struct{ deps PhoneLineDeps }

func NewCreatePhoneLineTool(deps PhoneLineDeps) copilot.Tool { return &createPhoneLineTool{deps: deps} }

func (t *createPhoneLineTool) Meta() copilot.Meta {
	return phoneLinesMeta(workspace.ActionCreate, true)
}

func (t *createPhoneLineTool) Definition() tools.Definition {
	return definition("create_phone_line",
		"Conecta uma linha telefônica nova (conta SIP da operadora) e testa a conexão. Só depois da aprovação do usuário. "+
			"A senha NUNCA passa por você: o usuário digita no cartão de aprovação, num campo protegido. Nunca peça a senha no chat; "+
			"se ele escrever a senha na conversa, avise que ela deve ir só no cartão. Linhas autenticadas pelo endereço IP, sem senha, "+
			"são criadas na tela de linhas telefônicas.",
		createPhoneLineArgs{})
}

func (t *createPhoneLineTool) Secrets(map[string]interface{}) []copilot.SecretField {
	return []copilot.SecretField{{Key: phoneLinePasswordKey, Label: "Senha da linha"}}
}

func (t *createPhoneLineTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[createPhoneLineArgs](nil, cc, args)
	if err != nil {
		return err
	}
	return phoneLineInputError(sip_trunk_usecase.DraftTrunk(a.input(cc.WorkspaceID, "")).ValidateWithoutPassword())
}

func (t *createPhoneLineTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createPhoneLineArgs
	bindArgs(args, &a)
	line := sip_trunk_usecase.DraftTrunk(a.input(cc.WorkspaceID, ""))
	return []copilot.Field{
		{Key: "name", Value: line.Name},
		{Key: "server", Value: serverOf(line)},
		{Key: "username", Value: line.Username},
		{Key: "direction", Value: lineDirections[line.TrunkType]},
		{Key: "enabled", Value: strconv.FormatBool(line.Enabled)},
	}
}

func (t *createPhoneLineTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createPhoneLineArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	password, _ := args[phoneLinePasswordKey].(string)
	line, err := t.deps.Create.Execute(ctx, a.input(cc.WorkspaceID, password))
	if err != nil {
		return phoneLineFailure("create_phone_line", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: connectionOutcome(line)}
}

func connectionOutcome(line *sip_trunk.SIPTrunk) map[string]interface{} {
	data := phoneLineData(line)
	switch {
	case !line.Enabled:
		data["next_step"] = "a linha foi salva desligada; ligue-a para testar a conexão"
	case line.RegistrationStatus == sip_trunk.RegistrationStatusFailed:
		data["next_step"] = "a operadora recusou a conexão: explique o last_error em palavras simples e sugira conferir endereço, usuário e senha (a senha é trocada com change_phone_line_password)"
	case line.RegistrationStatus == sip_trunk.RegistrationStatusRegistered:
		data["next_step"] = "a linha está conectada e pronta"
	default:
		data["next_step"] = "a conexão ainda está sendo testada; confira em alguns segundos com list_phone_lines"
	}
	return data
}

func serverOf(line *sip_trunk.SIPTrunk) string {
	port := line.Port
	if port == 0 {
		port = 5060
	}
	return fmt.Sprintf("%s:%d (%s)", line.Host, port, line.Transport)
}

type phoneLineRefArgs struct {
	LineID string `json:"line_id" req:"true" id:"true" desc:"line_id exato de list_phone_lines"`
}

type updatePhoneLineArgs struct {
	phoneLineRefArgs
	Name      string `json:"name" desc:"novo nome, se mudar"`
	Host      string `json:"host" desc:"novo endereço do servidor, se mudar"`
	Port      int    `json:"port" desc:"nova porta, se mudar"`
	Domain    string `json:"domain" desc:"novo domínio SIP, se mudar"`
	Transport string `json:"transport" enum:"UDP,TCP" desc:"novo transporte, se mudar"`
	Direction string `json:"direction" enum:"BIDIRECTIONAL,OUTBOUND,INBOUND" desc:"nova direção, se mudar"`
	Username  string `json:"username" desc:"novo usuário, se mudar"`
	Enabled   *bool  `json:"enabled" desc:"ligar (true) ou desligar (false) a linha"`
}

func (a updatePhoneLineArgs) input(workspaceID string) (sip_trunk_usecase.UpdateTrunkInput, error) {
	in := sip_trunk_usecase.UpdateTrunkInput{WorkspaceID: workspaceID, ID: strings.TrimSpace(a.LineID), Enabled: a.Enabled}
	in.Name = changedText(a.Name)
	in.Host = changedText(a.Host)
	in.Domain = changedText(a.Domain)
	in.Username = changedText(a.Username)
	if a.Port != 0 {
		in.Port = &a.Port
	}
	if a.Transport != "" {
		transport := sip_trunk.Transport(a.Transport)
		in.Transport = &transport
	}
	if a.Direction != "" {
		direction := sip_trunk.TrunkType(a.Direction)
		in.TrunkType = &direction
	}
	if in.Name == nil && in.Host == nil && in.Domain == nil && in.Username == nil && in.Port == nil &&
		in.Transport == nil && in.TrunkType == nil && in.Enabled == nil {
		return in, fmt.Errorf("%w: informe o que muda na linha", errInvalidArgs)
	}
	return in, nil
}

func changedText(raw string) *string {
	if value := strings.TrimSpace(raw); value != "" {
		return &value
	}
	return nil
}

type updatePhoneLineTool struct{ deps PhoneLineDeps }

func NewUpdatePhoneLineTool(deps PhoneLineDeps) copilot.Tool { return &updatePhoneLineTool{deps: deps} }

func (t *updatePhoneLineTool) Meta() copilot.Meta {
	return phoneLinesMeta(workspace.ActionUpdate, true)
}

func (t *updatePhoneLineTool) Definition() tools.Definition {
	return definition("update_phone_line",
		"Muda o nome, o servidor, o usuário, a direção de uma linha telefônica, ou liga e desliga a linha, e testa a conexão de novo. "+
			"Não muda a senha: para isso use change_phone_line_password. Só depois da aprovação do usuário.",
		updatePhoneLineArgs{})
}

func (t *updatePhoneLineTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[updatePhoneLineArgs](nil, cc, args)
	if err != nil {
		return err
	}
	in, err := a.input(cc.WorkspaceID)
	if err != nil {
		return err
	}
	line, err := t.deps.line(ctx, cc, a.LineID)
	if err != nil {
		return phoneLineInputError(err)
	}
	sip_trunk_usecase.ApplyUpdate(line, in)
	return phoneLineInputError(line.Validate())
}

func (t *updatePhoneLineTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a updatePhoneLineArgs
	bindArgs(args, &a)
	fields := []copilot.Field{t.deps.describeLine(ctx, cc, a.LineID)}
	in, err := a.input(cc.WorkspaceID)
	if err != nil {
		return fields
	}
	for _, change := range []struct {
		key   string
		value *string
	}{{"newName", in.Name}, {"host", in.Host}, {"domain", in.Domain}, {"username", in.Username}} {
		if change.value != nil {
			fields = append(fields, copilot.Field{Key: change.key, Value: *change.value})
		}
	}
	if in.Port != nil {
		fields = append(fields, copilot.Field{Key: "port", Value: strconv.Itoa(*in.Port)})
	}
	if in.Transport != nil {
		fields = append(fields, copilot.Field{Key: "transport", Value: string(*in.Transport)})
	}
	if in.TrunkType != nil {
		fields = append(fields, copilot.Field{Key: "direction", Value: lineDirections[*in.TrunkType]})
	}
	if in.Enabled != nil {
		fields = append(fields, copilot.Field{Key: "enabled", Value: strconv.FormatBool(*in.Enabled)})
	}
	return fields
}

func (t *updatePhoneLineTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a updatePhoneLineArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	in, err := a.input(cc.WorkspaceID)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	line, err := t.deps.Update.Execute(ctx, in)
	if err != nil {
		return phoneLineFailure("update_phone_line", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: connectionOutcome(line)}
}

type changePhoneLinePasswordTool struct{ deps PhoneLineDeps }

func NewChangePhoneLinePasswordTool(deps PhoneLineDeps) copilot.Tool {
	return &changePhoneLinePasswordTool{deps: deps}
}

func (t *changePhoneLinePasswordTool) Meta() copilot.Meta {
	return phoneLinesMeta(workspace.ActionUpdate, true)
}

func (t *changePhoneLinePasswordTool) Definition() tools.Definition {
	return definition("change_phone_line_password",
		"Troca a senha de uma linha telefônica e testa a conexão de novo. A senha NUNCA passa por você: o usuário digita no "+
			"cartão de aprovação, num campo protegido. Nunca peça a senha no chat. Só depois da aprovação do usuário.",
		phoneLineRefArgs{})
}

func (t *changePhoneLinePasswordTool) Secrets(map[string]interface{}) []copilot.SecretField {
	return []copilot.SecretField{{Key: phoneLinePasswordKey, Label: "Nova senha da linha"}}
}

func (t *changePhoneLinePasswordTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[phoneLineRefArgs](nil, cc, args)
	if err != nil {
		return err
	}
	_, err = t.deps.line(ctx, cc, a.LineID)
	return phoneLineInputError(err)
}

func (t *changePhoneLinePasswordTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a phoneLineRefArgs
	bindArgs(args, &a)
	return []copilot.Field{t.deps.describeLine(ctx, cc, a.LineID)}
}

func (t *changePhoneLinePasswordTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a phoneLineRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	password, _ := args[phoneLinePasswordKey].(string)
	if password == "" {
		return copilot.Result{Status: copilot.StatusError, Message: "a nova senha não foi preenchida; nada mudou"}
	}
	line, err := t.deps.Update.Execute(ctx, sip_trunk_usecase.UpdateTrunkInput{
		WorkspaceID: cc.WorkspaceID,
		ID:          strings.TrimSpace(a.LineID),
		Password:    &password,
	})
	if err != nil {
		return phoneLineFailure("change_phone_line_password", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: connectionOutcome(line)}
}

type deletePhoneLineTool struct{ deps PhoneLineDeps }

func NewDeletePhoneLineTool(deps PhoneLineDeps) copilot.Tool { return &deletePhoneLineTool{deps: deps} }

func (t *deletePhoneLineTool) Meta() copilot.Meta {
	return phoneLinesMeta(workspace.ActionDelete, true)
}

func (t *deletePhoneLineTool) Definition() tools.Definition {
	return definition("delete_phone_line",
		"Remove uma linha telefônica; as ligações em andamento por ela são encerradas. Só depois da aprovação do usuário.",
		phoneLineRefArgs{})
}

func (t *deletePhoneLineTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[phoneLineRefArgs](nil, cc, args)
	if err != nil {
		return err
	}
	_, err = t.deps.line(ctx, cc, a.LineID)
	return phoneLineInputError(err)
}

func (t *deletePhoneLineTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a phoneLineRefArgs
	bindArgs(args, &a)
	return []copilot.Field{t.deps.describeLine(ctx, cc, a.LineID), {Key: "risks", Value: "As ligações em andamento por esta linha são encerradas."}}
}

func (t *deletePhoneLineTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a phoneLineRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.Delete.Execute(ctx, cc.WorkspaceID, strings.TrimSpace(a.LineID)); err != nil {
		return phoneLineFailure("delete_phone_line", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deleted": true}}
}

var phoneLineInputMessages = []struct {
	err     error
	message string
}{
	{sip_trunk.ErrTrunkNotFound, "linha não encontrada; use um line_id de list_phone_lines"},
	{sip_trunk.ErrNameRequired, "a linha precisa de um nome"},
	{sip_trunk.ErrHostRequired, "informe o endereço do servidor SIP da operadora"},
	{sip_trunk.ErrInvalidHost, "o endereço do servidor deve ser só o nome ou o IP, sem porta, sem sip: e sem espaços"},
	{sip_trunk.ErrInvalidPort, "a porta deve estar entre 1 e 65535"},
	{sip_trunk.ErrCredentialsRequired, "a linha precisa do usuário e da senha da conta SIP"},
	{sip_trunk.ErrUnsupportedTransport, "o transporte deve ser UDP ou TCP"},
	{sip_trunk.ErrUnsupportedTrunkType, "a direção deve ser BIDIRECTIONAL, OUTBOUND ou INBOUND"},
	{sip_trunk.ErrHostNotPublic, "o servidor precisa ser um endereço público da internet; endereços internos da rede não são aceitos"},
}

func phoneLineInputError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range phoneLineInputMessages {
		if errors.Is(err, known.err) {
			return fmt.Errorf("%w: %s", errInvalidArgs, known.message)
		}
	}
	if sip_trunk.IsInvalidInput(err) {
		return fmt.Errorf("%w: %s", errInvalidArgs, err.Error())
	}
	return err
}

func phoneLineFailure(tool string, err error) copilot.Result {
	if inputErr := phoneLineInputError(err); errors.Is(inputErr, errInvalidArgs) {
		return copilot.Result{Status: copilot.StatusError, Message: inputErr.Error()}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível mudar as linhas telefônicas agora"}
}

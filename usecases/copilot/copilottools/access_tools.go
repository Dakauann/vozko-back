package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type AccessDeps struct {
	Diagnose workspace.DiagnoseAccessUseCase
}

func accessMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAIChat, Action: workspace.ActionRead}
}

type explainPermissionArgs struct {
	Permission string `json:"permission" req:"true" desc:"permissão no formato recurso:ação, como stages:assign; use list_permission_catalog para ver todas"`
}

type explainPermissionTool struct{}

func NewExplainPermissionTool() copilot.Tool { return explainPermissionTool{} }

func (explainPermissionTool) Meta() copilot.Meta { return accessMeta() }

func (explainPermissionTool) Definition() tools.Definition {
	return definition("explain_permission",
		"Explica uma permissão: o que ela libera, em quais funcionalidades e telas é usada, de quais outras permissões depende "+
			"e quais riscos traz. Use para responder 'o que essa permissão faz?' e antes de sugerir conceder uma permissão.",
		explainPermissionArgs{})
}

func (explainPermissionTool) Execute(_ context.Context, _ copilot.Context, args map[string]interface{}) copilot.Result {
	var a explainPermissionArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	entries, err := parsePermissions([]string{a.Permission})
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	entry := entries[0]
	data := map[string]interface{}{
		"permission":  entry.Key(),
		"description": workspace.DescribePermission(entry),
		"requires":    describedPermissions(workspace.RequirementsOf(entry)),
		"risks":       riskDescriptions(entry),
		"used_by":     featureUses(workspace.CapabilitiesUsing(entry)),
	}
	if len(workspace.CapabilitiesUsing(entry)) == 0 {
		data["effect"] = "Esta permissão ainda não libera nenhuma funcionalidade do produto; concedê-la não muda o que o membro vê ou faz."
	}
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

type diagnoseAccessArgs struct {
	Feature string `json:"feature" req:"true" desc:"funcionalidade a verificar"`
	UserID  string `json:"user_id" id:"true" desc:"user_id de list_workspace_members; omita para verificar o próprio usuário"`
}

type diagnoseAccessTool struct{ deps AccessDeps }

func NewDiagnoseAccessTool(deps AccessDeps) copilot.Tool { return diagnoseAccessTool{deps: deps} }

func (diagnoseAccessTool) Meta() copilot.Meta { return accessMeta() }

func (diagnoseAccessTool) Definition() tools.Definition {
	def := definition("diagnose_access",
		"Verifica o acesso de um membro a uma funcionalidade: o que ele consegue ver e fazer, quais permissões faltam para cada ação "+
			"e se departamentos ou a atribuição de conversas limitam o que aparece para ele. Use sempre antes de responder perguntas como "+
			"'por que não vejo...' ou 'por que não consigo...'; nunca responda de memória. Ver uma tela nunca depende de permissões de ação: "+
			"explique separadamente o que falta para ver e o que falta para agir. Funcionalidades: "+featureIndex()+".",
		diagnoseAccessArgs{})
	param := def.Parameters["feature"]
	for _, f := range workspace.Features {
		param.Enum = append(param.Enum, string(f.Key))
	}
	def.Parameters["feature"] = param
	return def
}

func (t diagnoseAccessTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a diagnoseAccessArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if _, ok := workspace.FeatureByKey(workspace.FeatureKey(a.Feature)); !ok {
		return copilot.Result{Status: copilot.StatusError, Message: "funcionalidade desconhecida; use uma das listadas na ferramenta"}
	}
	d, err := t.deps.Diagnose.Execute(workspace.DiagnoseAccessInput{
		ActorID: cc.UserID, WorkspaceID: cc.WorkspaceID, MemberUserID: strings.TrimSpace(a.UserID),
		CallerRole: platformRole(cc), Feature: workspace.FeatureKey(a.Feature),
	})
	if err != nil {
		return accessFailure("diagnose_access", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: diagnosisData(d)}
}

type openScreenArgs struct {
	Screen string `json:"screen" req:"true" desc:"tela a abrir; use as telas retornadas por diagnose_access ou explain_permission"`
	ID     string `json:"id" id:"true" desc:"id do item quando a tela é de um item específico, como um agente ou uma campanha"`
}

type openScreenTool struct{ deps AccessDeps }

func NewOpenScreenTool(deps AccessDeps) copilot.Tool { return openScreenTool{deps: deps} }

func (openScreenTool) Meta() copilot.Meta { return accessMeta() }

func (openScreenTool) Definition() tools.Definition {
	def := definition("open_screen",
		"Mostra na conversa um botão que leva o usuário a uma tela do produto. Só funciona para telas que o usuário pode abrir; "+
			"quando ele não pode, a resposta diz o que falta. Depois de chamar, diga em uma frase o que ele encontra na tela.",
		openScreenArgs{})
	param := def.Parameters["screen"]
	for _, s := range sortedScreens() {
		param.Enum = append(param.Enum, string(s))
	}
	def.Parameters["screen"] = param
	return def
}

func (t openScreenTool) Execute(_ context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a openScreenArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	screen := workspace.Screen(a.Screen)
	use, ok := workspace.CapabilityForScreen(screen)
	if !ok {
		return copilot.Result{Status: copilot.StatusError, Message: "tela desconhecida; use uma das listadas na ferramenta"}
	}
	card, err := copilot.NewNavigationCard(screen, strings.TrimSpace(a.ID))
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: "essa tela precisa do id exato do item, e só dele"}
	}
	d, err := t.deps.Diagnose.Execute(workspace.DiagnoseAccessInput{
		ActorID: cc.UserID, WorkspaceID: cc.WorkspaceID, CallerRole: platformRole(cc), Feature: use.Feature.Key,
	})
	if err != nil {
		return accessFailure("open_screen", err)
	}
	status, found := capabilityStatus(d, use.Capability.Key)
	if !found || !status.Allowed {
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário não pode abrir essa tela", Data: map[string]interface{}{
			"screen": a.Screen, "managers_only": status.ManagersOnly, "missing": describedPermissions(status.Missing),
		}}
	}
	return copilot.Result{Status: copilot.StatusOK, Card: card, Data: map[string]interface{}{
		"card_shown": a.Screen, "feature": use.Feature.Name, "location": use.Feature.Location,
	}}
}

func capabilityStatus(d *workspace.AccessDiagnosis, key workspace.CapabilityKey) (workspace.CapabilityStatus, bool) {
	for _, c := range d.Capabilities {
		if c.Key == key {
			return c, true
		}
	}
	return workspace.CapabilityStatus{}, false
}

func diagnosisData(d *workspace.AccessDiagnosis) map[string]interface{} {
	capabilities := make([]map[string]interface{}, 0, len(d.Capabilities))
	for _, c := range d.Capabilities {
		entry := map[string]interface{}{"capability": string(c.Key), "description": c.Description, "allowed": c.Allowed}
		if len(c.Missing) > 0 {
			entry["missing"] = describedPermissions(c.Missing)
		}
		if c.ManagersOnly {
			entry["managers_only"] = true
		}
		if len(c.Screens) > 0 {
			entry["screen"] = string(c.Screens[0])
		}
		capabilities = append(capabilities, entry)
	}
	scope := make([]map[string]interface{}, 0, len(d.Scope))
	for _, s := range d.Scope {
		scope = append(scope, map[string]interface{}{"kind": string(s.Kind), "description": s.Description})
	}
	return map[string]interface{}{
		"member":       memberLabel(d.Member),
		"role":         roleLabel(d.Member),
		"feature":      d.Name,
		"location":     d.Location,
		"full_access":  d.FullAccess,
		"capabilities": capabilities,
		"scope":        scope,
	}
}

func describedPermissions(entries []workspace.PermissionEntry) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]interface{}{"permission": e.Key(), "description": workspace.DescribePermission(e)})
	}
	return out
}

func riskDescriptions(entry workspace.PermissionEntry) []string {
	risks := workspace.RisksOf(entry)
	out := make([]string, 0, len(risks))
	for _, r := range risks {
		out = append(out, r.Description)
	}
	return out
}

func featureUses(uses []workspace.FeatureUse) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(uses))
	for _, u := range uses {
		entry := map[string]interface{}{
			"feature": u.Feature.Name, "location": u.Feature.Location,
			"capability": string(u.Capability.Key), "description": u.Capability.Description,
		}
		if len(u.Capability.Screens) > 0 {
			entry["screen"] = string(u.Capability.Screens[0])
		}
		out = append(out, entry)
	}
	return out
}

func featureIndex() string {
	parts := make([]string, 0, len(workspace.Features))
	for _, f := range workspace.Features {
		parts = append(parts, fmt.Sprintf("%s (%s)", f.Key, f.Name))
	}
	return strings.Join(parts, ", ")
}

func sortedScreens() []workspace.Screen {
	screens := workspace.Screens()
	sort.Slice(screens, func(i, j int) bool { return screens[i] < screens[j] })
	return screens
}

func accessFailure(tool string, err error) copilot.Result {
	switch {
	case errors.Is(err, workspace.ErrInsufficientPermissions), errors.Is(err, workspace.ErrUnauthorized):
		return copilot.Result{Status: copilot.StatusDenied, Message: "o usuário só pode verificar o próprio acesso; para verificar outro membro é preciso poder ver os membros do workspace"}
	case errors.Is(err, workspace.ErrUnknownFeature):
		return copilot.Result{Status: copilot.StatusError, Message: "funcionalidade desconhecida; use uma das listadas na ferramenta"}
	case errors.Is(err, workspace.ErrMemberNotFound):
		return copilot.Result{Status: copilot.StatusError, Message: errUnknownWorkspaceMember.Error()}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível verificar o acesso agora"}
}

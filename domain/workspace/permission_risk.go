package workspace

import "sort"

type RiskKind string

const (
	RiskSpendsBalance     RiskKind = "spends_balance"
	RiskContactsCustomers RiskKind = "contacts_customers"
	RiskManagesAccess     RiskKind = "manages_access"
	RiskChangesBilling    RiskKind = "changes_billing"
	RiskDeletesData       RiskKind = "deletes_data"
	RiskSensitiveData     RiskKind = "sensitive_data"
	RiskChangesAutomation RiskKind = "changes_automation"
	RiskConnectsAccounts  RiskKind = "connects_accounts"
)

type RiskLevel string

const (
	RiskHigh   RiskLevel = "high"
	RiskMedium RiskLevel = "medium"
)

var riskLevels = map[RiskKind]RiskLevel{
	RiskSpendsBalance:     RiskHigh,
	RiskContactsCustomers: RiskHigh,
	RiskManagesAccess:     RiskHigh,
	RiskChangesBilling:    RiskHigh,
	RiskDeletesData:       RiskHigh,
	RiskSensitiveData:     RiskMedium,
	RiskChangesAutomation: RiskMedium,
	RiskConnectsAccounts:  RiskMedium,
}

var riskDescriptions = map[RiskKind]string{
	RiskSpendsBalance:     "Gera consumo do saldo do workspace.",
	RiskContactsCustomers: "Permite enviar mensagens a clientes em nome da empresa, inclusive em massa e para contatos novos.",
	RiskManagesAccess:     "Permite conceder, alterar e revogar o acesso de membros ao workspace.",
	RiskChangesBilling:    "Permite alterar o plano e as condições de cobrança do workspace.",
	RiskDeletesData:       "Permite excluir dados de forma permanente, sem possibilidade de recuperação.",
	RiskSensitiveData:     "Concede acesso a dados sensíveis, como conversas de outros membros, gravações, informações financeiras e relatórios.",
	RiskChangesAutomation: "Permite alterar o comportamento de agentes de IA e automações no atendimento a clientes.",
	RiskConnectsAccounts:  "Permite conectar e desconectar canais e integrações da empresa.",
}

type Risk struct {
	Kind        RiskKind  `json:"kind"`
	Level       RiskLevel `json:"level"`
	Description string    `json:"description"`
}

func RisksOf(p PermissionEntry) []Risk {
	var kinds []RiskKind
	for _, def := range ResourceActions[p.Resource] {
		if def.ActionName == p.Action {
			kinds = def.Risks
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	out := make([]Risk, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, Risk{Kind: k, Level: riskLevels[k], Description: riskDescriptions[k]})
	}
	return out
}

type RiskyPermission struct {
	Permission PermissionEntry `json:"permission"`
	Risks      []Risk          `json:"risks"`
}

func (r RiskyPermission) Level() RiskLevel {
	for _, risk := range r.Risks {
		if risk.Level == RiskHigh {
			return RiskHigh
		}
	}
	return RiskMedium
}

func RiskyPermissions(permissions []PermissionEntry) []RiskyPermission {
	out := make([]RiskyPermission, 0)
	for _, p := range permissions {
		if risks := RisksOf(p); len(risks) > 0 {
			out = append(out, RiskyPermission{Permission: p, Risks: risks})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Permission.Key() < out[j].Permission.Key() })
	return out
}

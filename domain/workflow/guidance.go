package workflow

// Per-node guidance now lives ON each node's Definition() (the NodeGuidance field
// on NodeDefinition), authored by the executor that owns the node, so it travels
// with the catalog the AI Workflow Builder always sees. This file keeps only the
// GLOBAL, cross-node variable-system guide.

// VariableSystemGuide explains the runtime variable/expression system to the AI
// so it references upstream data correctly instead of guessing. Surfaced in the
// builder system prompt. Mirrors the n8n-style dot-notation expression standard.
func VariableSystemGuide() string {
	return `SISTEMA DE VARIÁVEIS ({{ ... }}), como referenciar dados em tempo de execução:

CHAVES DE SAÍDA: todo nó executado grava um conjunto de "chaves de saída" (listadas no catálogo e em get_node_spec). Referencie-as por:
- {{node.<id>.<chave>}} → chave de saída de QUALQUER nó ANTERIOR, pelo id. Forma preferida (explícita e estável). Ex.: {{node.n4.logradouro}}.
- {{last.<chave>}}      → mesma ideia, porém só do nó IMEDIATAMENTE anterior. Ex.: {{last.status_code}}.

CASOS COMUNS:
- Resposta de um agente de IA: a chave de saída é response_text → use {{node.<idDoAgente>.response_text}} (ou {{ai.response_text}}).
- Argumentos de uma ferramenta chamada pelo agente (tool_mode=route): CADA argumento é uma chave de saída → {{node.<idDoAgente>.<arg>}} (ex.: {{node.n2.cep}}). O objeto inteiro fica em tool_args.
- Resposta JSON de um http_request: CADA campo do JSON vira uma chave de saída → {{node.<id>.<campo>}}.
- Objeto retornado por um nó code: cada campo vira uma chave de saída → {{node.<id>.<campo>}}.

OUTROS ESCOPOS:
- {{var.<nome>}} → variáveis do fluxo: definidas por set_variable e pelos dados do gatilho/campanha. (Atenção: o response_variable de um agente guarda o OBJETO completo da resposta; para o texto prefira {{node.<idDoAgente>.response_text}}.)
- {{ai.<chave>}} → saídas do último agente de IA (ex.: {{ai.response_text}}).
- {{message}}    → texto recebido do contato.
- {{sys.date}}, {{sys.time}}, {{sys.timestamp}} → data/hora atuais.

ACESSO PROFUNDO (dot-notation, como no n8n): use pontos para entrar em objetos/listas, {{node.<id>.tool_args.cep}}, {{node.<id>.dados.0.nome}}.

REGRA: só referencie chaves de nós que sejam ANCESTRAIS no fluxo; uma referência inválida não resolve e permanece literal em tempo de execução.`
}

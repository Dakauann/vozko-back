package node_executors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/workflow"
	"vozko/domain/workflowsecret"
	"vozko/usecases/workflow/scriptvm"
)

type codeExecutor struct {
	sandbox *scriptvm.Sandbox
	secrets workflowsecret.Resolver
}

func NewCodeExecutor(secrets workflowsecret.Resolver) workflow.NodeExecutor {
	limits := scriptvm.DefaultLimits()
	limits.Wallclock = workflow.CodeNodeMaxTimeout
	return &codeExecutor{sandbox: scriptvm.New(limits), secrets: secrets}
}

const defaultCodeNodeSnippet = `// ─────────────────────────────────────────────────────────────────────
// Sandbox JavaScript, sem eval, sem require, sem timers, sem DOM.
// Retorne um objeto → seus campos viram a saída deste nó, acessíveis
// nos próximos nós como {{last.<campo>}}. Sem retorno → saída vazia.
// ─────────────────────────────────────────────────────────────────────

// ── Entrada ─────────────────────────────────────────────────────────
//   input                       Saída do nó anterior (objeto congelado).
//                               Ex.: input.body, input.status

// ── Estado do fluxo (persiste entre nós desta execução) ────────────
//   state.get(chave)            Lê uma variável salva antes.
//   state.set(chave, valor)     Escreve uma variável (auditado).
//
//   Chaves com pontos viram caminho aninhado, igual aos templates
//   {{a.b.c}}. Ex.: o nó AI Agent grava "ai_response" como objeto;
//   para ler o argumento "query" da chamada de ferramenta, use:
//     state.get("ai_response.tool_args.query")
//   Índices numéricos viajam em arrays: state.get("hits.0.title").

// ── Log (aparece no log da execução) ───────────────────────────────
//   log('info', 'mensagem', obj)
//   // Níveis: 'debug' | 'info' | 'warn' | 'error'

// ── HTTP (somente HTTPS, com proteção SSRF) ────────────────────────
//   // Bloqueia IPs privados/loopback/metadata da nuvem.
//   fetch('https://api.exemplo.com/x')
//   fetch({
//     url: 'https://api.exemplo.com/x',
//     method: 'POST',
//     headers: { 'X-Api-Key': secrets.get('minha_chave') },
//     body: { ola: 'mundo' },   // objeto → vira JSON automaticamente
//     timeout_ms: 3000,
//   })
//   // → { status, ok, headers, body, url, duration_ms }

// ── Segredos (escopo do workspace, nunca enumeráveis) ──────────────
//   secrets.get('stripe_key')   Lança erro se não estiver autorizado.

// ── JSON ────────────────────────────────────────────────────────────
//   json.parse(texto)
//   json.stringify(valor)

// ── Base64 ──────────────────────────────────────────────────────────
//   b64.encode(texto)
//   b64.decode(texto)

// ── Cripto ──────────────────────────────────────────────────────────
//   crypto.uuid()                       gera UUID v4
//   crypto.hash('sha256', 'texto')      também 'sha1' e 'md5'

// ── Aleatoriedade (Math padrão; NÃO use p/ segurança) ──────────────
//   Math.random()                       0..1
//   Math.floor(Math.random() * 10)      inteiro 0..9
//   crypto.uuid()                       use isto p/ unicidade real

// ── Datas (Date padrão do JS) ──────────────────────────────────────
//   new Date()                          agora
//   Date.now()                          epoch em ms
//   new Date().toISOString()            '2026-05-22T12:34:56.789Z'
//   new Date(Date.now() + 86400000)     amanhã
//   new Date('2026-12-31').getTime()    parse → epoch ms

// ── Strings, números, arrays, regex, ES5+ padrão ──────────────────
//   'oi'.toUpperCase()
//   [1, 2, 3].map(x => x * 2)
//   /\d+/.test('abc123')

// ── Pausa (conta no orçamento de tempo) ────────────────────────────
//   sleep(250)                  pausa 250 ms

// ── Limites (padrão; podem ser menores no seu plano) ───────────────
//   tempo 5 s (até 60 s em Timeout) · saída ≤ 256 KB · log ≤ 64 KB
//   fetch ≤ 20 chamadas · resposta ≤ 5 MB

// ─────────────────────────────────────────────────────────────────────

return {
  ok: true,
  saudacao: 'Olá ' + (input.nome || 'mundo'),
  em: new Date().toISOString(),
};
`

var (
	codeTimeoutMinMs = float64(1)
	codeTimeoutMaxMs = float64(workflow.CodeNodeMaxTimeout.Milliseconds())
)

func (e *codeExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionCode,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryLogic,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared, workflow.NodeScopeVoice},
		Label:        "Código (JavaScript)",
		Description: "Executa JavaScript em um sandbox seguro. " +
			"Tem acesso a input, state.get/set, fetch (HTTPS), secrets, log, json, b64, crypto, sleep. " +
			"O valor retornado vira a saída do nó (acessível via {{last.<campo>}}).",
		Icon: "Code",
		Guidance: workflow.NodeGuidance{
			When:     "Para lógica personalizada em sandbox JavaScript quando nenhum nó pronto resolve.",
			Behavior: "Os campos do objeto retornado pelo script tornam-se as saídas de dados deste nó (acessíveis adiante via {{node.<id>.<campo>}}). Roda em sandbox com input, state.get/set, fetch (HTTPS) e log.",
		},
		DefaultConfig: map[string]interface{}{
			"code":            defaultCodeNodeSnippet,
			"timeout_ms":      5000,
			"output_variable": "",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "*", Description: "Os campos do objeto retornado pelo script. Use {{last.<campo>}}."},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "code", Label: "Código JavaScript", Type: "code", Placeholder: defaultCodeNodeSnippet, Required: true},
			{Key: "output_variable", Label: "Salvar retorno em variável", Type: "text", Placeholder: "ex.: minha_resposta", Description: "Opcional. Se preenchido, o valor retornado pelo script também é gravado no estado do fluxo sob este nome (acessível por {{state.<nome>}} ou via state.get('<nome>') em nós seguintes)."},
			{Key: "timeout_ms", Label: "Timeout (ms)", Type: "number", Placeholder: "5000", Description: "Tempo máximo do script, até 60000 ms (60 s).", Min: &codeTimeoutMinMs, Max: &codeTimeoutMaxMs},
		},
	}
}

func (e *codeExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	code, _ := ctx.Node.Config["code"].(string)
	code = strings.TrimSpace(code)
	if code == "" {

		if legacy, ok := ctx.Node.Config["expressions"].(string); ok && strings.TrimSpace(legacy) != "" {
			return nil, fmt.Errorf("action_code: legacy 'expressions' config detected; please migrate to JavaScript 'code'")
		}
		return nil, workflow.ErrNodeConfigMissing
	}

	timeout := workflow.CodeNodeDefaultTimeout
	switch v := ctx.Node.Config["timeout_ms"].(type) {
	case float64:
		if v > 0 {
			timeout = time.Duration(v) * time.Millisecond
		}
	case int:
		if v > 0 {
			timeout = time.Duration(v) * time.Millisecond
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i > 0 {
			timeout = time.Duration(i) * time.Millisecond
		}
	}

	secrets, err := resolveNodeSecrets(ctx, e.secrets, workflow.CodeSecretNames(code))
	if err != nil {
		return secretFailure(err)
	}

	callCtx, releaseCall := ctx.CallContext()
	defer releaseCall()
	execCtx, cancel := context.WithTimeout(callCtx, min(timeout, workflow.CodeNodeMaxTimeout))
	defer cancel()

	caps := scriptvm.Capabilities{
		Input:   workflow.LastNodeOutput(ctx.State),
		State:   scrubbedState{state: ctx.State, scrub: secrets.scrub},
		Secrets: secrets.values,
		Audit: func(r scriptvm.AuditRecord) {
			slog.Info("workflow.code.audit", append(nodeLogAttrs(ctx),
				slog.Int64("wall_ms", r.WallMs),
				slog.Int("fetch_calls", r.FetchCalls),
				slog.Int("ssrf_blocked", r.SSRFBlocked),
				slog.Any("state_writes", r.StateWrites),
				slog.Any("secrets_accessed", r.SecretsAccessed),
				slog.Bool("errored", r.Errored),
			)...)
			ctx.Trace.RecordScriptLogs(scriptLogs(r.Logs))
			for _, fetch := range r.Fetches {
				ctx.Trace.RecordHTTP(fetch)
			}
		},
	}

	res, err := e.sandbox.Run(execCtx, code, caps)
	if err != nil {
		var scriptErr *scriptvm.ScriptError
		if errors.As(err, &scriptErr) {
			ctx.Trace.RecordStack(scriptErr.Stack)
		}
		return nil, workflow.Fail(codeFailureKind(err), fmt.Errorf("action_code: %w", secrets.scrubError(err)))
	}

	out := map[string]interface{}{}
	var returned interface{}
	if res.Returns {
		returned = secrets.scrub.Value(res.Value)

		if m, ok := returned.(map[string]interface{}); ok {
			for k, v := range m {
				out[k] = v
			}
		} else {
			out["result"] = returned
		}
	}

	if varName, ok := ctx.Node.Config["output_variable"].(string); ok {
		varName = strings.TrimSpace(varName)
		if varName != "" && ctx.State != nil && res.Returns {
			ctx.State.Set(varName, returned)
		}
	}

	return &workflow.NodeResult{Output: out}, nil
}

func codeFailureKind(err error) workflow.ErrorKind {
	switch {
	case errors.Is(err, scriptvm.ErrTimeout):
		return workflow.ErrorKindTimeout
	case errors.Is(err, scriptvm.ErrStaticReject), errors.Is(err, scriptvm.ErrDisabled):
		return workflow.ErrorKindConfig
	}
	return workflow.ErrorKindScript
}

func scriptLogs(entries []scriptvm.LogEntry) []workflow.ScriptLog {
	if len(entries) == 0 {
		return nil
	}
	out := make([]workflow.ScriptLog, len(entries))
	for i, entry := range entries {
		out[i] = workflow.ScriptLog{Level: entry.Level, Message: entry.Msg}
	}
	return out
}

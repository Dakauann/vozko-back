package copilottools

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"vozko/domain/callrouting"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
	wd "vozko/domain/workspace/workspace_department"
)

type queueCatalog interface {
	List(ctx context.Context, workspaceID string) ([]*callrouting.Queue, error)
	Get(ctx context.Context, workspaceID, id string) (*callrouting.Queue, error)
	Create(ctx context.Context, queue callrouting.Queue) (*callrouting.Queue, error)
	Update(ctx context.Context, queue callrouting.Queue) (*callrouting.Queue, error)
	Delete(ctx context.Context, workspaceID, id string) error
}

type queueMonitor interface {
	Live(ctx context.Context, workspaceID string) ([]callrouting.QueueLive, error)
}

type holdPresets interface {
	Presets() []callrouting.HoldPreset
}

type departmentLookup interface {
	Get(workspaceID, id string) (*wd.Department, error)
}

type CallQueueDeps struct {
	Queues      queueCatalog
	Monitor     queueMonitor
	Music       holdPresets
	Names       callrouting.MemberNames
	Departments departmentLookup
	Now         func() time.Time
}

func callQueuesMeta(action workspace.Action, mutating bool) copilot.Meta {
	return copilot.Meta{Mutating: mutating, Resource: workspace.ResourceCallQueues, Action: action}
}

var strategyLabels = map[callrouting.Strategy]string{
	callrouting.StrategyLongestIdle: "toca para quem está livre há mais tempo",
	callrouting.StrategyFewestCalls: "toca para quem atendeu menos ligações",
	callrouting.StrategyRoundRobin:  "rodízio entre as pessoas",
	callrouting.StrategyRandom:      "toca para alguém livre ao acaso",
}

var agentStateLabels = map[callrouting.AgentState]string{
	callrouting.AgentFree:    "livre",
	callrouting.AgentRinging: "tocando",
	callrouting.AgentOnCall:  "em ligação",
	callrouting.AgentWrapUp:  "fechando a última ligação",
	callrouting.AgentOffline: "fora do ar",
}

func (d CallQueueDeps) queue(ctx context.Context, cc copilot.Context, id string) (*callrouting.Queue, error) {
	queueID, err := knownID(id, "queue_id", "list_call_queues")
	if err != nil {
		return nil, err
	}
	return d.Queues.Get(ctx, cc.WorkspaceID, queueID)
}

func (d CallQueueDeps) describeQueue(ctx context.Context, cc copilot.Context, id string) copilot.Field {
	if queue, err := d.queue(ctx, cc, id); err == nil {
		return copilot.Field{Key: "queue", Value: queue.Name}
	}
	return copilot.Field{Key: "queue", Value: "fila desconhecida"}
}

func (d CallQueueDeps) presetName(ref callrouting.HoldMusicRef) string {
	if strings.TrimSpace(ref.MediaID) != "" {
		return "áudio enviado pelo workspace"
	}
	for _, preset := range d.Music.Presets() {
		if preset.ID == ref.PresetID {
			return preset.Name
		}
	}
	return ref.PresetID
}

func (d CallQueueDeps) departmentName(cc copilot.Context, id string) string {
	if department, err := d.Departments.Get(cc.WorkspaceID, id); err == nil && department != nil {
		return department.Name
	}
	return "departamento desconhecido"
}

func (d CallQueueDeps) answeredBy(cc copilot.Context, queue callrouting.Queue) string {
	if queue.DepartmentID != "" {
		return "todo o departamento " + d.departmentName(cc, queue.DepartmentID)
	}
	names := d.Names.ResolveUsernames(queue.MemberUserIDs)
	out := make([]string, 0, len(queue.MemberUserIDs))
	for _, id := range queue.MemberUserIDs {
		if name := strings.TrimSpace(names[id]); name != "" {
			out = append(out, name)
			continue
		}
		out = append(out, "pessoa desconhecida")
	}
	return strings.Join(out, ", ")
}

type listCallQueuesTool struct{ deps CallQueueDeps }

func NewListCallQueuesTool(deps CallQueueDeps) copilot.Tool { return &listCallQueuesTool{deps: deps} }

func (t *listCallQueuesTool) Meta() copilot.Meta { return callQueuesMeta(workspace.ActionRead, false) }

func (t *listCallQueuesTool) Definition() tools.Definition {
	return definition("list_call_queues",
		"Lista as filas de atendimento telefônico: quem atende cada uma, como a ligação é distribuída, os tempos e a música de espera, "+
			"e o que acontece agora (quantos clientes esperam, há quanto tempo, e quem está livre, tocando ou em ligação). "+
			"Traz também as músicas de espera disponíveis.",
		struct{}{})
}

func (t *listCallQueuesTool) Execute(ctx context.Context, cc copilot.Context, _ map[string]interface{}) copilot.Result {
	queues, err := t.deps.Queues.List(ctx, cc.WorkspaceID)
	if err != nil {
		return callQueueFailure("list_call_queues", err)
	}
	live, err := t.deps.Monitor.Live(ctx, cc.WorkspaceID)
	if err != nil {
		return callQueueFailure("list_call_queues", err)
	}
	now := t.deps.Now()
	out := make([]map[string]interface{}, 0, len(queues))
	for _, queue := range queues {
		data := t.deps.queueData(cc, *queue)
		if index := slices.IndexFunc(live, func(l callrouting.QueueLive) bool { return l.QueueID == queue.ID }); index >= 0 {
			data["now"] = liveData(live[index], now)
		}
		out = append(out, data)
	}
	presets := make([]map[string]string, 0)
	for _, preset := range t.deps.Music.Presets() {
		presets = append(presets, map[string]string{"hold_music_preset": preset.ID, "name": preset.Name, "mood": preset.Mood})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"queues": out, "hold_music_presets": presets}}
}

func (d CallQueueDeps) queueData(cc copilot.Context, queue callrouting.Queue) map[string]interface{} {
	return map[string]interface{}{
		"queue_id":         queue.ID,
		"name":             queue.Name,
		"answered_by":      d.answeredBy(cc, queue),
		"distribution":     strategyLabels[queue.Strategy],
		"ring_seconds":     queue.RingSeconds,
		"max_wait_seconds": queue.MaxWaitSeconds,
		"wrap_up_seconds":  queue.WrapUpSeconds,
		"hold_music":       d.presetName(queue.HoldMusic),
	}
}

func liveData(live callrouting.QueueLive, now time.Time) map[string]interface{} {
	agents := make([]map[string]string, 0, len(live.Agents))
	for _, agent := range live.Agents {
		agents = append(agents, map[string]string{"name": agent.Name, "state": agentStateLabels[agent.State]})
	}
	return map[string]interface{}{
		"callers_waiting":      len(live.Waiting),
		"longest_wait_seconds": int(live.LongestWait(now).Seconds()),
		"people":               agents,
	}
}

type queueFields struct {
	Strategy        string   `json:"strategy" enum:"longest_idle,fewest_calls,round_robin,random" desc:"como a ligação é distribuída: quem está livre há mais tempo (longest_idle, padrão), quem atendeu menos (fewest_calls), rodízio (round_robin) ou ao acaso (random)"`
	DepartmentID    string   `json:"department_id" id:"true" desc:"id de list_departments para que todo o departamento atenda a fila; não use junto com member_ids"`
	MemberIDs       []string `json:"member_ids" id:"true" desc:"user_id de list_workspace_members das pessoas que atendem a fila; não use junto com department_id"`
	RingSeconds     *int     `json:"ring_seconds" desc:"quantos segundos toca para cada pessoa antes de passar para a próxima (5 a 60, padrão 15)"`
	MaxWaitSeconds  *int     `json:"max_wait_seconds" desc:"quanto tempo o cliente pode esperar na fila (10 a 3600, padrão 300)"`
	WrapUpSeconds   *int     `json:"wrap_up_seconds" desc:"pausa entre uma ligação e a próxima para a mesma pessoa (0 a 300, padrão 10)"`
	HoldMusicPreset string   `json:"hold_music_preset" desc:"hold_music_preset de list_call_queues"`
}

func (f queueFields) empty() bool {
	return f.Strategy == "" && f.DepartmentID == "" && len(f.MemberIDs) == 0 && f.RingSeconds == nil &&
		f.MaxWaitSeconds == nil && f.WrapUpSeconds == nil && f.HoldMusicPreset == ""
}

func (f queueFields) apply(queue *callrouting.Queue) {
	if f.Strategy != "" {
		queue.Strategy = callrouting.Strategy(f.Strategy)
	}
	department := strings.TrimSpace(f.DepartmentID)
	if department != "" {
		queue.DepartmentID = department
		if len(f.MemberIDs) == 0 {
			queue.MemberUserIDs = nil
		}
	}
	if len(f.MemberIDs) > 0 {
		queue.MemberUserIDs = f.MemberIDs
		if department == "" {
			queue.DepartmentID = ""
		}
	}
	for _, timing := range []struct {
		value  *int
		target *int
	}{{f.RingSeconds, &queue.RingSeconds}, {f.MaxWaitSeconds, &queue.MaxWaitSeconds}, {f.WrapUpSeconds, &queue.WrapUpSeconds}} {
		if timing.value != nil {
			*timing.target = *timing.value
		}
	}
	if preset := strings.TrimSpace(f.HoldMusicPreset); preset != "" {
		queue.HoldMusic = callrouting.HoldMusicRef{PresetID: preset}
	}
}

func (d CallQueueDeps) admissible(cc copilot.Context, queue callrouting.Queue, fields queueFields) error {
	queue.ApplyDefaults()
	if err := queue.Validate(); err != nil {
		return callQueueInputError(err)
	}
	if queue.DepartmentID != "" && !cc.Departments.Allows(queue.DepartmentID) {
		return fmt.Errorf("%w: departamento fora do seu alcance; use um id de list_departments", errInvalidArgs)
	}
	if queue.DepartmentID != "" {
		if _, err := d.Departments.Get(cc.WorkspaceID, queue.DepartmentID); err != nil {
			return fmt.Errorf("%w: departamento desconhecido; use o id exato de list_departments", errInvalidArgs)
		}
	}
	if preset := strings.TrimSpace(fields.HoldMusicPreset); preset != "" && !slices.ContainsFunc(d.Music.Presets(), func(p callrouting.HoldPreset) bool { return p.ID == preset }) {
		return fmt.Errorf("%w: música de espera desconhecida; use um hold_music_preset de list_call_queues", errInvalidArgs)
	}
	return nil
}

func (d CallQueueDeps) describeFields(cc copilot.Context, queue callrouting.Queue, fields queueFields) []copilot.Field {
	queue.ApplyDefaults()
	var out []copilot.Field
	if fields.DepartmentID != "" || len(fields.MemberIDs) > 0 {
		out = append(out, copilot.Field{Key: "answeredBy", Value: d.answeredBy(cc, queue)})
	}
	if fields.Strategy != "" {
		out = append(out, copilot.Field{Key: "distribution", Value: strategyLabels[queue.Strategy]})
	}
	for _, timing := range []struct {
		key   string
		given *int
		value int
	}{{"ringSeconds", fields.RingSeconds, queue.RingSeconds}, {"maxWaitSeconds", fields.MaxWaitSeconds, queue.MaxWaitSeconds}, {"wrapUpSeconds", fields.WrapUpSeconds, queue.WrapUpSeconds}} {
		if timing.given != nil {
			out = append(out, copilot.Field{Key: timing.key, Value: strconv.Itoa(timing.value)})
		}
	}
	if fields.HoldMusicPreset != "" {
		out = append(out, copilot.Field{Key: "holdMusic", Value: d.presetName(queue.HoldMusic)})
	}
	return out
}

type createCallQueueArgs struct {
	Name string `json:"name" req:"true" desc:"nome da fila, até 80 caracteres"`
	queueFields
}

func (a createCallQueueArgs) queue(workspaceID string) callrouting.Queue {
	queue := callrouting.Queue{WorkspaceID: workspaceID, Name: strings.TrimSpace(a.Name)}
	a.apply(&queue)
	return queue
}

type createCallQueueTool struct{ deps CallQueueDeps }

func NewCreateCallQueueTool(deps CallQueueDeps) copilot.Tool { return &createCallQueueTool{deps: deps} }

func (t *createCallQueueTool) Meta() copilot.Meta {
	return callQueuesMeta(workspace.ActionCreate, true)
}

func (t *createCallQueueTool) Definition() tools.Definition {
	return definition("create_call_queue",
		"Cria uma fila de atendimento telefônico, atendida por um departamento inteiro ou por pessoas escolhidas. Só depois da aprovação do usuário.",
		createCallQueueArgs{})
}

func (t *createCallQueueTool) Validate(_ context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[createCallQueueArgs](nil, cc, args)
	if err != nil {
		return err
	}
	return t.deps.admissible(cc, a.queue(cc.WorkspaceID), a.queueFields)
}

func (t *createCallQueueTool) Describe(_ context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a createCallQueueArgs
	bindArgs(args, &a)
	queue := a.queue(cc.WorkspaceID)
	return append([]copilot.Field{{Key: "name", Value: queue.Name}}, t.deps.describeFields(cc, queue, a.queueFields)...)
}

func (t *createCallQueueTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a createCallQueueArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	created, err := t.deps.Queues.Create(ctx, a.queue(cc.WorkspaceID))
	if err != nil {
		return callQueueFailure("create_call_queue", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: t.deps.queueData(cc, *created)}
}

type callQueueRefArgs struct {
	QueueID string `json:"queue_id" req:"true" id:"true" desc:"queue_id exato de list_call_queues"`
}

type updateCallQueueArgs struct {
	callQueueRefArgs
	Name string `json:"name" desc:"novo nome, se mudar"`
	queueFields
}

func (a updateCallQueueArgs) changed(existing callrouting.Queue) (callrouting.Queue, error) {
	name := strings.TrimSpace(a.Name)
	if name == "" && a.empty() {
		return existing, fmt.Errorf("%w: informe o que muda na fila", errInvalidArgs)
	}
	if name != "" {
		existing.Name = name
	}
	existing.MemberUserIDs = slices.Clone(existing.MemberUserIDs)
	a.apply(&existing)
	return existing, nil
}

type updateCallQueueTool struct{ deps CallQueueDeps }

func NewUpdateCallQueueTool(deps CallQueueDeps) copilot.Tool { return &updateCallQueueTool{deps: deps} }

func (t *updateCallQueueTool) Meta() copilot.Meta {
	return callQueuesMeta(workspace.ActionUpdate, true)
}

func (t *updateCallQueueTool) Definition() tools.Definition {
	return definition("update_call_queue",
		"Muda uma fila de atendimento telefônico: nome, quem atende, como distribui, os tempos ou a música de espera. "+
			"Só envie o que muda; o resto continua como está. Só depois da aprovação do usuário.",
		updateCallQueueArgs{})
}

func (t *updateCallQueueTool) proposed(ctx context.Context, cc copilot.Context, a updateCallQueueArgs) (callrouting.Queue, error) {
	existing, err := t.deps.queue(ctx, cc, a.QueueID)
	if err != nil {
		return callrouting.Queue{}, callQueueInputError(err)
	}
	return a.changed(*existing)
}

func (t *updateCallQueueTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[updateCallQueueArgs](nil, cc, args)
	if err != nil {
		return err
	}
	queue, err := t.proposed(ctx, cc, a)
	if err != nil {
		return err
	}
	return t.deps.admissible(cc, queue, a.queueFields)
}

func (t *updateCallQueueTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a updateCallQueueArgs
	bindArgs(args, &a)
	fields := []copilot.Field{t.deps.describeQueue(ctx, cc, a.QueueID)}
	queue, err := t.proposed(ctx, cc, a)
	if err != nil {
		return fields
	}
	if name := strings.TrimSpace(a.Name); name != "" {
		fields = append(fields, copilot.Field{Key: "newName", Value: name})
	}
	return append(fields, t.deps.describeFields(cc, queue, a.queueFields)...)
}

func (t *updateCallQueueTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a updateCallQueueArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	queue, err := t.proposed(ctx, cc, a)
	if err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	updated, err := t.deps.Queues.Update(ctx, queue)
	if err != nil {
		return callQueueFailure("update_call_queue", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: t.deps.queueData(cc, *updated)}
}

type deleteCallQueueTool struct{ deps CallQueueDeps }

func NewDeleteCallQueueTool(deps CallQueueDeps) copilot.Tool { return &deleteCallQueueTool{deps: deps} }

func (t *deleteCallQueueTool) Meta() copilot.Meta {
	return callQueuesMeta(workspace.ActionDelete, true)
}

func (t *deleteCallQueueTool) Definition() tools.Definition {
	return definition("delete_call_queue",
		"Remove uma fila de atendimento telefônico; fluxos de voz que mandam ligações para ela deixam de transferir. Só depois da aprovação do usuário.",
		callQueueRefArgs{})
}

func (t *deleteCallQueueTool) Validate(ctx context.Context, cc copilot.Context, args map[string]interface{}) error {
	a, err := validateArgs[callQueueRefArgs](nil, cc, args)
	if err != nil {
		return err
	}
	_, err = t.deps.queue(ctx, cc, a.QueueID)
	return callQueueInputError(err)
}

func (t *deleteCallQueueTool) Describe(ctx context.Context, cc copilot.Context, args map[string]interface{}) []copilot.Field {
	var a callQueueRefArgs
	bindArgs(args, &a)
	return []copilot.Field{
		t.deps.describeQueue(ctx, cc, a.QueueID),
		{Key: "risks", Value: "Fluxos de voz que mandam ligações para esta fila deixam de transferir."},
	}
}

func (t *deleteCallQueueTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	var a callQueueRefArgs
	if err := decodeArgs(args, &a); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: err.Error()}
	}
	if err := t.deps.Queues.Delete(ctx, cc.WorkspaceID, strings.TrimSpace(a.QueueID)); err != nil {
		return callQueueFailure("delete_call_queue", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{"deleted": true}}
}

var callQueueInputMessages = []struct {
	err     error
	message string
}{
	{callrouting.ErrQueueNotFound, "fila não encontrada; use um queue_id de list_call_queues"},
	{callrouting.ErrQueueNameRequired, "a fila precisa de um nome de até 80 caracteres"},
	{callrouting.ErrQueueMembersRequired, "escolha um departamento ou as pessoas que atendem a fila"},
	{callrouting.ErrQueueMembersAmbiguous, "a fila é atendida por um departamento ou por pessoas escolhidas, não pelos dois"},
	{callrouting.ErrInvalidStrategy, "forma de distribuir desconhecida"},
	{callrouting.ErrQueueTimingOutOfRange, "tempos fora do limite: toque de 5 a 60 segundos, espera de 10 a 3600 e pausa de 0 a 300"},
	{callrouting.ErrHoldMusicNotFound, "música de espera desconhecida; use um hold_music_preset de list_call_queues"},
	{callrouting.ErrQueueDepartment, "o departamento não é deste workspace; use um id de list_departments"},
	{callrouting.ErrQueueMemberOutside, "todas as pessoas da fila precisam ser do workspace; use user_id de list_workspace_members"},
}

func callQueueInputError(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range callQueueInputMessages {
		if errors.Is(err, known.err) {
			return fmt.Errorf("%w: %s", errInvalidArgs, known.message)
		}
	}
	if callrouting.IsInvalidInput(err) {
		return fmt.Errorf("%w: %s", errInvalidArgs, err.Error())
	}
	return err
}

func callQueueFailure(tool string, err error) copilot.Result {
	if inputErr := callQueueInputError(err); errors.Is(inputErr, errInvalidArgs) {
		return copilot.Result{Status: copilot.StatusError, Message: inputErr.Error()}
	}
	log.Printf("[copilot] %s failed: %v", tool, err)
	return copilot.Result{Status: copilot.StatusError, Message: "não foi possível mudar as filas de atendimento agora"}
}

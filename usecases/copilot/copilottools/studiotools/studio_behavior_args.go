package studiotools

import (
	"fmt"

	"vozko/domain/studio"
	"vozko/usecases/copilot/copilottools"
)

type studioBehaviorParams struct {
	Frequency *float64 `json:"frequency,omitempty" desc:"wiggle: tremidas por segundo; overshoot: balanços por segundo depois da chave; de 0.1 a 30"`
	Amount    *float64 `json:"amount,omitempty" desc:"wiggle: o quanto treme para cada lado, de 0 até o tamanho da faixa da propriedade; audioReact: quanto soma no som mais alto, negativo diminui (scale 0.2 cresce 20 por cento no pico); na unidade da propriedade: x e y em fração do quadro, scale em vezes, rotation em graus, opacity de 0 a 1, blur em px"`
	Seed      *int64   `json:"seed,omitempty" desc:"wiggle: variação do tremor, inteiro de 0 a 999999; o mesmo seed repete o mesmo tremor"`
	Mode      string   `json:"mode,omitempty" enum:"cycle,pingpong,offset,continue" desc:"loop: cycle repete as chaves do começo, pingpong vai e volta, offset repete somando a diferença entre a primeira e a última chave (sobe sem parar), continue segue na velocidade da última chave"`
	Side      string   `json:"side,omitempty" enum:"out,in,both" desc:"loop: out repete depois da última chave (padrão), in antes da primeira, both dos dois lados"`
	Rate      *float64 `json:"rate,omitempty" desc:"drift: quanto a propriedade anda por segundo, sem parar; rotation 90 é um quarto de volta por segundo; negativo vai ao contrário"`
	Amplitude *float64 `json:"amplitude,omitempty" desc:"overshoot: força do balanço depois de cada chave, de 0 a 1 (0.05 é sutil); multiplica a velocidade com que a propriedade chega na chave"`
	Decay     *float64 `json:"decay,omitempty" desc:"overshoot: quão rápido o balanço some, de 0.1 a 30 (8 assenta em meio segundo)"`
	FloorDB   *float64 `json:"floor_db,omitempty" desc:"audioReact: abaixo deste volume em dB a propriedade fica parada, de -70 a 0 (padrão -40)"`
	CeilingDB *float64 `json:"ceiling_db,omitempty" desc:"audioReact: a partir deste volume em dB soma o amount inteiro, de -70 a 0 e acima de floor_db (padrão -10)"`
}

type studioBehaviorArgs struct {
	BehaviorID   string                `json:"behavior_id,omitempty" desc:"update_behavior, remove_behavior e bake_behavior: o comportamento, id de studio_read (behaviors do clipe) ou @ref de add_behavior"`
	BehaviorKind string                `json:"behavior_kind,omitempty" enum:"wiggle,loop,drift,overshoot,audioReact" desc:"add_behavior: wiggle treme ao acaso, loop repete as chaves, drift anda ou gira sem parar, overshoot balança depois de cada chave, audioReact segue o volume de uma faixa de som"`
	Behavior     *studioBehaviorParams `json:"behavior,omitempty" desc:"add_behavior e update_behavior: os ajustes do comportamento; mande só os do tipo dele; sem eles valem os padrões"`
	AudioTrackID string                `json:"audio_track_id,omitempty" desc:"audioReact: a faixa de som que conduz, id de studio_read ou @ref; sem ela usa a primeira faixa de som com clipes"`
}

func (a studioBehaviorArgs) argsIssue() string {
	b := a.Behavior
	if b == nil {
		return ""
	}
	inRange := func(v *float64, low, high float64) bool { return v == nil || (*v >= low && *v <= high) }
	switch {
	case !inRange(b.Frequency, studio.MinBehaviorFrequency, studio.MaxBehaviorFrequency):
		return fmt.Sprintf("behavior.frequency vai de %g a %g", studio.MinBehaviorFrequency, studio.MaxBehaviorFrequency)
	case !inRange(b.Amplitude, 0, studio.MaxBehaviorAmplitude):
		return fmt.Sprintf("behavior.amplitude vai de 0 a %g", studio.MaxBehaviorAmplitude)
	case !inRange(b.Decay, studio.MinBehaviorDecay, studio.MaxBehaviorDecay):
		return fmt.Sprintf("behavior.decay vai de %g a %g", studio.MinBehaviorDecay, studio.MaxBehaviorDecay)
	case !inRange(b.FloorDB, studio.MinBehaviorDB, studio.MaxBehaviorDB) || !inRange(b.CeilingDB, studio.MinBehaviorDB, studio.MaxBehaviorDB):
		return fmt.Sprintf("behavior.floor_db e behavior.ceiling_db vão de %g a %g", studio.MinBehaviorDB, studio.MaxBehaviorDB)
	case b.FloorDB != nil && b.CeilingDB != nil && *b.FloorDB >= *b.CeilingDB:
		return "behavior.ceiling_db precisa ficar acima de behavior.floor_db"
	case b.Seed != nil && (*b.Seed < 0 || *b.Seed > studio.MaxBehaviorSeed):
		return fmt.Sprintf("behavior.seed é um inteiro de 0 a %d", studio.MaxBehaviorSeed)
	case b.Mode != "" && !copilottools.Contains([]string{"cycle", "pingpong", "offset", "continue"}, b.Mode):
		return "behavior.mode deve ser um de: cycle, pingpong, offset, continue"
	case b.Side != "" && !copilottools.Contains([]string{"out", "in", "both"}, b.Side):
		return "behavior.side deve ser um de: out, in, both"
	}
	return ""
}

func (o studioVideoOperation) argsIssue() string {
	if msg := o.studioKeyEaseArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioVfxArgs.argsIssue(o.Op); msg != "" {
		return msg
	}
	if msg := o.studioMotionBlurArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioCameraArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioTiltArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.pathKeysIssue(); msg != "" {
		return msg
	}
	if msg := o.clipStyleIssue(); msg != "" {
		return msg
	}
	if msg := o.studioPathOpListArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioClipRepeatOpArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.chartIssue(); msg != "" {
		return msg
	}
	if msg := o.studioTableArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioQRArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioOrganizeArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.lottieIssue(); msg != "" {
		return msg
	}
	if msg := o.studioPresetArgs.argsIssue(o.Op); msg != "" {
		return msg
	}
	return o.studioBehaviorArgs.argsIssue()
}

const studioBehaviorGuide = "Comportamentos animam uma propriedade sozinhos, somando ao valor das chaves, iguais na prévia e na exportação (o mesmo instante dá sempre o mesmo valor): " +
	"add_behavior(clip_id, property, behavior_kind, behavior?, audio_track_id?: devolve o id em created), update_behavior(clip_id, behavior_id, behavior? e ou audio_track_id?), remove_behavior(clip_id, behavior_id), " +
	"bake_behavior(clip_id, behavior_id: troca o comportamento por chaves lineares comuns, uma por quadro; loop e overshoot da mesma propriedade vão junto, porque leem as chaves; recusado em x ou y com trajeto curvo e com chaves rivais de escala). " +
	"Propriedades: x, y, scale, scaleX, scaleY, rotation, opacity e blur (nulos só x, y, scale, scaleX, scaleY e rotation); um comportamento de cada tipo por propriedade, e vários tipos somam. " +
	"Tipos: wiggle(frequency, amount, seed) treme ao acaso; loop(mode, side) repete as chaves fora delas e precisa de pelo menos duas chaves; drift(rate) anda sem parar, e drift em rotation é um giro contínuo; " +
	"overshoot(amplitude, frequency, decay) balança depois de cada chave pela velocidade com que chega nela; audioReact(amount, floor_db, ceiling_db, audio_track_id) segue o volume da faixa de som (antes do volume da faixa e do ducking), como um logo que pulsa na batida. " +
	"studio_read mostra os comportamentos de cada clipe em behaviors, com os mesmos nomes de campo. "

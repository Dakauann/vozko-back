package studiotools

type studioFrameArgs struct {
	FrameAlign     string `json:"frame_align,omitempty" enum:"left,center,right,top,middle,bottom" desc:"align_clips: borda ou centro a alinhar no quadro; center e middle são o centro na horizontal e na vertical"`
	Axis           string `json:"axis,omitempty" enum:"horizontal,vertical" desc:"distribute_clips: espaça os clipes por igual da esquerda para a direita (horizontal) ou de cima para baixo (vertical); os dois das pontas ficam onde estão"`
	RelativeTo     string `json:"relative_to,omitempty" enum:"selection,frame" desc:"align_clips: selection (padrão com dois ou mais clipes) alinha pela caixa que junta os clipes, frame alinha pelo quadro do vídeo; um clipe sozinho sempre alinha pelo quadro"`
	GapMS          *int64 `json:"gap_ms,omitempty" desc:"sequence_clips: espaço entre o fim de um clipe e o começo do próximo, em ms (padrão 0, encostados); negativo sobrepõe, o que só vale para clipes em faixas diferentes"`
	StepMS         *int64 `json:"step_ms,omitempty" desc:"stagger_clips: cada clipe começa step_ms depois do anterior, contando do primeiro de clip_ids, que fica onde está; negativo inverte a cascata"`
	ProtectEdgesMS *int64 `json:"protect_edges_ms,omitempty" desc:"set_speed: as chaves nos primeiros e nos últimos protect_edges_ms do clipe mantêm o tempo (a entrada e a saída não ficam mais lentas nem mais rápidas) e só o meio estica; vale com stretch_keys ligado"`
}

const studioFrameGuide = "Layout no quadro e tempo: align_clips(clip_ids, frame_align, relative_to?, at_ms?: alinha as bordas ou os centros no instante at_ms, padrão o cursor de tempo; clipes animados em posição ganham uma chave nesse instante), " +
	"distribute_clips(clip_ids com três ou mais, axis, at_ms?: espaços iguais entre os clipes), " +
	"sequence_clips(clip_ids na ordem, gap_ms?, transition_kind? e duration_ms? da transição: põe um clipe depois do outro a partir do primeiro, o som ligado acompanha e as chaves andam junto; com transition_kind, cada corte entre clipes encostados na mesma faixa ganha a transição), " +
	"stagger_clips(clip_ids na ordem, step_ms: cascata, cada clipe começa step_ms depois do anterior); set_speed aceita protect_edges_ms para manter o tempo das chaves de entrada e de saída. "

package studiotools

import (
	"fmt"
	"math"
	"reflect"

	"vozko/domain/studio"
)

type studioClipRepeatArgs struct {
	Mode string `json:"mode,omitempty" enum:"linear,radial,grid,mirror" desc:"linear enfileira cópias como um eco ou um rastro; radial gira cópias em volta de um centro, grid enfileira em linhas e colunas, mirror espelha do outro lado de um eixo"`
	studioRepeatShapeArgs
	Copies       *int     `json:"copies,omitempty" desc:"linear: quantas cópias contando a original, a partir de 1; animar copies faz a fila crescer"`
	Shift        *float64 `json:"shift,omitempty" desc:"linear: desliza todas as cópias ao longo do passo, de -10000 a 10000; fracionário vale e animar shift faz a fila andar"`
	X            *float64 `json:"x,omitempty" desc:"linear: quanto cada cópia anda na horizontal em relação à anterior, em pixels do quadro de -4096 a 4096"`
	Y            *float64 `json:"y,omitempty" desc:"linear: quanto cada cópia anda na vertical em relação à anterior, em pixels do quadro de -4096 a 4096"`
	Scale        *float64 `json:"scale,omitempty" desc:"linear: tamanho de cada cópia em relação à anterior, de 0.01 a 10; 0.9 vai diminuindo"`
	Rotation     *float64 `json:"rotation,omitempty" desc:"linear: quanto cada cópia gira em relação à anterior, em graus de -3600 a 3600"`
	StartOpacity *float64 `json:"start_opacity,omitempty" desc:"opacidade da primeira cópia, de 0 a 1 (padrão 1); as do meio vão no meio do caminho"`
	EndOpacity   *float64 `json:"end_opacity,omitempty" desc:"opacidade da última cópia, de 0 a 1 (padrão 1); 0.1 faz o rastro sumir"`
	Order        string   `json:"order,omitempty" enum:"above,below" desc:"above deixa as cópias por cima da original, below por baixo (bom para rastros)"`
}

type studioClipRepeatOpArgs struct {
	Repeat         *studioClipRepeatArgs `json:"repeat,omitempty" desc:"set_repeat: a repetição do clipe; campos omitidos mantêm os atuais e mode novo recomeça dos padrões"`
	RepeatProperty string                `json:"repeat_property,omitempty" enum:"copies,shift,x,y,scale,rotation,startOpacity,endOpacity,radius,startAngle,sweep,gapX,gapY,angle,gap" desc:"animate_repeat: o número da repetição que as keys animam; linear anima copies, shift, x, y, scale e rotation, radial anima radius, startAngle e sweep, grid anima gapX e gapY, mirror anima angle e gap, e todos animam startOpacity e endOpacity"`
}

func clipRepeatRange(field string) (studio.ValueRange, bool) {
	return studio.ClipRepeatFieldRange(field)
}

func floorIssue(name string, value *int, least int) string {
	if value != nil && *value < least {
		return rangeText(name, studio.ValueRange{Low: float64(least), High: math.Inf(1)})
	}
	return ""
}

func (a studioRepeatShapeArgs) shapeIssue() string {
	if msg := floorIssue("count", a.Count, studio.MinRepeatCount); msg != "" {
		return msg
	}
	if msg := floorIssue("shown", a.Shown, 0); msg != "" {
		return msg
	}
	if msg := floorIssue("rows", a.Rows, studio.MinRepeatCells); msg != "" {
		return msg
	}
	if msg := floorIssue("columns", a.Columns, studio.MinRepeatCells); msg != "" {
		return msg
	}
	offset := studio.ValueRange{Low: 0, High: studio.MaxRepeatOffset}
	if a.Offset != nil && !offset.Holds(*a.Offset) {
		return rangeText("offset", offset)
	}
	return rangedIssue([]rangedArg{
		{"radius", "radius", a.Radius, clipRepeatRange},
		{"start_angle", "startAngle", a.StartAngle, clipRepeatRange},
		{"sweep", "sweep", a.Sweep, clipRepeatRange},
		{"gap_x", "gapX", a.GapX, clipRepeatRange},
		{"gap_y", "gapY", a.GapY, clipRepeatRange},
		{"angle", "angle", a.Angle, clipRepeatRange},
		{"gap", "gap", a.Gap, clipRepeatRange},
	}, "repeat")
}

func (a studioClipRepeatArgs) argsIssue() string {
	if msg := a.shapeIssue(); msg != "" {
		return msg
	}
	return rangedIssue([]rangedArg{
		{"copies", "copies", intArg(a.Copies), clipRepeatRange},
		{"shift", "shift", a.Shift, clipRepeatRange},
		{"x", "x", a.X, clipRepeatRange},
		{"y", "y", a.Y, clipRepeatRange},
		{"scale", "scale", a.Scale, clipRepeatRange},
		{"rotation", "rotation", a.Rotation, clipRepeatRange},
		{"start_opacity", "startOpacity", a.StartOpacity, clipRepeatRange},
		{"end_opacity", "endOpacity", a.EndOpacity, clipRepeatRange},
	}, "repeat")
}

func (a studioClipRepeatOpArgs) argsIssue() string {
	if a.Repeat == nil {
		return ""
	}
	if msg := operationIssue(reflect.ValueOf(*a.Repeat)); msg != "" {
		return fmt.Sprintf("repeat: %s", msg)
	}
	return ""
}

const studioClipRepeatGuide = "Repetição de clipes, em vez de duplicar: set_repeat(clip_id ou clip_ids de textos, formas, ícones e imagens sobrepostas, repeat), clear_repeat(clip_id), animate_repeat(clip_id, repeat_property, keys com at_ms dentro do clipe). " +
	"linear desenha cópias em fila, cada uma andando x e y pixels do quadro, girando rotation graus e mudando de tamanho por scale em relação à anterior, como um eco; shift desliza a fila ao longo do passo e animado faz a fila andar; " +
	"radial, grid e mirror funcionam como na repetição do editor de imagem; start_opacity e end_opacity apagam as cópias da primeira à última e order below põe as cópias por baixo da original. " +
	"As cópias seguem o clipe na hora (mudou o original, mudam todas) e não são clicáveis; studio_read mostra repeat no clipe. " +
	"Formato de um rastro (valores ilustrativos): {\"op\":\"set_repeat\",\"clip_id\":\"@bola\",\"repeat\":{\"mode\":\"linear\",\"copies\":4,\"x\":-48,\"end_opacity\":0.1,\"order\":\"below\"}}. "

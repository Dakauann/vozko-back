package studiotools

import (
	"fmt"
	"reflect"
)

func (a studioRepeatArgs) argsIssue() string {
	return a.shapeIssue()
}

func (o studioImageOperation) repeatIssue() string {
	if o.Repeat == nil {
		return ""
	}
	if msg := operationIssue(reflect.ValueOf(*o.Repeat)); msg != "" {
		return fmt.Sprintf("repeat: %s", msg)
	}
	return ""
}

type studioRepeatArgs struct {
	Mode string `json:"mode,omitempty" enum:"radial,grid,mirror" desc:"radial gira cópias em volta de um centro, grid enfileira em linhas e colunas, mirror espelha do outro lado de um eixo; obrigatório no repeat_layers"`
	studioRepeatShapeArgs
}

type studioRepeatShapeArgs struct {
	Count          *int     `json:"count,omitempty" desc:"radial: quantas posições na volta, a partir de 2 (padrão 8)"`
	Shown          *int     `json:"shown,omitempty" desc:"radial: quantas cópias aparecem a partir da original, sem mudar o espaçamento"`
	Radius         *float64 `json:"radius,omitempty" desc:"radial: distância do centro até a original, em pixels de 1 a 4096 (padrão o maior lado da seleção)"`
	StartAngle     *float64 `json:"start_angle,omitempty" desc:"radial: direção do centro até a original em graus, de -180 a 180; -90 (padrão) deixa a original em cima do centro"`
	Sweep          *float64 `json:"sweep,omitempty" desc:"radial: abertura do leque em graus, de 1 a 360 (padrão a volta inteira)"`
	Direction      string   `json:"direction,omitempty" enum:"cw,ccw" desc:"radial: sentido horário (padrão) ou anti-horário"`
	Upright        *bool    `json:"upright,omitempty" desc:"radial: true mantém as cópias em pé em vez de girá-las (bom para texto)"`
	ReverseOverlap *bool    `json:"reverse_overlap,omitempty" desc:"radial: inverte qual cópia fica por cima"`
	Rows           *int     `json:"rows,omitempty" desc:"grid: linhas, a partir de 1 (linhas vezes colunas pelo menos 2)"`
	Columns        *int     `json:"columns,omitempty" desc:"grid: colunas, a partir de 1"`
	GapX           *float64 `json:"gap_x,omitempty" desc:"grid: espaço horizontal entre as cópias em pixels, de -4096 a 4096"`
	GapY           *float64 `json:"gap_y,omitempty" desc:"grid: espaço vertical entre as cópias em pixels, de -4096 a 4096"`
	Layout         string   `json:"layout,omitempty" enum:"grid,brick-row,brick-column" desc:"grid: grade reta ou tijolo deslocando linhas ou colunas alternadas"`
	Offset         *float64 `json:"offset,omitempty" desc:"grid: deslocamento do tijolo de 0 a 1 (padrão 0.5)"`
	FlipRows       string   `json:"flip_rows,omitempty" enum:"none,horizontal,vertical,both" desc:"grid: espelha as linhas alternadas"`
	FlipColumns    string   `json:"flip_columns,omitempty" enum:"none,horizontal,vertical,both" desc:"grid: espelha as colunas alternadas"`
	Angle          *float64 `json:"angle,omitempty" desc:"mirror: ângulo do eixo em graus de 0 a 180; 90 (padrão) é vertical e a cópia fica ao lado, 0 é horizontal e a cópia fica embaixo"`
	Gap            *float64 `json:"gap,omitempty" desc:"mirror: distância entre a original e a cópia em pixels; negativo sobrepõe"`
	Side           string   `json:"side,omitempty" enum:"end,start" desc:"mirror: end (padrão) põe a cópia à direita ou embaixo, start do outro lado"`
}

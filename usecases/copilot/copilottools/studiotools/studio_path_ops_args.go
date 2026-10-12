package studiotools

import (
	"fmt"
	"math"
	"reflect"

	"vozko/domain/studio"
)

type studioPathOpArgs struct {
	ID         string   `json:"id,omitempty" desc:"id de um operador que já existe (de studio_read), para manter as chaves e os valores não mandados; sem ele o operador é novo"`
	Kind       string   `json:"kind" req:"true" enum:"offset,puckerBloat,roundCorners,zigZag,twist,merge" desc:"offset engorda ou afina o contorno e pode repetir anéis; puckerBloat murcha ou incha os lados; roundCorners arredonda os cantos; zigZag serrilha ou ondula o contorno; twist torce a forma em volta de um centro; merge combina uma forma recorte com a forma (unir, tirar, cruzar ou excluir)"`
	Hidden     *bool    `json:"hidden,omitempty" desc:"true desliga o operador sem apagá-lo"`
	Amount     *float64 `json:"amount,omitempty" desc:"offset: pixels da caixa de -1000 a 1000, positivo cresce e negativo encolhe; puckerBloat: de -100 (murcha, pontas para fora e lados para dentro) a 100 (incha, lados para fora)"`
	Copies     *int     `json:"copies,omitempty" desc:"offset: quantos contornos, a partir de 1; mais de 1 desenha anéis alternados"`
	CopyOffset *float64 `json:"copy_offset,omitempty" desc:"offset: distância entre um anel e o próximo, em pixels de -1000 a 1000"`
	Join       string   `json:"join,omitempty" enum:"miter,round,bevel" desc:"offset: cantos do contorno deslocado; miter pontudo, round redondo, bevel chanfrado"`
	MiterLimit *float64 `json:"miter_limit,omitempty" desc:"offset com join miter: até onde a ponta vai antes de virar chanfro, de 1 a 20"`
	Radius     *float64 `json:"radius,omitempty" desc:"roundCorners: raio dos cantos em pixels, de 0 a 2000"`
	Size       *float64 `json:"size,omitempty" desc:"zigZag: altura dos dentes em pixels, de -1000 a 1000; negativo inverte o lado"`
	Ridges     *int     `json:"ridges,omitempty" desc:"zigZag: quantos dentes por trecho do contorno, a partir de 0"`
	Points     string   `json:"points,omitempty" enum:"corner,smooth" desc:"zigZag: corner faz dentes pontudos, smooth faz ondas"`
	Angle      *float64 `json:"angle,omitempty" desc:"twist: quanto torce em graus, de -3600 a 3600; o centro gira mais e as pontas mais longe ficam no lugar"`
	CenterX    *float64 `json:"center_x,omitempty" desc:"twist: centro da torção na horizontal, em fração da caixa de -1 a 2; 0.5 é o centro"`
	CenterY    *float64 `json:"center_y,omitempty" desc:"twist: centro da torção na vertical, em fração da caixa de -1 a 2; 0.5 é o centro"`
	Mode       string   `json:"mode,omitempty" enum:"union,subtract,intersect,exclude" desc:"merge: union soma o recorte, subtract fura a forma com ele, intersect deixa só onde os dois se cruzam, exclude tira só a sobreposição"`
	Operand    string   `json:"operand,omitempty" enum:"ellipse,rect,triangle,star" desc:"merge: forma pronta do recorte"`
	Path       string   `json:"path,omitempty" desc:"merge: caminho SVG do recorte com coordenadas de 0 a 1 na caixa do recorte, no lugar de operand"`
	FillRule   string   `json:"fill_rule,omitempty" enum:"nonzero,evenodd" desc:"merge: preenchimento do caminho do recorte; evenodd vaza as sobreposições"`
	X          *float64 `json:"x,omitempty" desc:"merge: centro do recorte na horizontal, em fração da caixa da forma de -2 a 3; 0.5 é o meio"`
	Y          *float64 `json:"y,omitempty" desc:"merge: centro do recorte na vertical, em fração da caixa da forma de -2 a 3; 0.5 é o meio"`
	W          *float64 `json:"w,omitempty" desc:"merge: largura do recorte em fração da caixa da forma, de 0.001 a 8"`
	H          *float64 `json:"h,omitempty" desc:"merge: altura do recorte em fração da caixa da forma, de 0.001 a 8"`
	Rotation   *float64 `json:"rotation,omitempty" desc:"merge: giro do recorte em graus, de -3600 a 3600"`
}

type studioPathOpListArgs struct {
	PathOps []studioPathOpArgs `json:"path_ops,omitempty" desc:"set_path_ops: a pilha inteira de operadores, aplicados em ordem de cima para baixo; substitui a anterior; [] tira todos"`
}

type studioPathOpKeyArgs struct {
	PathOpID       string `json:"path_op_id,omitempty" desc:"animate_path_op: o operador, id de path_ops em studio_read"`
	PathOpProperty string `json:"path_op_property,omitempty" enum:"amount,copies,copyOffset,miterLimit,radius,size,ridges,angle,centerX,centerY,x,y,w,h,rotation" desc:"animate_path_op: o número do operador que as keys animam; precisa ser um campo do tipo dele"`
}

type studioLiveMergeArgs struct {
	Live *bool `json:"live,omitempty" desc:"combine_shapes: true mescla ao vivo: as camadas de cima viram operadores merge da camada de baixo, que continuam editáveis; sem live as formas viram um caminho novo"`
}

type rangedArg struct {
	name   string
	field  string
	value  *float64
	ranges func(string) (studio.ValueRange, bool)
}

func intArg(value *int) *float64 {
	if value == nil {
		return nil
	}
	v := float64(*value)
	return &v
}

func rangeText(name string, r studio.ValueRange) string {
	if math.IsInf(r.High, 1) {
		return fmt.Sprintf("%s vai a partir de %g", name, r.Low)
	}
	return fmt.Sprintf("%s vai de %g a %g", name, r.Low, r.High)
}

func rangedIssue(args []rangedArg, owner string) string {
	for _, arg := range args {
		if arg.value == nil {
			continue
		}
		r, owned := arg.ranges(arg.field)
		if !owned {
			return fmt.Sprintf("%s não vale para %s", arg.name, owner)
		}
		if !r.Holds(*arg.value) {
			return rangeText(arg.name, r)
		}
	}
	return ""
}

func (a studioPathOpArgs) argsIssue() string {
	if !studio.PathOpKnown(a.Kind) {
		return "kind é obrigatório: offset, puckerBloat, roundCorners, zigZag, twist ou merge"
	}
	texts := []struct{ name, owner, value string }{
		{"join", studio.PathOpOffset, a.Join},
		{"points", studio.PathOpZigZag, a.Points},
		{"mode", studio.PathOpMerge, a.Mode},
		{"operand", studio.PathOpMerge, a.Operand},
		{"path", studio.PathOpMerge, a.Path},
		{"fill_rule", studio.PathOpMerge, a.FillRule},
	}
	for _, text := range texts {
		if text.value != "" && text.owner != a.Kind {
			return fmt.Sprintf("%s não vale para %s", text.name, a.Kind)
		}
	}
	switch {
	case a.Operand != "" && a.Path != "":
		return "merge: mande operand ou path, não os dois"
	case a.Path != "" && !studio.ValidPath(a.Path):
		return "path do merge não é um caminho SVG válido"
	}
	ranges := func(field string) (studio.ValueRange, bool) { return studio.PathOpFieldRange(a.Kind, field) }
	return rangedIssue([]rangedArg{
		{"amount", "amount", a.Amount, ranges},
		{"copies", "copies", intArg(a.Copies), ranges},
		{"copy_offset", "copyOffset", a.CopyOffset, ranges},
		{"miter_limit", "miterLimit", a.MiterLimit, ranges},
		{"radius", "radius", a.Radius, ranges},
		{"size", "size", a.Size, ranges},
		{"ridges", "ridges", intArg(a.Ridges), ranges},
		{"angle", "angle", a.Angle, ranges},
		{"center_x", "centerX", a.CenterX, ranges},
		{"center_y", "centerY", a.CenterY, ranges},
		{"x", "x", a.X, ranges},
		{"y", "y", a.Y, ranges},
		{"w", "w", a.W, ranges},
		{"h", "h", a.H, ranges},
		{"rotation", "rotation", a.Rotation, ranges},
	}, a.Kind)
}

func (a studioPathOpListArgs) argsIssue() string {
	for i, item := range a.PathOps {
		if msg := operationIssue(reflect.ValueOf(item)); msg != "" {
			return fmt.Sprintf("path_ops[%d]: %s", i, msg)
		}
	}
	return ""
}

func (o studioImageOperation) argsIssue() string {
	if msg := o.studioPathOpListArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioStillVfxArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioTableArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.studioQRArgs.argsIssue(); msg != "" {
		return msg
	}
	if msg := o.repeatIssue(); msg != "" {
		return msg
	}
	if msg := o.artboardIssue(); msg != "" {
		return msg
	}
	return o.studioChartArgs.argsIssue()
}

const studioPathOpsIntro = "Operadores vivos de caminho mudam o contorno de uma forma (retângulo, elipse, triângulo, estrela e caminho; não linhas nem setas) sem destruí-la: a forma original fica guardada, os operadores se empilham e valem em ordem, de cima para baixo, e cada um pode ser ajustado, escondido com hidden ou tirado depois. " +
	"Tipos: offset(amount, copies?, copy_offset?, join, miter_limit?) engorda ou afina o contorno e, com copies, desenha anéis; puckerBloat(amount) murcha ou incha os lados; roundCorners(radius) arredonda os cantos; " +
	"zigZag(size, ridges, points) serrilha ou ondula o contorno; twist(angle, center_x?, center_y?) torce a forma em volta de um centro; merge(mode, operand ou path, fill_rule?, x, y, w, h, rotation?) une, fura, cruza ou exclui uma forma recorte posicionada na caixa da forma. "

const studioPathOpsVideoGuide = studioPathOpsIntro +
	"set_path_ops(clip_id ou clip_ids de formas, path_ops, at_ms? em ms dentro do clipe para gravar uma chave quando o valor já é animado), animate_path_op(clip_id, path_op_id, path_op_property, keys com at_ms dentro do clipe): os números dos operadores animam, como um selo que cresce em anéis ou um furo que abre. " +
	"studio_read mostra path_ops na camada do clipe, com os ids e as chaves. "

const studioPathOpsImageGuide = studioPathOpsIntro +
	"set_path_ops(layer_id ou layer_ids de formas, path_ops); combine_shapes com live true mescla ao vivo: as camadas de cima viram operadores merge da camada de baixo e continuam editáveis. " +
	"studio_read mostra path_ops na camada, com os ids. "

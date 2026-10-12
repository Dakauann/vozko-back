package studiotools

type studioLayoutArgs struct {
	LayoutDirection  string    `json:"layout_direction,omitempty" enum:"horizontal,vertical" desc:"set_layout: horizontal põe os itens lado a lado, vertical um embaixo do outro"`
	LayoutGap        *float64  `json:"layout_gap,omitempty" desc:"set_layout: espaço entre os itens em pixels, de 0 a 10000"`
	LayoutAutoGap    *bool     `json:"layout_auto_gap,omitempty" desc:"set_layout: true espalha os itens até as bordas (sem layout_gap nem layout_align_main)"`
	LayoutPadding    []float64 `json:"layout_padding,omitempty" desc:"set_layout: margem interna em pixels: 1 número para todos os lados, 2 para vertical e horizontal ou 4 para topo, direita, baixo e esquerda"`
	LayoutAlignMain  string    `json:"layout_align_main,omitempty" enum:"start,center,end" desc:"set_layout: onde os itens ficam na direção do layout"`
	LayoutAlignCross string    `json:"layout_align_cross,omitempty" enum:"start,center,end" desc:"set_layout: onde os itens ficam no outro eixo"`
	LayoutWidth      string    `json:"layout_width,omitempty" enum:"hug,fixed" desc:"set_layout: hug faz a base abraçar o conteúdo na largura, fixed mantém a largura"`
	LayoutHeight     string    `json:"layout_height,omitempty" enum:"hug,fixed" desc:"set_layout: hug faz a base abraçar o conteúdo na altura, fixed mantém a altura"`
	ItemWidth        string    `json:"item_width,omitempty" enum:"fixed,fill,hug" desc:"set_layout_item: largura do item; fill ocupa o espaço que sobra, hug (só texto) abraça o texto"`
	ItemHeight       string    `json:"item_height,omitempty" enum:"fixed,fill,hug" desc:"set_layout_item: altura do item; fill ocupa a altura toda, hug (só texto) abraça as linhas"`
	ItemAbsolute     *bool     `json:"item_absolute,omitempty" desc:"set_layout_item: true tira o item do fluxo, ele fica onde está"`
}

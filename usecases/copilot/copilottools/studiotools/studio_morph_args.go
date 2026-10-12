package studiotools

import (
	"fmt"

	"vozko/domain/studio"
)

type studioMorphKeyArgs struct {
	Path        string `json:"path,omitempty" desc:"para path: o caminho SVG desta chave, com coordenadas de 0 a 1 na caixa do clipe (ou da máscara em animate_mask); as chaves podem ter números diferentes de pontos: o editor reamostra e casa os pontos sozinho"`
	FirstVertex *int   `json:"first_vertex,omitempty" desc:"para path: o número do ponto desta chave (0 é o primeiro desenhado) que encontra o primeiro ponto da chave vizinha; sem ele o editor escolhe o casamento que menos distorce; use para a forma girar enquanto muda"`
}

const studioMorphGuide = "Forma que muda (morph): animate com property path em formas recebe chaves com path, o caminho SVG de cada momento, e first_vertex opcional; rect, ellipse, triangle e star viram path sozinhas na primeira chave, e textos, linhas, setas e mídia não animam o caminho. " +
	"animate_mask com mask_property path faz o mesmo numa máscara path. O editor casa os pontos de caminhos com números diferentes de pontos, acerta o sentido do desenho e escolhe o primeiro ponto que menos distorce; o easing de cada chave controla o ritmo como nas outras propriedades, e ease_keys e reverse_keys valem para essas chaves. " +
	"studio_read full traz cada chave com path e first_vertex. Formato de um quadrado que vira estrela (tempos ilustrativos): [{\"op\":\"animate\",\"clip_id\":\"@forma\",\"property\":\"path\",\"keys\":[{\"at_ms\":0,\"path\":\"M0 0 L1 0 L1 1 L0 1 Z\"},{\"at_ms\":900,\"path\":\"M0.5 0 L0.62 0.35 L1 0.35 L0.69 0.57 L0.81 0.91 L0.5 0.7 L0.19 0.91 L0.31 0.57 L0 0.35 L0.38 0.35 Z\"}]}]. "

func (o studioVideoOperation) pathKeysIssue() string {
	pathLane := (o.Op == "animate" && o.Property == "path") || (o.Op == "animate_mask" && o.MaskProperty == "path")
	for _, k := range o.Keys {
		switch {
		case !pathLane && (k.Path != "" || k.FirstVertex != nil):
			return "path e first_vertex nas chaves valem só para animate com property path e animate_mask com mask_property path"
		case pathLane && k.Path == "":
			return "as chaves de path precisam de path, o caminho SVG de cada momento"
		case pathLane && !studio.ValidPath(k.Path):
			return fmt.Sprintf("o path da chave em %d ms não é um caminho SVG válido; use os comandos M L H V C S Q T A Z com coordenadas de 0 a 1", k.AtMS)
		case k.FirstVertex != nil && *k.FirstVertex < 0:
			return "first_vertex conta os pontos do caminho a partir de 0"
		}
	}
	return ""
}

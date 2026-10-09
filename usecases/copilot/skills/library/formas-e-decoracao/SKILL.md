---
name: formas-e-decoracao
description: Formas vetoriais e decoração no Estúdio (como escrever um path SVG na caixa da camada, as formas prontas de path_preset, estrelas e selos, motivos como marcas de pneu, linhas de velocidade, xadrez, listras, meio-tom e grão, e o orçamento de decoração por estilo); carregue ao desenhar formas, faixas, selos, molduras ou enfeites.
---

# Formas e decoração

Toda decoração aponta, agrupa, emoldura ou conta o assunto. Se não faz nada disso, apague.

## Formas vetoriais
- add_shape com shape path e path: um caminho SVG em coordenadas de 0 a 1 dentro da caixa da camada (0 0 é o canto de cima à esquerda, 1 1 o de baixo à direita). x, y, w e h posicionam a caixa na arte; o caminho estica junto com ela.
- Comandos: M (mover), L (linha), H e V (linha horizontal e vertical), C e S (curvas de Bézier), Q e T (curvas quadráticas), A (arco: rx ry rotação grande sentido x y), Z (fechar). Minúscula é relativa ao ponto anterior. Números separados por espaço; o caminho começa com M.
- Caminho fechado (com Z) preenche com fill ou gradient. Caminho aberto vira traço: fill "#00000000", stroke e stroke_width.
- Valores fora de 0 a 1 saem da caixa; para sangrar, prefira aumentar a caixa.
- Troque o desenho depois com update_layer e path. Formas vetoriais servem de máscara como qualquer forma: a foto logo acima com clip true.
- Recortes e silhuetas compostas sem calcular pontos: desenhe as peças com formas simples e junte com combine_shapes. union funde (nuvem de círculos, balão de fala com rabicho), subtract fura a peça de baixo com as de cima (anel, selo vazado, letra recortada num bloco), intersect guarda só o encontro, exclude vaza o encontro, flatten junta sem cortar. O resultado é um caminho comum: aceita fill, gradient, stroke, sombra e clip.
- Caminho aberto aceita arrow_start e arrow_end, para setas curvas e traços que apontam.

### Formas prontas (path_preset)
diagonalBand (faixa inclinada), diagonalSplit (campo diagonal), arch (arco de janela), ribbon (fita com pontas cortadas), chevron, wave (borda em onda), blob (forma orgânica), speechBubble (balão de fala), heart, swoosh (traço curvo), tornPaper (papel rasgado), cornerBracket (canto de enquadramento, aberto), squiggle (linha ondulada, aberta), burst (explosão de 16 pontas), seal (selo serrilhado de 24 pontas). Depois de criada, a forma pronta é uma forma comum: mude cor, contorno, sombra, caixa e rotação.

### Receitas
- Campo diagonal na arte inteira: caixa (0.5, 0.5, 1, 1), path "M0 0.58 L1 0.40 L1 1 L0 1 Z".
- Faixa inclinada: "M0 0.3 L1 0 L1 0.7 L0 1 Z", numa caixa larga.
- Janela em arco com topo redondo: ry = largura da caixa em pixels ÷ (2 × altura em pixels); path "M0 1 L0 ry A0.5 ry 0 0 1 1 ry L1 1 Z" (troque ry pelo número).
- Paralelogramo para etiqueta de velocidade: "M0.12 0 L1 0 L0.88 1 L0 1 Z".
- Seta grossa: "M0 0.35 L0.6 0.35 L0.6 0.1 L1 0.5 L0.6 0.9 L0.6 0.65 L0 0.65 Z".
- Marca de pneu: dois caminhos abertos paralelos com curva suave, como "M0 0.4 C0.3 0.1 0.6 0.7 1 0.4", stroke escuro, stroke_width grosso, opacity de 0.5 a 0.8, dash para o desenho do pneu.
- Estrela, selo e explosão: shape star com points (3 a 64) e inner (0.05 a 1, a fração da ponta onde ficam os vales). Explosão de preço: points 12 a 24, inner 0.65 a 0.8. Selo serrilhado: points 20 a 32, inner 0.85 a 0.92. Brilho Y2K: points 4, inner 0.2 a 0.3. Polígono: inner perto de 1 com poucos points.

## Vocabulário de decoração

| Elemento | Para | Como, e o limite |
|---|---|---|
| Fios | ancorar grupos, suíço, editorial | line com stroke_width fino (0.2% a 0.4% da largura) ou barra (1% a 2%); nunca um fio entre título centralizado e data |
| Marcas de canto | editorial, técnico, arquitetura | path_preset cornerBracket em L, 0.03 a 0.05 da arte, girado 0, 90, 180 e 270 nas margens |
| Moldura | clássico, retrô, foto emoldurada | rect só de contorno com recuo de 0.03 a 0.05; o título atravessa a moldura |
| Selo e explosão | garantia, preço, novidade | um por peça, girado de 8 a 15 graus |
| Adesivo | pop, zine, varejo | forma ou texto com contorno branco grosso, rotation até 8 graus, no máximo 3 |
| Carimbo | comida, artesanal, vintage | uma tinta, blend_mode multiply, opacity 0.8 a 0.9, com grão por cima |
| Fita adesiva | colagem | rect ou path com ponta rasgada, opacity 0.4 a 0.6; nunca em peça corporativa |
| Seta | apontar chamada, recurso, ingrediente | uma por peça |
| Linhas de velocidade | automobilismo, esporte, entrega | de 5 a 12 linhas finas de comprimentos variados, no ângulo do movimento, com opacity caindo |
| Meio-tom | retrô, pop, impresso | grade de pequenas ellipses com tamanhos decrescentes, blend_mode multiply, opacity 0.05 a 0.15 sobre campos chapados; nunca grande sobre rostos |
| Grão | quase toda peça | textura de grão gerada (studio_generate_image) na arte inteira, blend_mode overlay, opacity 0.03 a 0.06; acima de 0.10 fica sujo |
| Xadrez | bandeira de chegada, lanchonete, skate | faixa de quadrados de 0.04 a 0.08 de altura; nunca atrás de texto |
| Listras | pintura de carro (3:1:1), toldo, urgência | de 2 a 3 faixas; 45 graus para alerta |
| Grade e pontos | suíço, tecnologia, educação | opacity 0.05 a 0.10 |
| Ícones | linhas de dados | um peso de traço só, altura igual à maiúscula do texto ao lado |
| Estrelas e brilhos | Y2K, beleza, infantil | no máximo 3, tamanhos 1 : 0.6 : 0.35 |

Padrões repetidos (xadrez, meio-tom, listras, grade) são feitos de muitas formas pequenas: o editor não tem limite de camadas. Crie em lote, alinhe com align_layers e distribute_layers e agrupe com group_layers para mover junto.

## Orçamento
- Uma família de motivo repetida 2 ou 3 vezes, no máximo uma textura e um recurso estrutural (moldura, grade ou faixa).
- Estilos limpos: até 5 elementos decorativos. Estilos maximalistas (colagem, rave, varejo): até 12, porque a densidade é o estilo.
- Decoração menor e com menos contraste que o herói, com uma família de traço (espessuras na proporção 1 : 2 : 4), e ou na grade ou claramente girada (nunca 1 ou 2 graus, que parece erro).

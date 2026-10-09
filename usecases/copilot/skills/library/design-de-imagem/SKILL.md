---
name: design-de-imagem
description: O editor de imagem do Estúdio e a higiene de uma arte profissional (recursos do editor, formas vetoriais, máscaras, degradês, recorte e filtros, contraste, espaço, tamanho de texto, marca e os erros de amador com a correção); carregue antes de criar ou ajustar qualquer imagem, junto com direcao-de-arte quando a peça for nova.
---

# Design de imagem no Estúdio

Esta habilidade é a base: o que o editor faz e o que nenhuma peça pode errar. O que a peça vai ser (conceito, estilo, composição) vem de direcao-de-arte e das habilidades que ela indica.

## Antes de compor
- Objetivo, formato ou estilo pouco claros: pergunte com studio_ask.
- Leia com studio_read e veja com studio_look. Logo, fotos e referências anexadas são material do projeto: use-os, nunca os troque por formas.
- Formato e zonas seguras de cada rede: formatos-e-zonas-seguras.

## O editor
- Camadas sem limite de quantidade: monte com quantos elementos a peça pedir, agrupe com group_layers e dê nome com name.
- A ordem das camadas é a ordem de empilhamento: a primeira fica atrás. order_layers com above_id ou below_id posiciona com precisão.
- Texto: fonte, peso, itálico, alinhamento, line_height, letter_spacing, contorno (stroke e stroke_width; com fill "#00000000" fica só o contorno), caixa atrás (highlight), arco (curve), degradê e sombra.
- Formas: rect (com radius), ellipse, line, arrow, triangle, star (com points e inner para selos e explosões) e path, a forma vetorial livre: um caminho SVG de 0 a 1 dentro da caixa da camada, ou uma forma pronta com path_preset. Detalhes e receitas em formas-e-decoracao.
- Degradê linear ou radial, com cor do meio (via), em textos, formas e no fundo da prancheta (update_artboard).
- Modos de mistura (blend_mode) e opacidade em qualquer camada.
- Máscara: a forma, o texto ou a imagem embaixo e a camada logo acima com clip true; ela só aparece dentro da de baixo. Serve para foto dentro de forma vetorial e foto dentro de letras.
- Foto: frame (ellipse, triangle, star), crop, filters e filter_preset, flip_x e flip_y (cuidado com texto e logo dentro da foto).
- Remoção de fundo: studio_start_job kind cutout troca a imagem da camada pelo recorte quando fica pronto (texto atrás da pessoa em profundidade-e-camadas).
- Ícones do catálogo com add_icon. Efeitos saem com clear.
- Pranchetas: um projeto guarda várias artes lado a lado, cada uma com tamanho e fundo próprios. Para "outra versão", "uma opção mais escura" ou "a mesma arte para o story", duplique com duplicate_artboard e mexa na cópia, deixando a original como estava. Com width e height a cópia já sai reorganizada para o novo formato, e você só refina o que ficou apertado.
- Versões boas para comparar mudam uma coisa clara cada (cor, título, imagem ou composição). Dê a cada prancheta um nome que diga o que muda e confira todas juntas com studio_look e artboard_id all.

## Hierarquia
- Um ponto focal por peça. No máximo 3 tamanhos de texto, e o mais importante é o maior; do herói ao nível seguinte, pelo menos 3 para 1.
- A mensagem principal entendida em 3 segundos.
- Rostos e texto puxam o olho: ponha-os onde quer atenção, com o olhar do rosto apontando para o título ou o produto.
- Centro, terços e assimetria são escolhas: escolha uma de propósito.

## Espaço
- Margens de 6% a 8% da largura; mais espaço acima de um título do que abaixo dele.
- Apertado dentro dos grupos, pelo menos 3 vezes mais entre grupos. Use align_layers e distribute_layers.
- Estilos limpos pedem respiro; estilos maximalistas (colagem, rave, varejo) usam densidade de propósito. O que não pode é espaço sem intenção.

## Tamanho e legibilidade do texto
- Numa arte de 1080 de largura: título de pelo menos 84 px e apoio de pelo menos 44 px. Como font_size é fração da altura, numa arte quadrada isso dá 0.078 e 0.041.
- Títulos com line_height de 0.85 a 1.15; textos corridos de 1.2 a 1.45. Linhas de 45 a 75 caracteres.
- Português ocupa até 30% mais espaço que o inglês: deixe folga nas caixas de texto.
- Pesos finos só em tamanho grande.

## Contraste (WCAG 2.2)
- Mínimo de 4.5:1 para texto e de 3:1 para títulos grandes (24 px ou mais, ou 18.5 px em negrito) e elementos gráficos. A checagem automática mede contra o fundo real.
- Cor de marca clara (turquesa, amarelo) não serve para texto sobre branco: use-a no fundo, numa forma ou num detalhe.
- Nunca texto cinza sobre fundo colorido: use um tom mais claro ou mais escuro da própria cor do fundo.
- Texto sobre foto: a área calma da foto, um campo de cor, uma caixa (highlight ou rect) ou um véu escuro só onde o texto fica.

## Sombra
- Luz de um lado só para a peça inteira.
- Sombra suave: deslocamento y de 2% a 8% da altura do objeto, blur de cerca de 2 vezes o deslocamento, cor translúcida tirada do tom escuro da paleta (#00000026 a #00000059 se for neutra). Nunca sombra preta opaca, nunca sombra escura sobre fundo escuro.

## Marca
- Logo com respiro de pelo menos a altura da letra ou do símbolo principal. Nunca esticar, recolorir ou aplicar efeito no logo.
- A marca aparece com clareza, de preferência no topo ou no rodapé, sem competir com o ponto focal.

## Erros de amador e a correção
- Tudo do mesmo tamanho: escolha o foco e reduza o resto.
- Fontes e cores demais: uma ou duas famílias e um acento.
- Texto sobre foto confusa: área calma, campo de cor ou véu, e contraste de 4.5:1.
- Bordas apertadas sem intenção: margens de 6% a 8%, ou sangre de verdade.
- Alinhamentos misturados: um eixo só por grupo.
- Imagem esticada ou ampliada demais: mantenha a proporção.
- Efeito de acabamento por todo lado (brilho, chanfro, sombra pesada): um sistema de profundidade só.
- Texto demais: título de até 7 palavras e no máximo 20% da área com texto corrido.
- Peça certinha mas sem graça: falta conceito, não detalhe. Volte para direcao-de-arte.

## Conferir
Corrija as linhas "erro" do campo checks de cada studio_edit_image, veja com studio_look e avalie com critica-de-design antes de entregar. No máximo duas rodadas de ajuste; se o conceito não funciona, refaça em vez de polir.

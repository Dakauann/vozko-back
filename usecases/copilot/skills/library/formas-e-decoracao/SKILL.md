---
name: formas-e-decoracao
surface: studio
description: Formas vetoriais e decoração no Estúdio (como escrever um path SVG na caixa da camada, as formas prontas de path_preset, estrelas e selos, motivos como marcas de pneu, linhas de velocidade, xadrez, listras, meio-tom e grão, e o orçamento de decoração por estilo); carregue ao desenhar formas, faixas, selos, molduras ou enfeites.
---

# Formas e decoração

Toda decoração aponta, agrupa, emoldura ou conta o assunto. Se não faz nada disso, apague.

## Biblioteca primeiro
- Antes de calcular pontos, procure o elemento com studio_library_search: linhas (tracejada, pontilhada, com bolinhas, de medida, com seta aberta), formas básicas, polígonos, estrelas, setas cheias (direita, esquerda, cima, baixo, duplas, em quatro direções, dobrada, de retorno), fluxograma (processo, decisão, dados, documento, banco de dados, início ou fim), balões, faixas e selos (escudo, roseta, etiqueta de preço, ingresso) já existem prontos. A busca entende o idioma da conversa e o inglês, sem acento, e devolve item_id, a proporção (ratio) e as cores de cada item.
- Ícones: a categoria icons traz mais de mil ícones vetoriais em seis pesos (subcategorias regular, thin, light, bold, fill e duotone; o item_id termina no peso, como icon-heart-bold). Escolha um peso só para a peça inteira, regular ou bold para leitura em celular, fill para destaque. Entram como caminhos editáveis na cor da tinta do editor; troque a cor com colors (from é a cor listada pela busca). duotone traz uma forma de fundo clara sob o traço, no mesmo grupo.
- Emojis: a categoria emoji traz emojis vetoriais coloridos, agrupados como no teclado (carinhas, pessoas, animais, comida, viagens, atividades, objetos, símbolos). Cada um entra como um grupo de formas editáveis; colors troca uma cor por vez. Use para dar tom informal (no máximo dois por peça) e nunca no lugar de uma palavra importante.
- Insira com add_element(item_id, x?, y?, w? ou h?, colors?): w ou h sozinho mantém a proporção, colors troca cada cor do item (from) pela da peça (to). O resultado é uma camada comum, que aceita update_layer, combine_shapes, clip e repeat_layers como qualquer outra.
- Desenhe à mão só o que a biblioteca não tem ou o que precisa de uma forma exata da peça.

## Molduras e grades
- Foto num formato (círculo, arco, coração, bolha, polaroide): procure na categoria frames e insira com add_element. A moldura entra vazia, com um padrão de espera, e as notas da resposta trazem o id de cada moldura vazia. Preencha com fill_frame(layer_id, media_id): a foto entra recortada na forma, cobrindo-a pelo centro, e fica logo acima da moldura.
- Para enquadrar melhor, mande crop na foto já dentro da moldura: ela desliza e aproxima sem sair do lugar. Para trocar a foto, fill_frame de novo na moldura ou na foto.
- Colagem: a categoria grids monta a prancheta inteira em layout automático, com o mesmo espaço entre as fotos e na borda. Preencha cada espaço com fill_frame; mude o espaço com set_layout no grupo da grade e a cor da borda trocando o fill da base.
- Uma foto por espaço, a principal no espaço maior. Rosto e produto longe das bordas da forma; numa forma pequena (estrela, coração), prefira um assunto simples e centrado.
- No vídeo, a mesma moldura recorta clipes que já existem: add_element com item_id da moldura e clip_ids dos vídeos ou imagens. Uma grade divide a tela entre os clipes na ordem de clip_ids, cada um cobrindo o seu espaço.

## Mockups
- Celular, tablet, notebook, janela de navegador, post de rede social e story estão na categoria mockups. Todos são genéricos, sem marca: não imite aparelhos nem telas de uma empresa real.
- Na imagem, add_element põe o aparelho com a tela vazia; as notas trazem o id da tela e fill_frame(layer_id, media_id) coloca a foto ou o print dentro dela.
- No vídeo, add_element com item_id do mockup e clip_ids de um único vídeo ou imagem: o clipe vai para a tela, recortado nos cantos dela, e o aparelho entra por cima durante o mesmo tempo, em clipes ligados.
- Use mockup para mostrar um app, um site ou um anúncio no feed; o conteúdo da tela é o assunto, então ele precisa ser legível no tamanho final.

## Elementos gráficos, ilustrações e 3D
- A categoria graphics traz bolhas, ondas, confete, brilhos, rabiscos, pontilhados, pinceladas e fundos decorativos que enfeitam as bordas e deixam o meio livre. São vetores editáveis: recolora com colors no add_element, para que conversem com a paleta da peça.
- Detalhes de assinatura prontos: em graphics, molduras ornamentais (art déco, filete, linha dupla, botânica) com o topo aberto para o título e medidores (gastos, anel de progresso, radial); em textCombos, monogramas de 2 ou 3 letras num selo com texto curvo, trocados por texts no add_element.
- Na mesma categoria há pessoas e ilustrações em domínio público (CC0), também vetoriais e recoloríveis com colors.
- A categoria threeD traz ícones e emojis em 3D como imagens prontas; add_element os coloca como imagem da biblioteca de mídia do workspace. Eles não mudam de cor; escolha um que já combine.
- Quando nada disso tem o estilo, o ângulo ou o assunto da peça, gere o elemento sob medida com studio_generate_image: o objeto sozinho, centralizado, num fundo liso e sem texto, depois remova o fundo com studio_start_job kind cutout (como pedir e manter um estilo só em direcao-de-arte, Material da peça). A pessoa faz o mesmo no botão Gerar elemento com IA das categorias graphics e threeD.
- Um ou dois elementos de apoio por peça. Eles apontam, enfeitam a borda ou dão clima; nunca disputam com o produto ou com o título.

## Formas vetoriais
- add_shape com shape path e path: um caminho SVG em coordenadas de 0 a 1 dentro da caixa da camada (0 0 é o canto de cima à esquerda, 1 1 o de baixo à direita). x, y, w e h posicionam a caixa na arte; o caminho estica junto com ela.
- Comandos: M (mover), L (linha), H e V (linha horizontal e vertical), C e S (curvas de Bézier), Q e T (curvas quadráticas), A (arco: rx ry rotação grande sentido x y), Z (fechar). Minúscula é relativa ao ponto anterior. Números separados por espaço; o caminho começa com M.
- Caminho fechado (com Z) preenche com fill ou gradient. Caminho aberto vira traço: fill "#00000000", stroke e stroke_width.
- Valores fora de 0 a 1 saem da caixa; para sangrar, prefira aumentar a caixa.
- Troque o desenho depois com update_layer e path. Formas vetoriais servem de máscara como qualquer forma: a foto logo acima com clip true.
- Recortes e silhuetas compostas sem calcular pontos: desenhe as peças com formas simples e junte com combine_shapes. union funde (nuvem de círculos, balão de fala com rabicho), subtract fura a peça de baixo com as de cima (anel, selo vazado, letra recortada num bloco), intersect guarda só o encontro, exclude vaza o encontro, flatten junta sem cortar. O resultado é um caminho comum: aceita fill, gradient, stroke, sombra e clip.
- Linhas, setas e caminhos abertos aceitam pontas: arrow_start e arrow_end ligam a ponta, e arrow_start_kind e arrow_end_kind escolhem o estilo de cada uma: arrow (seta cheia, o padrão), openArrow (seta aberta), round (ponta arredondada), square (quadrado), bar (barra, para linha de medida), dot (bolinha, para linha do tempo e conectores) ou diamond (losango). Mandar só o estilo já liga a ponta; arrow_end false tira a ponta e o estilo. Pontas diferentes nas duas extremidades contam direção: bolinha no início e seta no fim.
- No vídeo, formas, textos e ícones aceitam fills, strokes e effects como aqui (update_clip), e clipes de vídeo e imagem aceitam contorno e sombra ou brilho em volta da silhueta; animate_effect faz um brilho pulsar ou uma sombra andar.
- No vídeo, o desenho também anima: animate com property path e uma chave por forma (cada uma com path) faz um quadrado virar estrela ou uma mancha virar outra; o editor casa os pontos de caminhos diferentes sozinho e first_vertex escolhe por onde a forma gira. As receitas abaixo servem de chaves; o ritmo e os tempos estão em motion-design.

### Formas prontas (path_preset)
diagonalBand (faixa inclinada), diagonalSplit (campo diagonal), arch (arco de janela), ribbon (fita com pontas cortadas), chevron, wave (borda em onda), blob (forma orgânica), speechBubble (balão de fala), heart, swoosh (traço curvo), tornPaper (papel rasgado), cornerBracket (canto de enquadramento, aberto), squiggle (linha ondulada, aberta), burst (explosão de 16 pontas), seal (selo serrilhado de 24 pontas). Depois de criada, a forma pronta é uma forma comum: mude cor, contorno, sombra, caixa e rotação.

### Receitas
- Campo diagonal na arte inteira: caixa (0.5, 0.5, 1, 1), path "M0 0.58 L1 0.40 L1 1 L0 1 Z".
- Faixa inclinada: "M0 0.3 L1 0 L1 0.7 L0 1 Z", numa caixa larga.
- Janela em arco com topo redondo: ry = largura da caixa em pixels ÷ (2 × altura em pixels); path "M0 1 L0 ry A0.5 ry 0 0 1 1 ry L1 1 Z" (troque ry pelo número).
- Paralelogramo para etiqueta de velocidade: "M0.12 0 L1 0 L0.88 1 L0 1 Z".
- Seta grossa: "M0 0.35 L0.6 0.35 L0.6 0.1 L1 0.5 L0.6 0.9 L0.6 0.65 L0 0.65 Z".
- Marca de pneu: dois caminhos abertos paralelos com curva suave, como "M0 0.4 C0.3 0.1 0.6 0.7 1 0.4", stroke escuro, stroke_width grosso, opacity de 0.5 a 0.8, dash para o desenho do pneu.
- Estrela, selo e explosão: shape star com points (3 a 64) e inner (0.05 a 1, a fração da ponta onde ficam os vales). Explosão de preço: points 12 a 24, inner 0.65 a 0.8. Selo serrilhado: points 20 a 32, inner 0.85 a 0.92. Brilho Y2K: points 4, inner 0.2 a 0.3.
- Polígono regular: shape polygon com points de 3 a 64 lados (6 é o hexágono, 8 o octógono); mude os lados depois com update_layer e points.
- Faixa arredondada atravessando a peça (pedida em palavras como faixa, fita ou tarja arredondada): add_shape rect com radius 1, ou um rect comum com set_path_ops roundCorners de raio grande (metade da altura da faixa em px), girado de 6 a 12 graus com rotation. O arredondado só aparece se a ponta estiver dentro da arte: w de 0.75 a 0.95 da largura deixa as duas pontas visíveis, ou sangre uma ponta só e mostre a outra. Logo atrás, uma cópia com duplicate_layers, deslocada alguns px na direção da luz e num tom mais escuro, dá espessura. Várias faixas: a mesma rotação, o mesmo passo entre elas, cores alternadas e o texto de cada uma com a mesma rotation da faixa.
- Fita com pontas dobradas (rabo de andorinha): três partes, de trás para frente. Duas caudas em path com o corte em V, um pouco mais baixas que a faixa, saindo das laterais dela e num tom mais escuro: à esquerda "M0 0 L1 0 L1 1 L0 1 L0.32 0.5 Z", à direita "M0 0 L1 0 L0.68 0.5 L1 1 L0 1 Z". Dois triângulos de dobra no tom mais escuro de todos, no encontro entre a faixa e cada cauda, logo abaixo da borda de baixo da faixa: à esquerda "M0 0 L1 0 L1 1 Z", à direita "M0 0 L1 0 L0 1 Z". Por cima, a faixa (rect) com o texto. Acerte a ordem com order_layers. Pronta: path_preset ribbon dá a fita de pontas cortadas numa forma só, e studio_library_search em faixas e selos traz fitas, faixas e selos desenhados.

### Operadores de caminho (vivos)
- set_path_ops muda o contorno de retângulos, elipses, triângulos, estrelas e caminhos sem destruir a forma: a pilha vale em ordem, de cima para baixo, e cada operador pode ser escondido (hidden), reordenado ou tirado depois. Linhas e setas ficam de fora; para elas use o traço.
- Cantos são um sistema da marca, escolhido uma vez e repetido em botões, selos, barras e janelas: retos (radius 0 a 0.04) para suíço, brutalista e luxo; suaves (radius 0.1 a 0.3, ou roundCorners de 8 a 24 px numa estrela ou caminho) para marcas amigáveis; pílula (radius 1) para adesivo, Y2K e app; orgânicos (blob, puckerBloat) para o estilo suave. Selo: zigZag com points smooth, ridges de 3 a 5 e size de 6 a 12 px, e depois roundCorners com radius pequeno. Borda serrilhada de etiqueta ou papel picotado: zigZag points corner com ridges de 6 a 10.
- Contorno duplo ou anéis: offset com amount positivo cresce e negativo encolhe; copies acima de 1 com copy_offset desenha anéis concêntricos que se alternam entre cheio e vazado (bom para alvo, sonar, carimbo). join round deixa as quinas redondas, miter mantém o bico.
- Formas orgânicas: puckerBloat positivo incha (balão, bolha), negativo murcha (estrela de pontas finas, brilho). twist com angle de 90 a 360 vira redemoinho; funciona melhor em formas com muitos lados.
- Recortes: merge com mode subtract e operand ellipse faz uma rosca, uma mordida (x perto da borda) ou um furo para um ícone; intersect deixa só a sobreposição; union junta; exclude vaza o encontro. x, y, w e h são frações da caixa da forma. combine_shapes com live true faz o mesmo com formas que já existem: as de cima viram operadores merge da de baixo e continuam editáveis.
- Um ou dois operadores bastam; a forma tem de continuar legível no tamanho do anúncio. Confira com studio_look.

### Curvas que parecem desenhadas
- Curva bonita é contínua: onde dois trechos C se encontram, a alça que chega, o ponto e a alça que sai ficam na mesma reta. S repete a alça espelhada sozinho e garante isso. Quina só onde o desenho pede quina.
- Poucos pontos: uma onda pede um ponto por crista e por vale, uma bolha de 4 a 6. Alças com cerca de 1/3 do trecho dão curva redonda; alças longas achatam e curtas viram quina mole.
- Onda de rodapé: "M0 0.35 C0.17 0.05 0.33 0.05 0.5 0.35 S0.83 0.65 1 0.35 L1 1 L0 1 Z" numa caixa larga e baixa, sangrando nas laterais. Para uma onda calma, a diferença de y entre crista e vale fica abaixo de 0.3 da caixa.
- Bolha orgânica, de propósito irregular (nenhum lado igual ao outro): "M0.5 0.02 C0.8 0.02 0.98 0.25 0.96 0.52 C0.94 0.82 0.7 0.98 0.45 0.96 C0.18 0.94 0.02 0.74 0.04 0.47 C0.06 0.2 0.24 0.02 0.5 0.02 Z". Duas ou três bolhas em tamanhos 1 : 0.6 : 0.35, em tons da paleta e atrás do produto, dão fundo sem roubar o foco.
- Canto contínuo (o canto dos ícones de aplicativo, sem a quebra visível onde a reta vira arco do radius comum): "M0.5 0 C0.92 0 1 0.08 1 0.5 C1 0.92 0.92 1 0.5 1 C0.08 1 0 0.92 0 0.5 C0 0.08 0.08 0 0.5 0 Z" numa caixa quadrada, para ícone, selo ou foto em destaque. Em retângulos largos, prefira radius ou roundCorners, que não deformam quando a caixa estica.
- Linha que conduz: um caminho aberto (swoosh, squiggle ou um C próprio) começa perto do título e termina apontando para o produto ou para o botão; traço fino, de uma cor só, com arrow_end_kind dot ou openArrow quando precisa dizer direção.
- Ritmo: a mesma curva repetida em escala (três ondas paralelas com traços 1 : 2 : 4, a mesma bolha em três tamanhos) dá unidade. Curvas de famílias diferentes na mesma peça brigam.
- Peça cheia de curvas ainda precisa de um eixo firme: formas orgânicas no fundo e nas bordas, texto alinhado numa margem reta por cima.
- Ilustração com volume ou detalhe (objeto, veículo, personagem, planta, cena) sai melhor gerada com IA e recortada; feita à mão em SVG ela fica tosca. Desenho que precisa ser vetor editável (ornamento, letra desenhada, ícone ou mascote chapado de poucas formas, um logo vetorial que o usuário colou): escreva ou cole o SVG e traga com import_svg; cada caminho vira uma camada de vetor editável dentro da caixa que você der. Texto, imagens e filtros do SVG ficam de fora: recrie o texto com add_text por cima.

## Detalhes de assinatura
Um detalhe que só esta peça teria vale mais que três enfeites genéricos.
- Quebrar a moldura: a forma (rect, ellipse, path_preset arch ou um path) e a foto logo acima com clip true, ou fill_frame numa moldura de frames. duplicate_layers da foto, update_layer na cópia com o mesmo x, y, w e h e clip false, e studio_start_job kind cutout com o layer_id da cópia. Com order_layers, o recorte fica acima da janela: dentro dela coincide com a foto, e fora só aparece o que passa da borda (a cabeça, a folha, o vapor da xícara, o telhado), de 0.03 a 0.08. Enquadre para o assunto escapar por uma borda só; as outras seguram a moldura. O título pode passar entre a foto e o recorte (profundidade-e-camadas).
- Moldura ornamental: rect só de contorno com recuo de 0.03 a 0.05 e uma segunda linha fina por dentro (set_path_ops offset com amount negativo). Desenhe um ornamento de canto uma vez (path com curvas C ou import_svg) e repita com duplicate_layers girado 90, 180 e 270, ou use repeat_layers mirror num ornamento simétrico do meio da borda. Abra a moldura onde o título passa: combine_shapes subtract com um rect atrás do título.
- Monograma: duas ou três iniciais do cliente, uma grande em serifa de alto contraste e outra menor em itálico ou de outra família, sobrepostas, com um contorno da cor do fundo na de cima para o entrelaçado ler. Dentro de um círculo, losango ou selo (shape star, inner perto de 0.9), com o nome em arco por curve. Serve de selo num canto, de marca d'água grande com opacity baixa cortada pela borda, ou no lugar de um enfeite genérico.
- Moldura ornamental e monograma vêm do gênero, não por padrão: cabem em herança e tradição, comida artesanal, luxo e convites (estilos-por-genero). Fora deles, só quando a ideia da peça pede; na dúvida, a peça fica sem.

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

Padrões regulares (xadrez, listras, grade, pontos, ondas, zigue-zague, confete) saem prontos com pattern numa forma só: um rect com pattern preset checker para a faixa de chegada, stripes com angle 45 para alerta, dots com tile hex-row para um fundo pop. size é o tamanho do ladrilho em pixels; background vazio deixa o fill aparecer entre os desenhos; uma foto ou textura da biblioteca também vira ladrilho com media_id. Para repetir uma peça desenhada (pétalas de flor, raios de sol, ícones em fileira, uma moldura simétrica), use repeat_layers em vez de duplicar: radial com count e radius gira as cópias em volta de um centro (upright true para texto), grid com rows, columns, gap_x e gap_y enfileira, mirror completa a outra metade de um desenho simétrico. As cópias acompanham a original; expand_repeat só quando cada cópia precisar de um ajuste próprio. O meio-tom com bolinhas de tamanho decrescente continua feito de muitas formas pequenas: o editor não tem limite de camadas. Crie em lote, alinhe com align_layers e distribute_layers e agrupe com group_layers para mover junto.

## Orçamento
- Uma família de motivo repetida 2 ou 3 vezes, no máximo uma textura e um recurso estrutural (moldura, grade ou faixa).
- Estilos limpos: até 5 elementos decorativos. Estilos maximalistas (colagem, rave, varejo): até 12, porque a densidade é o estilo.
- Decoração menor e com menos contraste que o herói, com uma família de traço (espessuras na proporção 1 : 2 : 4), e ou na grade ou claramente girada (nunca 1 ou 2 graus, que parece erro).

---
name: motion-design
surface: studio
description: Como criar motion design profissional no Estúdio (como a animação funciona, tempos, curvas com nome e cubic-bezier, chegada e saída das chaves, leitura do gráfico de velocidade e do pico de cada trecho, desfoque, desfoque de movimento e obturador, cenas, linhas que se desenham, ondulam e tremem, formas e máscaras que se transformam (morph), sombra, brilho e contorno animados no vídeo, âncora, escala por eixo, trajeto curvo e orientação pelo trajeto, revelação de logo e acessibilidade) e qual habilidade irmã carregar para cada parte; carregue antes de criar qualquer animação, vinheta ou vídeo de apresentação.
---

# Motion design no Estúdio

Peça nova do zero: valide o brief antes com a habilidade briefing-criativo (texto exato, datas, formato, duração, cores e o visual pedido).

## Conceito de movimento
Antes da primeira chave, decida em uma frase a ideia do movimento: o que se move, por quê e em qual batida (da música, da fala ou da leitura). Exemplos: "o preço cai como um carimbo na terceira batida", "a tela se monta como uma revista sendo diagramada", "tudo sobe devagar como vapor". Pense em duas ou três ideias, fique com a que mais combina com o conceito visual da peça e escolha um vocabulário abaixo; mantenha a mesma família de curvas, a mesma faixa de tempos e o mesmo tipo de transição no vídeo inteiro. Deslize de baixo com fade em todo elemento é a média, não uma ideia.
Ajuda resumir o conceito num verbo da marca: pulsar, deslizar, montar, revelar, empurrar, orbitar, carimbar, desdobrar. O verbo escolhe o resto:
- pelo menos duas famílias de entrada na peça (deslize, revelação por máscara ou wipe, foco que entra, escala, letra por letra, trajeto, morph, giro 3D), não a mesma entrada com tempos diferentes;
- deslocamento fora do eixo central: diagonais, entradas pela borda, trajetos em arco com set_path, cada mensagem num lugar diferente do quadro em vez de uma substituir a outra no mesmo centro;
- um momento herói com coreografia de verdade: partes que se sobrepõem e chegam em sequência, continuidade com acomodação, revelação por máscara (animate_mask), morph ou trajeto (auto_orient);
- saídas com intenção: o que sai empurra, abre caminho ou passa a vez para o que entra, em vez de tudo sumir no corte.
Mídia da biblioteca citada no brief (foto do produto, logo, vídeo) é o herói: entra com add_media, recortada com studio_start_job kind cutout e o clip_id quando precisa, e nunca é trocada por um desenho ou ícone no lugar dela.

## Vocabulários de movimento
- **Rápido e cinético:** entradas de 150 a 300 ms, chegada seca com easeOut forte ou cubic-bezier(0.05,0.7,0.1,1), escalonamento apertado (40 a 80 ms), herói em scale de 0,6 a 1, motion_blur nos deslizes, cortes na batida com whip, push ou zoom.
- **Editorial suave:** de 500 a 900 ms com cubic-bezier(0.45,0,0.55,1) ou easeInOut, revelação por máscara (animate_mask), texto por linha, blurDissolve ou dissolve, Ken Burns lento, nenhum exagero.
- **Brincalhão com quique:** backOut, spring e overshoot nos destaques, comprimir e esticar (scale_axes), pop e drop, rotação de 6 a 10 graus, cascata por letra, um elemento com bounce.
- **Revelação cinematográfica lenta:** de 1 a 3 s, foco que entra (blur de 16 a 0), luz que varre (lightLeak), escala de 1,08 para 1, câmera com nulo (add_null) e paralaxe, dipToColor e fades longos, um assunto por vez.
- **Glitch:** hold e saltos de 1 a 3 quadros, glitch e chromaticAberration curtos, posterizeTime com rate de 8 a 12, shake, cortes secos e troca de palavra; no máximo 3 flashes por segundo.
- **Coreografia tipográfica:** o texto é o elenco; add_text_animator com range_order centerOut ou random, espaçamento que fecha, palavras que empurram as outras, set_text_path, contador e troca de palavra, cada frase com uma entrada própria dentro da mesma família de curvas.
Misture no máximo dois, um dominante. A marca ou um preset salvo da equipe (studio_animation_presets) vem antes do vocabulário.

## Riqueza no movimento
Duas chaves de scale e opacity em todo clipe ficam corretas e planas. Quando o brief permite, some:
- ação secundária: o selo gira, a sombra cresce ou um brilho passa logo depois que o herói chega;
- sobreposição e continuidade: as partes chegam de 2 a 4 quadros depois da principal, com overshoot ou uma terceira chave de acomodação;
- paralaxe: fundo, meio e frente andando no mesmo sentido em distâncias diferentes (1, 2 e 4 vezes), por chaves ou por nulos;
- revelação mascarada: animate_mask, matte no texto, ou entrance com transition_kind wipe na caixa do clipe;
- morph: uma forma que vira o ícone ou o logo (animate com property path);
- trajeto: position em arco com auto_orient, ou uma linha que se desenha (trimEnd) levando o olho até a oferta;
- saída com intenção: exit ou animate_text side out, em vez de tudo sumir junto no corte.
Um herói por momento: a riqueza mora no fundo e nos detalhes, nunca disputa com a leitura.

Habilidades irmãs: principios-de-animacao (dar vida ao movimento e receitas como quicar, pulsar e partículas), gif-creator (loops curtos como GIF, figurinha ou reação), tipografia-cinetica (texto animado, escalonamento, máquina de escrever, destaque e troca de palavra) e transicoes (quando cortar, catálogo de transições e como montar cada uma).

Granulado, vazamento de luz, glitch, partículas, reflexo de lente, tremor de câmera, vinheta, aberração cromática, brilho, desfoques de velocidade, ondas, redemoinho, cantos livres, fundos gerados (ruído e degradês), barras que seguem o som, quadros travados e eco: carregue efeitos-visuais. Eco e desfoque direcional reforçam um movimento que já existe nas chaves; quadros travados em rate 8 a 12 dão cara de animação quadro a quadro a qualquer animação.

## Animações prontas da biblioteca
- A categoria stickers do studio_library_search traz animações feitas só de camadas nossas: círculos e retângulos desenhados à mão, sublinhados (reto, duplo, ondulado, ziguezague, marca-texto), setas que se desenham, visto e xis, traços de ênfase, brilho, coração, estrela, explosões, confete e palavras animadas com slot de texto (OBRIGADO, NOVO, PROMOÇÃO, OFERTA, GRÁTIS, COMPRE JÁ e outras).
- Use para apontar ou destacar algo que já está na tela, nunca como enfeite solto: o círculo em volta do preço, a seta para o botão, o sublinhado sob a palavra principal. Uma marca de destaque por cena; duas no máximo quando contam passos.
- Entre a marca logo depois de o assunto aparecer (uns 300 ms) e deixe parada até a fala ou o texto terminar. duration_ms estica a animação inteira, sem cortar o desenho; para ela durar mais sem ficar lenta, insira com a duração padrão e estique o clipe parado com trim_clip.
- As peças são clipes comuns e ligados: mude cor com colors no add_element ou update_clip, o tempo com as chaves, e o traço com set_line. Na imagem, o mesmo item entra parado no último quadro.

## Animações Lottie da pessoa
- Quando a pessoa tem um arquivo Lottie próprio (.json ou .lottie) na biblioteca, de tipo lottie em studio_read com detail full, traga com import_lottie(media_id, at_ms?, track_id?). Ele vira um grupo de clipes comuns: formas e contornos, chaves com as mesmas curvas, cortes de traçado como desenho de linha, máscaras e pré-composições como grupos internos, nulos onde havia camadas pai.
- Depois de importar, trate como qualquer grupo: entre nele para trocar cores, mexer em tempos e curvas, ou mova e escale o grupo inteiro. A animação entra centralizada e no tamanho do quadro; ajuste com update_clip no grupo.
- Se o arquivo usa recursos que não viram clipes (expressões, efeitos, imagens, textos, degradês, uma camada recortando outra), nada entra e a resposta lista o que impede, camada por camada. Conte isso à pessoa e ofereça refazer a peça com formas e chaves nossas, ou com as animações prontas da biblioteca.
- Só importe arquivos da própria pessoa. Não procure nem baixe animações de terceiros para ela.

## Gráficos animados
- Um gráfico do Estúdio (add_chart) anima sozinho com animate_chart, sem chaves à mão: grow sobe as barras a partir da base (e as linhas e áreas crescem do eixo), draw traça as linhas como uma caneta, sweep abre a pizza e a rosca girando a partir do topo, fade faz cada parte surgir. Eixos, grade e legenda ficam parados; só os dados entram. Os rótulos de valor aparecem junto com a parte deles.
- chart_order category faz entrar uma barra ou fatia depois da outra; series faz entrar uma série inteira por vez, o natural para linhas. stagger_ms de 80 a 200 ms dá ritmo de leitura às barras; na pizza, stagger_ms igual a duration_ms dá uma volta contínua.
- Barras: duration_ms de 400 a 900 ms com easeOut ou backOut. Linhas: 800 a 1500 ms com easeInOut. A entrada inteira (duração mais um intervalo por parte) deve caber em uns 2 segundos para não atrasar a leitura.
- Depois da entrada, deixe o gráfico parado tempo bastante para ler: pelo menos 2 segundos, mais 1 segundo por série. Destaque o número principal com um texto ou um contador ao lado, entrando logo depois da barra dele.
- Prefira animate_chart a barras desenhadas com add_shape e animate: o gráfico guarda os dados, se ajusta quando eles mudam e a entrada continua certa.

## Como a animação funciona aqui
- animate grava keyframes de x, y (centro, de 0 a 1), scale (1 é o tamanho atual), rotation (graus), opacity, blur (desfoque em pixels do quadro, 0 é nítido) e as cores fill e stroke de textos, formas e ícones (cada chave com color em #rrggbb; as cores se misturam entre as chaves). O at_ms conta do início do clipe. update_clip com blur deixa um desfoque fixo, como um fundo desfocado atrás de um texto.
- Desfoque animado: foco que entra (blur de 12 a 24 para 0 em 300 a 500 ms junto com a entrada), troca de assunto (o que sai desfoca) e ênfase de profundidade (fundo desfocado, texto nítido).
- A easing de uma chave vale para o trecho que sai dela. entrance e exit dão entradas e saídas prontas (effect fade e deslizes, ou transition_kind com qualquer transição visual medida na caixa do clipe, como um wipe que revela um logo; detalhes em transicoes); motion_preset aplica animações completas.
- Os campos de animate, das chaves, dos comportamentos e dos animadores de texto vêm de studio_op_guide (famílias animacao, chaves, comportamentos e texto): peça antes do primeiro lote da resposta.
- Crie e anime no mesmo lote com ref e @ref. O vídeo vai até 10 minutos e cada propriedade aceita quantas chaves precisar.
- A 30 quadros por segundo, 1 quadro dura 33 ms: 100 ms são 3 quadros, 300 ms são 9, 500 ms são 15.

## Princípios
- Uma ação por vez e um elemento que manda nos outros. O que está perto do gatilho reage primeiro.
- Entrar desacelerando, sair acelerando, mover acelerando e desacelerando. Movimento linear parece mecânico.
- Antecipação discreta antes de um movimento grande (5% a 10% para o lado oposto) e acomodação depois.
- Movimento conta algo: entrada, ênfase, troca de assunto. Movimento sem motivo só cansa.

## Tempos
- Pelo tamanho do que se move: detalhe pequeno de 150 a 250 ms; elemento médio de 300 a 500 ms; tela cheia ou destaque de 500 a 800 ms.
- Entradas de 400 a 800 ms; saídas de 20% a 30% mais curtas, de 250 a 500 ms. Saída nunca mais lenta que a entrada.
- Transições de cena de 400 a 700 ms.
- Texto fica parado o tempo de leitura (cerca de 3 palavras por segundo) mais 0,5 a 1 s.
- Elemento pequeno se move rápido; elemento grande ou que atravessa a tela leva mais tempo.

## Curvas (easing)
- Entrada e chegada desaceleram; saída acelera; mudança de lugar dentro do quadro acelera e desacelera. Movimento linear parece mecânico.
- linear só para movimento constante (giro contínuo, Ken Burns lento, barra de progresso) e para trocas de opacidade.
- hold para trocas de uma vez: contador, texto que muda, efeito de máquina de escrever.
- Curvas com nome: easeOut, easeIn, easeInOut (suaves); backOut passa uns 10% do alvo e volta; backIn recua uns 10% antes de sair (antecipação); backInOut faz os dois; spring chega com impulso de mola (passa uns 16% e se acomoda); elastic vibra em volta do alvo (passa até 37%); bounce quica no alvo.
- Curvas exatas da indústria com cubic-bezier(x1,y1,x2,y2), escritas como no CSS:
  - entrada enfática, nítida e moderna: cubic-bezier(0.05,0.7,0.1,1); entrada calma: cubic-bezier(0,0,0.3,1);
  - saída enfática: cubic-bezier(0.3,0,0.8,0.15); mudança de lugar padrão: cubic-bezier(0.2,0,0,1);
  - editorial, simétrica e elegante: cubic-bezier(0.45,0,0.55,1); exagero brincalhão: cubic-bezier(0.34,1.56,0.64,1).
  - y abaixo de 0 ou acima de 1 faz a curva passar do ponto; x fica entre 0 e 1.
- Escolha pelo tom: produtivo e corporativo pede curvas sem exagero e tempos curtos; expressivo e jovem aceita backOut e spring nos momentos de destaque; elastic e bounce são cômicos, use em um elemento só e só quando o tom pedir.
- Exagero nunca em opacity (ela é limitada a 0 e 1) e nunca em todos os elementos.
- Animações prontas com essas curvas: motion_preset pop (cresce passando do tamanho), drop (cai e quica) e springIn (entra com mola).
- Escala: entrada sutil de 0,92 a 0,96 para 1; entrada marcante de 0,6 a 0,8 para 1. Nunca de 0 em elementos grandes. Com backOut, spring ou elastic o pico passa do valor da chave; o clipe animado precisa caber em 4 vezes o quadro contando esse pico.
- Deslocamentos de 2% a 8% do quadro. Ken Burns de 1 para 1,08 a 1,15 durante a cena.

## Chegada e saída das chaves
- Cada chave tem dois lados: a chegada (o fim do trecho que vem da chave anterior) e a saída (o começo do trecho que vai para a próxima). O easing gravado numa chave vale para o trecho inteiro até a próxima; ease_keys muda só um lado.
- ease_keys side in suaviza a chegada: o movimento desacelera até parar na chave (pousar, frear, encaixar). side out suaviza a saída: parte devagar e acelera (arrancar). both faz os dois: para na chave e sai devagar, como algo que pausa no meio do caminho.
- Sem key_speed nem influence, o lado vai a velocidade 0 com 33,3% de influência, a suavização padrão. Influence de 60 a 80 segura mais tempo perto da chave e dá um pouso elegante; abaixo de 20 quase não muda nada.
- key_speed é a velocidade daquele lado por segundo, na unidade dos valores: x 0,5 cruza meio quadro em um segundo. Para emendar dois movimentos sem tranco, dê à chave a mesma velocidade na chegada e na saída (side both com um key_speed só) e o embalo continua.
- smooth_keys deixa a chave automática: a velocidade passa contínua pela chave e se recalcula quando as chaves mudam. Bom para trajetos com várias chaves que não devem parar, como uma câmera que passeia. Num pico (o valor sobe e desce) a chave automática para em vez de passar do ponto.
- reverse_keys toca as chaves ao contrário no mesmo trecho, então uma entrada vira saída; time_stretch_keys com percent 150 deixa tudo 50% mais lento sem mudar os valores; rove_keys espalha as chaves do meio para a velocidade ficar constante.
- studio_read full mostra, em cada chave, a influência e a velocidade da chegada e da saída: confira que um pouso não ficou com velocidade alta na chegada.

## Ler o gráfico de velocidade
- O gráfico de velocidade mostra quão rápido o valor muda a cada instante, não onde ele está. Altura alta é movimento rápido; a linha no zero é parado. Num trajeto de posição a velocidade é medida ao longo do caminho, em alturas do quadro por segundo, então nunca fica abaixo de zero.
- Formas que você vai reconhecer: uma rampa que sobe é aceleração (arrancar); uma rampa que desce até zero é desaceleração (pousar); um morro com o topo no meio é o ease in e out de uma mudança de lugar; uma linha reta e alta é velocidade constante, que parece mecânica; um degrau vertical numa chave é um tranco, porque a chegada e a saída têm velocidades diferentes.
- Para um pouso elegante, a curva chega ao zero devagar e deitada perto da chave (influence alta na chegada). Para um impacto, chega alta e cai de uma vez. Para emendar movimentos sem tranco, a altura na chegada e na saída da chave do meio é a mesma.
- O pico mostra a energia do movimento: quanto mais estreito e alto, mais rápido e curto o gesto. Dois elementos que devem andar juntos precisam de picos parecidos e no mesmo instante; picos escalonados de 40 a 80 ms dão o efeito cascata.
- Cada chave em studio_read full traz o pico de velocidade do trecho que sai dela, na unidade dos valores por segundo. Use para conferir o ritmo sem ver o gráfico: um pico muito maior que os vizinhos é um trecho apressado; picos parecidos em trechos seguidos dão um ritmo uniforme; um pouso deve terminar com a chegada em velocidade 0 mesmo quando o pico do trecho é alto.
- As pessoas editam o mesmo dado no gráfico do Estúdio (botão Gráfico no foco do clipe, Shift+F3): arrastar a alça no gráfico de velocidade muda key_speed e influence daquele lado, exatamente como ease_keys.

## Escalonamento
- Elementos que entram juntos chegam um depois do outro, com a mesma entrada e a mesma curva, somando no máximo cerca de 600 ms. Os atrasos por letra, palavra e linha e as receitas de texto estão em tipografia-cinetica.
- Para clipes inteiros, stagger_clips com step_ms faz a cascata numa operação: dê a mesma entrada a todos antes (entrance ou motion_preset com clip_ids) e depois escalone; as chaves andam com cada clipe.
- O mesmo papel usa sempre a mesma entrada.

## Layout no quadro
- Alinhe em vez de digitar posições: align_clips com frame_align left, center, right, top, middle ou bottom. Um clipe sozinho alinha pelo quadro; vários alinham pela caixa que os junta, ou pelo quadro com relative_to frame.
- distribute_clips com três ou mais clipes deixa espaços iguais na horizontal ou na vertical (axis); os das pontas ficam onde estão. Bom para ícones, cartões e listas.
- As duas leem a posição no instante at_ms (padrão o cursor de tempo). Um clipe com posição animada ganha uma chave nesse instante, então alinhe no momento em que ele está parado, depois da entrada.
- Margens: os textos importantes ficam dentro de 6% das bordas e acima de 20% da base, a área segura das redes. studio_read full mostra as bordas de cada clipe na tela, em pixels do quadro, para conferir o alinhamento e as margens.

## Cenas
- Monte em cenas em sequência: o fundo da cena na faixa de baixo, textos e detalhes acima. Cenas em sequência reaproveitam as faixas.
- add_text sem track_id fica acima do que está na tela; um fundo novo vai numa faixa abaixo (track_id).
- Cores e contraste seguem a marca e as regras de design-de-imagem. Sombra só sobre foto ou vídeo, translúcida.

## Transições
- O padrão é o corte; transição precisa de motivo. Catálogo, durações e montagem de cada uma estão em transicoes.

## Máscaras
- A máscara recorta um clipe em coordenadas de 0 a 1 da caixa do próprio clipe, não da tela. Várias máscaras combinam em ordem: add soma área, subtract recorta, intersect mantém só o cruzamento; invert troca dentro e fora.
- feather de 0,02 a 0,1 dá borda natural em vídeo; 0 fica gráfico, bom para formas de marca. expansion cresce ou encolhe a área sem mexer no tamanho.
- Vinheta: um retângulo preto sobre o vídeo, inteiro na tela, com add_mask ellipse w 1,1, h 1,1, invert, feather 0,3 e opacity 0,4 a 0,6. As bordas escurecem e o centro fica limpo.
- Tela dividida: dois vídeos em faixas diferentes; o de cima com add_mask rectangle x 0,75, w 0,5, h 1 mostra a metade direita e o de baixo aparece na esquerda. Uma linha ou forma fina na emenda esconde a costura.
- Revelação: animate_mask com mask_property w de 0,001 até o tamanho final (ou uma linear com x andando de -0,1 a 1,1) em 400 a 800 ms com easeInOut; o clipe nasce da forma.
- Foco num detalhe: retângulo escuro translúcido por cima com add_mask ellipse mask_mode subtract sobre o detalhe; animate_mask em x e y acompanha o detalhe se ele se mexe.
- Aparecer só num momento: animate_mask com opacity de 0 a 1 no instante certo, ou w e h saindo de 0,001. As chaves usam at_ms dentro do clipe.
- Texto com vídeo dentro: o texto numa faixa e o vídeo na faixa logo acima com update_clip matte true; o vídeo só aparece dentro das letras. Texto grosso (font_weight 800 ou mais) e grande, para a imagem caber.
- Texto vazado (o vídeo em volta e as letras mostrando o que está embaixo): o mesmo arranjo com matte_invert true; o clipe de cima aparece onde o texto não tem imagem.
- Recorte por brilho do clipe de baixo (matte_channel luma): o claro mostra o clipe de cima e o escuro esconde. Serve para revelar por um vídeo de tinta, fumaça ou transição de banco em preto e branco; matte_invert troca claro por escuro. Com matte_channel alpha (padrão) vale só onde o clipe de baixo tem imagem.
- O recorte lê a imagem do clipe de baixo e a opacidade dele: um fade no texto apaga junto o vídeo recortado.
- Confira a máscara parada e em movimento com studio_look focus_id no clipe.
- Com focus_id ou from_ms e to_ms, a resposta de studio_look traz motion_report: os valores, tempos e easing de cada movimento da janela e os problemas medidos (salto entre quadros, saída da tela no meio do caminho, textos que se cruzam, movimento longo linear, flash). Os quadros mostram poses; motion_report mostra o caminho entre elas.

## Formas que se transformam (morph)
- animate com property path muda o desenho de uma forma ao longo do tempo: cada chave traz path, o caminho SVG daquele momento, de 0 a 1 na caixa do clipe. rect, ellipse, triangle e star viram path sozinhas na primeira chave; textos, linhas, setas e mídia não animam o caminho.
- As chaves podem ter números diferentes de pontos: o editor reamostra, acerta o sentido do desenho e escolhe o primeiro ponto que menos distorce. Desenhe as duas formas no mesmo sentido e com os pontos fortes (pontas, cantos) parecidos em número quando quiser uma transformação limpa.
- first_vertex escolhe qual ponto de uma chave encontra o primeiro ponto da chave vizinha. Use para a forma girar enquanto muda (o ponto de cima de um quadrado indo para a ponta da direita de uma estrela) ou para desfazer uma torção que o casamento automático escolheu.
- Tempos: de 300 a 600 ms para trocar um ícone (play para pausa, menu para fechar, coração vazio para cheio), de 600 a 1200 ms para uma forma orgânica que respira ou uma mancha que vira logo. easeInOut ou cubic-bezier(0.65,0,0.35,1) na maior parte; backOut para a forma chegar com um leve passar do ponto; hold quando a troca precisa ser seca, de um quadro para o outro.
- Mancha viva de fundo: três ou quatro chaves de blobs parecidos, a última igual à primeira, em loop pela duração do clipe; movimento lento (2 a 4 s por ciclo) e cor suave, para não roubar a atenção do texto.
- Máscara que se transforma: animate_mask com mask_property path numa máscara path revela o vídeo por uma forma que muda (um círculo que vira a silhueta de um produto, uma onda que sobe). Junte com feather de 0,02 a 0,05 para a borda não serrilhar.
- Uma transformação por vez, com contorno ou preenchimento fortes; formas muito detalhadas mudando ao mesmo tempo viram ruído. Confira no meio da transformação com studio_look focus_id no clipe e at_ms no meio entre as chaves.

## Sombra, brilho e contorno no vídeo
- update_clip com fills, strokes e effects funciona como no editor de imagem em textos, formas e ícones: várias tintas, vários contornos e os quatro efeitos (drop-shadow, inner-shadow, outer-glow, inner-glow). Mande a lista inteira; um effect com o id que já existe mantém os valores e as chaves dele.
- Em clipes de vídeo e imagem, strokes e effects (só drop-shadow e outer-glow) seguem a silhueta do clipe: a borda da imagem, o recorte das máscaras e as partes transparentes de um PNG. Recorte de produto ou pessoa com contorno branco de 6 a 12 px vira adesivo; um vídeo menor por cima de outro (picture-in-picture) ganha profundidade com drop-shadow de blur 20 a 40, y de 8 a 16 e opacity de 0,3 a 0,5.
- animate_effect anima um valor do efeito com as curvas das chaves. Brilho que pulsa num CTA: opacity de 0,4 a 1 ou spread de 0 a 6 em 600 a 900 ms com easeInOut, ida e volta; deixe o brilho parado no resto do tempo. Sombra que acompanha uma luz que anda: x e y das chaves no sentido oposto da luz. Objeto que sobe: blur e y da sombra crescendo junto com scale, a sombra mais suave e mais longe.
- Um brilho animado por cena; brilho em tudo vira ruído. Cor do brilho tirada da marca ou do destaque, nunca branco puro sobre fundo claro.
- Cada chave de efeito redesenha a camada a cada quadro: prefira animar um ou dois valores por clipe. Confira com studio_look focus_id no clipe, num quadro do pico e num do vale.

## Linhas que se desenham, ondulam e tremem
- Vale para linhas, setas e formas com contorno (stroke e stroke_width). set_line liga os grupos: desenhar o traço (trim_start, trim_end, trim_offset), onda (wave_*), tremor (wiggle_*) e pontas afinadas (taper_*); animate com property trimEnd, waveAmount, wiggleSize e as outras anima cada valor com as curvas das chaves. O preenchimento da forma fica parado, só o traço se mexe.
- Desenhar o traço: animate trimEnd de 0 a 1. Sublinhado, seta ou check de 300 a 500 ms; contorno de forma ou ícone de 600 a 1000 ms; caminho longo, assinatura ou rota num mapa de 1,2 a 2 s. Curva easeInOut ou cubic-bezier(0.65,0,0.35,1) para o traço que se desenha sozinho; easeOut quando ele chega em algo (sublinhar uma palavra). linear só para traço de ritmo constante, como um contador ou uma barra.
- Apagar o traço: depois de desenhado, animate trimStart de 0 a 1 com easeIn, de 20% a 30% mais rápido que a entrada. Desenhar e apagar no mesmo sentido parece o traço passando.
- Traço que viaja pelo caminho: deixe um trecho curto (trim_end menos trim_start de 0,1 a 0,25) e anime trimOffset de 0 a 360 com linear para uma volta. Calmo: de 1,5 a 3 s por volta; nervoso: menos de 0,8 s.
- Formas com vários traços: trim_sequential true desenha um traço depois do outro, como uma letra escrita; false desenha todos juntos.
- Onda que se move sozinha: wave_speed é ondas por segundo. De 0,3 a 0,8 é calmo (água, respiração, fundo); de 1 a 2 é vivo; acima de 3 fica nervoso ou elétrico. wave_amount de 1 a 3 vezes a espessura do traço é sutil, de 4 a 8 vezes é marcante; wave_length de 80 a 200 px fica suave, abaixo de 40 px vira serrilhado. Onda alta num traço fino some; onda curta e rápida cansa.
- Para a onda nascer suave, anime waveAmount de 0 ao valor com easeOut em 400 a 600 ms e deixe a velocidade fixa. Mudar wave_speed com chaves acelera sem pulo (a fase acompanha a velocidade); use easeInOut nessas chaves. wave_mode width faz a espessura pulsar: sinal de áudio, eletricidade, contas num fio.
- Tremor: wiggle_rate de 1 a 2 tremores por segundo é orgânico, como traço feito à mão; de 3 a 5 é vivo; acima de 8 é nervoso, choque ou falha. wiggle_size de 1 a 3 px imita o desenho à mão; de 6 a 15 px é um tremor visível. wiggle_detail de 1 a 3 faz curvas largas; de 5 a 10, um tremido fino. Desenho à mão: tremor baixo e lento, pontas afinadas e o traço se desenhando. Troque wiggle_seed para que linhas vizinhas não tremam iguais.
- Pontas afinadas: pincel ou caligrafia com taper_start_length de 0,1 a 0,2, taper_end_length de 0,2 a 0,4 e larguras 0; ease positivo arredonda a ponta, negativo afina longo. Com o traço se desenhando, a ponta acompanha a frente do traço.
- Loop limpo: o período da onda é 1 dividido por wave_speed (1 onda por segundo repete a cada 1 s); escolha a duração do clipe como múltiplo dele. Para o traço que viaja, uma volta inteira (360) por ciclo.
- Um ritmo por cena: linhas vizinhas com a mesma velocidade ou múltiplos dela. Tremor e onda o vídeo inteiro sem motivo distraem; ligue no destaque e desligue (waveAmount ou wiggleSize para 0) quando o assunto muda.
- Confira com studio_look focus_id no clipe: os quadros mostram o traço em cada instante.

## Formas vivas (operadores de caminho)
- Os números dos operadores de caminho (set_path_ops) animam com animate_path_op e as curvas de sempre: offset amount de 0 a 20 px com backOut faz a forma pulsar ou ganhar um contorno que nasce; zigZag size de 0 ao valor faz a borda vibrar ao chegar; twist angle de 0 a 180 com easeInOut torce na transição; puckerBloat de -30 a 30 em loop respira.
- Recorte que anima: merge subtract com w e h crescendo de 0,05 a 1 abre um buraco que revela o que está atrás (transição de íris dentro da forma); x animado faz uma mordida passar.
- Uma mudança de contorno por vez, de 300 a 800 ms. Valores grandes de offset ou twist mudando o tempo todo viram ruído.

## Repetidor
- set_repeat em textos, formas, ícones e imagens sobrepostas cria cópias vivas do clipe. mode linear: copies em fila, cada uma andando x e y pixels do quadro, girando rotation graus e mudando de tamanho por scale em relação à anterior; start_opacity e end_opacity fazem as cópias sumirem aos poucos; order below deixa a original por cima.
- Rastro: linear com copies de 4 a 6, x de 40 a 80 px, end_opacity de 0,1 a 0,2 e order below num objeto que se move. Eco: copies 3 a 4, scale de 1,1 a 1,2, end_opacity 0, order below. Espiral: copies de 10 a 16, rotation de 20 a 40, scale de 0,9 a 0,95. Escada: x e y juntos.
- Animar: animate_repeat com repeat_property shift de 0 a 1 faz a fila andar um passo (com linear e um clipe de duração inteira, vira esteira em loop); copies de 1 ao valor faz as cópias nascerem uma a uma; radius no radial abre o círculo; end_opacity anima o rastro sumindo.
- radial, grid e mirror são os mesmos modos do editor de imagem (count, radius, start_angle, sweep, upright; rows, columns, gap_x, gap_y; angle, gap). Mude o original e todas as cópias mudam; as cópias não são clicáveis.
- Confira com studio_look focus_id no clipe em dois ou três instantes.

## Âncora, escala por eixo e trajeto curvo
- A âncora é o ponto em volta do qual o clipe gira e cresce; x e y continuam sendo o centro da caixa. set_anchor com anchor (nove pontos prontos, de top-left a bottom-right) ou com anchor_x e anchor_y em frações da caixa, de -1 a 2 (fora de 0 a 1 o pivô fica fora do clipe). keep_visual true, o padrão, deixa a imagem parada e corrige a posição; false só troca o pivô.
- Escolha a âncora pelo gesto: porta, cartão ou página que abre gira pela borda (left ou right); pêndulo, placa pendurada e ponteiro giram pela ponta de cima (top, ou anchor_y -0,2 para um ponto de pendurar acima da caixa); barra de gráfico cresce da base (bottom); sublinhado e barra de progresso crescem da esquerda (left); balão de fala cresce da ponta (bottom-left ou bottom-right); peça que tomba como dominó gira pelo canto de baixo. Selo, ícone e botão crescem do centro.
- Troque a âncora antes de animar rotation e scale. Com rotation ou scale já animados, a imagem fica no lugar só no instante do cursor de tempo e o giro muda nos outros; com x e y animados, as chaves de posição são corrigidas uma a uma.
- Escala por eixo: scale_axes com separate true troca scale por scaleX e scaleY; separate false junta de volta (keep_axis escolhe o eixo quando as chaves diferem). Comprimir e esticar dá peso: no impacto scaleX 1,15 e scaleY 0,85 por 60 a 100 ms e volta a 1 com easeOut ou spring; no salto estica (scaleY 1,1 e scaleX 0,92). Mantenha scaleX vezes scaleY perto de 1 para o objeto não parecer inchar.
- Com a âncora na base (bottom), o mesmo comprimir e esticar fica no chão em vez de flutuar. Barra de gráfico: anchor bottom e scaleY de 0,05 a 1 em 400 a 700 ms com easeOut, uma barra depois da outra.
- Virar um cartão: scaleX de 1 para 0,05 em 150 a 200 ms com easeIn, o verso entra no mesmo instante (outro clipe, ou o mesmo com flip_x) e vai de 0,05 a 1 com easeOut.
- Esticar um texto deforma as letras: em texto, mude o tamanho com w e h (o texto refaz as linhas) e deixe scaleX e scaleY para efeitos curtos.
- flip_x e flip_y (update_clip) espelham a imagem dentro da caixa sem mover o clipe: um personagem, carro ou seta virado para o lado em que anda. Texto e logo espelhados ficam ilegíveis ou fora da marca.
- Trajeto curvo: quase todo movimento natural faz arco. animate com property position grava chaves com x e y; a forma em cada chave vem de spatial: auto (o padrão) passa suave por todas as chaves; linear faz quina (zigue-zague, ricochete seco); smooth usa as alças in e out alinhadas (out aponta para o lado oposto de in) para desenhar a curva que você quer; free quebra a curva na chave (quique, mudança de rumo com curva).
- Três chaves (partida, ponto alto do arco, chegada) desenham um arco limpo; chaves demais deixam o trajeto nervoso. Alças de 0,05 a 0,2 do quadro dão curvas suaves; alças longas demais fazem laço.
- A easing da chave controla a velocidade ao longo da curva, não a forma dela: easeInOut para sair e pousar; easeOut para chegar; linear para velocidade constante (carro, avião, órbita, partícula). Uma curva que passa do ponto (backOut, spring) segue reto na direção da chegada antes de voltar.
- set_path com curved true transforma chaves de x e y já feitas num trajeto curvo com curvas automáticas; curved false separa de volta em x e y (o trajeto curvo vira chaves lineares que seguem a curva). position não anima junto com x e y, nem scale junto com scaleX e scaleY: animate troca as chaves rivais e avisa.
- auto_orient (update_clip) soma à rotação o rumo do trajeto, então o clipe aponta para onde anda: avião de papel, seta, carro, peixe, cometa. O rumo 0 é andar para a direita: um desenho que aponta para cima precisa de rotation 90 para a ponta ir na frente. Sem position, auto_orient não muda nada.
- Junte as três: âncora na roda de trás de um carro com auto_orient, ou na base de um balão que sobe em arco com um leve comprimir no pouso.
- Confira com studio_look focus_id no clipe: compare posição, rotação e escala de cada quadro com o arco que você quis; pontos do trajeto muito afastados entre dois quadros mostram trechos rápidos demais.

## Vínculo (pai e filho) e nulos
- parent_clip com parent_id faz um clipe seguir outro: o filho acompanha a posição, a rotação e a escala do pai em volta do ponto de âncora do pai. A opacidade não passa do pai para o filho; o vínculo vale entre faixas diferentes e um pai pode ter pai. Fora do trecho do pai, o filho segue a última pose dele (ou a primeira, antes dele começar).
- keep_position true, o padrão, deixa a imagem parada no instante de at_ms e converte os números para o espaço do pai; false faz o clipe pular, porque os mesmos números passam a ser lidos no espaço do pai. unparent true solta e também mantém a imagem.
- Enquanto vinculado, x, y, w, h e rotation do filho ficam no espaço do pai (o studio_read mostra parent no clipe); o centro do quadro do pai é o ponto de âncora dele, então x 0,5 e y 0,5 põem o filho sobre esse ponto. Anime o filho normalmente: o movimento dele soma ao do pai.
- add_null cria um nulo: um controle invisível, que não aparece no vídeo, só para conduzir filhos. Anime o nulo com x, y, position, rotation, scale, scaleX e scaleY; o tamanho da caixa do nulo é a escala que ele passa aos filhos.
- Use um nulo para mover um grupo como uma peça só: logo e texto que entram juntos, um cartão com ícone e legenda, um movimento de câmera que empurra a cena toda (escala de 1 a 1,08 em 3 a 5 s com easeInOut). Ponha o nulo onde o grupo deve girar e crescer (o centro do grupo, ou a âncora de onde ele nasce) antes de vincular.
- Planetas e engrenagens: um nulo girando com outro nulo vinculado nele dá órbita dentro de órbita. Um ponteiro de relógio é um filho com âncora na ponta de baixo, vinculado a um nulo que gira.
- Vínculo em círculo é recusado; apagar o pai passa os filhos para o pai de cima (ou deixa soltos) mantendo a imagem. Uma escala diferente por eixo no pai, com o filho girado, não deforma o filho: a caixa continua um retângulo.

## Grupos (pré-composição)
- group_clips(clip_ids, name?) junta clipes num clipe de grupo com a própria linha do tempo por dentro, como uma pré-composição: as faixas e os clipes de dentro mantêm o tempo entre si. Por fora o grupo é um clipe só: mova, corte, acelere, anime posição, escala e rotação, ponha máscara, efeito, mistura, recorte (matte), cor, desfoque, transição e vínculo de pai, e tudo vale para a imagem já composta do grupo inteiro.
- Use quando várias peças precisam de um tratamento único: um cartão com fundo, ícone e texto que entra com uma máscara só, um bloco de títulos que desfoca junto, uma cena que gira e encolhe para dar lugar à próxima, um logo montado em várias camadas que vira um elemento só. Para só mover peças juntas, um nulo (add_null com parent_clip) é mais leve; o grupo é para quando a máscara, o efeito, a opacidade ou a mistura precisam valer para o conjunto, sem somar por peça (duas peças sobrepostas com 50% de opacidade cada não dão o mesmo que o grupo inteiro em 50%).
- Para editar por dentro, use group_id na operação (o id do grupo ou o @ref de group_clips): os tempos ficam na linha do tempo de dentro do grupo, onde 0 é o começo do conteúdo. studio_read mostra as faixas e os clipes de dentro embaixo de cada grupo, com a duração do conteúdo.
- Dentro do grupo, clipes de efeito, recortes e misturas só enxergam o que está dentro do mesmo grupo; um pai e o filho ficam na mesma linha do tempo. Grupo dentro de grupo funciona.
- ungroup_clips(clip_id) devolve o conteúdo para a linha do tempo nos mesmos instantes e na mesma ordem de faixas, cortando o que estava fora do corte do grupo; se o grupo foi movido, girado ou escalado, um nulo herda essa animação e o conteúdo segue o nulo. O que não dá para repartir sem mudar a imagem (opacidade, fades, máscaras, efeitos, mistura, cor, desfoque, transições, velocidade diferente de 1) precisa sair do grupo antes; a recusa diz o que tirar.

## Predefinições de animação

- Antes de animar à mão, veja studio_animation_presets: a marca pode já ter uma entrada ou um movimento salvo pela equipe, e repetir o mesmo movimento em todas as peças dá consistência.
- apply_animation_preset(preset_id, clip_ids, preset_timing): stretch serve para movimentos contínuos (respirar, flutuar); edges serve para entradas e saídas, porque mantém a velocidade delas em clipes longos ou curtos; keep serve quando o tempo exato importa (uma batida). As chaves de posição são relativas à caixa do clipe, então o mesmo deslize funciona em qualquer lugar do quadro.
- Quando criar uma animação que a pessoa aprovou, ofereça salvar com studio_save_animation_preset e um nome que diga o efeito ("Entrada do logo da marca"), não o clipe.

## Comportamentos (tremor, repetição, giro, balanço e som)
- Comportamento é uma animação que roda sozinha numa propriedade e soma ao valor das chaves: add_behavior(clip_id, property, behavior_kind, behavior). Vale para x, y, scale, scaleX, scaleY, rotation, opacity e blur (nulos só para posição, escala e rotação). Prévia e exportação dão o mesmo valor no mesmo quadro, então o que você confere com studio_look é o que sai no arquivo.
- wiggle treme ao acaso: frequency é quantas tremidas por segundo e amount o quanto vai para cada lado. Sutil e vivo: frequency 1 a 2 com amount pequeno (x ou y 0,005 a 0,01, rotation 1 a 3 graus) para uma câmera na mão ou um objeto flutuando. Nervoso: frequency 6 a 12 para choque, alarme, risada. seed troca o desenho do tremor; dê seeds diferentes a x e a y, ou a dois clipes, para não tremerem iguais.
- loop repete as chaves fora delas: cycle volta ao começo (pulso de botão, piscar), pingpong vai e volta (balanço de placa, respirar), offset repete somando (escada, contador que sobe), continue segue na velocidade da última chave (algo que sai voando). side out repete depois da última chave, in antes da primeira, both dos dois lados. Faça duas ou três chaves de um ciclo bonito e deixe o loop cuidar do resto do clipe; para um GIF que fecha sem pulo, o ciclo precisa caber inteiro na duração.
- drift anda sem parar: rate é quanto por segundo. Em rotation é um giro contínuo (90 é um quarto de volta por segundo; um selo ou um disco gira de 30 a 60). Em scale, um zoom lento (0,01 a 0,02 por segundo) dá vida a uma foto parada; em x, um fundo que corre.
- overshoot balança depois de cada chave, pela velocidade com que a propriedade chega nela: é a inércia natural de algo que para. amplitude 0,03 a 0,08 fica elegante, frequency 2 a 4, decay 6 a 10 assenta em menos de um segundo. Precisa de pelo menos duas chaves; funciona melhor com chaves de chegada rápida (linear ou easeIn) do que com easeOut, que já chega parado.
- audioReact segue o volume de uma faixa de som: amount é quanto soma no som mais alto (scale 0,1 a 0,2 faz um logo pulsar na batida; negativo encolhe ou apaga), floor_db é o volume em que fica parado e ceiling_db o volume em que soma tudo. Para a batida aparecer, use uma faixa com a música sozinha; voz e música juntas viram um pulso sem ritmo. O volume é lido antes do volume da faixa e do ducking.
- Um comportamento de cada tipo por propriedade; tipos diferentes somam (chaves de entrada mais wiggle leve mais audioReact). update_behavior muda os ajustes, remove_behavior tira.
- bake_behavior troca o comportamento por chaves lineares comuns, uma por quadro, quando você quer ajustar à mão um trecho ou levar a animação para outro clipe. loop e overshoot da mesma propriedade vão junto, porque leem as chaves. Em x ou y com trajeto curvo, separe o trajeto com set_path antes; em scale com scaleX e scaleY, junte ou separe com scale_axes antes.
- Não exagere: tremor em texto que precisa ser lido atrapalha a leitura, e tudo se mexendo ao mesmo tempo cansa. Deixe a mensagem principal firme e use comportamento no que é fundo, ícone ou detalhe. Confira com studio_look em dois ou três instantes e com studio_read (behaviors do clipe).

## Desfoque de movimento
- update_clip com motion_blur true borra o clipe na direção em que ele anda, gira ou cresce, como o obturador de uma câmera. O rastro vem do próprio movimento (chaves, trajeto curvo, pai que se move, entrada deslizando, tremor): clipe parado continua nítido, então ligar não estraga os momentos de leitura.
- Ligue nos movimentos rápidos que precisam parecer filmados: cartão que entra varrendo a tela, logo que gira e para, texto que sobe com mola, troca de cena empurrada, zoom forte. Em movimento lento o rastro é quase invisível e só gasta tempo de exportação.
- Não ligue em texto que precisa ser lido enquanto anda devagar, nem em letreiros e preços parados; borrar letra em movimento curto atrapalha a leitura. Deixe a frase assentar nítida e use o rastro só na chegada e na saída.
- O obturador é do projeto: set_canvas shutter_angle (padrão 180, o rastro natural de cinema; 270 a 360 dá um rastro longo e dramático para esporte e ação; 90 deixa seco e nervoso; 0 desliga tudo) e shutter_phase (padrão -90, rastro centrado no quadro; 0 põe o rastro depois do quadro, como um cometa que segue o objeto).
- Combine com as curvas: um easeOut forte chega rápido e para limpo, então o rastro aparece na largada e some quando o clipe assenta. Com backOut e spring o rastro acompanha a passada do ponto e a volta.
- Mesmo resultado na prévia e na exportação. Enquanto a pessoa arrasta o cursor na linha do tempo a prévia pode pular o desfoque para responder rápido; o vídeo exportado sempre tem.
- Confira com studio_look nos quadros do meio do movimento: o rastro deve seguir a direção do caminho e o clipe deve ficar nítido nas chaves de parada.

## Rotação 3D e letras virando
- rotationX e rotationY giram o clipe em profundidade pela âncora, como uma carta: rotationY de 0 a 180 vira o cartão e mostra o verso espelhado, rotationX de 90 a 0 faz um painel cair de pé, como uma porta de garagem fechando. Em 90 graus o clipe fica de lado e some; use isso para trocar a frente pelo verso no meio do giro (dois clipes, um saindo de 0 a 90 e o outro entrando de -90 a 0).
- A âncora decide a dobradiça: âncora na borda esquerda abre como uma porta, na borda de cima como uma aba que cai; no centro o clipe gira no lugar. Ajuste a âncora antes de animar.
- Inclinações pequenas e paradas (10 a 25 graus) dão profundidade a mockups de tela, cartões de produto e fotos em sequência sem distrair; giros inteiros cansam rápido e servem para revelar, não para enfeitar.
- A perspectiva é do projeto: set_canvas perspective em graus (40 é o padrão, natural; 60 a 90 exagera o lado de perto, bom para impacto; 0 deixa plano, só achata). O ponto de fuga é o centro do quadro, então um clipe na borda gira com mais distorção que um no meio.
- Curvas: easeOut ou backOut na chegada do giro, para o clipe assentar de frente; um giro linear parece máquina. Um leve overshoot (backOut) num cartão que vira dá peso.
- Letras virando: nos animadores de texto, rotationX ou rotationY em animator_properties gira cada letra (ou palavra) em profundidade. O pronto flipIn faz as letras entrarem tombando em cascata. Em palavras, a palavra gira inteira em volta do próprio centro. Junte com opacity 0 para a letra aparecer durante o giro.
- O giro de um pai não passa para os filhos; para girar um conjunto, agrupe (group_clips) e gire o grupo. Confira com studio_look no meio do giro: o lado que vai para trás deve diminuir e o clipe não pode atravessar a câmera.

## Contadores e texto em caminho
- Número que conta (clientes, preço, porcentagem): set_counter num clipe de texto com counter_from, counter_to e easing easeOut; detalhes de formato e ritmo em tipografia-cinetica.
- Texto que segue uma curva: set_text_path com arc, circle, wave ou custom; animate property pathOffset faz a frase correr pela curva. Combine com o trajeto curvo do clipe só quando o movimento tiver um motivo; uma coisa de cada vez é mais legível.
- Os dois são desenhados quadro a quadro com as mesmas letras, iguais na prévia e na exportação, e aceitam animate_text, cores animadas e as chaves com as curvas de sempre.

## Animadores de texto
- Movimento por letra, palavra ou linha além das entradas prontas: add_text_animator com animator_properties (posição, escala, giro, opacidade, desfoque, espaçamento, cor) e um seletor; o seletor range escolhe quem recebe e quanto (range_shape, range_order, range_offset animado), o wiggly treme cada unidade em loop.
- Os princípios de sempre valem por unidade: a cascata inteira cabe em cerca de 600 ms, todas as unidades usam a mesma curva e a variação vem da ordem; animate a faixa com easeOut na chave de range_offset para a revelação desacelerar no fim.
- Ordem com intenção: forward para leitura, centerOut para um título que abre, edgesIn para algo que se fecha num ponto, random só para efeitos de textura (o mesmo selector_seed repete o mesmo sorteio).
- Uma ideia por animador: um para a entrada, outro para o destaque ou o tremor; a lista aplica em ordem, posições somam e escalas multiplicam. Detalhes, prontos e exemplos em tipografia-cinetica.

## Logo e marca
- A marca aparece cedo, nos primeiros 5 segundos, e volta ao longo do vídeo; não deixe o logo só para o fim.
- Revelação de logo de 2 a 4 s, com o logo final parado por pelo menos 1 s. A revelação segue o vocabulário da peça: sóbria (opacity de 0 a 1 e scale de 0,9 para 1 com easeOut em cerca de 600 ms), desenhada (o contorno com trimEnd e o preenchimento depois), transformada (uma forma que vira o logo por morph), mascarada (wipe ou animate_mask), montada (as partes chegam em cascata com overshoot) ou na batida (cai com comprimir e esticar).
- O logo entra como mídia (add_media com o media_id do anexo) com fit contain, sem distorcer, com área de respiro.

## Acessibilidade
- No máximo 3 flashes por segundo. Nada de tremor de tela cheia nem zoom em excesso.
- Nunca passe informação só pelo movimento: o texto continua legível parado.

## Erros comuns
- Movimento linear, bounce em tudo, tudo animando ao mesmo tempo ou a mesma entrada em todos os elementos.
- O mesmo deslize de baixo com fade de uns 400 ms e só duas chaves de scale e opacity em toda peça, venha o brief que vier.
- Elementos pequenos com mais de 800 ms. Texto que some antes de ser lido.
- Direção trocando a cada cena. Exagero em opacidade.

## Conferir
Corrija os erros do campo checks de cada studio_edit_video antes da próxima cena. Confira o movimento com studio_look e focus_id (o id da transição ou do clipe animado): os quadros vêm próximos, do primeiro ao último instante da entrada, da saída, dos keyframes ou da animação do texto. Para um trecho livre, use from_ms e to_ms. Em cada quadro a resposta traz posição, escala, opacidade, desfoque e o progresso da transição ou do texto: compare esses números com a curva que você quis, porque a folha de quadros não mostra velocidade. Com mais de 4 quadros cada um fica pequeno; para ler texto em movimento, olhe poucos quadros ou aproxime com region.

Para conferir a peça inteira, não espere o fim: depois de cada cena, olhe com times_ms nos momentos de leitura e com from_ms e to_ms na transição; antes de responder, uma última olhada nos momentos principais do vídeo todo. Corrija na hora o que já viu de errado (texto cortado, sombra que suja a letra, elemento fora da margem) em vez de listar como pendência; pendência é só o que depende da pessoa.

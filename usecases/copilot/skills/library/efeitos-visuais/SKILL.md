---
name: efeitos-visuais
surface: studio
description: Como usar efeitos visuais no Estúdio (granulado, vazamento de luz, glitch, partículas, reflexo de lente, tremor de câmera, vinheta, aberração cromática, brilho, mosaico, posterizar, espelho, deslocar, desfoques direcional, de zoom e giratório, ondas, ondulação, redemoinho, lente, cantos livres, ruído fractal, degradês, espectro e onda de áudio, quadros travados e eco) com gosto: quando cada um ajuda, quanto usar, quantos de uma vez, como casar com cortes e música, como usar a versão parada em fotos e pranchetas, o clipe de ajuste (cor, desfoque, câmera 2D e máscara sobre tudo o que está abaixo) e o que deixa a peça com cara de barato; carregue antes de pôr qualquer efeito num vídeo ou numa imagem.
---

# Efeitos visuais no Estúdio

Efeito bom quase não se nota: ele dá textura, clima ou impacto a um momento que já funciona. Se o vídeo precisa do efeito para ficar interessante, o problema está no material ou no ritmo, e isso se resolve na edição (carregue edicao-de-video).

## Dois lugares, o mesmo efeito
- No clipe (add_vfx com clip_id): vale só para aquele clipe e anda com ele. Use para tratar uma cena, um logo ou uma foto.
- Clipe de efeito (add_effect_clip com vfx_kind, at_ms e duration_ms): vale para tudo o que aparece abaixo dele durante o seu tempo, como uma camada de ajuste. Use para um visual do vídeo todo (granulado, vinheta) ou para um impacto que atravessa vários clipes (um glitch na troca de cena).
- A força do clipe de efeito é o opacity em update_clip (1 é o efeito inteiro, 0,5 a metade). fade_in_ms e fade_out_ms fazem o efeito entrar e sair aos poucos; prefira isso a um efeito que liga e desliga seco.
- Ajuste com update_vfx (vfx_params, vfx_seed, vfx_hidden), anime com animate_vfx, mude a ordem com move_vfx (to_index) e tire com remove_vfx. Com mais de um efeito no clipe, mande vfx_id; studio_read mostra os efeitos e os valores atuais.
- Ordem: o que distorce a imagem (glitch, aberração cromática) vem antes do que se soma por cima (granulado, luz, partículas, vinheta). Granulado depois do glitch fica limpo; antes, o glitch rasga o grão junto.

## Clipe de ajuste: cor, desfoque, câmera e máscara
- O clipe de efeito também é um clipe de ajuste. add_effect_clip sem vfx_kind cria um vazio; nele, set_color trata tudo o que está abaixo de uma vez (color_grade, curves, hsl, lut_media_id, gradient_map), update_clip com blur desfoca o que está abaixo e set_camera move, aproxima e gira tudo o que está abaixo.
- Use o ajuste para unificar cenas filmadas em lugares diferentes (um look para o vídeo todo), para escurecer e desfocar o fundo atrás de um texto e para um zoom de impacto que atravessa vários clipes sem animar cada um.
- Câmera 2D: camera_zoom de 1,05 a 1,2 dá um empurrão lento de documentário; de 1,3 a 1,6 em 200 a 400 ms, no golpe da batida, dá um soco de impacto. camera_pan_x e camera_pan_y em frações do quadro levam o olhar até um detalhe; camera_rotation de 2 a 6 graus dá tensão, acima de 15 vira efeito de clipe musical. Anime com animate (cameraZoom, cameraPanX, cameraPanY, cameraRotation) e easeInOut ou easeOut; um zoom linear parece máquina.
- Zoom menor que 1 mostra a borda do quadro com o fundo do projeto: só use de propósito. Com motion_blur true no clipe de ajuste, o movimento da câmera borra pelo obturador do projeto, como uma câmera de verdade; ligue em zooms e giros rápidos.
- add_mask no clipe de ajuste limita tudo isso à forma (cor, desfoque, câmera e efeitos): uma elipse com feather de 0,15 a 0,3 faz um foco suave no rosto, um retângulo desfocado atrás de um texto, uma faixa mais quente no céu. Fora da máscara a imagem fica como estava; anime a máscara com animate_mask para seguir o assunto.
- A força (opacity em update_clip) e os fades dosam o ajuste inteiro: com opacity 0,5 a cor fica no meio do caminho, a câmera anda a metade e o desfoque fica mais leve. Um ajuste que entra com fade_in_ms de 300 a 600 é mais elegante que um corte seco.

## Quanto usar de cada um
- Granulado (grain): textura de película. amount de 0,1 a 0,25 é o normal; acima de 0,4 vira chiado. rate 24 imita película; rates baixos (8 a 12) dão cara de filme antigo. chroma perto de 0 parece filme; grão colorido forte parece câmera ruim. Bom no vídeo todo como clipe de efeito.
- Vinheta (vignette): leva o olhar ao centro. amount de 0,2 a 0,4 com softness alta. Vinheta escura e dura é o erro mais comum de vídeo amador. Em fundo claro ou marca com fundo branco, use pouco ou nada.
- Vazamento de luz (lightLeak): clima quente e nostálgico, bom em abertura, troca de cena e fecho. amount de 0,4 a 0,7 por 1 a 2 segundos, entrando e saindo com fade_in_ms e fade_out_ms. Ligado o vídeo todo cansa. Cores da marca em color e color2 ficam mais elegantes que o laranja padrão quando o tom do vídeo é frio.
- Reflexo de lente (lensFlare): um brilho que confirma uma fonte de luz que já existe na cena (sol, refletor, janela). Ponha x e y onde a luz está. ghosts e streak baixos (0,2 a 0,4). Reflexo sem fonte de luz na imagem parece colado.
- Partículas (particles): poeira, neve, faíscas, brilho de festa. count de 30 a 120 e size pequeno; softness alta parece profundidade. Partícula grande e nítida na frente de um rosto distrai. Combine a direção (angle) com a cena: neve cai (90), faísca sobe (-90).
- Tremor de câmera (shake): impacto, tensão, energia. Em impacto, amount de 0,3 a 0,6 por 150 a 400 ms, no golpe da batida, com frequency alta (12 a 20). Para câmera na mão contínua, amount de 0,05 a 0,15 com frequency de 2 a 5. Tremor forte e longo dá enjoo.
- Glitch: tecnologia, virada, erro de propósito. Em rajadas curtas (200 a 500 ms) num corte ou numa batida, nunca o tempo todo. frequency de 2 a 6 enquanto ligado; split de 8 a 20.
- Aberração cromática (chromaticAberration): lente barata, sonho, impacto. amount de 0,1 a 0,25 com radial 1 parece lente real; acima de 0,5 só como pico curto de impacto. Em texto pequeno atrapalha a leitura.

## Brilho, estilo e desfoques
- Brilho (glow): as partes claras irradiam luz. threshold de 0,5 a 0,7 faz só letreiros, faíscas e reflexos brilharem; abaixo de 0,3 a imagem inteira vira névoa. amount de 0,3 a 0,6 é elegante; size acompanha o tamanho do que brilha (20 a 40 px num texto grande). color tinge o brilho; branco mantém as cores. Ótimo em texto neon e produto com reflexo.
- Mosaico (mosaic): censura ou estética de pixel. Para esconder um rosto ou uma placa, use num clipe com máscara e size de 20 a 40; para estética retrô, anime size de grande para pequeno numa revelação.
- Posterizar (posterize): poucos tons, cara de cartaz ou serigrafia. levels de 3 a 6. Em pele fica estranho; em fundo e grafismo fica bonito.
- Espelho (mirror): reflete metade da imagem. Bom em padrões, arquitetura e transições estilizadas; em pessoa vira criatura, use de propósito.
- Deslocar (offset): a imagem anda e dá a volta na borda. Animar shiftX de 0 a 1 faz um carrossel infinito de padrão ou textura; em foto comum mostra a emenda.
- Desfoque direcional (directionalBlur): rastro de velocidade. length de 20 a 60 px no sentido do movimento (angle), por 100 a 300 ms num corte rápido. Desfoque de zoom (zoomBlur): impacto saindo de um ponto; length de 0,1 a 0,3 com x e y no assunto, bom com tremor na batida. Desfoque giratório (spinBlur): giro de transição; angle de 10 a 40 graus, curto.
- Desfoque forte e longo cansa: use como pico, entrando e saindo com fades ou chaves de amount.

## Distorções
- Ondas (waveWarp): calor, água, sonho. height de 4 a 15 px e length de 100 a 300 px parecem naturais; speed de 0,5 a 2 deixa as ondas andando. Para um ritmo exato, anime phase de 0 a 360.
- Ondulação (ripple): gota na água, toque na tela, impacto de som. x e y no ponto do toque, size de 0,5 a 1, height de 6 a 12 px; uma ondulação curta numa batida funciona melhor que ondulação contínua.
- Redemoinho (twirl): transição de hipnose ou de portal. Anime angle de 0 a 360 ou mais e volte a 0; parado, quase nunca fica bom.
- Lente (bulge): height positivo incha (lupa sobre um detalhe do produto), negativo aperta. size de 0,2 a 0,4 sobre o detalhe; height acima de 0,7 deforma demais.
- Cantos livres (cornerPin): encaixa uma foto ou um vídeo numa tela de celular, num outdoor ou num quadro. Ponha cada canto (topLeftX, topLeftY e os outros) exatamente sobre o canto da tela na imagem de baixo; se a câmera se move, anime os cantos. Fora do quadrilátero a imagem fica transparente.

## Fundos gerados
- Ruído fractal (fractalNoise), degradê (gradientRamp) e degradê de quatro cores (fourColorGradient) pintam a imagem. Num clipe de efeito na faixa mais baixa viram um fundo animado atrás de texto e produto; com amount de 0,2 a 0,4 sobre um vídeo viram uma névoa ou um tom de cor.
- Ruído fractal: size de 100 a 300 px com complexity 3 a 5 parece fumaça ou nuvem; speed de 0,2 a 0,6 deixa a fumaça viva sem distrair. Use color e color2 nas cores da marca (escuro e claro) para um fundo próprio.
- Degradê: duas cores da marca, de cima para baixo (y 0, toY 1) ou circular (radial 1) a partir do centro do produto. Animar as posições bem devagar dá vida a um fundo parado.
- Quatro cores: tons próximos (análogos) ficam sofisticados; quatro cores saturadas e opostas parecem tela de teste. Animar os pontos devagar faz um fundo líquido.

## Som que vira imagem
- Espectro de áudio (audioSpectrum) mostra barras das frequências e onda de áudio (audioWaveform) mostra o volume ao longo do tempo; os dois precisam de vfx_track_id, a faixa de som que eles mostram (narração para um podcast em vídeo, música para um clipe).
- Posição e tamanho: x e y no lugar das barras (embaixo do quadro, sobre a foto do locutor), width de 0,5 a 0,8, size de 0,15 a 0,3. Barras gigantes competem com o rosto e o texto.
- count de 24 a 48 barras é legível no celular; mais que isso vira textura. gap de 0,2 a 0,4 e roundness 1 deixam moderno. gain sobe ou desce a altura sem mexer no som: se as barras quase não mexem, gain positivo.
- Cores da marca em color (base) e color2 (ponta). Se o som não puder ser lido, a exportação recusa em vez de sair com barras paradas.

## Tempo: quadros travados e eco
- Quadros travados (posterizeTime): menos quadros por segundo, cara de animação quadro a quadro ou de stop motion. rate de 6 a 12. Num clipe vale só para ele; num clipe de efeito trava tudo o que está abaixo. Bom em colagem, recorte e estilo artesanal; em vídeo de produto parece travamento.
- Eco (echo): cópias do clipe em instantes anteriores atrás dele, um rastro de movimento estilizado. count de 3 a 8, delay de 40 a 120 ms e decay de 0,5 a 0,7. Só aparece quando o clipe se move; num clipe parado as cópias ficam embaixo dele. Vale só em clipes, não em clipe de efeito, e cada cópia de vídeo custa uma decodificação a mais, então prefira eco em texto, logo e forma.

## Quantos de uma vez
- Um efeito que dá o visual do vídeo inteiro (granulado ou vinheta, no máximo os dois juntos e bem sutis) e, por cima, efeitos de momento.
- Efeito de momento só quando o momento pede: entrada, troca de assunto, batida forte, revelação, fecho. Se todo corte tem efeito, nenhum tem peso.
- Receita de visual de filme: granulado 0,15 + vinheta 0,3 + vazamento de luz só na abertura e no fecho.
- Receita de fita antiga: glitch em rajadas + aberração cromática 0,3 + granulado com rate 12. Use só quando o tema pede nostalgia digital.
- Três ou mais efeitos fortes ao mesmo tempo é poluição; tire até sobrar o que conta a história.

## Tempo com cortes e música
- Efeito de impacto cai exatamente no corte ou na batida, nunca um pouco antes ou depois. Ponha at_ms do clipe de efeito no corte e duration_ms curto.
- Fade de entrada e saída dosa o efeito: um vazamento de luz com fade_in_ms 300 e fade_out_ms 600 nasce e some como luz de verdade.
- Para crescer junto com a música, anime amount com animate_vfx (por exemplo, de 0 a 0,6 na subida antes do refrão). Velocidade, ritmo, frequência e quantidade não recebem chaves; mude esses valores fixos com update_vfx.
- O mesmo vfx_seed dá sempre o mesmo resultado. Se a variação sorteada ficou feia (um glitch no rosto, uma partícula no olho), troque o vfx_seed em vez de mexer em tudo.

## Em imagens paradas
- No editor de imagem existem os efeitos parados: granulado, vazamento de luz, vinheta, aberração cromática, glitch, reflexo de lente, brilho, mosaico, posterizar, espelho, deslocar, os três desfoques, ondas, ondulação, redemoinho, lente, cantos livres, ruído fractal e os dois degradês. Partículas, tremor, quadros travados, eco e os efeitos de som só existem no vídeo, porque precisam de tempo ou de som.
- Cantos livres numa foto encaixa a foto numa tela de celular ou num quadro de outra foto; brilho num título claro faz neon; ruído fractal ou degradê na prancheta dá um fundo próprio sem precisar de imagem.
- add_vfx com layer_id de uma foto vale só para ela. Sem layer_id (artboard_id ou a prancheta ativa) é um ajuste sobre tudo o que está na prancheta, o fundo incluído, como o clipe de efeito do vídeo. update_vfx, move_vfx e remove_vfx usam o mesmo alvo.
- Não há tempo nem chaves: o efeito é um instante fixo escolhido pelo vfx_seed. Valores de ritmo (rate, speed, frequency, flicker) não valem na imagem; o glitch sempre aparece e a força dele vem de amount, split e size.
- O mesmo vfx_seed dá a mesma imagem na tela e em toda exportação (PNG, JPG, PDF e SVG). Se o grão, a luz ou a falha caiu num lugar feio, troque o vfx_seed.
- Na prancheta, o efeito passa por cima do texto também. Granulado e vinheta sutis não atrapalham a leitura; glitch, aberração e reflexo sobre um título ou preço atrapalham. Para tratar só a foto, ponha o efeito nela e deixe o texto limpo acima.
- Receita de foto com acabamento de filme: na prancheta, granulado 0,15 e vinheta 0,25 com softness alta. Receita de capa nostálgica: vazamento de luz 0,5 na foto, com color e color2 nas cores da marca, e granulado 0,2 na prancheta.
- Num PDF ou SVG, a prancheta com efeito sai como uma imagem só, sem vetores editáveis; a foto com efeito sai como imagem e o resto continua vetor.

## O que deixa com cara de barato
- Efeito para esconder material fraco, efeito em todo corte, vinheta preta forte, glitch o vídeo todo, reflexo de lente sem fonte de luz, partículas cobrindo o produto ou o rosto, tremor longo.
- Efeito em cima de texto pequeno, preço ou dado: o que precisa ser lido fica nítido. Ponha o texto numa faixa acima do clipe de efeito, ou use o efeito no clipe da imagem em vez de um clipe de efeito.
- Estilo que briga com a marca: marca sóbria (saúde, advocacia, finanças) pede no máximo granulado e vinheta sutis.

## Conferir
- Veja com studio_look no começo, no meio e no pico de cada efeito. Pergunte: o efeito ajuda a contar a história ou só aparece?
- Rosto, produto, logo e texto continuam limpos e legíveis.
- Na dúvida entre dois valores, fique com o menor.

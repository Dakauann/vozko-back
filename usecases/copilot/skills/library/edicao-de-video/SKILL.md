---
name: edicao-de-video
surface: studio
description: Como editar um vídeo profissional no Estúdio de ponta a ponta (gancho, estrutura, ritmo, cortes, som, duração por rede, erros comuns e conferência); carregue antes de montar ou ajustar qualquer vídeo.
---

# Edição de vídeo no Estúdio

## Antes de cortar
- Vídeo novo do zero: valide o brief antes com a habilidade briefing-criativo. Num ajuste com objetivo, rede, duração ou tom pouco claros, pergunte com studio_ask (até 4 perguntas num só cartão).
- Leia com studio_read e veja o material com studio_look (com media_id para um vídeo da biblioteca). Saiba o que acontece em cada trecho antes de cortar.
- Use só o material do cliente ou o que ele aprovou gerar. Nada inventado sobre produto, preço, equipe ou resultados.
- Mídia da biblioteca citada no brief (a foto do produto, o logo, um vídeo) é o herói da peça: entra com add_media, recortada com studio_start_job kind cutout quando o fundo atrapalha (Tirar o fundo, abaixo), e nunca é trocada por um ícone ou desenho no lugar dela. Uma foto parada vira placa animada com chaves de escala e posição, máscara ou paralaxe.
- Formato e zonas seguras: carregue formatos-e-zonas-seguras.

## Prioridades de um corte (Walter Murch)
Quando duas regras brigam, sacrifique de baixo para cima: emoção, história, ritmo, para onde o olho vai, plano da tela, espaço. Nunca corte no tempo da música contra a emoção da cena.
- Corte só com motivo; na dúvida, corte mais tarde. Corte no movimento sempre que puder (Dmytryk).

## Gancho
- Os primeiros 3 segundos concentram a maior parte do valor de um vídeo nas redes (Meta e Nielsen). Abra com o resultado, a dor, o produto em uso ou um rosto no meio de uma expressão.
- Texto na abertura com até 5 palavras dizendo o problema de quem assiste. Nada de logo de abertura nem cumprimentos.
- Marca e produto aparecem nos primeiros 5 segundos, e com mais de dois planos nesse trecho. A marca volta ao longo do vídeo.

## Estruturas que funcionam
- Gancho, corpo e fechamento. Num vídeo de 30 s: gancho de 0 a 3 s, produto e prova até cerca de 20 s, chamada nos últimos 3 a 5 s.
- Problema, agitação e solução: a dor em 0 a 3 s, a virada de 3 a 8 s, depois a demonstração.
- Antes e depois: mesmo enquadramento e mesma luz nos dois, revelados com wipe ou corte seco.
- Lista: prometa o número de itens no começo, um item por batida de 2 a 4 s, contador fixo na tela, o melhor por último.
- Depoimento e tutorial: carregue anuncio-ugc-e-depoimento ou explicativo-e-dados.

## Ritmo e cortes
- Nas redes, planos de 1 a 3 s e alguma mudança visual a cada 2 a 4 s. Um plano longo só quando algo acontece nele.
- Corte o tempo morto: começo antes da fala, pausas, respirações e repetições (trim_clip, split_clip e delete_clips com ripple).
- Num rosto falando, esconda o corte com um aproximar de 110% a 120% (update_clip com w e h maiores ou animate em scale) ou com uma imagem por cima.
- A imagem de apoio aparece no momento em que a fala cita aquilo, numa faixa acima, de 1 a 3 s.
- Corte nas batidas da música (mark_beats no clipe da trilha marca cada batida) e mude o ritmo nas viradas da música; o ponto alto da emoção cai no ponto alto da música.
- Cortes secos são o padrão. Transições só com motivo (veja motion-design).
- Antes de animar textos e elementos, decida o conceito de movimento e o vocabulário da peça em motion-design (cinético, editorial, brincalhão, cinematográfico, glitch ou tipográfico) e mantenha no vídeo inteiro. As entradas prontas (entrance com slideUp ou fade) são ponto de partida, não a linguagem do vídeo.
- Vinheta, tela dividida, revelação por forma, foco num detalhe e texto com vídeo dentro usam máscaras (add_mask, animate_mask, matte); receitas em motion-design. update_mask troca a forma (mask_shape), o feather e o lugar de uma máscara pronta; com mais de uma no clipe, a ordem conta e move_mask muda essa ordem.

## Ajuste fino de cortes
- Escolha a edição pelo que deve ficar parado. trim_clip muda um clipe só e pode abrir buraco; delete_clips com ripple fecha o espaço e muda a duração do vídeo; slip_clip troca o trecho da mídia sem mexer no tempo.
- roll_edit quando o ponto do corte está errado mas a duração total está certa: corte no movimento, na batida ou depois da última palavra. Um clipe encurta, o outro cresce e nada mais anda, então legendas, música e textos continuam no lugar. edge end é o corte depois do clipe, start o corte antes dele.
- slide_clip quando o plano está certo mas no lugar errado: um plano de apoio que deve cair na palavra certa, um produto que deve entrar na batida. O conteúdo do clipe não muda; o anterior e o seguinte se ajustam.
- Os dois respeitam o fim da mídia e o mínimo de 100 ms por clipe; a resposta diz onde parou e por quê. Transições no corte vão junto e encurtam se um clipe ficar curto. O som ligado ao vídeo acompanha; linked false só quando quiser de propósito que a fala entre antes ou depois da imagem (corte J ou L).

## Arranjar clipes no tempo
- sequence_clips põe os clipes de clip_ids um depois do outro, na ordem da lista, a partir do primeiro, que fica onde está. O som ligado vai junto e as animações andam com o clipe. Use para montar uma sequência de fotos, uma lista de produtos ou cenas que estavam soltas.
- gap_ms deixa um respiro entre os clipes (200 a 500 ms numa lista calma); negativo sobrepõe, o que só vale para clipes em faixas diferentes, como textos que entram antes do anterior sair.
- Com transition_kind, cada corte entre clipes encostados na mesma faixa ganha a transição (duration_ms é a duração dela); clipes em faixas diferentes não têm corte e ficam sem.
- stagger_clips faz a cascata: cada clipe começa step_ms depois do anterior. 80 a 150 ms para itens de uma lista ou ícones, 200 a 400 ms para blocos de texto. A ordem de clip_ids é a ordem de entrada.
- Se a resposta disser que um clipe cairia em cima de outro, os dois estão na mesma faixa: mova um com move_clip para outra faixa antes.
- Ao mudar a velocidade de um clipe animado com set_speed, protect_edges_ms guarda o tempo das chaves de entrada e de saída (300 a 600 ms cobrem a maioria das entradas) e só o meio estica, como um título que fica mais tempo na tela sem entrar mais devagar.

## Agrupar clipes
- group_clips(clip_ids, name?) transforma clipes num grupo: um clipe só na linha do tempo, com as faixas e os clipes de dentro no mesmo tempo entre si. O som ligado a um vídeo e os clipes vinculados por pai entram junto sozinhos (a resposta diz quais). Dê um nome que diga o papel, como Abertura, Oferta ou Cena 3.
- Agrupe um trecho pronto para movê-lo, cortá-lo e repeti-lo como peça: uma abertura de cinco camadas vira um clipe que você arrasta, encurta (o corte do grupo esconde o resto do conteúdo, como num vídeo), acelera com set_speed (o som de dentro acompanha mantendo o tom) e liga a outro clipe com uma transição de corte. duplicate_clips de um grupo cria uma cópia independente.
- O som do grupo é a mistura das faixas de áudio de dentro, com o volume e os fades do grupo por cima; a redução automática (ducking) funciona dentro de uma linha do tempo, então agrupar metade de uma regra é recusado: inclua as faixas da regra inteiras ou asse a redução com bake_ducking antes.
- Agrupar é recusado quando mudaria a imagem: um clipe de fora entre as faixas escolhidas no mesmo instante, um clipe com mistura, recorte ou efeito sobre clipes que ficariam fora, uma transição entre um clipe escolhido e um de fora, faixas travadas. A recusa diz qual clipe resolver.
- Para mexer por dentro, mande as operações com group_id (os ids de dentro também levam a operação para dentro sozinhos); os tempos são os de dentro do grupo. Não misture numa operação ids de dentro e de fora.
- ungroup_clips(clip_id) devolve tudo para a linha do tempo nos mesmos instantes e na mesma ordem de faixas, cortando o que estava escondido pelo corte do grupo. Antes, tire do grupo o que não dá para repartir sem mudar o resultado (opacidade, fades, máscaras, efeitos, mistura, cor, desfoque, transições e velocidade diferente de 1); posição, rotação e escala passam para um nulo que conduz o conteúdo.

## Marcadores, batidas e organização

- mark_beats(clip_id da trilha) lê a pulsação e põe um marcador de batida preso ao clipe em cada batida; beat_every 2 ou 4 marca só as batidas fortes, para cortes menos frenéticos. A resposta traz o BPM: até 100 BPM pede cortes longos, acima de 120 aceita cortes a cada batida.
- Os marcadores dos clipes aparecem em studio_read com at_ms na linha do tempo; use esses instantes em move_clip, trim_clip e split_clip. Cortar alguns quadros antes da batida (2 a 3) parece mais no tempo do que cortar em cima.
- add_marker com duration_ms marca um trecho (o refrão, a oferta) e com note deixa um lembrete para a pessoa; com clip_id o marcador anda com o clipe. Cores: use poucas e com sentido (por exemplo vermelho para o que falta resolver). update_marker acerta o nome, a nota, a cor ou o trecho de um marcador que já existe, em vez de criar outro.
- Nomeie os clipes com update_clip name quando o projeto tem muitos parecidos ("Depoimento 2", "Logo final"); dê label_color por função (trilha, falas, textos) e deixe faixas de apoio discretas com update_track shy quando a linha do tempo estiver cheia. Organize por função: add_track cria uma faixa vazia onde ela faz sentido (legendas no topo, trilha embaixo) e move_track reordena, lembrando que a faixa de cima cobre a de baixo. link_clips prende clipes que precisam andar juntos (um logo e o texto dele, o vídeo e um áudio gravado à parte).
- guide em update_clip serve para referências que não podem sair no vídeo: área segura do Reels, um rascunho de layout, uma locução guia. Um guia aparece na prévia e nunca entra na exportação; studio_look mostra o que a prévia mostra, guias incluídos.

## Velocidade
- Câmera lenta destaca um momento (o produto em uso, um detalhe, uma reação): set_speed de 0,5 a 0,7. Abaixo de 0,5 um vídeo comum fica travado, porque não tem quadros a mais.
- Acelerado comprime processos longos (preparo, montagem, deslocamento): de 2 a 8, com música no lugar do som original.
- Rampa dá impacto: speed_ramp no momento certo, caindo para 0,3 em 300 a 600 ms; outra rampa mais adiante volta para 1. Faça a queda cair numa batida da música. O padrão é ramp_mode smooth, uma curva contínua dentro do mesmo clipe; ramp_mode steps só quando você quer degraus visíveis.
- Curva de velocidade: set_speed_curve com speed_curve_preset hero ou bullet faz a câmera lenta de impacto no meio do clipe, montage acelera e freia duas vezes, jumpCut dá um salto rápido, flashIn e flashOut entram ou saem correndo. Para desenhar a sua, mande speed_points de at 0 a at 1 (por exemplo 1 no começo, 0,3 no instante do impacto, 2 no fim). Por padrão a curva toca a mesma parte da mídia e a duração do clipe muda; keep_duration mantém a duração. O som ligado segue a mesma curva.
- Câmera lenta abaixo de 0,5 em vídeo comum trava; set_speed com frame_mix true mistura os quadros vizinhos e deixa o movimento mais liso. Confira em studio_read: o clipe mostra a curva com os pontos (at e speed), se ela é suave e quanto da mídia ele usa.
- freeze_frame segura um quadro por 1 a 2 s para um texto ou um destaque; combina com um aproximar ou um flash na batida.
- reverse rebobina, como um efeito de desfazer ou de repetição; use pouco.
- Fala fica natural até cerca de 1,25 com preserve_pitch. Fora de 0,5 a 2 o som esticado soa artificial: desligue preserve_pitch quando o efeito for de propósito, ou abaixe o volume desse clipe.
- set_speed muda a duração para tocar a mesma parte da mídia e puxa o resto da faixa; keep_duration mantém o tempo do clipe e toca mais ou menos mídia. studio_read mostra speed, reverse e freeze em cada clipe.

## Tirar o fundo
- studio_start_job kind cutout com clip_id tira o fundo de clipes de imagem (foto, produto, imagem sobreposta ao vídeo), sem custo, e o recorte entra no lugar da imagem. Vídeo gravado em fundo verde ou azul: set_chroma_key, abaixo.
- Tirar o fundo de um vídeo comum, com a pessoa ou o produto se movendo, o Estúdio não faz: nunca prometa nem diga que vai tentar. Ofereça o que funciona: gravar de novo em fundo verde ou azul liso, pôr por cima do vídeo uma foto recortada do produto ou da pessoa, ou isolar o assunto com uma forma (add_shape) grudada nele por track_point e attach_to_track, com o vídeo na faixa logo acima em update_clip matte true (ou add_mask com animate_mask à mão). A forma isola uma área de borda suave, não a silhueta.

## Fundo verde ou azul
- set_chroma_key com chroma_preset green ou blue liga o recorte com valores que já servem para a maioria dos vídeos de celular e webcam; ponha o fundo novo (imagem, vídeo ou cor) numa faixa abaixo. Quando a luz muda no meio do clipe (a pessoa anda, uma nuvem passa), animate_chroma anima similarity ou spill só no trecho que pede.
- Tela de cor incomum ou mal iluminada: chroma_preset auto lê a borda do quadro (em at_ms, ou no quadro do playhead quando ele está no clipe) e usa a cor dominante; se a resposta disser que não achou fundo verde ou azul, olhe o quadro e mande chroma_color.
- Ajuste olhando de perto as bordas (studio_look com region no contorno da pessoa): sobrou fundo, suba similarity de 0,05 em 0,05; apareceram furos na pessoa, desça; borda dura, suba smoothness; contorno esverdeado ou azulado, suba spill.
- Fundo com sombra ou mal iluminado pede similarity mais alta; antes disso, uma máscara (add_mask rectangle) em volta da pessoa corta os cantos que o recorte não pega.
- Se a pessoa veste a cor do fundo, nenhum valor resolve: use máscara subtract na roupa ou outro corte.
- O fundo novo precisa combinar com a luz da pessoa: corrija a cor do vídeo recortado (set_color) para casar temperatura e brilho com o fundo.

## Fundo preto ou branco e misturas
- Vídeo de banco com fundo preto (vazamento de luz, fogo, faíscas, fumaça, poeira, lens flare): a primeira escolha é update_clip blend_mode add, que soma a luz ao que está embaixo e some com o preto sem cortar nada; screen clareia mais suave. Recorte por brilho só quando a mistura clareia demais o fundo novo.
- set_luma_key luma_mode dark remove o escuro, light remove o claro (logo ou desenho em fundo branco). luma_threshold diz quanto da ponta some, de 0,05 a 0,2 na maioria dos casos; luma_softness de 0,1 a 0,3 deixa a borda macia, perto de 0 fica serrilhado. Sobrou fundo, suba luma_threshold de 0,05 em 0,05; partes do assunto sumiram, desça.
- O recorte por brilho vem depois do recorte de fundo e antes da cor; num mesmo clipe os dois podem ficar juntos. animate_luma faz o assunto aparecer ou sumir aos poucos (luma_threshold de 1 a 0,1 revela do claro para o escuro).
- multiply escurece (sombra, textura de papel ou de grão sobre o vídeo); overlay e soft-light dão contraste e cor de textura sem cobrir; difference é efeito, use pouco. blend_mode vale para clipes de vídeo e imagem e para textos e formas.

## Rastrear e estabilizar
- Para um texto, seta ou adesivo grudar em algo que se move (o produto na mão, um rosto, um carro), rastreie um ponto com track_point e depois attach_to_track. Escolha um detalhe com contraste e que não saia do quadro: um olho, uma quina, uma letra do rótulo. Área lisa (parede, céu, pele sem marca) não tem o que seguir e é recusada.
- track_size de 0,04 a 0,08 serve para detalhes; objetos grandes e borrados pedem 0,1 a 0,2. Olhe o quadro com studio_look antes para achar as coordenadas do ponto em at_ms.
- Leia a confiança nas notas: média acima de 0,9 é um rastreio firme; mínima perto de 0,6 diz onde o ponto quase escapou, então confira esse trecho com studio_look. Quando o ponto se perde (alguém passa na frente, o objeto sai do quadro), rastreie até ali com to_ms ou keep_partial true e continue de outro ponto.
- attach_to_track com attach_mode link deixa o clipe seguir ao vivo e acompanha cortes de trim e mudanças de velocidade do vídeo; keys grava a posição em cada quadro e serve quando você quer editar o caminho depois. keep_position true mantém o texto onde você o pôs, só que agora andando com o ponto; false põe o centro do texto em cima do ponto.
- Um nulo seguindo o ponto (add_null e attach_to_track) conduz vários clipes juntos: vincule o texto e a seta ao nulo com parent_clip.
- Câmera na mão tremida: stabilize_clip com o ponto de algo parado na cena. Sem smooth_ms o ponto fica travado, bom para planos que deviam ser fixos; com smooth_ms de 500 a 1500 o tremor some e a panorâmica continua. Dois pontos afastados (second_tracker_id) com stabilize_rotation corrigem o giro do celular.
- O zoom esconde as bordas; acima de 1,3 a imagem perde nitidez visível. Se a resposta recusar por movimento grande demais, suavize em vez de travar ou estabilize um trecho menor. Confira o resultado com studio_look em dois ou três momentos.

## Efeitos visuais
- Granulado, vinheta, vazamento de luz, glitch, tremor e os outros efeitos (quando usar, quanto e como casar com cortes e música): carregue efeitos-visuais.

## Som
- Mixe para ouvir com som e confira se a história ainda funciona sem som: carregue legendas-e-texto e legende toda fala.
- Voz sempre clara, volume 1. Com chiado, vento ou eco, studio_start_job kind denoise no clipe de áudio da fala (sem custo) e confira com studio_listen; se a voz ficar metálica, o original com legenda vale mais que a limpeza.
- Música sob a voz: de 12 a 20 dB mais baixa, o que dá volume entre 0,1 e 0,25. Sem voz, até 0,8. Fade de entrada e de saída de 200 a 500 ms nas trocas, e de 1 a 2 s no fim.
- Efeitos sonoros reforçam o que se vê; nada de efeito solto. Whoosh, impacto, riser, pop e ambiente são gerados com studio_generate_sfx; quando, onde e em que volume estão em video-com-som.
- Para gerar música ou locução use studio_generate_music e studio_generate_voiceover. Enquanto geram, siga editando.

## Duração por rede
- TikTok: de 21 a 34 s costuma render bem; anúncios curtos de 9 a 15 s também.
- Reels e Shorts: até 3 minutos, mas o anúncio ideal fica entre 15 e 30 s.
- Status do WhatsApp: até 60 s por vídeo.
- Explicativo: o engajamento cai depois de 2 minutos; partes com menos de 6 minutos.

## Fluxo de trabalho
1. studio_read e studio_look no material.
2. Diga em até 3 tópicos o plano.
3. Edite em lotes de 8 a 12 operações com studio_edit_video, depois de pedir de uma vez com studio_op_guide as famílias de operações que a edição vai usar (por exemplo criar, animacao e transicoes); peça de novo em cada nova resposta. Setas, selos, ícones vetoriais em seis pesos, emojis, faixas de nome, botões de ação e o contador já existem prontos: procure com studio_library_search e insira com add_element (at_ms, duration_ms, x e y, texts para o texto de cada slot); itens de várias peças entram como clipes ligados. Animações prontas (stickers: círculo à mão, seta que se desenha, sublinhado, explosão, palavras como OBRIGADO ou PROMOÇÃO) entram animadas no cursor de tempo, e duration_ms estica a animação inteira. Para foto num formato ou tela dividida, add_element com uma moldura ou grade e clip_ids dos clipes de vídeo ou imagem: a moldura recorta cada clipe e a grade põe cada um no seu espaço, na ordem de clip_ids.
4. Comece cedo os trabalhos de fila (legendas, música, locução) e continue editando; acompanhe com studio_jobs. Se o pedido tem som, peça a música logo depois do plano; quando o modo pede aprovação para gerações pagas, o cartão pausa a resposta até a pessoa aprovar, então diga no texto que a música espera a aprovação dela.
5. Corrija os erros do campo checks (buraco preto entre clipes e fundo aparecendo nas bordas já vêm ali) e confira com studio_look: texto ilegível, corte no meio de palavra. alerts, em todo studio_look, traz o que a folha de quadros esconde entre um quadro e outro: elemento que salta, que sai da tela e volta, textos que se cruzam em movimento, tela piscando mais de 3 vezes por segundo (corrija sempre), movimento longo com easing linear e sombra de um clipe contra a luz dos outros. Com focus_id ou from_ms e to_ms, motion_report lista cada movimento da janela com valores, tempos e easing. view depth e view guides valem no vídeo: desenham as marcas em cada quadro da folha e trazem depth ou layout por quadro (sombras, camadas cobertas, alinhamento, margens, níveis de texto). Transições e animações com focus_id; legendas com 1 ou 2 quadros ou region na faixa da legenda. Depois de mexer em música, voz ou volumes, meça com studio_listen (voz pelo menos 10 dB acima da música, cerca de -14 LUFS, pico até -1 dB) em vez de pedir ao usuário para escutar. Conferir o vídeo tocando é isto: studio_look com times_ms em vários momentos (cada cena parada para leitura), com from_ms e to_ms em cada transição e com focus_id no clipe animado, lendo o motion_report; nunca diga que não conseguiu ver a reprodução. Confira por cena enquanto edita e guarde uma olhada final para o vídeo inteiro.
6. Antes de resumir, corrija os defeitos pequenos que você já encontrou (texto cortado, sombra que suja a letra, marca entrando tarde); na resposta, pendência é só o que depende da pessoa (aprovação, arquivo, escolha). Resuma o que fez e ofereça o próximo passo com studio_ask. Se o usuário pedir o vídeo pronto, studio_export começa a gerar o MP4; studio_read mostra o andamento e o media_id em export. MP4 é o padrão: tem som e é o que WhatsApp, redes sociais e anúncios aceitam. Peça format gif para uma animação curta e muda que toca sozinha em site, e-mail ou blog (até uns 10 s; largura 480 e 10 a 15 fps deixam o arquivo leve; dither none para arte chapada, diffusion para foto e degradê). Peça format webp para a mesma animação com cores completas e arquivo bem menor que o GIF, para sites. GIF e WebP saem sem som. Para virar anúncio, siga criativos-que-performam: espere export done com outdated false e use o media_id como VIDEO em create_ad.
7. Trecho e quadro: se o usuário marcou entrada e saída, studio_read mostra esse trecho marcado; para exportar só esse trecho (ou qualquer outro) passe from_ms e to_ms em studio_export com mp4, gif ou webp. Um loop de 6 s para GIF sai melhor de um trecho bem escolhido do que do vídeo inteiro. Para capa de Reels, miniatura ou imagem de anúncio, peça format png (sem perda) ou jpeg (mais leve, quality de 0.5 a 1) com at_ms no quadro mais forte: rosto nítido, produto inteiro e texto legível, nunca no meio de uma transição. Confira esse quadro com studio_look antes de exportar. A imagem salva vai para create_ad como IMAGE.

## Quando o editor não responde
- Uma ferramenta do Estúdio pode voltar dizendo que o editor não respondeu a tempo: a aba do projeto ficou em segundo plano ou o navegador a adormeceu. O resultado diz exatamente o que aconteceu. Um lote que expirou não foi aplicado e não entra depois, nem quando a aba voltar; um lote que ficou sem confirmação pode ter entrado.
- Não repita o lote e não peça ao usuário para deixar a aba aberta nem para avisar quando estiver pronto. Quando o editor voltar a responder, o app avisa você na mesma resposta, logo depois da ferramenta em andamento, ou retoma a conversa sozinho com esse aviso.
- Enquanto isso, siga com o que não depende do editor (trabalhos de fila já pedidos, o plano das próximas cenas) ou encerre dizendo em poucas linhas o que já está pronto e o que falta. Quando ele voltar, confira com studio_read antes de repetir qualquer coisa.

## Erros comuns
- Logo ou cumprimento antes do gancho. Sem legenda, ou legenda sob a interface da rede.
- Música cobrindo a voz; volumes diferentes entre clipes.
- Transição em todo corte. Corte de rosto sem reenquadrar. Palavras cortadas e silêncios mortos.
- Texto que some antes de ser lido. Chamada vaga ou mostrada uma vez só.
- O mesmo ritmo do começo ao fim, sem crescer.
- A mesma entrada em todo texto e em todo vídeo, qualquer que seja o tom.

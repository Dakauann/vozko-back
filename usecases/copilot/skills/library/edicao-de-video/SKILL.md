---
name: edicao-de-video
description: Como editar um vídeo profissional no Estúdio de ponta a ponta (gancho, estrutura, ritmo, cortes, som, duração por rede, erros comuns e conferência); carregue antes de montar ou ajustar qualquer vídeo.
---

# Edição de vídeo no Estúdio

## Antes de cortar
- Se o objetivo, a rede, a duração ou o tom não estiverem claros, pergunte com studio_ask (uma pergunta, 2 a 4 opções).
- Leia com studio_read e veja o material com studio_look (com media_id para um vídeo da biblioteca). Saiba o que acontece em cada trecho antes de cortar.
- Use só o material do cliente ou o que ele aprovou gerar. Nada inventado sobre produto, preço, equipe ou resultados.
- Formato e zonas seguras: carregue formatos-e-zonas-seguras.

## Prioridades de um corte (Walter Murch)
Quando duas regras brigam, sacrifique de baixo para cima: emoção, história, ritmo, para onde o olho vai, plano da tela, espaço. Nunca corte no tempo da música contra a emoção da cena.
- Corte só com motivo; na dúvida, corte mais tarde. Corte no movimento sempre que puder (Dmytryk).

## Gancho
- Os primeiros 3 segundos concentram a maior parte do valor de um vídeo nas redes (Meta e Nielsen). Abra com o resultado, a dor, o produto em uso ou um rosto no meio de uma expressão.
- Texto na abertura com até 5 palavras dizendo o problema de quem assiste. Nada de logo de abertura nem cumprimentos.
- Marca e produto aparecem nos primeiros 5 segundos, e com mais de dois planos nesse trecho (Google ABCD). A marca volta ao longo do vídeo.

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
- Corte nas batidas da música (marque com add_marker) e mude o ritmo nas viradas da música; o ponto alto da emoção cai no ponto alto da música.
- Cortes secos são o padrão. Transições só com motivo (veja motion-design).

## Som
- Mixe para ouvir com som e confira se a história ainda funciona sem som: carregue legendas-e-texto e legende toda fala.
- Voz sempre clara, volume 1; com ruído, studio_start_job com denoise.
- Música sob a voz: de 12 a 20 dB mais baixa, o que dá volume entre 0,1 e 0,25. Sem voz, até 0,8. Fade de entrada e de saída de 200 a 500 ms nas trocas, e de 1 a 2 s no fim.
- Efeitos sonoros reforçam o que se vê; nada de efeito solto.
- Para gerar música ou locução use studio_generate_music e studio_generate_voiceover. Enquanto geram, siga editando.

## Duração por rede
- TikTok: de 21 a 34 s costuma render bem; anúncios curtos de 9 a 15 s também.
- Reels e Shorts: até 3 minutos, mas o anúncio ideal fica entre 15 e 30 s.
- Status do WhatsApp: até 60 s por vídeo.
- Explicativo: o engajamento cai depois de 2 minutos; partes com menos de 6 minutos.

## Fluxo de trabalho
1. studio_read e studio_look no material.
2. Diga em até 3 tópicos o plano.
3. Edite em lotes de 8 a 12 operações com studio_edit_video.
4. Comece cedo os trabalhos de fila (legendas, música, locução) e continue editando; acompanhe com studio_jobs.
5. Corrija os erros do campo checks e confira com studio_look: texto ilegível, buraco preto, corte no meio de palavra.
6. Resuma o que fez e ofereça o próximo passo com studio_ask.

## Erros comuns
- Logo ou cumprimento antes do gancho. Sem legenda, ou legenda sob a interface da rede.
- Música cobrindo a voz; volumes diferentes entre clipes.
- Transição em todo corte. Corte de rosto sem reenquadrar. Palavras cortadas e silêncios mortos.
- Texto que some antes de ser lido. Chamada vaga ou mostrada uma vez só.
- O mesmo ritmo do começo ao fim, sem crescer.

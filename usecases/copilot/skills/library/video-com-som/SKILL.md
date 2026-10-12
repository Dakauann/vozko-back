---
name: video-com-som
description: Como montar um vídeo de anúncio com música, locução e efeitos sonoros (generate_music, generate_voiceover, render_video, studio_generate_sfx) que funcione mudo e com som; carregue antes de criar qualquer vídeo ou som de anúncio.
---

# Vídeo de anúncio com som

## Antes de começar
- Peça nova do zero: valide o brief antes com a habilidade briefing-criativo.
- Som só existe em vídeo. Um anúncio de imagem nunca toca música.
- A maioria das pessoas vê o feed sem som: o vídeo precisa contar a história só com as imagens. O som reforça, não carrega.
- Use imagens e vídeos reais do cliente ou gerados com as referências dele (veja criativo-com-a-marca). Nada inventado sobre produto, equipe ou resultados.

## Estrutura que funciona
- Os 3 primeiros segundos decidem: comece pela dor, pelo resultado ou pelo produto em uso, nunca por logo ou abertura.
- Curto: 6 a 15 segundos para Stories e Reels, até 30 para o feed. Cada cena de 2 a 4 segundos; cenas longas só quando algo acontece nelas.
- Formato do lugar: story (9:16) para Stories e Reels, portrait (4:5) para o feed. Monte uma versão por formato.
- Termine com o pedido claro (o mesmo do botão do anúncio) e a marca visível na última cena.

## Locução
- Roteiro curto e falado, como se fosse para um amigo: cerca de 2 a 3 palavras por segundo. 15 segundos aceitam umas 35 palavras.
- Uma ideia por frase, a oferta ou o benefício logo no começo, o pedido no fim.
- Escreva no idioma do público e confira nomes e números com o usuário antes de gerar; a voz lê exatamente o que estiver escrito.
- Escolha a voz pelo tom da marca: mais calorosa para serviços próximos, mais firme para B2B.

## Música
- Descreva o clima e o uso: "instrumental leve e acústico para fundo de anúncio de cafeteria, sem voz". Sem pedir músicas ou artistas conhecidos.
- Com locução, a música fica baixa por baixo da voz (o render_video já abaixa); sem locução, ela pode conduzir o ritmo das cenas.
- Cada clipe tem cerca de 30 segundos e o vídeo usa só o trecho necessário.
- No Estúdio, deixe a música abaixar sozinha sob a fala: set_ducking na faixa da música com trigger_track_ids da faixa da voz. reduce_db de 12 a 18 (12 discreto, 18 forte), attack_ms de 200 a 300, release_ms de 500 a 800 e hold_ms de 300 para a música não pulsar entre frases. A regra acompanha cortes e movimentos da voz; bake_ducking só quando precisar ajustar ponto a ponto.
- Volume em dB: update_track com gain_db acerta uma faixa inteira (-3 a -6 abaixa sem sumir); animate com property gainDb num clipe de áudio desenha a curva de volume (chaves em dB, -60 é silêncio) para subir a música num respiro sem fala ou baixá-la antes do encerramento.

## Efeitos sonoros (Estúdio)
- O efeito marca um acontecimento da imagem; som sem motivo na tela vira ruído. Poucos e certeiros: em um anúncio de 15 segundos, de 3 a 6 bastam.
- Whoosh nas transições e em textos ou elementos que entram rápido: comece de 100 a 200 ms antes do corte, para o pico cair junto com a troca. De 400 a 1200 ms.
- Impacto (batida grave, thump) em cortes secos, na revelação do produto, do preço ou do logo: o ataque cai exatamente no quadro do corte. De 500 a 1500 ms.
- Riser antes de uma revelação: termina no instante dela, e o impacto entra ali. De 1500 a 4000 ms.
- Pop e clique para ícones, botões, contadores e listas que aparecem; ambiente (rua, café, escritório, natureza) como cama baixa durante a cena inteira, de 5000 a 22000 ms.
- Volume: com locução, de 12 a 18 dB abaixo da voz, o que dá volume do clipe entre 0,13 e 0,25; acentos sem voz até 0,5; ambiente entre 0,05 e 0,15. Fade de saída de 100 a 300 ms quando o efeito for cortado antes do fim.
- O mesmo tipo de som para o mesmo tipo de acontecimento: o espectador aprende o padrão. Para variar, gere de novo com outra descrição, não com outro volume.
- Como pedir: studio_generate_sfx com prompt descrevendo um só som pelo que se ouve (tipo, material, peso, velocidade, espaço; sem fala e sem música), length_ms com a duração e at_ms com o início na linha do tempo. Use uma faixa de áudio só para efeitos (track_id), separada da música e da voz, para mexer no volume de todos juntos com update_track e gain_db.
- Cada efeito gerado é cobrado. Antes de gerar o mesmo som de novo para outro ponto, reaproveite o media_id que já está no projeto com studio_edit_video (add_media).
- Antes de gerar, veja os sons prontos da biblioteca: studio_library_search com category audio traz whoosh, pop, clique, plim e sons de interface (notificação, chamada conectada, encerrada e toque de chamada). Entram sem custo com add_element(item_id, at_ms, track_id?) numa faixa de áudio, com a duração do próprio som. Servem bem para pops de ícones, cliques em botões e transições curtas; para um som com caráter próprio da marca, gere com studio_generate_sfx.
- Os áudios que a pessoa já subiu ou gerou continuam na biblioteca: no painel Elementos eles aparecem em Áudios, Seus áudios, e para você entram com add_media pelo media_id.
- Música, locução, efeitos e imagens com IA dividem uma fila com poucas vagas ao mesmo tempo no workspace; o pedido que passa do limite é recusado sem cobrança. Com música e vários efeitos, peça a música e um ou dois efeitos, espere com studio_jobs e wait_seconds e só então peça os seguintes. Se um pedido for recusado pela fila, espere e repita só esse.

## Quando o pedido tem som (Estúdio)
- Peça o som cedo, logo depois do plano: studio_generate_music vai para a fila e fica pronto enquanto você monta as cenas. Deixar o som para o fim arrisca entregar um vídeo mudo.
- Whoosh, pop e clique da biblioteca entram sem custo e sem aprovação com add_element; use esses primeiro e gere só o que precisa de caráter próprio.
- Geração paga pede aprovação nos modos Perguntar e Editar sozinha: o cartão pausa a resposta até a pessoa aprovar. Diga no texto o que o cartão vai gerar e por quê.
- Nunca termine uma peça que pedia som sem som e sem cartão: se não deu para pedir, diga que o som falta e ofereça gerar.

## Mixagem (Estúdio)
- Pan coloca o som no estéreo, de -1 (tudo à esquerda) a 1 (tudo à direita), com 0 no centro. Voz e música ficam no centro. Um efeito pode acompanhar o lado da tela onde o elemento aparece, com pan entre 0.3 e 0.6; pan total só em detalhes curtos, porque muita gente ouve no celular.
- update_clip com pan ajusta um clipe de áudio; update_track com pan ajusta a faixa inteira e soma ao pan dos clipes. Para um som que atravessa a tela (whoosh acompanhando um elemento), animate com property pan e chaves de -0.6 a 0.6 no tempo do movimento.
- Cruzamento de som: add_transition entre dois clipes de áudio que se encostam na mesma faixa. transition_kind constantPower (o padrão, 1000 ms) troca de música ou de ambiente sem buraco nem queda no meio; constantGain serve para dois trechos da mesma música ou sons muito parecidos, que somados ficariam altos demais. duration_ms de 300 a 2000; cruzamentos curtos soam como corte.
- Normalizar: normalize_loudness com target_lufs leva o som ao alvo medindo como ele toca: -14 para redes sociais, -16 para podcast, -23 para TV. Com clip_ids, cada clipe vai ao alvo por conta própria, bom para igualar falas e efeitos de origens diferentes; com track_id, a faixa inteira vai ao alvo pelo volume da faixa, mantendo a diferença entre os clipes.
- A resposta traz o que foi medido e o ganho aplicado. Quando avisa que o ganho chegou ao máximo, o clipe ficou abaixo do alvo: suba a faixa com update_track e gain_db ou aceite um pouco abaixo.
- Normalize a voz primeiro e ajuste a música e os efeitos em relação a ela; depois confira a mixagem inteira com studio_listen. studio_read mostra a loudness de cada faixa de áudio como ela soa, e studio_listen mostra, janela por janela, para que lado o som pende.

## Conferir o som (Estúdio)
- Você não ouve, mas mede: depois de colocar ou mexer em música, voz, efeitos, ducking ou volumes, rode studio_listen e decida pelos números. Não peça ao usuário para escutar no seu lugar.
- Metas para redes sociais: loudness integrada perto de -14 LUFS (de -16 a -12), pico até -1 dB e, enquanto alguém fala, a voz pelo menos 10 dB acima do resto (12 é mais seguro).
- Música perto da voz num trecho: abaixe a faixa com gain_db, aumente reduce_db do ducking ou desenhe a curva com gainDb; meça de novo só aquele trecho com from_ms e to_ms.
- Limitador agindo por muito tempo: a mixagem está alta demais; abaixe as faixas em vez de deixar o limitador espremer o som.
- Silêncio inesperado no meio: clipe de áudio faltando, mudo ou cortado. Se a voz está numa faixa que não dispara ducking, informe essa faixa em voice_track_ids.
- window_ms de 250 a 500 para conferir um efeito ou uma entrada; o padrão de 1000 serve para o vídeo inteiro.

## Fluxo
1. Combine com o usuário a mensagem, as cenas (media_id e segundos) e se terá música, locução ou as duas.
2. Gere a música e ou a locução e mostre para ele ouvir.
3. render_video com as cenas na ordem, o formato e os media_id do som. Mostre o vídeo.
4. Ajustes de duração ou ordem só remontam o vídeo (sem custo); trocar música ou voz gera de novo e é cobrado.
5. Use o media_id do vídeo como criativo VIDEO em create_ad, save_ad_draft ou swap_ad_creative.

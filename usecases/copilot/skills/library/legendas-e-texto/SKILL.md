---
name: legendas-e-texto
description: Como legendar a fala e escrever textos na tela no Estúdio (gerar legendas pela fila, estilo para vídeos curtos, posição segura, regras de leitura profissionais, contraste e revisão); carregue antes de legendar ou pôr texto em vídeo ou imagem.
---

# Legendas e texto na tela

## Gerar legendas
1. studio_start_job com kind captions e o clip_id do clipe com fala (vídeo ou áudio). Ele vai para a fila na hora; continue editando.
2. Quando ficar pronto, o editor cria sozinho uma faixa de legendas. Confira com studio_read.
3. Se o editor estava fechado, pegue o media_id do arquivo de legendas em studio_jobs e use add_captions com esse media_id e o clip_id da fala.

## Estilo para vídeos curtos (Reels, TikTok, Shorts)
- De 1 a 4 palavras por legenda, em fonte sem serifa e forte (font_weight 700 ou 800).
- Tamanho de 50 a 70 px numa largura de 1080. Como font_size é fração da altura: cerca de 0,03 no 9:16, 0,045 no 4:5 e 0,055 no 1:1.
- Branco com contorno escuro (stroke #000000, stroke_width de 4 a 6), ou caixa atrás, ou sombra translúcida (#00000080, blur perto de 20% do tamanho da letra). Sombra preta opaca e grande vira mancha.
- Mude todas de uma vez com update_clip e clip_ids (os ids da faixa de legendas em studio_read).

## Posição
- 9:16: centro do texto entre y 0,56 e 0,62, x 0,5, w 0,84, para a legenda inteira ficar acima dos 35% de baixo cobertos pela interface. Nada importante acima de y 0,14 nem nos 6% das laterais.
- 4:5 e 1:1: y entre 0,78 e 0,84. 16:9: y 0,85.
- A legenda nunca cobre rosto nem produto; se cobrir, suba ou desça a faixa inteira.

## Regras de leitura
- No máximo 42 caracteres por linha e 2 linhas; uma linha sempre que couber. Em vídeo vertical, linhas de 15 a 25 caracteres.
- Até 17 a 20 caracteres por segundo, cerca de 3 palavras por segundo. Cada legenda fica de 0,8 a 7 s na tela.
- Quebre depois da pontuação ou antes de conjunções e preposições. Nunca separe artigo do substantivo, adjetivo do substantivo, sujeito do verbo, nem o número da unidade. Em duas linhas, a de baixo é a mais longa.
- Revise o texto transcrito: nomes, marcas, números e termos técnicos costumam sair errados. Corrija com update_clip e o novo text.

## Texto na tela
- Uma ideia por texto e títulos com até 7 palavras; frases com até 5 palavras na abertura do vídeo.
- Tempo na tela: o tempo de leitura (cerca de 3 palavras por segundo) mais 0,5 a 1 s. A checagem automática avisa quando falta tempo.
- Contraste de pelo menos 4,5:1 com o fundo real (3:1 em títulos grandes). Sobre vídeo: contorno, caixa ou sombra translúcida.
- Estilos prontos: headline, caption, cta, lowerThird e tag. Nome e cargo em lowerThird ficam 3 s ou mais.
- O texto da tela não repete palavra por palavra a legenda ao mesmo tempo; um complementa o outro.
- Português é mais longo que o inglês: deixe 30% de folga na caixa.

## Conferir
studio_look em 3 a 6 momentos com fala: o texto cabe, não cobre rostos nem o produto e fica fora das áreas da interface. Corrija os erros do campo checks de cada studio_edit_video.

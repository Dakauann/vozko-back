---
name: explicativo-e-dados
description: Como fazer vídeos explicativos, tutoriais e peças com números ou gráficos que as pessoas entendem e lembram (princípios de aprendizagem multimídia, passos numerados, gráficos limpos e animados em etapas); carregue antes de explicar um processo, ensinar algo ou mostrar dados.
---

# Explicativos, tutoriais e dados

## Princípios de aprendizagem multimídia (Mayer)
- Coerência: corte tudo que não ajuda a entender, inclusive música e enfeites que competem com a fala.
- Sinalização: destaque a estrutura (títulos de etapa, números, setas, cor de acento no que importa agora).
- Contiguidade: o rótulo fica ao lado do objeto, e aparece no momento em que a fala cita o objeto.
- Segmentação: em etapas curtas, uma por vez, cada uma com seu título.
- Modalidade: fala com imagem explica melhor que imagem com muito texto escrito; não repita a fala inteira na tela.
- Voz humana e tom de conversa explicam melhor que voz robótica e tom formal.
- Pré-treino: apresente os termos-chave antes de usá-los.

## Tutorial
- Diga o resultado primeiro ("em 1 minuto você vai..."), depois os passos numerados com o número na tela.
- Um passo por cena, com o título do passo em add_text e a ação acontecendo na tela.
- Engajamento estável até 2 minutos e queda depois disso; partes com menos de 6 minutos. Divida conteúdos longos em capítulos.
- Alternar rosto falando e tela explicando, com fala animada, prende mais.

## Gráficos e números (Cleveland e McGill, Tufte, Knaflic)
- Compare por posição ou comprimento (barras) antes de ângulo, área ou cor. Pizza só com 2 ou 3 partes.
- O título diz a conclusão ("Vendas dobraram em março"), não só o assunto.
- Uma cor de destaque para o dado que importa e o resto em cinza. Rótulo direto no dado, sem legenda separada.
- Barras começam no zero. Tire grades, bordas e efeitos que não informam.
- Animação em etapas, uma mudança por vez; o estado final fica parado pelo menos 2 s.

## Montar no Estúdio
- Barras com add_shape rect que entram uma por vez com entrance slideUp, de 100 a 150 ms entre elas, ou com animate em scale de 0,9 para 1 e easeOut.
- Números grandes com add_text, font_weight 800, e um rótulo curto embaixo.
- Contador que muda: vários add_text em sequência com animate de opacity em hold, ou textos curtos trocados no tempo.
- Confira com studio_look no fim de cada etapa.

## Erros comuns
- Texto na tela repetindo a fala inteira. Gráfico com tudo colorido. Eixo que não começa no zero.
- Etapas longas demais, sem títulos. Números que somem antes de serem lidos.

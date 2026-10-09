---
name: profundidade-e-camadas
description: Como dar profundidade a uma arte no Estúdio (ordem dos planos, texto atrás da pessoa com remoção de fundo, quebrar a moldura, sombras de contato, brilho, vazamento de luz, vinheta, tratamento de cor da foto, duotone e atmosfera); carregue ao empilhar camadas ou tratar fotos.
---

# Profundidade e camadas

Ordem típica, de baixo para cima: cor ou degradê do fundo, foto de fundo, camadas de tratamento, texto de trás, recorte da pessoa ou do produto, texto da frente e rótulos, luz, textura, informação sobre um campo limpo. De 3 a 6 planos dão uma peça com profundidade. order_layers com above_id ou below_id posiciona com precisão.

## Texto atrás da pessoa
1. add_image da foto, sangrando a arte.
2. duplicate_layers da foto com ref: a cópia fica logo acima, um pouco deslocada; no mesmo lote, update_layer na cópia com o mesmo x, y, w e h da foto de baixo para as duas coincidirem.
3. add_text do título e order_layers com above_id na foto de baixo: o texto fica entre as duas.
4. studio_start_job kind cutout com o layer_id da cópia de cima. O editor troca a imagem dela pelo recorte quando ficar pronto; continue o resto da peça enquanto isso e acompanhe com studio_jobs.
5. A pessoa cobre de 15% a 35% das letras. Sombra suave no recorte: cor no tom mais escuro da paleta com alfa (#1a0f0866), x 0, y de 0.5% a 1% da altura, blur de cerca de 2% da altura.
6. Opcional: uma cópia da palavra só de contorno acima do recorte, para as letras escondidas continuarem sugeridas.
7. Veja com studio_look se ficaram halos no cabelo.

## Quebrar a moldura
Uma forma (rect, ellipse, path_preset arch ou um path), a foto logo acima com clip true e o recorte da mesma foto acima de tudo, saindo de 5% a 15% da altura da moldura por cima ou por um lado só.

## Sombras e luz
- Uma direção de luz para a peça inteira.
- Sombra de contato de produto: ellipse com w de 0.8 a 1.0 e h de 0.06 a 0.10 do produto, logo abaixo dele, no tom mais escuro da paleta, opacity de 0.3 a 0.5; um degradê radial do escuro para transparente faz a borda macia.
- Brilho, só no que emite luz sobre fundo escuro (neon, farol, tela): shadow com a mesma cor, mais saturada, x 0, y 0 e blur alto; ou uma ellipse com degradê radial da cor para transparente atrás do objeto, blend_mode screen, opacity de 0.4 a 0.7.
- Vazamento de luz: ellipse grande (w de 0.6 a 1.0) saindo de um canto, degradê radial de laranja quente ou magenta para transparente, blend_mode screen, opacity de 0.3 a 0.6; um ou dois, do lado da luz da foto.
- Vinheta: rect da arte inteira com degradê radial de transparente no centro para o tom mais escuro da paleta (não preto) nas bordas, radius de 1.2 a 1.5, blend_mode multiply, opacity de 0.3 a 0.6.

## Tratamento de cor da foto
- Ajustes na própria foto: filters (brightness, contrast, saturation) ou filter_preset.
- Foto sangrando a arte inteira: camadas de cor da arte inteira por cima com blend_mode:
  - unificar com uma cor da paleta: blend_mode color, opacity de 0.15 a 0.35;
  - quente e frio: degradê de frio para quente, blend_mode overlay ou soft-light, opacity de 0.2 a 0.4 (pele é quente, sombra fria separa a pessoa do fundo);
  - escurecer a área do texto: tom escuro da paleta, blend_mode multiply, opacity de 0.3 a 0.6, só sobre essa área;
  - filme fosco: azul acinzentado escuro, blend_mode screen, opacity de 0.1 a 0.2.
- Foto dentro de uma forma ou de outra camada: ajuste com filters e use, se precisar de tinta, uma camada de cor logo acima com clip true e opacity baixa.
- Duotone: saturation no mínimo e contrast mais alto na foto; acima dela, um rect da cor clara com blend_mode multiply e um rect da cor escura com blend_mode screen ou lighten. A cor escura perto de luminosidade 15 a 25, a clara acima de 75. Pares: marinho e rosa choque, violeta e amarelo ácido, verde floresta e menta, vinho e pêssego. Funciona melhor com a foto ocupando a arte inteira.

## Atmosfera e escala
- O que está longe fica mais frio, mais desfocado (filters blur) e com menos contraste; o que está perto, quente, nítido e saturado. Um leve desfoque no fundo atrás de um recorte separa os planos.
- Profundidade por escala: um elemento enorme cortado na borda e um pequeno ao longe.

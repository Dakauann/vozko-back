---
name: cor-e-tratamento
description: Cor de cartaz e post no Estúdio (paleta tirada do assunto ou da marca, estratégia de cor antes das amostras, disciplina do acento, os contrastes de Itten como ferramenta, foto puxada para a paleta, tema escuro e claro, degradês caros e baratos); carregue ao escolher cores ou quando a peça parecer sem identidade.
---

# Cor e tratamento

Contraste mínimo e legibilidade estão em design-de-imagem. Aqui fica a identidade.

## Da marca ou do assunto
- A cor principal da marca vem primeiro. Sem marca, tire cinco cores da foto: a sombra mais funda, um meio-tom, a cor do assunto, uma luz e um complemento para o acento.
- Quatro famílias de clima saem da mesma foto: viva (saturação e luminosidade altas), suave (saturação baixa, luminosidade alta), profunda (saturação alta, luminosidade baixa) e escura (as duas baixas). Uma família por peça.

## Estratégia antes das amostras
- Contida: neutros com um acento. Luxo, corporativo, editorial.
- Comprometida: uma cor dona de 30% a 60% da área. A maioria dos posts.
- Paleta cheia: 3 ou 4 cores com papéis definidos. Varejo, infantil, pop.
- Encharcada: a peça inteira numa cor, com tons dela. Noite, música, lançamentos.

## Disciplina do acento
- Um acento, em no máximo 10% da área, em uma ou duas coisas (palavra chave ou chamada, e um eco no motivo).
- No total, 3 a 4 cores além da foto.

## Contrastes como ferramenta (Itten)
- Claro e escuro: legibilidade e drama.
- Quente e frio: profundidade (quente vem para frente).
- Complementar: vibração, sempre com áreas desiguais.
- Saturação: um campo apagado e um elemento saturado parece o mais caro de todos.
- Extensão: uma área pequena e saturada equilibra uma grande e neutra.
- Simultâneo: um neutro ao lado de uma cor forte puxa para a complementar; tinja os neutros com a cor da peça.

## Foto puxada para a paleta
- Sombras na direção do tom mais escuro da paleta, luzes na direção do mais claro (camadas de tratamento em profundidade-e-camadas).
- A cor do assunto vira o acento ou é neutralizada; fundo menos saturado, assunto mais saturado.

## Correção de cor de foto e vídeo
- set_color corrige imagens (layer_id) e clipes de vídeo ou imagem (clip_id) como um colorista: primeiro a luz, depois a cor, por último o look.
- Luz: exposure em stops (0,3 a 0,7 salva uma foto escura; acima de 1,5 estoura o céu). Contraste de 10 a 30 dá corpo; highlights negativo recupera céu e pele brilhante; shadows positivo abre rostos no contraluz; blacks positivo desbota os pretos para um ar de filme.
- Cor: corrija a temperatura e o tint até o branco parecer branco antes de criar clima. Prefira vibrance a saturation para avivar sem queimar a pele; saturation entre -15 e 20 em quase tudo.
- Looks (look e look_intensity) são pontos de partida: warm, cool, faded, mono, punchy, dramatic, soft e vintage. Use de 0,4 a 0,8 de intensidade e ajuste color_grade por cima. Um look para a peça ou o vídeo inteiro, igual em todos os clipes.
- Combine os clipes de um vídeo: a mesma temperatura e a mesma exposição percebida entre cenas. Confira com studio_look lado a lado.
- reset_color tira tudo; um valor 0 em color_grade tira só aquele ajuste.

## Tema escuro e claro
- Escuro: quase preto tingido com a cor da peça (luminosidade 8 a 15, como #12101a), texto em branco tingido, acento de croma alto, brilho só no que emite luz.
- Claro: papel tingido em vez de branco puro (o branco puro combina com suíço e tecnologia limpa), tinta quase preta da cor da peça, uma versão mais escura do acento para texto pequeno.

## Degradês
- Caros: tons vizinhos (até 60 graus de distância na roda de cores) ou uma cor do claro ao escuro; de 10 a 25 pontos de luminosidade atrás de texto; radial e fora do centro como uma fonte de luz (cy 0.30 a 0.40); uma cor do meio saturada (via) para não passar pela zona cinza; grão por cima contra faixas.
- Baratos: arco-íris, complementares passando pelo cinza, índigo para roxo (o sinal de IA mais conhecido), dois tons saturados na vertical, degradê em todos os elementos.

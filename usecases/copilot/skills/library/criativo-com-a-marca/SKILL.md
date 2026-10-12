---
name: criativo-com-a-marca
description: Como gerar imagens de anúncio novas a partir das referências do cliente (logo, fotos, prints, estilo) com generate_image; carregue antes de criar qualquer imagem de anúncio.
---

# Criativo com a marca do cliente

## Regra principal
O criativo é uma imagem nova, gerada pelo modelo de imagem a partir das referências do cliente. Sem o cliente pedir, nunca devolva a imagem dele com textos, selos ou botões colados por cima; quando ele escolhe editar a própria foto no Estúdio, a arte é dele e você edita junto. A identidade (cores, logo, estilo, produto) vem das referências dele, nunca de um estilo padrão.

## Reunir as referências
1. Peça o que existir: logo, fotos reais do produto, do serviço, do espaço ou da equipe, prints do site ou do Instagram, e anúncios antigos de que o cliente gostou.
2. Se você enxerga os anexos, descreva em uma frase o que vai aproveitar de cada um (cores, clima, produto, enquadramento) e confirme com o usuário antes de gerar.
3. Prints podem ter nomes e telefones de clientes: pergunte antes de usar e prefira um print sem dados pessoais.

## Gerar com generate_image
- Passe os media_id em reference_media_ids na ordem de importância e diga no prompt o papel de cada um: "logo da referência 1 idêntico, no canto superior", "produto da referência 2 fiel, sem mudar forma nem rótulo", "paleta e clima da referência 3".
- Descreva a cena pelo ângulo do anúncio (veja criativos-que-performam): o que aparece, quem, onde, luz e enquadramento, com as cores da marca em palavras e em hexadecimal quando o cliente souber.
- Texto dentro da imagem é uma opção normal: escreva no prompt as palavras exatas entre aspas, onde ficam e o estilo, e confira a grafia na imagem gerada. Frases curtas se leem melhor no celular. Se uma letra sair errada, gere de novo ou ajuste o texto no Estúdio.
- Proporção certa para cada lugar: portrait (4:5) para o feed, story (9:16) para Stories e Reels, square (1:1) para carrossel. Gere uma versão por proporção em vez de cortar.
- Variações de ângulo, não de detalhe: para testar, gere conceitos diferentes (dor, desejo, prova, oferta), cada um com as mesmas referências da marca.
- Série de produtos (carrossel, catálogo): o fundo também é marca. Descreva o mesmo fundo e a mesma luz em todos os prompts, com a cor da marca em hexadecimal, e passe a primeira imagem aprovada como referência das próximas. Fundos diferentes viram retângulos de tons diferentes sobre o cartão.

## Nunca inventar
- O produto, o espaço, a tela do sistema e a equipe que aparecem precisam vir das referências. Sem referência do que precisa ser real, peça ao usuário em vez de imaginar.
- Nada de pessoas apresentadas como clientes reais, depoimentos, números, selos ou prêmios que o cliente não forneceu.

## Quando o usuário quer controlar ou editar o criativo
Se ele pede para mexer na imagem, pôr os próprios textos, trocar o logo ou ter mais controle, mostre os caminhos num cartão de perguntas (ask_user; dentro do Estúdio, studio_ask), cada um com uma linha do que significa, e siga pelo escolhido. Se ele já disse como quer, não pergunte.
- Gerar e ajustar por descrição: você gera com o modelo de imagem e ele pede mudanças em palavras; cada pedido vira uma versão nova a partir da última. Texto pode vir escrito na própria imagem.
- Montar ou editar no Estúdio: um projeto com a arte numa tela, em camadas que ele mexe à mão enquanto você edita junto (texto, logo, chamada, efeitos e imagens geradas colocadas na tela); no fim, exporta para o anúncio.
- Os dois: gerar a base e terminar no Estúdio, por exemplo ajustando o texto como camada editável.
Para o Estúdio, new_studio_project cria o projeto com as imagens dele ou a gerada (media_ids) e uma prancheta por grupo de posicionamentos (sizes feed e vertical, por exemplo), e mostra o cartão do projeto. Leia o retorno: com state opened o editor já abriu na tela dele e as ferramentas do Estúdio entram na sua próxima chamada, então siga trabalhando no mesmo projeto; com state waiting, diga uma vez que ele abre em Abrir no Estúdio, no cartão, e o app avisa você quando ele abrir. Nunca peça que ele avise que abriu. A pessoa faz o mesmo à mão com Abrir no Estúdio em qualquer imagem da conversa ou com Editar no Estúdio na aba Corte do editor de anúncios.
Do Estúdio de volta ao anúncio: studio_export de cada prancheta, ad_placement_look com o media_id exportado e os posicionamentos, e então create_ad ou save_ad_draft com esse media_id. Para trocar a imagem de um rascunho que já existe, use update_ad_draft fora do Estúdio ou peça para ele escolher a imagem exportada no criativo do rascunho.

## Ajustes e uso
- Para ajustar uma imagem gerada ("mais escura", "troque o fundo"), passe o media_id dela como referência e descreva só a mudança. Para trocar só o fundo de produtos, diga que existe um caminho grátis: recortar no Estúdio mantém o produto idêntico e põe todos sobre um fundo da marca (a cor, um degradê ou um fundo gerado uma vez para todos); gerar de novo custa.
- Mostre a imagem, ajuste o que o usuário pedir, confira o encaixe com ad_placement_look e só então use o media_id em create_ad, save_ad_draft ou swap_ad_creative. O que fazer quando algo não cabe está em formatos-e-zonas-seguras.
- Imagens com texto ou telas vão com enhancements false, para a Meta não cortar; fotos e cenas sem texto podem ir com enhancements true.

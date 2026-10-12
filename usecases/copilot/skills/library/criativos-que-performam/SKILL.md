---
name: criativos-que-performam
description: Como pensar, variar e renovar criativos de anúncio (ganchos, formatos, proporções, zonas seguras, fadiga); carregue antes de propor textos, imagens ou vídeos.
---

# Criativos que performam

Criativo novo do zero no Estúdio: valide o brief antes com a habilidade briefing-criativo.

## O criativo é a segmentação
A Meta lê o criativo para decidir quem vai vê-lo. Criativos parecidos entre si são tratados como um só. Diversidade real é o que dá alcance e reduz custo.

## Diversidade de verdade
- Varie o ângulo, não só a cor: dor do cliente, desejo, prova (depoimento real, antes e depois real, números reais do negócio), oferta, objeção respondida, bastidores, demonstração do produto.
- Varie o formato: foto, vídeo curto, carrossel, arte com texto.
- Para começar, 3 a 6 criativos de ângulos diferentes no mesmo conjunto. Cada um precisa fazer sentido sozinho.

## Gancho
- Quase metade do valor de um vídeo acontece nos 3 primeiros segundos. Abra com o resultado, um rosto, um movimento ou uma frase que nomeia a situação de quem vê.
- Texto na imagem: a mensagem principal em até 5 palavras.
- 80% dos Reels tocam sem som: vídeo sempre com legenda.

## Formatos e proporções
- Vertical 9:16 (1080 x 1920) para Stories e Reels; 4:5 (1080 x 1350) para o feed; 1:1 para carrossel.
- O ideal é ter cada criativo nas proporções certas em vez de deixar a Meta cortar.
- Zonas seguras de Stories e Reels: texto, rosto e logo fora dos 14% de cima, dos 35% de baixo e dos 6% de cada lateral, que a interface cobre. Antes de anunciar uma imagem, confira o encaixe em cada posicionamento com ad_placement_look (formatos-e-zonas-seguras diz o que fazer quando algo não cabe).
- Quem quer controlar a arte escolhe entre gerar e ajustar por descrição, editar no Estúdio ou os dois (criativo-com-a-marca).
- Vídeos curtos (até uns 15 segundos) funcionam melhor para mensagem e conversa.

## Texto do anúncio
- Texto principal: a primeira linha precisa funcionar sozinha, porque o resto fica escondido. Fale do problema ou do resultado, depois da oferta, depois do próximo passo.
- Título curto e concreto. Chamada para ação que diz o que acontece ("Fale com a gente no WhatsApp").
- Nada de promessas que o negócio não comprova. Sem travessões.

## Nunca inventar
- Nada de telas, produtos, pessoas, depoimentos, números ou selos que o usuário não forneceu. Imagem gerada por IA não pode se passar pelo produto, pelo espaço ou pelo cliente reais do negócio.
- Para mostrar algo real do negócio, use as fotos e prints do usuário (veja a habilidade criativo-com-a-marca).

## Renovação e fadiga
- Renove os criativos a cada 2 a 4 semanas, mais rápido quando o orçamento é alto.
- Sinais de cansaço: frequência acima de 3 por semana em público novo, queda de 20 a 30% na taxa de cliques a partir do pico, custo por resultado subindo. Quando o custo dobra em relação ao histórico, a própria Meta marca o criativo como cansado.
- Para renovar sem perder o histórico, adicione criativos novos ao conjunto ou duplique o anúncio e troque o criativo da cópia.

## Do Estúdio para o anúncio
Quando a arte ou o vídeo está no Estúdio, o criativo é o arquivo exportado do projeto, feito no navegador do usuário e salvo na biblioteca.
1. Confira o formato antes de exportar. A Meta veicula imagem de 9:16 a 1,91:1, com pelo menos 600 px de largura e de altura, até 30 MB, em PNG ou JPEG; vídeo de 9:16 a 16:9, em MP4, de 1 segundo a 241 minutos. Prancheta menor que 600 px vai com scale 2.
2. Imagem: studio_export com format jpeg, ou png com transparent false, e artboards active; a resposta traz o media_id de cada prancheta salva. Várias pranchetas (artboards all) viram os cards de um CAROUSEL, na ordem, ou, quando são uma prancheta por grupo de posicionamentos, versões do mesmo criativo (formatos-e-zonas-seguras).
3. Vídeo: studio_export começa o MP4 e responde na hora. Leia studio_read até export ter status done; em preparing, continue o que dá para fazer e leia de novo; failed traz o motivo. Com outdated true o projeto mudou depois da exportação: exporte de novo antes de usar.
4. Veja o arquivo com studio_look e o media_id e confira o encaixe por posicionamento com ad_placement_look antes de propor.
5. Conta: list_ad_accounts; sem conta, connect_ad_account; com can_spend false, ad_account_readiness e o anúncio vai como rascunho em save_ad_draft. Página e números vêm de list_ad_pages, locais de search_ad_locations e formulários de list_lead_forms.
6. Proponha create_ad (ou save_ad_draft) com format IMAGE ou VIDEO e o media_id exportado. Em arte com texto, enhancements false, para a Meta não cortar nem mexer. O cartão de aprovação mostra a prévia.
7. Se a recusa vier pelo arquivo (proporção, tamanho, duração ou formato), ela diz o limite: ajuste o projeto, exporte de novo e use o media_id novo.
8. Quem prefere fazer à mão usa Exportar e depois Usar em anúncio no editor: o arquivo é salvo e a criação do anúncio abre com ele escolhido.

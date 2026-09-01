# Discord Unlocker

Launcher para Windows que valida uma proxy SOCKS5 e abre o Discord Stable com uma rota dedicada somente ao processo do Discord. Ele não instala VPN, adaptador de rede, driver, serviço, servidor local ou processo residente. Depois de iniciar o Discord, o launcher termina.

Este é um projeto independente, sem vínculo, patrocínio ou aprovação da Discord Inc.

O Chromium interno do Discord mantém as conexões TCP de controle, como API e Gateway, pela mesma proxy durante a sessão. Os hosts conhecidos de CDN, anexos, arquivos estáticos, storage e mídia são ignorados pela rota, e o SOCKS5 do Chromium não transporta UDP/WebRTC. O proxy do Windows, os demais programas e a conexão de jogos não são alterados. Assim, voz, vídeo e compartilhamento de tela continuam no caminho direto.

O launcher usa uma única saída validada e não troca silenciosamente para o IP brasileiro. Com cache válido, só essa saída é revalidada nas próximas aberturas. Se ela cair, encerre completamente o Discord e abra o atalho novamente para selecionar outra saída.

## Uso

1. Instale `discord-unlocker-setup.exe`. A instalação é por usuário e não pede UAC.
2. Continue abrindo **Discord** normalmente pelo Menu Iniciar, pela área de trabalho ou por um pin existente na barra de tarefas. O instalador preserva o nome, o ícone e a identidade visual dos atalhos, mas troca o destino deles pelo launcher silencioso.
3. O instalador também troca a inicialização automática do Discord pelo launcher (`--autostart`). Ele não fica aberto após o bootstrap.

Antes de alterar qualquer atalho, o instalador salva uma cópia binária do `.lnk` original. O desinstalador restaura esses atalhos e a inicialização automática somente quando ainda apontam para o launcher; mudanças posteriores feitas pelo usuário ou pelo Discord são preservadas.

Se o Discord já estiver aberto, executar o atalho novamente não encerra nem reinicia a sessão. Para aplicar a rota a uma instância aberta diretamente, use **Sair do Discord** no ícone ao lado do relógio e abra o atalho outra vez.

Se não encontrar uma saída válida e o Discord estiver fechado, o programa o abre diretamente para manter texto e voz disponíveis. O modo liberado não é aplicado nessa situação.

## Segurança e privacidade

As listas públicas de proxies são tratadas como dados não confiáveis: o launcher limita a resposta, aceita somente IPs públicos e portas válidas, testa SOCKS5, exige TLS válido, verifica uma saída fora do Brasil e testa tanto o Gateway quanto a API pública do Discord. Apenas um IP e uma porta já validados entram nos argumentos do Chromium; nenhum texto arbitrário recebido da lista vira argumento ou código.

O cache versionado guarda somente endpoint, país, latência e horário da validação por até 24 horas. O launcher não lê tokens, mensagens ou arquivos de perfil do Discord.

Mesmo com TLS, uma proxy pública conhece o IP de origem, os destinos, os horários e o volume aproximado das conexões de controle que ela encaminha. O conteúdo continua protegido por TLS, mas a proxy não é uma ferramenta de anonimato e não deve ser tratada como tal. A mídia direta preserva a latência e a qualidade da chamada.

## Limites

- Compatível inicialmente com Discord Stable do instalador oficial em Windows 10/11 x64, em `%LOCALAPPDATA%\Discord`.
- Não modifica arquivos do Discord, não injeta código, não usa `Ctrl+R` e não usa `--no-sandbox`.
- Não dá suporte a verificação de idade, conta, assinatura, bloqueios de servidor ou limitações sem relação com a região da conexão.
- Não cria Cloudflare Worker: Workers não oferece um servidor TCP/CONNECT de entrada adequado para atuar como VPN.
- Não existe plugin do Discord neste projeto; o cliente não oferece API oficial para isso e modificar o cliente aumenta risco de quebra e de conta.

Antes de distribuir uma versão, teste manualmente a abertura de canais, câmera e Go Live, confirme que API/Gateway usam SOCKS5 e mídia/CDN/UDP seguem diretos, simule uma proxy indisponível e verifique que não restaram processos, serviços, drivers ou adaptadores do launcher.

## Desenvolvimento

Requer Go 1.26+, Inno Setup 6.7.3 e `go-winres` v0.3.3 para empacotar. O script
gera manifesto e VersionInfo, executa testes e cria um instalador de validação
sem compressão:

```powershell
go install github.com/tc-hib/go-winres@v0.3.3
.\scripts\build-windows.ps1 -Version 0.1.5
```

O arquivo produzido mantém o nome `discord-unlocker-setup.exe`. Uma compilação
local continua sem assinatura digital. As versões oficiais ficam na página de
Releases; `SHA256SUMS.txt` é disponibilizado para verificação opcional dos
downloads. Sem uma assinatura paga, o Windows pode identificar o instalador
como **Editor desconhecido**.

## Licença

O código é distribuído sob a [licença MIT](LICENSE). O nome e as marcas do
Discord pertencem aos seus respectivos titulares. O instalador copia o ícone da
instalação oficial já existente no computador; o ícone não é redistribuído por
este repositório. Os avisos das ferramentas e bibliotecas usadas na compilação
estão em [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).

## Referências

- [API pública ProxyScrape](https://docs.proxyscrape.com/api-reference/public-api/get-proxy-list)
- [Configuração de proxy, SOCKS5 e bypass no Chromium](https://chromium.googlesource.com/chromium/src/+/main/net/docs/proxy.md)
- [Switches de proxy do Electron](https://www.electronjs.org/docs/latest/api/command-line-switches)
- [DiscordGoLiveBypass, referência de comportamento](https://github.com/thomassolcia/DiscordGoLiveBypass)
- [Cloudflare trace](https://developers.cloudflare.com/fundamentals/reference/cdn-cgi-endpoint/)
- [Aviso do Discord sobre vídeo no Brasil](https://support.discord.com/hc/pt-br/articles/42704051358359-Por-que-os-recursos-de-v%C3%ADdeo-est%C3%A3o-indispon%C3%ADveis-no-Brasil-no-momento)
- [Medida preventiva da ANPD](https://www.gov.br/anpd/pt-br/assuntos/noticias/em-medida-preventiva-anpd-determina-que-discord-suspenda-transmissoes-ao-vivo-no-brasil)
- [TCP sockets no Cloudflare Workers](https://developers.cloudflare.com/workers/runtime-apis/tcp-sockets/)
- [Termos do Discord](https://discord.com/terms)

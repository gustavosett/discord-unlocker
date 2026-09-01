# Discord Unlocker

Launcher para Windows que prepara uma rota SOCKS5 temporária e abre o Discord Stable com um PAC embutido na própria linha de inicialização. Ele não instala VPN, adaptador de rede, driver, serviço, servidor local ou processo residente. Depois de iniciar o Discord, o launcher termina.

Este é um projeto independente, sem vínculo, patrocínio ou aprovação da Discord Inc.

O PAC tenta encaminhar somente `gateway.discord.gg`, seus hosts regionais de reconexão (`gateway-*.discord.gg`) e `remote-auth-gateway.discord.gg` por até três proxies SOCKS5 e usa `DIRECT` como último fallback. API, CDN, mídia, voz, vídeo, jogos, navegador e UDP não são desviados pelo aplicativo. Essa escolha reduz o escopo da rota intermediária, mas não é uma garantia de que recursos de vídeo serão liberados: o comportamento do Discord pode mudar e precisa ser validado manualmente em cada release.

## Uso

1. Instale `discord-unlocker-setup.exe`. A instalação é por usuário e não pede UAC.
2. Continue abrindo **Discord** normalmente pelo Menu Iniciar, pela área de trabalho ou por um pin existente na barra de tarefas. O instalador preserva o nome, o ícone e a identidade visual dos atalhos, mas troca o destino deles pelo launcher silencioso.
3. O instalador também troca a inicialização automática do Discord pelo launcher (`--autostart`). Ele não fica aberto após o bootstrap.

Antes de alterar qualquer atalho, o instalador salva uma cópia binária do `.lnk` original. O desinstalador restaura esses atalhos e a inicialização automática somente quando ainda apontam para o launcher; mudanças posteriores feitas pelo usuário ou pelo Discord são preservadas.

Se não encontrar uma saída válida, o programa preserva um Discord já aberto. Se ele estiver fechado, abre o Discord diretamente para manter texto e voz disponíveis. O modo com PAC não é aplicado nessa situação.

Se o Windows negar o encerramento de um Discord iniciado com permissões diferentes, feche-o uma vez com `Alt+F4` enquanto a janela estiver em foco (ou **Sair do Discord** no ícone ao lado do relógio) e execute o atalho novamente. O launcher não pede elevação nem tenta contornar a proteção do processo.

## Segurança e privacidade

As listas públicas de proxies são tratadas como dados não confiáveis: o launcher limita a resposta, aceita somente IPs públicos e portas válidas, testa SOCKS5, exige TLS válido, verifica uma saída fora do Brasil e testa o Gateway do Discord antes de gerar o PAC. Apenas IPs e portas já validados entram no PAC. O conteúdo é codificado em base64 numa URL `data:` aceita pelo Chromium; nenhum texto recebido da API vira JavaScript.

O cache versionado guarda somente endpoint, país, latência e horário da validação por até 24 horas. O launcher não lê tokens, mensagens ou arquivos de perfil do Discord.

Mesmo com TLS, uma proxy pública conhece o IP de origem, o destino, os horários e o volume aproximado das conexões que ela encaminha. Ela não é uma ferramenta de anonimato e não deve ser usada para dados sensíveis. O fallback `DIRECT` mantém o Discord utilizável caso as proxies caiam, mas pode fazer o recurso regional voltar a ser bloqueado até que o atalho seja executado de novo.

## Limites

- Compatível inicialmente com Discord Stable do instalador oficial em Windows 10/11 x64, em `%LOCALAPPDATA%\Discord`.
- Não modifica arquivos do Discord, não injeta código, não usa `Ctrl+R` e não usa `--no-sandbox`.
- Não dá suporte a verificação de idade, conta, assinatura, bloqueios de servidor ou qualquer limitação que não dependa da rota de Gateway.
- Não cria Cloudflare Worker: Workers não oferece um servidor TCP/CONNECT de entrada adequado para atuar como VPN.
- Não existe plugin do Discord neste projeto; o cliente não oferece API oficial para isso e modificar o cliente aumenta risco de quebra e de conta.

Antes de distribuir uma versão, teste manualmente a abertura de canais, câmera e Go Live, confirme que gateway usa SOCKS5 e mídia/CDN/UDP seguem diretos, derrube as proxies para observar o fallback e verifique que não restaram processos, serviços, drivers ou adaptadores do launcher.

## Desenvolvimento

Requer Go 1.26+, Inno Setup 6.7.3 e `go-winres` v0.3.3 para empacotar. O script
gera manifesto e VersionInfo, executa testes e cria um instalador de validação
sem compressão:

```powershell
go install github.com/tc-hib/go-winres@v0.3.3
.\scripts\build-windows.ps1 -Version 0.1.4
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
- [PAC, SOCKS5 e fallback no Chromium](https://chromium.googlesource.com/chromium/src/+/main/net/docs/proxy.md)
- [Carregamento de PAC `data:` no Chromium](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/net/proxy_resolution/pac_file_fetcher_impl.cc)
- [Switches de proxy do Electron](https://www.electronjs.org/docs/latest/api/command-line-switches)
- [Cloudflare trace](https://developers.cloudflare.com/fundamentals/reference/cdn-cgi-endpoint/)
- [Aviso do Discord sobre vídeo no Brasil](https://support.discord.com/hc/pt-br/articles/42704051358359-Por-que-os-recursos-de-v%C3%ADdeo-est%C3%A3o-indispon%C3%ADveis-no-Brasil-no-momento)
- [Medida preventiva da ANPD](https://www.gov.br/anpd/pt-br/assuntos/noticias/em-medida-preventiva-anpd-determina-que-discord-suspenda-transmissoes-ao-vivo-no-brasil)
- [TCP sockets no Cloudflare Workers](https://developers.cloudflare.com/workers/runtime-apis/tcp-sockets/)
- [Termos do Discord](https://discord.com/terms)

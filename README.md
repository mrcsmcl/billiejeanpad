# Billie Jean Pad 0.3.1

Soundboard colaborativo para Windows controlado por uma PWA no celular. A linha 0.3 adiciona agenda persistente, configuração local protegida e atualização automática silenciosa com assinatura Ed25519 e rollback.

## Builds

Copie os 22 WAVs selecionados do pack local:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\curate.ps1
```

Gere os executáveis:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
```

O script cria `group.local.json` na primeira execução e reutiliza a mesma chave nas seguintes. Esse arquivo não entra no Git.

A pasta `dist/` recebe:

```text
BillieJeanPad-Share.exe
BillieJeanPad-Keep.exe
BillieJeanPad-Update.exe
```

`Share` instala o aplicativo e apaga somente o lançador distribuído. `Keep` instala e preserva o lançador. `Update` é o payload genérico usado pelo atualizador; ele não contém chave de grupo e só funciona numa instalação que já tenha `state.dat`.

## Instalação

Ao abrir Share ou Keep, o aplicativo copia-se para `%LOCALAPPDATA%\BillieJeanPad\BJPAgent.exe`, registra inicialização automática e inicia em segundo plano sem abrir janela. O processo aparece como `BJPAgent.exe` no Gerenciador de Tarefas. Ele tenta uma Scheduled Task e usa `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` quando a política do Windows bloquear tarefas.

A versão 0.3 migra a chave incorporada no bootstrap para `%LOCALAPPDATA%\BillieJeanPad\state.dat`. O arquivo inteiro é criptografado pelo DPAPI no escopo do usuário do Windows. Ele guarda a chave, o modo automático e os agendamentos.

`Ctrl + Shift + F12` interrompe o som e encerra o processo. A remoção permanente fica somente na aba Máquinas da PWA, com confirmação.

## PWA

O GitHub Pages publica a pasta `docs/`. A URL atual é:

```text
https://mrcsmcl.github.io/billiejeanpad/
```

No primeiro acesso, informe a única chave de `group.local.json`. O navegador deriva o identificador público dos topics, salva a configuração localmente e reconecta automaticamente nas próximas aberturas.

As abas são:

- **Pad:** sons imediatos e modo automático.
- **Máquinas:** inventário autenticado, versão instalada e desinstalação remota.
- **Agenda:** criação e exclusão de regras executadas localmente em cada Windows.

## Agenda

A agenda não depende de a PWA ficar aberta. O painel envia uma regra assinada, o cliente salva no `state.dat` e executa localmente.

Há três tipos:

- **Uma vez:** data e hora específicas.
- **Toda semana:** dias da semana e horário.
- **Janela aleatória:** uma execução em horário aleatório entre o início e o fim da janela, nos dias escolhidos.

Cada regra pode tocar um som, uma categoria ou uma surpresa ponderada. Horários usam o fuso IANA enviado pelo navegador; o cliente incorpora a base de fusos do Go para funcionar no Windows. Há limite de 8 regras por máquina.

Se o computador estiver desligado ou suspenso, uma execução atrasada em mais de 5 minutos é descartada. Não existe rajada de eventos acumulados ao acordar. Sons da agenda respeitam o microfone ativo; cliques manuais no Pad continuam tocando durante chamadas.

## Modo automático

O modo começa desligado, toca apenas entre 09:00 e 18:00 e pausa entre 12:00 e 13:30.

| Modo | Intervalo aproximado |
|---|---:|
| Fantasma | 6–10 horas |
| Discreto | 3–5 horas |
| Travesso | 1–2 horas |

A escolha fica persistida no `state.dat`. Trocar o preset recalcula imediatamente o próximo intervalo.

## Atualização automática

O cliente instalado consulta silenciosamente `docs/update.json` após o início e depois aproximadamente a cada 12 horas, com jitter. O manifesto contém versão, URL imutável do asset, tamanho, SHA-256 e assinatura Ed25519.

O cliente possui somente a chave pública de lançamento. A chave privada fica em `release-key.local.json`, está no `.gitignore` e nunca deve ser enviada ao GitHub.

Quando existe uma versão mais nova, o cliente baixa `BillieJeanPad-Update.exe`, valida URL, tamanho, hash e assinatura, guarda a versão anterior, substitui o executável e inicia a nova. A nova versão grava `healthy.version` após 5 segundos. Se isso não acontecer em 30 segundos, o helper restaura e inicia automaticamente a versão anterior. Uma versão que falhou não é tentada novamente por 24 horas.

A primeira migração para a linha 0.3 é manual. Depois que ela cria o `state.dat`, os releases seguintes podem usar o payload genérico sem conhecer a chave do grupo.

## Publicar uma atualização

Atualize `Version` em `config.go` e execute:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\release.ps1
```

O script recompila tudo, assina `docs/update.json` e informa a tag esperada. Faça upload de `dist/BillieJeanPad-Update.exe` num GitHub Release com a tag correspondente, por exemplo `v0.3.0`. Só depois publique o novo `docs/update.json`.

Se o GitHub CLI estiver instalado e autenticado:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\release.ps1 -Upload
```

O endereço do asset segue o formato:

```text
https://github.com/mrcsmcl/billiejeanpad/releases/download/vVERSAO/BillieJeanPad-Update.exe
```

## Transporte e presença

O cliente assina o topic geral e o específico da máquina. Heartbeats e comandos usam HMAC-SHA256. O painel valida os anúncios, conserva inventário autenticado localmente e mostra a versão de cada cliente.

Para respeitar os limites do ntfy.sh público, o cliente anuncia ao iniciar, quando recebe `ping` e a cada 6 horas. A conexão longa tem watchdog de 2 minutos baseado nos keepalives do ntfy. O painel limita pings automáticos e explica HTTP 429.

## Segurança

A chave do grupo nunca fica no GitHub Pages. O código público do topic é derivado dos primeiros 128 bits de SHA-256 da chave. Comandos vencem em 30 segundos. O updater aceita somente URLs HTTPS de releases deste repositório e exige assinatura Ed25519 válida. O estado local é protegido pelo DPAPI.

## Arquivos privados e gerados

Não entram no Git:

```text
group.local.json
release-key.local.json
dist/
sounds/**/*.wav
```

Preserve `group.local.json` para gerar builds do mesmo grupo e `release-key.local.json` para assinar atualizações futuras. Perder a chave de lançamento impede publicar atualizações aceitas pelos clientes existentes.

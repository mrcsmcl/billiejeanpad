# Billie Jean Pad

Soundboard colaborativo para Windows, controlado por uma PWA no celular. Cada grupo tem uma única chave privada. O cliente deriva dessa chave os canais ntfy e usa a própria chave para assinar comandos e anúncios de presença.

## Gerar as duas builds

Primeiro copie os 22 WAVs selecionados do pack local:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\curate.ps1
```

Depois gere os executáveis:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1
```

Na primeira execução, o script cria `group.local.json` com uma chave aleatória de 256 bits. Nas execuções seguintes, reutiliza a mesma chave. O arquivo está no `.gitignore` e não deve ser publicado.

O resultado fica em `dist/`:

```text
BillieJeanPad-Share.exe
BillieJeanPad-Keep.exe
```

A build **Share** instala o aplicativo e, quando a instalação termina com sucesso, apaga somente o executável distribuidor que foi aberto. A cópia instalada em `%LOCALAPPDATA%\BillieJeanPad\billiejeanpad.exe` continua funcionando. Use essa variante para distribuição.

A build **Keep** faz a mesma instalação, mas preserva o executável distribuidor. Use para arquivo, diagnóstico e testes.

Para imprimir também um convite pronto para o painel:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1 `
  -SiteUrl "https://SEU-USUARIO.github.io/NOME-DO-REPO/"
```

O convite contém a chave depois de `#`. Essa parte não é enviada ao GitHub Pages, mas qualquer pessoa que receba o link passa a controlar o grupo.

## Hospedar a PWA no GitHub Pages

A pasta `docs/` está pronta para publicação. Em **Settings → Pages**, selecione **Deploy from a branch**, a branch `main` e a pasta `/docs`.

O site publicado não contém a chave de nenhum grupo. No primeiro acesso, ele solicita somente a chave. O navegador salva a configuração localmente. Depois disso, abrir a PWA pelo ícone do celular reconecta automaticamente sem pedir a chave novamente.

Para instalar no celular:

- Android/Chrome: menu → **Instalar app**.
- iPhone/Safari: Compartilhar → **Adicionar à Tela de Início**.

## Como as máquinas aparecem

Ao abrir, o painel carrega heartbeats assinados das últimas 12 horas, mantém no aparelho um histórico autenticado de até 30 dias e envia no máximo um `ping` por minuto. O botão **Atualizar** solicita uma presença nova. O cliente também anuncia ao iniciar e a cada 6 horas, evitando estourar a cota diária do ntfy.sh público quando várias máquinas compartilham o mesmo IP.

A aba **Pad** controla a máquina escolhida ou todas. A aba **Máquinas** lista todos os dispositivos autenticados, mostra o modo automático atual e oferece acesso direto ao Pad ou desinstalação remota com confirmação.

## Segurança do grupo

A única credencial é `groupKey`, salva em `group.local.json` e injetada nos dois executáveis durante o build. O código público dos topics é derivado assim:

```text
bjp- + primeiros 16 bytes de SHA-256(groupKey)
```

O identificador público tem 128 bits, mas não permite descobrir a chave. Comandos e heartbeats usam HMAC-SHA256. Comandos com timestamp fora da janela de 30 segundos são descartados. Heartbeats sem assinatura válida ou antigos não aparecem no painel.

O GitHub Pages hospeda apenas HTML, CSS, JavaScript, manifest, Service Worker e ícones. Nenhum desses arquivos contém a chave.

## Instalação no Windows

Ao abrir uma das builds, o aplicativo:

1. copia-se para `%LOCALAPPDATA%\BillieJeanPad\billiejeanpad.exe`;
2. tenta registrar a Scheduled Task `BillieJeanPad` para o logon;
3. se a política bloquear tarefas, usa `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`;
4. inicia a cópia instalada em segundo plano, sem abrir janela.

A build Share apaga o lançador original somente depois desses passos.

Controles locais:

- `Ctrl + Shift + F12`: interrompe o som e encerra o processo;
- log: `%LOCALAPPDATA%\BillieJeanPad\log.txt`;
- remoção: somente pela aba **Máquinas** do painel, com confirmação.

## Modo automático

Começa desligado e só toca durante 09:00–18:00, com pausa 12:00–13:30. Sons automáticos são ignorados se o microfone estiver em uso; comandos manuais enviados pelo Pad continuam tocando durante chamadas.

| Modo | Intervalo aleatório aproximado |
|---|---:|
| Fantasma | 6–10 horas, cerca de 1 vez por dia |
| Discreto | 3–5 horas, cerca de 2–3 vezes por dia |
| Travesso | 1–2 horas, cerca de 4–8 vezes por dia |

## Atualizar ou trocar de grupo

A versão nova pode ser executada sobre uma instalação existente: o lançador encerra a cópia instalada anterior, substitui o arquivo e inicia a atualização. Preserve `group.local.json` para manter o mesmo grupo. Para criar um grupo independente, mova o arquivo atual para um local seguro e rode o script sem ele; uma nova chave será gerada.

## Verificação recomendada

Use uma VM Windows ou máquina de teste. Instale a build Keep, abra o painel, informe a chave de `group.local.json` e confirme que a máquina aparece na aba Máquinas. Teste um som, os três modos automáticos, a hotkey e a desinstalação. Depois repita em uma cópia descartável com a build Share e confirme que apenas o lançador original desaparece.

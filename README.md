<h1 align="center">* swagSSH</h1>
<p align="center"><strong>Мгновенный обратный SSH</strong> &mdash; self-hosted аналог tmate / ngrok для интерактивного терминала</p>

<p align="center">
  <a href="https://ssh.swag.best"><strong>ssh.swag.best</strong></a>
  &nbsp;&bull;&nbsp;
  <a href="#установка">Установка</a>
  &nbsp;&bull;&nbsp;
  <a href="#как-работает">Как работает</a>
  &nbsp;&bull;&nbsp;
  <a href="#сборка">Сборка</a>
</p>

<hr>

## Что это

**swagSSH** позволяет подключиться к любому компьютеру за NAT, файрволом или CG-NAT одной командой. Никакого проброса портов, VPN, Tailscale или WireGuard. Только исходящее SSH-соединение к вашему серверу.

- **Пользователь** выполняет одну команду и получает ID сессии
- **Оператор** вводит этот ID и получает полноценный интерактивный терминал
- **Сервер** релеит трафик между ними, не храня ничего на диске

## Установка

### Для пользователя (поделиться терминалом)

**Windows:**
```powershell
iwr -useb https://ssh.swag.best/install.ps1 | iex
```

**Linux / macOS:**
```bash
curl -fsSL https://ssh.swag.best/install.sh | bash
```

После выполнения появится ID сессии вида `swag-a8f2d1`.

### Для оператора (подключиться)

Скачайте бинарник под вашу платформу из [Releases](https://ssh.swag.best/releases/) или соберите из исходников:

```bash
swagssh connect swag-a8f2d1
```

## Как работает

```
Пользователь ──(исходящее SSH)──▶  РЕЛЕ-СЕРВЕР  ◀──(исходящее SSH)── Оператор
    │                              swagSSH                               │
    │                          ssh.swag.best:2222                        │
    ▼                                                                    ▼
 [PTY] shell                                                      интерактивный
powershell / bash                                              терминал оператора
```

1. **Пользователь** запускает `swagssh share` &mdash; клиент создаёт локальный PTY, запускает shell и устанавливает исходящее SSH-соединение с сервером
2. **Сервер** генерирует короткий ID сессии (`swag-xxxxxx`), сохраняет канал в памяти и выводит ID пользователю
3. **Оператор** запускает `swagssh connect <id>` &mdash; сервер находит сессию по ID и прозрачно проксирует байтовый поток между оператором и пользователем

## Возможности

| | |
|---|---|
| **Одна команда** | Без установщиков, конфигураций и регистраций |
| **Обход NAT** | Работает через CG-NAT, файрволы и любые ограничения |
| **Self-hosted** | Весь трафик через ваш сервер, полный контроль |
| **Кроссплатформенность** | Windows (ConPTY), Linux, macOS &mdash; один бинарник |
| **End-to-End шифрование** | SSH + Ed25519, эфемерные сессии с TTL |
| **Интерактивный терминал** | Полная поддержка PTY, resize, vim, htop, tmux, PowerShell |

## Архитектура

```
/opt/swagssh/
├── cmd/
│   ├── server/main.go    # SSH Relay Server (systemd-сервис)
│   └── client/           # CLI клиент (share + connect)
│       ├── main.go
│       ├── pty_unix.go   # Unix PTY (Linux/macOS)
│       ├── pty_windows.go # Windows ConPTY / pipe fallback
│       ├── signal_unix.go
│       └── signal_windows.go
├── web/
│   ├── index.html        # Лендинг
│   ├── install.sh        # Установщик Linux/macOS
│   └── install.ps1       # Установщик Windows
└── dist/                 # Скомпилированные бинарники
```

## Стек

| Слой | Технология |
|------|-----------|
| Язык | Go 1.23+ |
| SSH | `golang.org/x/crypto/ssh` |
| PTY (Unix) | `github.com/creack/pty` |
| PTY (Windows) | Native ConPTY / pipe |
| Веб-сервер | Nginx + Let's Encrypt |
| Управление сессиями | `sync.Map` in-memory + TTL cleanup |
| Сервис | systemd |

## Сборка

```bash
# Сервер
go build -ldflags="-s -w" -o swagssh-server ./cmd/server/

# Клиент (все платформы)
GOOS=linux   GOARCH=amd64 go build -ldflags="-s -w" -o dist/swagssh-linux-amd64     ./cmd/client/
GOOS=linux   GOARCH=arm64 go build -ldflags="-s -w" -o dist/swagssh-linux-arm64     ./cmd/client/
GOOS=darwin  GOARCH=amd64 go build -ldflags="-s -w" -o dist/swagssh-darwin-amd64    ./cmd/client/
GOOS=darwin  GOARCH=arm64 go build -ldflags="-s -w" -o dist/swagssh-darwin-arm64    ./cmd/client/
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o dist/swagssh-windows-amd64.exe ./cmd/client/
GOOS=windows GOARCH=386   go build -ldflags="-s -w" -o dist/swagssh-windows-386.exe   ./cmd/client/
```

## Серверное развёртывание

```bash
# systemd сервис
systemctl enable --now swagssh

# Nginx конфигурация
# /etc/nginx/sites-available/swagssh
# server_name ssh.swag.best;
# Проксирует: web/ → лендинг, /releases/ → dist/, /install.* → скрипты
```

## Безопасность

- Ed25519 host-ключи (генерируются автоматически при первом запуске)
- Эфемерные сессии &mdash; ничего не хранится на диске
- TTL сессии: 1 час, автоматическая очистка каждые 5 минут
- Криптографически безопасная генерация ID сессий (`crypto/rand`)
- Все чувствительные данные только в памяти

## Лицензия

[MIT](LICENSE)

---

<p align="center">
  <sub>* swagSSH®</sub>
</p>

# abuse-control — форк remnawave-limiter

Отдельный Go-модуль (`github.com/remnawave/limiter`), не часть корневого. Корневой `go test ./...` его не видит: запускать отсюда. На проде лежит в `/opt/abuse-control` на машине с панелью, образ собирается локально (`docker compose build`).

## Что отличается от оригинала

| Где | Что |
|---|---|
| `internal/monitor/geo.go` | гео-проверка: города по IP, порог, действие `auto`/`manual`/`ban`, откат на отключение |
| `internal/geoip/city.go`, `downloader.go` | база GeoLite2-City; загрузчик принимает название издания (по умолчанию ASN) |
| `internal/botban/` | клиент `POST /internal/abuse-ban` бота TunnelKey |
| `internal/telegram/checker.go` | экраны «Статус / События / Люди», карточка, кнопки действий, сторож цикла проверки |
| `cmd/limiter/geo.go`, `checker_actions.go` | загрузка базы городов; связка действий карточки с панелью, Redis и ботом |
| `internal/i18n/geo.go` | тексты и логи гео-проверки (ru/en/zh) |

`checker.go` освобождён от проверки «русский текст в коде» (`internal/i18n/logs_test.go`): там тексты только на русском. Всё остальное — через `i18n.T`, иначе падает `TestNoCyrillicStringLiterals`.

## Инварианты

- **Автобан касается только неплативших.** Решает бот TunnelKey (`internal/handlers/abuse_ban.go`): платившему он отвечает `paid`, и здесь тот отключается на `AUTO_DISABLE_DURATION`, а не удаляется. Любой другой ответ, ошибка или отсутствие Telegram id — тоже откат на отключение. Пины — `internal/monitor/geo_ban_test.go`.
- **Кнопки карточки работают только для админов бота** (проверка `adminIDs` в `handleCallback`), а бан из карточки — через экран подтверждения: `ck:b:` показывает вопрос, `ck:B:` выполняет.
- **Сторож следит за самим циклом проверки**, а не за контейнером: упавший процесс о себе не сообщит. Для этого нужен внешний монитор (`HEALTH_ADDR`).
- **`ABUSE_BAN_URL` — адрес шлюза docker-сети** (`docker network inspect abuse-control_default`). Пересоздали сеть — проверить адрес, иначе бан молча превратится в отключение, а в журнале будет «бот не ответил на запрос бана».
- **Токен `ABUSE_BAN_TOKEN` один на два сервиса:** в `.env` этого сервиса и в `.env` бота. Разошёлся — бот считает `tk_abuse_ban_unauthorized`.

Связанное: `internal/handlers/CLAUDE.md` в корне, раздел «Бан по сигналу Abuse Control».

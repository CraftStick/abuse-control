# Abuse Control

Telegram-бот для панели [Remnawave](https://github.com/remnawave): ограничивает число устройств на подписку, ищет раздачу подписок по городам и сетям и показывает, кто сейчас онлайн.

Основан на [remnawave-limiter](https://github.com/syvlech/remnawave-limiter) (GPL-3.0, лицензия в `LICENSE`). Всё, что отличается от оригинала, описано в `CLAUDE.md`. Подробно про сам лимитер — `docs/LIMITER.md`.

## Что добавлено в этом форке

- **Гео-проверка** (`GEO_BLOCK`): российские города по IP, порог `лимит устройств × GEO_CITY_MULTIPLIER`. Действие — `auto`, `manual` или `ban` (удалить и забанить в боте TunnelKey, если человек никогда не платил).
- **Экраны «Статус / События / Люди»**: жив ли цикл проверки, нарушения за 24 часа, список онлайн-пользователей с городами и провайдерами. Из карточки можно отключить или включить подписку, добавить в белый список, удалить и забанить.
- Тексты на русском, английском и китайском через `internal/i18n`.

## Установка

Нужны Docker с Compose и панель Remnawave с API-токеном (Настройки → API-токены).

```bash
git clone https://github.com/CraftStick/abuse-control.git
cd abuse-control
cp .env.example .env
```

В `.env` заполните пять значений, остальное работает по умолчанию:

| Переменная | Что это |
|---|---|
| `REMNAWAVE_API_URL` | адрес панели |
| `REMNAWAVE_API_TOKEN` | API-токен панели |
| `TELEGRAM_BOT_TOKEN` | токен бота от @BotFather |
| `TELEGRAM_CHAT_ID` | чат, куда приходят уведомления |
| `TELEGRAM_ADMIN_IDS` | ваш Telegram id (кому доступны кнопки) |

```bash
docker compose up -d --build
```

Откройте бота и отправьте `/start`. Логи: `docker compose logs -f limiter`.

Redis (Valkey) поднимается вместе с сервисом, ставить его отдельно не нужно. Обновление: `git pull && docker compose up -d --build`.

### Гео-проверка (по желанию)

Включается `GEO_BLOCK=true` и ключом `MAXMIND_LICENSE_KEY` (бесплатный, на maxmind.com): базу городов сервис скачивает сам. Действие `GEO_ACTION` — `auto` (отключить подписку), `manual` (прислать алерт с кнопками) или `ban`. `ban` работает только вместе с ботом TunnelKey (`ABUSE_BAN_URL`, `ABUSE_BAN_TOKEN`), без него оставьте `auto` или `manual`.

Подробные настройки лимитера — в `docs/LIMITER.md` (это документация оригинала: команды запуска там описаны для него, для этого форка используйте шаги выше).

## Проверки

```bash
go vet ./...
go test ./...
```

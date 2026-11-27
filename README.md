# Abuse Control

Telegram-бот для панели [Remnawave](https://github.com/remnawave): ограничивает число устройств на подписку, ищет раздачу подписок по городам и сетям и показывает, кто сейчас онлайн.

Основан на [remnawave-limiter](https://github.com/syvlech/remnawave-limiter) (GPL-3.0, лицензия в `LICENSE`). Всё, что отличается от оригинала, описано в `CLAUDE.md`. Подробно про сам лимитер — `docs/LIMITER.md`.

## Что добавлено в этом форке

- **Гео-проверка** (`GEO_BLOCK`): российские города по IP, порог `лимит устройств × GEO_CITY_MULTIPLIER`. Действие — `auto`, `manual` или `ban` (удалить и забанить в боте TunnelKey, если человек никогда не платил).
- **Экраны «Статус / События / Люди»**: жив ли цикл проверки, нарушения за 24 часа, список онлайн-пользователей с городами и провайдерами. Из карточки можно отключить или включить подписку, добавить в белый список, удалить и забанить.
- Тексты на русском, английском и китайском через `internal/i18n`.

## Запуск

1. `cp .env.example .env` и заполнить: токен бота, `TELEGRAM_ADMIN_IDS`, адрес и токен панели, `MAXMIND_LICENSE_KEY`.
2. `docker compose -f deploy/docker-compose.yml up -d --build` (контекст сборки — этот каталог, `.env` лежит рядом с compose-файлом).
3. `/start` в боте.

Сервис вместе со своим Redis работает на машине с панелью; боту TunnelKey он ходит по адресу `ABUSE_BAN_URL` (шлюз docker-сети контейнера).

## Проверки

```bash
go vet ./...
go test ./...
```

Два теста в `internal/telegram` (`TestSendUpdateNotice_LinksToRelease`, `TestFormatStartupMessage`) падают из-за названия «Remnawave Limiter» в ожидаемом тексте: сборка называется «Abuse Control». Это расхождение осталось от прежнего форка.

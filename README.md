# time-table-bot

Telegram bot на Go + PostgreSQL для расписания мастера, записи клиентов и напоминаний.

## Локальный Запуск

```bash
cp .env.example .env
# заполнить TELEGRAM_BOT_TOKEN
docker compose up --build
```

`docker-compose.yml` автоматически читает `.env`, поднимает PostgreSQL и запускает bot container. В compose `DATABASE_URL` собирается автоматически с host `postgres`; не используйте `localhost`, потому что внутри bot container это будет сам bot container, а не база. Для запуска без Docker:

```bash
DATABASE_URL='postgres://timetable:timetable@localhost:5432/timetable?sslmode=disable' go run ./cmd/time-table-bot
```

## Переменные

- `TELEGRAM_BOT_TOKEN` обязательный токен Telegram bot.
- `DATABASE_URL` строка подключения PostgreSQL.
- `SUPER_ADMIN_USERNAME` по умолчанию `tim1106`.
- `TIMEZONE` по умолчанию `Europe/Nicosia`.
- `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_PORT` используются локальным compose.

## Роли

- `super_admin`: по умолчанию `@tim1106`, может делать всё, включая назначение админов.
- `admin`: мастер, управляет профилем, услугами, графиком, слотами и записями.
- `user`: смотрит расписание, записывается, меняет язык, просит перенос записи.

## Команды

- Для клиента: `/schedule` показывает свободные места с номерами, `/book 3` записывает на номер 3, `/my` показывает свои записи, `/move 1 3` переносит запись 1 на свободное место 3, `/lang ru|en` меняет язык, `/settravel 30` задает время в пути.
- Для админа: `/sethours пн-пт 10:00-18:00`, `/setduration 60`, `/generate 2026-06`, `/appoint @username YYYY-MM-DD HH:MM`, `/cancel @username YYYY-MM-DD HH:MM`, `/reschedule @username from_date from_time to_date to_time`, `/block YYYY-MM-DD HH:MM`.
- Super admin: `/admin_add @username`, `/admin_remove @username`, `/role @username [user|admin|super_admin]`.

Длинные форматы тоже поддерживаются как fallback: `/schedule 2026-06`, `/book YYYY-MM-DD HH:MM`, `/move FROM_DATE FROM_TIME TO_DATE TO_TIME`. Обычный сценарий для не-IT пользователя: открыть `/schedule`, выбрать номер, отправить `/book 3`.

`/generate 2026-06` создает слоты на месяц по настройкам из `/sethours` и `/setduration`. Повторный запуск идемпотентный: существующие слоты пропускаются.

## Напоминания

Scheduler раз в минуту готовит и отправляет напоминания:

- за день до записи пользователю и админу;
- в день записи пользователю за `travel_minutes + 10` минут до начала;
- если время в пути не задано, используется 30 минут.

Сообщения bot и reminder отправляются на выбранном языке пользователя или админа: `ru` или `en`.

## Kubernetes / Helm

Chart находится в `deploy/helm/time-table-bot` и повторяет паттерн `cluster-cfg/repeater`: bot `Deployment`, PostgreSQL `StatefulSet`, `Service`, опциональный hostPath PV и secrets. CI публикует multi-arch image `linux/amd64` и `linux/arm64`, поэтому один и тот же tag подходит для обычного сервера и Raspberry Pi.

```bash
kubectl create secret generic time-table-bot-secrets \
  --from-literal=telegram-bot-token='<telegram-token>' \
  --from-literal=postgres-password='<postgres-password>'

helm lint deploy/helm/time-table-bot
helm template ttb deploy/helm/time-table-bot
helm install ttb deploy/helm/time-table-bot --namespace time-table-bot --create-namespace
```

Кластер сейчас может быть недоступен, поэтому безопасная локальная проверка деплоя: `helm template`.

## Тесты

```bash
GOCACHE=/tmp/go-build go test ./...
GOCACHE=/tmp/go-build go test -tags=integration ./internal/store
helm lint deploy/helm/time-table-bot
docker compose --env-file .env.example config
```

Integration tests используют Testcontainers и поднимают реальный `postgres:16-alpine`, поэтому нужен доступ к Docker socket.

## Multi-Arch Docker

Dockerfile не фиксирует `amd64`: при обычном `docker compose build` собирается нативная архитектура машины, а через Buildx можно собрать обе платформы:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t t1mon1106/time-table-bot:latest .
```

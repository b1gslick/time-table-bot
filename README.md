# time-table-bot

Telegram-бот на Go + PostgreSQL для записи клиентов к мастеру и отправки напоминаний.

## Запуск

1. Скопируйте переменные окружения:
   - `cp .env.example .env`
2. Заполните `TELEGRAM_BOT_TOKEN`.
3. Убедитесь, что PostgreSQL доступен по `DATABASE_URL`.
4. Запустите:

```bash
go mod tidy
go run ./cmd/time-table-bot
```

Бот использует long polling Telegram API, PostgreSQL и встроенный scheduler.

## Переменные окружения

- `TELEGRAM_BOT_TOKEN` (обязательная)
- `DATABASE_URL` (обязательная, пример в `.env.example`)
- `SUPER_ADMIN_USERNAME` (по умолчанию `tim1106`)
- `TIMEZONE` (по умолчанию `Europe/Nicosia`)

## Роли

- `user`:
  - регистрируется автоматически при первом сообщении в бота;
  - получает напоминания о своей записи.
- `super_admin`:
  - создается при первом старте (bootstrap) по `SUPER_ADMIN_USERNAME`.
- `admin`:
  - предусмотрен схемой записей (поле `admin_id`) для привязки мастера к записи.

## Напоминания

Scheduler запускается в `main` и срабатывает раз в минуту:

- за день до записи:
  - отправка `user`;
  - отправка `admin` (если у записи есть `admin_id` и `chat_id` мастера);
- в день записи:
  - `user` получает напоминание за `travel_minutes + 10` минут до `start_at`;
  - если `travel_minutes` не задано, используется `30`.

## Команды

Сейчас реализована минимальная команда:

- `/start` — регистрация пользователя и ответ, что бот активен.

## Kubernetes / Helm

- Для локального запуска достаточно `DATABASE_URL` на локальную/удаленную Postgres.
- Для кластера Kubernetes используйте `DATABASE_URL` на сервис БД внутри namespace.
- По аналогии с `/home/timon/projects/lang-repeater` и `/home/timon/projects/cluster-cfg/repeater` предполагается деплой через Helm.
- Если появятся чарты, ожидаемый путь: `deploy/helm/time-table-bot`.

## Ограничения и Roadmap

Текущие ограничения:

- нет полноценного command router для создания/редактирования записей;
- интеграция с уже существующими пакетами `internal/store`, `internal/bot`, `internal/telegram` требует отдельной стыковки в следующих итерациях;
- нет ретраев и backoff для отправки напоминаний.

Roadmap:

- вынести SQL-store, бот-логику и Telegram transport в отдельные пакеты;
- добавить команды управления расписанием (создать/перенести/отменить запись);
- добавить ACL по ролям и панели администрирования;
- покрыть scheduler и store интеграционными тестами.

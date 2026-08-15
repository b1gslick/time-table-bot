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
- `QWEN_API_KEY` или `DASHSCOPE_API_KEY` включает LLM-разбор обычного текста клиента через Qwen Cloud. Без ключа бот работает в прежнем пошаговом режиме.
- `QWEN_BASE_URL` опционально задает OpenAI-compatible endpoint Qwen Cloud под нужный регион. По умолчанию `https://dashscope.aliyuncs.com/compatible-mode/v1`.
- `QWEN_MODEL` по умолчанию `qwen-plus`.
- `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_PORT` используются локальным compose.

## Роли

- `super_admin`: по умолчанию `@tim1106`, может делать всё, включая назначение админов.
- `admin`: мастер, управляет профилем, услугами, графиком, слотами и записями.
- `user`: смотрит расписание, записывается, меняет язык, просит перенос записи.

## UX и Команды

- Подробная инструкция для админа: [docs/admin-guide.md](docs/admin-guide.md).

- Для клиента: reply-клавиатура максимально простая: `Начать запись` и `/help`. Если включен Qwen Cloud, клиент может писать обычным текстом: `хочу эпиляцию на полтора часа завтра вечером` или `можно на брови на следующей неделе`, а бот извлечет услугу, дату/период и покажет свободные варианты кнопками. Кнопка `Начать запись` запускает тот же сценарий, что и `/start`: язык, представление мастера, категория, подкатегория, одна или несколько услуг, ближайшее время или конкретные даты, выбор дня, части дня и свободного времени. Команды `/services`, `/schedule 1 4`, `/book 3`, `/my`, `/move 1 3`, `/month 2026-08`, `/lang ru|en` остаются как ручной режим для тех, кому он нужен.
- Для админа: после `/start` доступны кнопки-разделы `Календарь`, `Записи`, `Услуги`, `Расписание`, `Настройки`. В `Календарь` есть кнопка `Неделя`: бот отправляет PNG-обзор недели с цветными слотами, где зеленый - свободно, красный - занято, желтый - частично свободно, серый - закрыто. Кнопки, которым нужны данные, запускают пошаговый ввод, например добавить или удалить услугу, задать часы, создать или удалить месяц расписания, записать клиента, удалить или перенести запись.
- Super admin: дополнительно видит раздел `Админы`. Через него можно назначить или снять админа и посмотреть или изменить роль. Команды `/admin_add`, `/admin_remove`, `/role` без параметров тоже запускают пошаговый ввод.

Командные форматы поддерживаются как fallback: `/week`, `/week 2026-06-15`, `/week next`, `/schedule 2026-06`, `/service_add 30 Категория > Подкатегория > Название`, `/service_delete 2`, `/generate 2026-06 2`, `/generate 2026-06 чт 10:00-18:00 30`, `/generate 2026-06-15 10:00-18:00 30`, `/calendar_delete 2026-06`, `/appoint @username YYYY-MM-DD HH:MM`, `/book YYYY-MM-DD HH:MM`, `/move FROM_DATE FROM_TIME TO_DATE TO_TIME`.

`/generate 2026-06` создает базовые ячейки на месяц по настройкам из `/sethours` и `/setduration`. `/generate 2026-06 чт 10:00-18:00 30` одноразово добавляет слоты на все четверги только в июне 2026. Для услуг разной длительности лучше ставить базовый шаг `15` или `30` минут: клиент выбирает услуги через `/services`, отправляет `/schedule 1 4`, а бот показывает только интервалы, где подряд хватает свободных ячеек на суммарную длительность услуг.

## Напоминания

Scheduler раз в минуту готовит и отправляет напоминания:

- за день до записи пользователю и админу;
- за 1 час до записи пользователю.

Сообщения bot и reminder отправляются на выбранном языке пользователя или админа: `ru` или `en`.

## Kubernetes / Helm

Chart находится в `deploy/helm/time-table-bot` и повторяет паттерн `cluster-cfg/repeater`: bot `Deployment`, PostgreSQL `StatefulSet`, `Service`, опциональный hostPath PV и secrets. CI публикует image `linux/arm64` для Raspberry Pi. ArgoCD Application лежит в `deploy/argocd/time-table-bot.yaml`.

```bash
kubectl create secret generic time-table-bot-secrets \
  --from-literal=telegram-bot-token='<telegram-token>' \
  --from-literal=postgres-password='<postgres-password>' \
  --from-literal=qwen-api-key='<qwen-api-key>'

helm lint deploy/helm/time-table-bot
helm template ttb deploy/helm/time-table-bot
helm install ttb deploy/helm/time-table-bot --namespace time-table-bot --create-namespace
```

Для включения Qwen в Helm укажите secret с ключом:

```bash
helm upgrade --install ttb deploy/helm/time-table-bot \
  --namespace time-table-bot --create-namespace \
  --set env.qwen.apiKey.secretName=time-table-bot-secrets \
  --set env.qwen.apiKey.secretKey=qwen-api-key
```

Кластер сейчас может быть недоступен, поэтому безопасная локальная проверка деплоя: `helm template`.

Для ArgoCD:

```bash
kubectl apply -f deploy/argocd/time-table-bot.yaml
```

## Тесты

```bash
GOCACHE=/tmp/go-build go test ./...
GOCACHE=/tmp/go-build go test -tags=integration ./internal/store
GOCACHE=/tmp/go-build go test -tags=integration ./cmd/time-table-bot
helm lint deploy/helm/time-table-bot
docker compose --env-file .env.example config
```

Integration tests используют Testcontainers и поднимают реальный `postgres:16-alpine`, поэтому нужен доступ к Docker socket.

## Docker для Raspberry Pi

Dockerfile не фиксирует `amd64`: при обычном `docker compose build` собирается нативная архитектура машины, а через Buildx CI собирает `linux/arm64` для Raspberry Pi:

```bash
docker buildx build --platform linux/arm64 -t t1mon1106/time-table-bot:latest .
```

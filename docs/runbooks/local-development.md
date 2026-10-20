# Локальная разработка и диагностика

Все команды выполняются из корня репозитория. Секреты находятся в игнорируемом `.local/.env`; не прикладывайте содержимое файла или полный вывод `docker compose config` к issue.

## Запуск и изменение кода

```sh
make up
make seed
docker compose --env-file .local/.env ps
```

`migrate` и `kafka-init` — одноразовые контейнеры; состояние `Exited (0)` для них нормально. HTTP-процессы работают на внутреннем порту 8080, метрики — на 9090. Снаружи опубликован только Gateway.

После изменения одного сервиса:

```sh
docker compose --env-file .local/.env up -d --build --wait listings
docker compose --env-file .local/.env logs --tail=100 listings
```

Если меняли миграции, сначала выполните `make migrate`, затем обновите сервисы. Миграции встроены в бинарник, поэтому после изменения SQL нужно пересобрать образ `migrate`:

```sh
docker compose --env-file .local/.env build migrate
make migrate
```

Миграции создают схему каждой отдельной базы. Создание самих баз и ролей находится в `deploy/compose/init-postgres.sh` и выполняется при первом запуске пустого тома PostgreSQL. Изменение этого скрипта не меняет уже существующую БД.

## Разбор запроса

Начните с `cmd/<service>/main.go`: он подключает handler и фоновые workers. Для HTTP найдите маршрут в `services/<service>/handler.go` и SQL-операцию; маршрутизация Gateway находится в `services/gateway/router.go`. Для событий проследите цепочку `outbox.Insert` → publisher → `Consumer.Run` → обработчик Search или Notifications (`services/notifications/inbox.go`).

```sh
curl -i http://127.0.0.1:8080/health/ready
docker compose --env-file .local/.env logs --since=5m gateway listings search
docker compose --env-file .local/.env exec -T listings wget -qO- http://127.0.0.1:9090/metrics
```

Для HTTP-ошибки найдите `X-Request-ID` в ответе и соответствующий JSON-лог. При включённом OTel можно перейти к trace в Jaeger по `trace_id`. SQL spans не содержат текст запросов с пользовательскими аргументами.

Только для локальной диагностики можно подключиться от имени владельца базы без вывода пароля:

```sh
docker compose --env-file .local/.env exec postgres psql -U listings -d listings
```

Полезные запросы:

```sql
SELECT count(*) AS pending, min(created_at) AS oldest
FROM outbox WHERE sent_at IS NULL;
```

Для Notifications откройте `psql -U notifications -d notifications`:

```sql
SELECT state, count(*) FROM jobs GROUP BY state;
SELECT id, attempts, next_attempt, last_error FROM jobs WHERE state='failed';
```

Прямой SQL здесь — инструмент администратора. Сервисный код не должен подключаться к чужой базе.

## Наблюдаемость

```sh
make observability-up
```

Откройте Grafana на `http://127.0.0.1:13000`, Jaeger на `http://127.0.0.1:16686` и Prometheus на `http://127.0.0.1:19090`. После включения сделайте несколько новых запросов: старые traces задним числом не появятся.

Основные сигналы:

| Симптом | Что проверить |
|---|---|
| Изменение карточки видно, а в поиске старые данные | `marketplace_outbox_pending`, возраст outbox, `marketplace_consumer_lag`, логи Search |
| Сообщение есть, письма нет | outbox Messaging, lag Notifications, `marketplace_notification_jobs`, доступность Accounts/SMTP |
| API возвращает 502/504 | состояние целевого сервиса, БД, длительность upstream-запросов |
| API возвращает 429 | лимит Gateway; дождитесь `Retry-After` |
| Фото не открывается | адрес `S3_PUBLIC_ENDPOINT`, доступность storage, права и статус объявления |

Правила Prometheus — примеры для стенда. Alertmanager и доставка уведомлений оператору не настроены. Grafana доступна анонимно с ролью Viewer только через локально опубликованный порт. Историю Jaeger не следует считать долговременным хранилищем.

## Тесты и локальные инструменты

`make test` работает без внешних зависимостей. Интеграционные тесты используют настоящие PostgreSQL/Kafka/S3; стенд запускается через `make up`. Smoke и браузерные тесты проверяют сервисы через Gateway. Тесты создают служебных пользователей и объявления, поэтому каталог после них может содержать дополнительные данные.

`make test-resilience` преднамеренно останавливает Kafka и Search проекта `marketplace`. Не запускайте его одновременно с демонстрацией приложения или другими тестами, которым нужен доступный поиск.

Для ручных Go-команд можно сохранить кеши внутри проекта:

```sh
export GOCACHE="$PWD/.local/go-cache"
export GOMODCACHE="$PWD/.local/go-mod"
go test ./services/listings ./internal/platform/kafka
```

## Остановка и очистка

`make down` сохраняет тома PostgreSQL, Kafka и S3. Если наблюдаемость включена, используйте оба Compose-файла:

```sh
docker compose --env-file .local/.env -f compose.yaml -f compose.observability.yaml down
```

Следующая команда **удаляет данные этого локального стенда**, включая аккаунты, объявления, сообщения, файлы, историю Kafka и Prometheus. Выполняйте её только если нужен полностью чистый учебный запуск:

```sh
docker compose --env-file .local/.env -f compose.yaml -f compose.observability.yaml down -v
make up
make seed
```

`.local/.env` при этом сохраняется. Не заменяйте его случайными новыми credentials при сохранённых томах: пароли ролей PostgreSQL и ключи S3 в данных могут перестать совпадать с конфигурацией. Не используйте глобальный `docker system prune` для очистки только этого проекта.

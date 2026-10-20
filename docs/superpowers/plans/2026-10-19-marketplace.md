# Go Marketplace Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Реализовать согласованный учебный аналог Авито с Go-микросервисами, Kafka и воспроизводимым локальным развёртыванием.

**Architecture:** Один репозиторий и Go module, шесть независимо запускаемых компонентов. Данные принадлежат сервисам; HTTP используется для запросов, Kafka и transactional outbox — для событий. Три последовательных этапа дают проверяемые результаты: HTTP API аккаунтов/объявлений; событийные сценарии; интерфейс и эксплуатация.

**Tech Stack:** Go, стандартный `net/http`, `pgx/v5`, `franz-go`, `golang-jwt/jwt/v5`, `x/crypto/bcrypt`, AWS SDK for Go v2 для S3, `golang-migrate/migrate/v4`, `slog`, OpenTelemetry, Prometheus, PostgreSQL, Kafka KRaft, S3-совместимое хранилище, Mailpit, Docker Compose, Helm, GitHub Actions. Интерфейс: HTML/CSS и JavaScript modules без сборщика, встроенные в Gateway через `embed`.

**Spec:** [Согласованная спецификация](../specs/2026-10-19-marketplace-design.md). Читать вместе с планом. Спецификация описывает продукт; этот документ фиксирует детали реализации и проверки.

## Итог исполнения — 20 октября 2026

Реализованы все двенадцать функциональных задач. Приложение работает в Docker Compose и отдельном kind-кластере; фактические проверки и ограничения собраны в [отчёте](../../verification.md). Исходная последовательность ниже сохранена как проектный документ: её пункты не являются журналом выполненных команд. Имена файлов, группировка тестов и границы коммитов уточнялись при реализации.

| Задачи | Результат и проверка |
|---|---|
| 1 — основа | Конфигурация, lifecycle, миграции, генерация секретов, Compose; unit/race, запуск стенда |
| 2 — аккаунты | Регистрация, профиль, JWT, одноразовый refresh/logout; accounts integration и браузерный параллельный refresh |
| 3–4 — объявления и фото | Версии, права владельца, переходы, private S3; listings/media integration и загрузка изображения в браузере |
| 5 — доставка событий | Kafka/outbox, DLQ, commit после обработки; transport/outbox integration и остановка брокера |
| 6 — поиск | PostgreSQL-проекция, фильтры, версии и reindex; search/reindex integration, проверка неполных snapshot |
| 7–8 — сообщения и письма | Участники, идемпотентность, inbox, аренда заданий и retry; messaging/notification integration, HTTP smoke с письмом |
| 9–10 — Gateway и интерфейс | Allowlist, rate limit, единые ошибки, формы и чат; gateway tests, три Chromium-сценария в Compose и kind |
| 11 — наблюдаемость и восстановление | slog, HTTP/SQL/Kafka traces, метрики и dashboard; проверены Jaeger/Prometheus/Grafana и сценарий восстановления |
| 12 — эксплуатация | Образы, Helm, kind, CI workflow, runbooks и упражнения; schema validation и реальный Kubernetes smoke |

Уточнения реализации:

- Использован выделенный исходный каталог и существующая ветка `feat/marketplace`; удалённого репозитория и основной ветки для слияния нет. Результат остаётся локально.
- Связанные проверки объединены в сценарные тесты; не заявляется отдельный red/green-прогон каждого первоначально предложенного имени теста. Коммиты объединяют связанные задачи.
- Конструкторы сервисов — `New(db, config)`; небольшие обработчики и модели сгруппированы без лишних слоёв. Представления браузера находятся в `app.js`, решения по архитектуре — в `docs/architecture/overview.md`.
- Reindex сам активирует новую проекцию после snapshot, проверки retention и достижения Kafka-барьера; при ошибке сохраняет предыдущую. Отдельного ручного переключателя нет.
- RustFS служит локальным S3; PostgreSQL на хосте использует 15432, чтобы не занимать порт другого приложения. Публичные адреса явно используют `127.0.0.1`.
- Независимое финальное ревью проверило права, транзакции, outbox/offsets, reindex, UI и развёртывание. Найденные ошибки проверены регрессионными тестами; исправление ожидания миграций сверено с реальными именами Helm Jobs.
- Удалённый CI, облачный rollout, нагрузка, потеря узла и восстановление резервных копий не проверялись. Это учебный проект, а не аттестация для публичной эксплуатации.

## Global Constraints

- «В первой версии используется один Go module»; путь модуля `example.com/marketplace`, пока нет адреса удалённого репозитория.
- «Межсервисные SQL-запросы, общие таблицы и внешние ключи между базами запрещены».
- «Kafka работает в режиме KRaft»; локально один брокер, три партиции на основной топик, retention `604800000` мс.
- Топики `marketplace.listings.v1`, `marketplace.messages.v1`; группы `search-v1`, `notifications-v1`.
- «Физическое удаление объявлений не входит в первую версию».
- «Email и токены не попадают в события и логи»; текст переписки также не включается в события и логи.
- «Переписка использует HTTP и периодическое обновление списка сообщений»; интервал UI — 5 секунд только для открытого диалога.
- «Секреты не включаются в репозиторий»; локальные секреты генерируются командой настройки в игнорируемый каталог `.local/`.
- «Реальное применение в Kubernetes отмечается как проверенное только после выполнения в доступном кластере».
- Весь пользовательский текст и учебные инструкции — по-русски. Цена хранится целым числом копеек, валюта первой версии — RUB.
- Версии зависимостей и образов выбираются по официальным источникам при выполнении задачи 1, фиксируются в lock-файлах/Compose; теги `latest` не используются. Проверяется наличие образов для arm64 и amd64.

## Review Focus

1. Одновременное обновление сессии: один refresh token не создаёт две действующие дочерние сессии; UI объединяет параллельные refresh-запросы. Проверки задачи 2.
2. Чужой ID в URL и поддельный заголовок пользователя: не раскрывают черновик, email или переписку и не дают изменить объявление. Проверки задач 3, 7, 9.
3. Событие старой версии после снятия с продажи и во время перестроения поиска: объявление не возвращается в выдачу. Проверки задачи 6.
4. Обрыв после принятия события брокером или после SMTP-отправки: дубликат события безопасен, неопределённый результат email явно отражён в документации. Проверки задач 5, 8.
5. Сохранённый HTML в названии/сообщении и изображение с поддельным MIME: браузер не исполняет введённый код, загрузка не принимает подмену. Проверки задач 4, 10.

## Структура файлов и общие соглашения

```text
cmd/{gateway,accounts,listings,search,messaging,notifications}/main.go
cmd/{migrate,devsetup,seed,replay,reindex}/main.go
internal/platform/{config,httpx,auth,telemetry,kafka,outbox}/
services/{accounts,listings,search,messaging,notifications}/
services/<name>/{service,repository,handler,models}.go
services/<name>/migrations/*.sql
services/gateway/{router,proxy,web}.go
services/gateway/web/{index.html,app.js,api.js,views.js,styles.css}
api/openapi.yaml
api/events/{envelope,listing,message}.schema.json
tests/{integration,smoke,browser}/
deploy/{compose,observability,helm/marketplace,kind}/
docs/{architecture,runbooks,exercises}/
.github/workflows/ci.yml
Dockerfile, compose.yaml, Makefile, .env.example, .gitignore, README.md
```

Каталоги создаются в задаче, которой они нужны. Общая типовая сигнатура HTTP-сервиса: `NewHandler(svc *Service, verifier *auth.Verifier) http.Handler`; конкретный сервис определяет собственные модели. SQL хранится в `repository.go`, а не прячется за универсальный repository framework.

`httpx.Error` содержит `Code string`, `Message string`, `Fields map[string]string`; HTTP-ответ добавляет `request_id`. Все ID — UUID; API использует UTC RFC3339. Списки имеют `limit` (по умолчанию 20, максимум 100) и непрозрачный cursor; поиск может использовать page (максимум 1000). Некорректные параметры дают 400, конфликт версии/идемпотентности — 409. Публичные маршруты начинаются с `/api/v1`, служебные — с `/internal/v1`.

Тесты бизнес-правил не требуют инфраструктуры. Интеграционные тесты имеют build tag `integration`, используют реальные зависимости Compose, уникальные данные и отдельные тестовые базы/топики. Команды ниже выполняются из корня. Каждый кодовый шаг следует циклу: написать указанный тест → увидеть ожидаемое падение → реализовать → получить PASS. Коммиты создаются после зелёных проверок соответствующей задачи; исходный каталог пока не является Git-репозиторием, его инициализация относится к началу исполнения.

## Этап 1 — аккаунты и объявления через HTTP

### Task 1: Запускаемый сервис и локальные зависимости

**Files:** `go.mod`, `go.sum`, `.gitignore`, `.env.example`, `Makefile`, `compose.yaml`, `Dockerfile`, `cmd/accounts/main.go`, `cmd/devsetup/main.go`, `cmd/migrate/main.go`, `internal/platform/config/config.go`, `internal/platform/httpx/server.go`, `internal/platform/httpx/server_test.go`, `deploy/compose/init-postgres.sh`, `deploy/compose/init-kafka.sh`, `README.md`.

**Interfaces:** `config.Load(service string) (Config, error)`; `httpx.Run(ctx context.Context, addr string, handler http.Handler) error`; команды `make setup`, `make infra-up`, `make migrate`, `make test`, `make test-integration`. `devsetup` генерирует локальные пароли, ключи Ed25519 и служебные токены, не печатая их; повторный запуск сохраняет существующие значения.

- Написать `TestRunGracefulShutdown`, `TestConfigMissingSecret` и `TestDevSetupPreservesSecrets`: незавершённый запрос успевает завершиться в shutdown timeout, отсутствие секрета даёт понятную ошибку, повторная настройка не меняет ключи.
- Запустить `go test ./internal/platform/... ./cmd/devsetup/...`; зафиксировать ожидаемое падение до реализации.
- Реализовать bootstrap, shutdown timeout 15 секунд и раздельные HTTP read-header/write/idle timeouts. Ввести локальные зависимости PostgreSQL, официальный Kafka KRaft, Mailpit и S3-совместимый сервер с проверенной arm64-сборкой; решение о конкретном S3-сервере записать в ADR. Все вспомогательные порты привязать к `127.0.0.1`.
- Добавить health endpoints, мигратор с флагом `--service` и инициализацию пяти баз/ролей и Kafka-топиков. Локальная конфигурация не использует производственные учётные данные. `make setup` и `make infra-up` документировать.
- Проверить `go test ./internal/platform/... ./cmd/devsetup/...`, `docker compose config --quiet` и доступность зависимостей после `make infra-up`. Отсутствующий Docker Engine сначала запустить разрешённым способом; невозможность запуска описать как ограничение проверки.
- Инициализировать Git при его отсутствии, проверить `.gitignore`, сохранить документацию и основу: `chore: bootstrap marketplace development environment`.

### Task 2: Аккаунты, токены и профиль

**Files:** `services/accounts/service.go`, `repository.go`, `handler.go`, `models.go`, `password.go`, `service_test.go`, `migrations/000001_accounts.up.sql`, `migrations/000001_accounts.down.sql`; `internal/platform/auth/tokens.go`, `tokens_test.go`; `tests/integration/accounts_test.go`; `api/openapi.yaml`.

**Interfaces:** `auth.NewVerifier(publicKey ed25519.PublicKey, issuer, audience string) *Verifier`; `(*Verifier).Verify(raw string) (Claims, error)`, `Claims{UserID string}`. HTTP: `POST /auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout`; `GET/PATCH /me`, `GET /users/{id}` с префиксом `/api/v1`. `GET /internal/v1/users/{id}/notification-address` требует токен Notifications.

- Написать `TestRegisterAndLogin`, `TestPublicProfileOmitsEmail`, `TestRefreshSingleUse`, `TestConcurrentRefresh`, `TestLogoutRevokesRefresh`, `TestWrongJWTAlgorithm`, `TestRefreshRejectsForeignOrigin`. Проверять 201/200 для успеха, 401 для неверных credentials/повторного refresh, 403 для Origin, отсутствие email в публичном JSON, ровно один успешный конкурентный refresh.
- Запустить `go test ./services/accounts/... ./internal/platform/auth/...` и после миграций `go test -tags=integration ./tests/integration -run 'TestAccounts'`; увидеть падение новых сценариев.
- Реализовать bcrypt (cost 12; пароль 12–72 байта), нормализацию email, generic ошибку входа, подпись EdDSA с проверкой алгоритма/issuer/audience/expiry. Access TTL — 15 минут, refresh TTL — 30 дней, refresh — случайные 32 байта, хранить SHA-256. Ротация и отзыв — атомарные SQL-операции. Продакшен cookie Secure/HttpOnly/SameSite=Lax; локальная настройка HTTP явно отдельная.
- Зафиксировать HTTP-контракты, проверить unit/integration команды до PASS и выполнить миграцию чистой базы.
- Коммит: `feat: add accounts sessions and profiles`.

### Task 3: Жизненный цикл объявлений и права владельца

**Files:** `cmd/listings/main.go`; `services/listings/models.go`, `service.go`, `repository.go`, `handler.go`, `service_test.go`, `migrations/000001_listings.up.sql`, `migrations/000001_listings.down.sql`; `tests/integration/listings_test.go`; `api/openapi.yaml`.

**Interfaces:** `Listing{ID, OwnerID, Title, Description, CategoryID, Status string; PriceMinor, Version int64}`; `ValidateDraft(input DraftInput) error`; `CanTransition(from, to string) bool`. HTTP: `GET /categories`, `POST /listings`, `GET/PATCH /listings/{id}`, `POST /listings/{id}/publish`, `/withdraw`, `GET /me/listings`. Изменения требуют `expected_version`; внутренняя версия начинается с 1.

- Написать таблицу `TestCanTransition`: `draft→published=true`, `published→withdrawn=true`, `withdrawn→published=true`, `published→draft=false`. Добавить `TestDraftPrivate`, `TestOtherOwnerCannotEdit`, `TestOptimisticConflict`, `TestPriceUsesInteger`, `TestPublishedValidation`.
- Запустить unit и `go test -tags=integration ./tests/integration -run 'TestListings'`; получить ожидаемое падение.
- Реализовать ограничения: название 1–120 Unicode-символов, описание до 10000, цена 0–100000000000 копеек. Черновик допускает незаполненные поля; публикация требует название, описание, существующую категорию и хотя бы одно фото после задачи 4. Неавторизованный доступ к черновику возвращает 404; изменение чужого объекта — 403. Снятая карточка публична со статусом, новые диалоги по ней запрещены.
- Реализовать транзакционные изменения с увеличением version и подготовить сервисную точку экспорта `GET /internal/v1/listings/export?after_id=...&limit=...` (только опубликованные/снятые объявления, с версиями). Добавить служебное чтение ID владельца и статуса для Messaging; доступ по токенам конкретных сервисов.
- Проверить unit/integration до PASS. Коммит: `feat: add listing lifecycle and ownership checks`.

### Task 4: Фотографии и безопасная очистка объектов

**Files:** `services/listings/media.go`, `media_test.go`, `migrations/000002_media.up.sql`, `migrations/000002_media.down.sql`; `tests/integration/media_test.go`; изменения Listings handler, Compose, OpenAPI.

**Interfaces:** `ObjectStore.Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error`, `Delete(ctx context.Context, key string) error`, `PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)`; `POST /listings/{id}/photos`, `DELETE /listings/{id}/photos/{photo_id}`. S3 endpoint для браузера отделён от внутреннего endpoint контейнеров.

- Написать `TestMediaRejectsSpoofedMIME`, `TestMediaSizeLimit`, `TestOtherOwnerCannotAttach`, `TestPublishedListingKeepsPhoto`, `TestCleanupSkipsInFlightUpload`; лимит 5 MiB, не более 8 фото, только декодируемые JPEG/PNG, не более 20 млн пикселей.
- Убедиться в падении `go test ./services/listings/...` и соответствующих integration-тестов до реализации.
- Реализовать предварительную запись загрузки в PostgreSQL, случайный object key и загрузку в private bucket, затем привязку фото. Имя файла пользователя не используется как путь; SVG/HTML отклоняются. URL чтения выдаются с TTL 15 минут после проверки доступа к карточке; долгоживущие URL не сохраняются в событиях.
- Добавить worker очистки записей загрузок старше часа с lease: незавершённая загрузка не может быть привязана после захвата очисткой. Ошибки удаления повторяются. Запретить удаление последнего фото опубликованного объявления.
- Проверить тесты и получение фотографии из браузерного адреса хранилища. Коммит: `feat: add listing photos and orphan cleanup`.

## Этап 2 — события, поиск и сообщения

### Task 5: Transactional outbox и Kafka transport

**Files:** `internal/platform/kafka/envelope.go`, `producer.go`, `consumer.go`, `dlq.go`; `internal/platform/outbox/publisher.go`; `services/listings/events.go`, `migrations/000003_outbox.up.sql`, `migrations/000003_outbox.down.sql`; `api/events/envelope.schema.json`, `listing.schema.json`; `tests/integration/outbox_test.go`; `cmd/replay/main.go`.

**Interfaces:** `Envelope{EventID, EventType string; SchemaVersion int; OccurredAt time.Time; AggregateID string; AggregateVersion int64; Payload json.RawMessage}`; `Producer.Publish(ctx context.Context, topic, key string, event Envelope) error`; `Consumer.Run(ctx context.Context, handle func(context.Context, Envelope) error) error`; `outbox.Run(ctx context.Context, db *pgxpool.Pool, producer *kafka.Producer, topic string) error`. `kafka.PermanentError` обозначает ошибку контракта; временные ошибки не считаются permanent.

- Написать `TestListingAndOutboxAtomic`, `TestPublishAcceptedThenDBFailure`, `TestKafkaUnavailableKeepsOutbox`, `TestPublisherLockExclusive`, `TestDLQFailureDoesNotCommitOffset`. Проверять rollback обоих изменений, повторный event_id после сбоя и отсутствие продвижения offset до устойчивого результата.
- Запустить `go test -tags=integration ./tests/integration -run 'TestOutbox|TestKafka|TestDLQ|TestPublisher'`; получить ожидаемое падение.
- Реализовать публикацию с `acks=all`, idempotent producer и конечным timeout. Удерживать advisory lock на выделенном DB connection; потеря соединения завершает publisher. Событие записывается в бизнес-транзакции после блокировки агрегата; неотправленные записи идут по sequence ID, ошибка останавливает продвижение. Черновики не попадают в публичный топик; опубликованные обновления и снятие передают полный публичный снимок без персональных данных.
- Реализовать consumer без auto commit: последовательная обработка партиции и commit только после успеха. Permanent error публикуется в `<source-topic>.<consumer-group>.dlq` с source topic/partition/offset; raw payload считается чувствительным, доступ ограничивается. Backoff временных ошибок — от 1 до 30 секунд с jitter. Replay CLI принимает конкретные topic/partition/offset и публикует выбранную запись обратно с сохранением event_id.
- Проверить тесты до PASS, включая перезапуск publisher; коммит `feat: add Kafka events and transactional outbox`.

### Task 6: Поиск и перестроение проекции

**Files:** `cmd/search/main.go`, `cmd/reindex/main.go`; `services/search/models.go`, `projection.go`, `repository.go`, `handler.go`, `reindex.go`, `projection_test.go`, `migrations/000001_search.up.sql`, `migrations/000001_search.down.sql`; `tests/integration/search_test.go`; OpenAPI.

**Interfaces:** `Projection.Apply(ctx context.Context, event kafka.Envelope) error`; `SearchQuery{Text, CategoryID string; MinPriceMinor, MaxPriceMinor *int64; Page, Limit int}`; `GET /api/v1/search`; `reindex --projection <uuid>`, `reindex --activate <uuid>`. Проекции разделяются `projection_id`; читаемый ID хранится в базе Search и переключается атомарно.

- Написать `TestApplyNewerOnly` с последовательностью published v3 → withdrawn v4 → published v3: выдача остаётся пустой, версия равна 4. Добавить `TestSearchFiltersAndPagination`, `TestInvalidPriceRange`, `TestReindexConcurrentWithdrawal`, `TestReindexAbortKeepsOldProjection`.
- Убедиться в падении новых unit/integration-тестов.
- Реализовать полнотекстовый поиск PostgreSQL с русской конфигурацией, параметризованные SQL и стабильную сортировку rank/created_at/id. Версия обновляется через условный UPSERT; URL фото получаются из Listings при открытии карточки, в результатах допускаются отдельные защищённые media-URL через Listings без межсервисного SQL.
- Реализовать перестроение в новый projection_id: зафиксировать начальные offsets, запустить отдельную группу `search-reindex-<uuid>`, загрузить снимки через служебный export, применяя только более новые версии. Перед активацией проверить достижение offsets, снятых после завершения экспорта, и непрерывность истории; при истёкшей истории отменить процедуру. Передать новую проекцию её consumer-группе без сброса offsets; старую остановить после переключения.
- Проверить сценарии до PASS и повторное чтение; коммит `feat: add search projection and safe rebuild`.

### Task 7: Диалоги, сообщения и события

**Files:** `cmd/messaging/main.go`; `services/messaging/models.go`, `service.go`, `repository.go`, `handler.go`, `listings_client.go`, `events.go`, `service_test.go`, `migrations/000001_messaging.up.sql`, `migrations/000001_messaging.down.sql`; `api/events/message.schema.json`; `tests/integration/messaging_test.go`; OpenAPI.

**Interfaces:** `POST /api/v1/conversations` с listing_id; `GET /conversations`, `GET /conversations/{id}/messages`, `POST /conversations/{id}/messages` с `Idempotency-Key`. `ListingReader.GetForConversation(ctx context.Context, listingID string) (ListingRef, error)`; `ListingRef{ID, OwnerID, Title, Status string}`. Все маршруты требуют проверенный JWT.

- Написать `TestOpenConversationReturnsExisting`, `TestConcurrentOpenOneConversation`, `TestNonParticipantCannotReadOrSend`, `TestSelfConversationRejected`, `TestSendIdempotency`, `TestSameKeyDifferentBodyConflict`, `TestWithdrawnListingKeepsExistingConversation`.
- Запустить unit/integration тесты, получить ожидаемые FAIL до реализации.
- Реализовать уникальность `(listing_id,buyer_id,seller_id)`, создавать новый диалог только для опубликованного чужого объявления. Сохранить название объявления как снимок для списка диалогов; имя собеседника читать через публичный профиль Accounts с timeout. Сообщение — 1–4000 Unicode-символов после проверки непустого текста. Повтор ключа с тем же запросом возвращает исходный message ID; с другим — 409.
- Сохранить сообщение и событие одной транзакцией, увеличить версию диалога под блокировкой. Переиспользовать outbox transport задачи 5; в событии только идентификаторы. Пагинация сообщений — по sequence ID; список диалогов сортируется по последнему сообщению и ID.
- Проверить до PASS, включая отдельную проверку отсутствия message body в event payload; коммит `feat: add private conversations and message events`.

### Task 8: Email-уведомления

**Files:** `cmd/notifications/main.go`; `services/notifications/inbox.go`, `worker.go`, `smtp.go`, `accounts_client.go`, `worker_test.go`, `migrations/000001_notifications.up.sql`, `migrations/000001_notifications.down.sql`; `tests/integration/notifications_test.go`.

**Interfaces:** `Inbox.Accept(ctx context.Context, event kafka.Envelope) error`; `Sender.Send(ctx context.Context, to, subject, text string) error`; `Worker.Run(ctx context.Context) error`; `AddressReader.Get(ctx context.Context, userID string) (string, error)` использует служебный токен Accounts.

- Написать `TestDuplicateEventOneJob`, `TestSMTPRetry`, `TestRetryLimit`, `TestExpiredLeaseRecovered`, `TestMailContainsConversationLink`. Один event_id создаёт одно задание; после пяти неудачных попыток состояние `failed`; зависшее задание доступно после истечения lease.
- Запустить unit и integration тесты до ожидаемого падения.
- Реализовать inbox+job в одной транзакции и worker с `FOR UPDATE SKIP LOCKED`, lease и условной записью результата владельцем lease. Задержки повторов — 10 секунд, 1 минута, 5 минут, 30 минут. SMTP timeout 15 секунд, lease 60 секунд. Адрес получать только через Accounts; отсутствие аккаунта — permanent failure. Сбой получения адреса до попытки SMTP тоже учитывается, чтобы задания не зависали бесконечно.
- Письмо содержит нейтральный текст о новом сообщении и ссылку на диалог, без текста сообщения. Проверить получение письма через Mailpit API. Отдельный тест демонстрирует неопределённость после SMTP acceptance до DB update: допускается повтор, но не потеря задания.
- Проверить до PASS; коммит `feat: add asynchronous email notifications`.

## Этап 3 — пользовательский интерфейс и эксплуатация

### Task 9: Gateway и публичный контракт

**Files:** `cmd/gateway/main.go`; `services/gateway/router.go`, `proxy.go`, `router_test.go`; `internal/platform/httpx/errors.go`, `middleware.go`; обновление всех handlers и `api/openapi.yaml`; `tests/integration/gateway_test.go`.

**Interfaces:** `gateway.NewHandler(cfg Config, verifier *auth.Verifier) http.Handler`; явная таблица публичных маршрутов из задач 2–7; внутренние API отсутствуют в маршрутизации. Authorization bearer проверяется Gateway и конечным сервисом.

- Написать `TestGatewayCannotReachInternal`, `TestSpoofedUserHeaderIgnored`, `TestUnknownRoute404`, `TestUpstreamTimeout504`, `TestRateLimit429`, `TestRequestIDPreservedSafely`; запрос `/api/v1/../internal/v1/...` тоже не проходит.
- Убедиться в FAIL, реализовать reverse proxy с явными маршрутами, ограниченным timeout, удалением доверенных внутренних заголовков от клиента и корректной работой streaming upload. Rate limit: 120 запросов/минуту на IP для API, 10/минуту для auth; bounded cache и очистка записей. Forwarded headers принимаются только от настроенного доверенного ingress.
- Добавить единый формат ошибок и проверяемые OpenAPI-контракты всех маршрутов, включая служебные в отдельной секции с service auth. Проверить ответы на негативных кейсах integration-тестами.
- Запустить unit/integration до PASS; коммит `feat: expose marketplace through API gateway`.

### Task 10: Минимальный веб-интерфейс и демоданные

**Files:** `services/gateway/web.go`; `services/gateway/web/index.html`, `app.js`, `api.js`, `views.js`, `styles.css`; `cmd/seed/main.go`; `tests/browser/package.json`, `package-lock.json`, `playwright.config.js`, `marketplace.spec.js`; `tests/smoke/marketplace_test.go`; обновления Makefile/Compose.

**Interfaces:** `api.request(path, options)` — единый browser client с access token в памяти, single-flight refresh и одноразовым повтором запроса; `seed --base-url` использует только публичные API и загрузку фотографий. `make up`, `make seed`, `make smoke`, `make test-browser`.

- Написать Playwright-сценарии: регистрация двух пользователей; создание черновика с фото; публикация; поиск; диалог и ответ; снятие объявления; сохранение текста при HTTP 500; название `<img src=x onerror=...>` отображается текстом; два параллельных 401 вызывают один refresh.
- Запустить тесты и убедиться в ожидаемом отсутствии экранов до реализации.
- Реализовать согласованные экраны на русском, адаптивную верстку, подписи полей и клавиатурную навигацию. Данные вставлять через textContent/DOM, без innerHTML для пользовательского текста. Открытый диалог обновлять раз в 5 секунд; отменять таймер при уходе и запросы при logout. При неуспешном сохранении сохранять ввод; статус публикации объясняет задержку поиска.
- Реализовать idempotent seed с двумя демопользователями, категориями и объявлениями; демопароли доступны только в локальной инструкции/генерируемом файле. Изображения фикстур — локальные с известной лицензией или простые собственные SVG, преобразованные в допустимые PNG; продуктовый upload SVG по-прежнему запрещён.
- Проверить `make smoke` и `make test-browser` до PASS; вручную посмотреть каталог, редактор и диалог на desktop/mobile viewport. Проверить письмо в Mailpit. Коммит: `feat: add marketplace web interface and demo scenario`.

### Task 11: Телеметрия и отказоустойчивость процессов

**Files:** `internal/platform/telemetry/logging.go`, `tracing.go`, `metrics.go`, `telemetry_test.go`; `deploy/observability/otel-collector.yaml`, `prometheus.yaml`, `alerts.yaml`, `grafana/datasources.yaml`, `grafana/marketplace.json`; `tests/integration/resilience_test.go`; изменения bootstrap, middleware, repository и event transport.

**Interfaces:** `telemetry.Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error)`; HTTP middleware, pgx tracer, Kafka propagation. `/metrics` доступен на служебном порту, не проксируется публично. Метрики используют низкую кардинальность: route template/service/status, без user/listing/message ID.

- Написать `TestTraceContextThroughKafka`, `TestLogsExcludeSecrets`, `TestListingsReadyWithoutKafka`, `TestSearchCatchesUpAfterRestart`, `TestOutboxDrainsAfterBrokerRestart`, `TestShutdownDoesNotCommitUnfinishedMessage`.
- Убедиться в падении тестов, добавить структурированные логи и spans HTTP/SQL/events без значений SQL-параметров. Добавить метрики HTTP, outbox age/count, consumer lag по topic/partition/group и notification jobs; остановка экспортера не должна ломать бизнес-запросы.
- Реализовать readiness по роли, liveness, завершение фоновых workers и истечение timeout. Добавить профиль `observability` с Collector, Jaeger, Prometheus и Grafana. Панели показывают p95, ошибки, backlog/lag; alert rules срабатывают при росте очередей/ошибок, без высококардинальных labels.
- Проверить integration сценарии остановки и восстановления зависимостей, наличие одной связанной трассировки и метрик в интерфейсах. Коммит: `feat: instrument services and verify recovery scenarios`.

### Task 12: Контейнеры, Kubernetes, CI и учебная документация

**Files:** `Dockerfile`, `.dockerignore`, `compose.yaml`; `deploy/helm/marketplace/Chart.yaml`, `values.yaml`, `values.schema.json`, `templates/deployments.yaml`, `services.yaml`, `configmaps.yaml`, `ingress.yaml`, `migrations.yaml`, `networkpolicies.yaml`; `deploy/kind/config.yaml`, `README.md`; `.github/workflows/ci.yml`; `docs/architecture/overview.md`, `decisions.md`; `docs/runbooks/local.md`, `deploy.md`, `reindex.md`, `replay.md`, `backups.md`; `docs/exercises/failures.md`; `scripts/check-helm.sh`.

**Interfaces:** `make images`, `make lint`, `make check`, `make helm-check`, `make kind-up`, `make kind-smoke`; CI вызывает те же команды. `make check` включает gofmt check, go vet, unit/race, integration и smoke; UI и Helm проверяются отдельными job.

- Проверить образ каждого сервиса: запускается непривилегированным пользователем, читает env/secrets, корректно обрабатывает SIGTERM. Убедиться, что сборка включает миграции для migrate image и embedded web для Gateway. CI runner для race имеет C toolchain; runtime images не требуют компилятора.
- Реализовать chart с отдельными images и конфигурациями сервисов, requests/limits, probes, securityContext, references на заранее созданные Secrets и служебные NetworkPolicies. Все сервисы используют одну подтверждённую версию image tag; секреты не генерируются Helm-шаблоном. Миграции оформлены отдельным Job/релизным шагом, а не стартом каждой replica.
- Добавить тест рендера Helm: шесть Deployments, корректные probes/resources, отсутствие plain-text secrets, gateway ingress и отсутствие публичных служебных endpoints. Выполнить `helm lint`, `helm template` и Kubernetes schema validation с зафиксированными версиями инструментов. Helm сейчас не обнаружен в PATH; установку проводить на этапе исполнения с нужными разрешениями.
- Подготовить отдельный локальный kind-профиль зависимостей, загрузку локальных образов, настройку Secrets, миграции и smoke. Повторно проверить доступность фото из браузера через адреса kind. Если кластер недоступен, не засчитывать `make kind-smoke` и явно сохранить ограничение в отчёте.
- Создать GitHub Actions без автоматической публикации/деплоя: checkout, Go, зависимости, проверки, container builds, Helm. Зафиксировать actions по SHA. Не публиковать секреты в логи/артефакты; test reports не содержат паролей и токенов.
- Написать README: быстрый старт, команды, карта кода, API, демосценарий, измеренные ресурсы стенда, точные версии, различия local/production. Runbooks покрывают восстановление PostgreSQL/объектов, reindex, DLQ replay, backlog, смену ключей и миграции; упражнения показывают наблюдаемые последствия сбоев и способы восстановления.
- Выполнить финальный `make check`, `make test-browser`, `make images`, `make helm-check`, доступный `make kind-smoke`; сверить результат со сценарием спецификации. Зафиксировать реальные результаты и ограничения. Коммит: `chore: add deployment pipeline and operational documentation`.

## Завершение и ревью

После исполнения всех задач провести отдельное ревью прав доступа, транзакционных границ, offset commits, shutdown и отсутствия секретов. Исправления проверяются тестами, которые воспроизводят найденную проблему. Перед итоговым отчётом перечислить реально выполненные команды и результаты; отдельно отметить проверки, которые среда не позволила выполнить. Создание удалённого репозитория, публикация образов и развёртывание в облаке не входят в текущую задачу.

## Самопроверка плана

- Все пользовательские экраны и сценарии спецификации покрыты задачами 2–4, 7–10.
- Kafka/outbox, версия проекции, DLQ, восстановление и SMTP-ограничения покрыты задачами 5–8 и 11.
- Безопасность контрактов и маршрутизации покрыта задачами 2–4, 7, 9–10; эксплуатация и документация — 11–12.
- Пять условий из Review Focus имеют именованные проверки в соответствующих задачах.
- Реализация завершена; актуальное состояние и проверенные ограничения перечислены в разделе «Итог исполнения» и отчёте проверок.

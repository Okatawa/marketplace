# Развёртывание, обновление и резервное копирование

## Что содержит Helm chart

`deploy/helm/marketplace` разворачивает шесть приложений: Gateway, Accounts, Listings, Search, Messaging и Notifications. У каждого — Deployment, Service, health probes, requests/limits и security context с непривилегированным пользователем. Файловая система контейнера read-only; токен Kubernetes ServiceAccount не монтируется. Опциональны ingress с TLS и входящие NetworkPolicy.

PostgreSQL, Kafka, S3, SMTP и сборщик телеметрии должны уже существовать. Для учебного kind зависимости описаны отдельно в `deploy/kind/dependencies.yaml`; это локальная конфигурация с одной репликой, а не шаблон высокой доступности.

Сетевые политики разрешают HTTP между приложениями release, входящий HTTP от ingress к Gateway и метрики из namespace мониторинга. Они не ограничивают исходящий трафик и не заменяют сервисную авторизацию. Нужен CNI, который действительно применяет NetworkPolicy. Настройте `networkPolicy.ingressNamespace` и `networkPolicy.monitoringNamespace` под свой кластер.

## Подготовить окружение

Используйте неизменяемый тег сборки для всех образов: `<registry>/<service>:<release>`, включая `migrate`. Приложения имеют самостоятельные Dockerfile build arguments `SERVICE`; публикацию в реестр владелец окружения настраивает отдельно.

В собственном values-файле задайте:

- `image.registry`, `image.tag` и при необходимости `imagePullSecrets`;
- адреса Kafka, S3, SMTP и Collector в `config`;
- внешний HTTPS `PUBLIC_URL`, `S3_PUBLIC_ENDPOINT`, host ingress и TLS Secret;
- реплики, ресурсы, namespace ingress/мониторинга;
- существующие Secret для каждого сервиса.

Не кладите пароли и DSN в values. Chart ссылается на Kubernetes Secrets через `envFrom`, но не создаёт их из открытого текста в репозитории.

| Secret по умолчанию | Обязательные поля |
|---|---|
| `marketplace-gateway` | `JWT_PUBLIC_KEY`, `SERVICE_TOKEN` |
| `marketplace-accounts` | предыдущие + `DATABASE_URL`, `JWT_PRIVATE_KEY` |
| `marketplace-listings` | публичный JWT и service token + `DATABASE_URL`, `S3_ACCESS_KEY`, `S3_SECRET_KEY` |
| `marketplace-search` | публичный JWT и service token + `DATABASE_URL` |
| `marketplace-messaging` | публичный JWT и service token + `DATABASE_URL` |
| `marketplace-notifications` | публичный JWT и service token + `DATABASE_URL`; при SMTP-аутентификации `SMTP_USERNAME`, `SMTP_PASSWORD` |

Для Kafka с SASL добавьте `KAFKA_USERNAME` и `KAFKA_PASSWORD` тем сервисам, которые обращаются к брокеру. `KAFKA_TLS=true` использует системные корневые сертификаты и TLS 1.2+. Пример поддерживает SASL/PLAIN; для другого механизма или корпоративного CA потребуется соответствующая настройка кода/образа.

`DATABASE_URL` каждого сервиса должен указывать на собственную базу и роль. Миграции используют тот же Secret; роли должны иметь права на изменение своей схемы. Локальный общий пароль ролей — учебное упрощение. Внешнее окружение должно выдавать разные credentials и исключать доступ к чужим базам.

Production SMTP использует STARTTLS. Настройте сервер, доверенный сертификат и `SMTP_FROM`; Mailpit не является почтовой доставкой реальным пользователям.

## Обновление: миграции перед приложением

Сначала сделайте резервную копию и проверьте совместимость миграции со старой версией сервиса. Укажите **целевой** Kubernetes context явно. Команды ниже — схема процедуры; замените значения на своё окружение:

```sh
export PATH="$PWD/.local/bin:$PATH"
target_context='your-staging-context'
target_namespace='marketplace'
release_id='20261020-01'
values_file='/absolute/path/to/staging-values.yaml'

helm template marketplace deploy/helm/marketplace \
  -f "$values_file" \
  --set workloads.enabled=false \
  --set migrations.enabled=true \
  --set "migrations.runID=$release_id" \
  > .local/migrations.yaml

kubectl --context "$target_context" -n "$target_namespace" apply -f .local/migrations.yaml
for service in accounts listings search messaging notifications; do
  kubectl --context "$target_context" -n "$target_namespace" \
    wait --for=condition=complete "job/marketplace-migrate-$service-$release_id" --timeout=300s
done

helm upgrade --install marketplace deploy/helm/marketplace \
  -f "$values_file" \
  --set migrations.enabled=false \
  --kube-context "$target_context" -n "$target_namespace" \
  --wait --timeout=5m
```

Миграционная фаза создаёт только пять Jobs и использует заранее созданные Secrets. Это отдельный управляемый шаг, а не hook, неявно запускаемый каждой репликой приложения. При ошибке Job изучите его логи и состояние миграций; не продолжайте rollout.

Для изменения схемы используйте expand/contract: сначала совместимое расширение, затем версия приложения, использующая его, и только потом удаление старого поля. `helm rollback` меняет манифесты приложения и **не откатывает базу**. Наличие SQL down-миграции не означает, что её безопасно выполнять над реальными данными.

После rollout проверьте readiness, ошибки, lag и outbox. Выполните сквозной сценарий на тестовых данных или smoke против стенда. Внешняя SMTP-доставка при этом должна быть отделена от реальных адресатов.

## Резервные копии: что именно сохранять

| Данные | Почему нужны |
|---|---|
| Accounts DB | Аккаунты, хеши паролей, сессии |
| Listings DB и S3 objects | Объявления и соответствующие им фотографии |
| Messaging DB | Переписка, ключи идемпотентности, outbox |
| Notifications DB | Inbox и результаты отправок, чтобы не создавать повторные задания |
| Search DB | Можно сохранить для быстрого старта; проекция восстанавливается из Listings |
| Секреты и конфигурация | Доступ к данным, JWT-проверка и адреса зависимостей |
| Kafka и состояние групп | Журнал для продолжения обработки; retention не заменяет backup |

Копии разных БД не образуют автоматически общий снимок. При восстановлении старой Notifications DB с новыми Kafka offsets можно пропустить задания; при сбросе offsets со старым inbox можно повторить письма. `sent_at` в outbox тоже должен соответствовать доступной истории Kafka. Сначала определите общую точку восстановления и ожидаемую потерю/повтор обработки.

Для реальной эксплуатации используйте PostgreSQL base backups + WAL/PITR, версионирование и копии объектов S3, отдельное хранение секретов и отработанную процедуру восстановления. Назначьте RPO/RTO и проверяйте их учебным восстановлением в изолированном окружении.

### Простой локальный снимок PostgreSQL

В учебном стенде проще остановить приложения, чтобы бизнес-данные не менялись между дампами. Команда не останавливает PostgreSQL, Kafka или S3:

```sh
docker compose --env-file .local/.env stop gateway accounts listings search messaging notifications
umask 077
backup_dir=".local/backups/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$backup_dir"
for service in accounts listings search messaging notifications; do
  docker compose --env-file .local/.env exec -T postgres \
    pg_dump -U "$service" -d "$service" -Fc > "$backup_dir/$service.dump"
done
docker compose --env-file .local/.env start accounts listings search messaging notifications gateway
```

Это **дампы БД**, не полный backup стенда: фотографии, Kafka и секреты нужно сохранить отдельно согласованным способом. Не копируйте файлы работающего PostgreSQL или Kafka тома как замену их штатному backup. Для S3 используйте поддерживаемый S3-клиент или механизм резервирования поставщика, сохраняя ключи объектов из Listings.

Проверьте, что каждая команда дампа завершилась успешно, и восстановите копию в отдельные пустые базы. Для каждой базы можно использовать `pg_restore --exit-on-error --no-owner --no-privileges`, заранее создав нужную роль и схему доступа. Не применяйте команды восстановления к рабочему тому для проверки.

### Порядок восстановления

1. Поднимите изолированные зависимости и загрузите секреты; остановите consumers/publishers и внешнюю почтовую отправку.
2. Восстановите базы, объекты S3 и согласованное состояние Kafka. Проверьте версии миграций.
3. Сопоставьте отправленный outbox, имеющиеся события, offsets и inbox. Если Kafka потеряна, Search можно перестроить из Listings; для Notifications нет универсальной безопасной команды «восстановить всё» без решения о повторной отправке.
4. Запустите Accounts/Listings/Messaging, выполните reindex при необходимости, затем включите consumers и отправку писем.
5. Проверьте чтение фото, права на переписку, поиск, новые сообщения, lag и конечные состояния заданий.

Хранить backup на том же диске, что и данные, удобно для упражнения, но недостаточно для защиты от потери машины.

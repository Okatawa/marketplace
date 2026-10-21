# Развёртывание, обновление и резервное копирование

[К оглавлению](../README.md)

Эта инструкция состоит из двух частей: обновление приложения во внешнем Kubernetes и локальная проверка копий PostgreSQL. Для учебного Kubernetes используйте [kind](../../deploy/kind/README.md); для обычной работы — [Compose](local-development.md).

**Команды Compose и резервного копирования выполняются из корня репозитория.** В разделе внешнего Kubernetes сначала нужно подготовить собственное окружение. Домены `example.com`, адрес реестра и имена контекстов в образцах не являются готовым подключением к вашему серверу.

## Короткий словарь

| Термин | Простое объяснение |
|---|---|
| Kubernetes, кластер | Система, которая запускает и перезапускает контейнеры на узлах |
| Context | Именованное подключение `kubectl` к конкретному кластеру и учётной записи |
| Namespace | Область имён внутри кластера, здесь обычно `marketplace` |
| Pod / Deployment | Запущенный экземпляр приложения / правило, сколько таких экземпляров поддерживать |
| Job | Одноразовая работа, например применение миграции |
| Helm chart / values | Шаблоны ресурсов Kubernetes / настройки для подстановки в них |
| Release / rollout | Установленный набор ресурсов Helm / процесс обновления запущенных приложений |
| Secret | Ресурс Kubernetes для чувствительной конфигурации; доступ к нему тоже нужно ограничивать |
| Backup / restore | Резервная копия / восстановление из копии |
| RPO / RTO | Какую потерю последних данных допускаем / за какое время обязуемся восстановить работу |

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

Сначала сделайте резервную копию и проверьте совместимость миграции со старой версией сервиса. Namespace, зависимости и Secrets должны существовать заранее. В собственном values-файле зафиксируйте одинаковый тег версии приложений и инструмента миграции. Перед отправкой в кластер проверьте chart командой `make helm-check` после `make tools` и установки зависимостей `tests/browser`.

Ниже — **шаблон для администратора внешнего окружения**, не команда запуска локального приложения. Сначала заполните четыре переменные в одной сессии терминала. `release_id` нужен уникальный для этой попытки, из строчных латинских букв, цифр и дефисов; он входит в имя migration Job. Например, `20261021-01`.

```sh
target_context=''
target_namespace=''
release_id=''
values_file=''
```

Проверьте выбранный context и настройки в values-файле. Затем выполните блок целиком. Защита в начале остановит выполнение при пустых значениях; `set -e` прервёт дальнейшие шаги после ошибки миграции:

```sh
(
  set -eu
  : "${target_context:?Укажите context целевого кластера}"
  : "${target_namespace:?Укажите namespace}"
  : "${release_id:?Укажите новый ID запуска миграций}"
  : "${values_file:?Укажите путь к собственному values-файлу}"
  test -f "$values_file"
  export PATH="$PWD/.local/bin:$PATH"
  mkdir -p .local

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
)
```

`helm template` только создаёт файл манифестов. `kubectl apply` запускает пять migration Jobs. `wait` ждёт завершения каждого. Только затем `helm upgrade --install` обновляет приложения и ждёт их готовности. Ожидаются успешные сообщения `condition met` для всех пяти Jobs и успешное завершение Helm. Тайм-аут — повод проверить состояние; он не доказывает, что кластер отменил уже выполненные действия.

При ошибке посмотрите состояние и события целевого namespace, затем логи конкретного Job. Выберите имя из фактического вывода `get jobs`, не угадывайте его:

```sh
kubectl --context "$target_context" -n "$target_namespace" get pods,jobs
kubectl --context "$target_context" -n "$target_namespace" get events --sort-by=.lastTimestamp
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

### Локальный снимок PostgreSQL с остановленными приложениями

Условия: Compose уже запущен, все шесть приложений работают, `make migrate` ранее завершился успешно. На время копирования приложение станет недоступным. Не запускайте параллельно тесты, seed, reindex или ручные записи в БД. PostgreSQL, Kafka и S3 остаются запущенными.

Блок ниже запускает отдельный Bash, создаёт уникальную папку с ограниченными правами и делает пять копий. Существующие копии не перезаписываются. В каждой используется формат `-Fc` (custom): его читает `pg_restore`. Приложения запускаются снова и при обычной ошибке дампа. Если процесс терминала или Docker аварийно завершится, используйте команду ручного восстановления запуска ниже.

```sh
bash <<'BASH'
set -euo pipefail
umask 077
mkdir -p .local/backups
backup_dir=$(mktemp -d ".local/backups/postgres-$(date +%Y%m%d-%H%M%S)-XXXXXX")
printf 'Папка копии: %s\n' "$backup_dir"
resume_apps() {
  docker compose --env-file .local/.env start accounts listings search messaging notifications gateway
}
trap resume_apps EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker compose --env-file .local/.env stop gateway accounts listings search messaging notifications
for service in accounts listings search messaging notifications; do
  docker compose --env-file .local/.env exec -T postgres \
    pg_dump -U "$service" -d "$service" -Fc > "$backup_dir/$service.dump.partial"
  test -s "$backup_dir/$service.dump.partial"
  mv "$backup_dir/$service.dump.partial" "$backup_dir/$service.dump"
done
printf 'Созданы все пять дампов: %s\n' "$backup_dir"
BASH
```

Ожидаются пять непустых файлов `accounts.dump`, `listings.dump`, `search.dump`, `messaging.dump`, `notifications.dump` и сообщение о завершении. `.partial` означает незавершённую попытку; такую папку не считайте полной копией. Не считайте наличие файлов доказательством возможности восстановления — проверьте их следующей процедурой.

После копирования проверьте работу:

```sh
docker compose --env-file .local/.env ps -a
curl -fsS http://127.0.0.1:8080/health/ready
```

Если приложения не запущены после прерывания, выполните:

```sh
docker compose --env-file .local/.env start accounts listings search messaging notifications gateway
```

Это **копии пяти БД**, а не всего стенда. `pg_dump` не сохраняет глобальные роли PostgreSQL, файлы S3, историю Kafka или секреты. Остановка приложений уменьшает риск изменения бизнес-данных между дампами, но сама по себе не создаёт общий согласованный снимок PostgreSQL + Kafka + S3. Не копируйте файлы работающего тома PostgreSQL или Kafka вместо штатного backup. Для фотографий нужен поддерживаемый S3-клиент или механизм резервирования поставщика с сохранением ключей объектов из Listings.

### Проверить восстановление, не трогая рабочие данные

Проверка создаёт **отдельный временный PostgreSQL 17.6**. У контейнера нет сети (`--network none`), опубликованных портов и подключений к рабочим томам. Его данные хранятся во временной памяти (`--tmpfs`) и исчезают после удаления. Нужны Docker, достаточная свободная память и образ `postgres:17.6-alpine`, уже используемый Compose; если образа нет локально, Docker попробует скачать его.

Сначала задайте путь **к одной конкретной завершённой папке**, напечатанной при копировании. Не выбирайте автоматически «самую новую»: последняя попытка могла оборваться. Выполните в терминале:

```sh
export RESTORE_BACKUP_DIR=''
```

Вместо пустой строки впишите реальный путь, например путь к вашей папке внутри `.local/backups`. Затем выполните блок целиком. Проверка пустой переменной не позволит начать без выбранной копии:

```sh
bash <<'BASH'
set -euo pipefail
: "${RESTORE_BACKUP_DIR:?Укажите папку с пятью завершёнными дампами}"
for service in accounts listings search messaging notifications; do
  test -s "$RESTORE_BACKUP_DIR/$service.dump"
done

restore_container_id=''
cleanup_restore() {
  if [ -n "$restore_container_id" ]; then
    docker rm -f "$restore_container_id" >/dev/null
  fi
}
trap cleanup_restore EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

restore_container_id=$(docker run --rm -d --network none \
  --tmpfs /var/lib/postgresql/data:rw \
  -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17.6-alpine)
ready=0
for attempt in $(seq 1 60); do
  if docker exec "$restore_container_id" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  docker logs --tail=50 "$restore_container_id"
  exit 1
fi

for service in accounts listings search messaging notifications; do
  docker exec "$restore_container_id" createuser -U postgres "$service"
  docker exec "$restore_container_id" createdb -U postgres -O "$service" "$service"
  docker exec -i "$restore_container_id" pg_restore \
    --exit-on-error --no-owner --no-privileges -U "$service" -d "$service" \
    < "$RESTORE_BACKUP_DIR/$service.dump"
  printf '\nВосстановлена база: %s\n' "$service"
  docker exec -i "$restore_container_id" psql -X -v ON_ERROR_STOP=1 -U "$service" -d "$service" <<'SQL'
SELECT format('SELECT %L AS table_name, count(*) AS rows FROM %I.%I;',
              tablename, schemaname, tablename)
FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename
\gexec
SQL
done
printf '\nВсе пять дампов восстановлены во временный PostgreSQL.\n'
BASH
```

Пароль здесь не используется только внутри изолированного проверочного контейнера. **Не переносите `POSTGRES_HOST_AUTH_METHOD=trust` в рабочее или доступное по сети окружение.** Роли создаются заново, `--no-owner --no-privileges` пропускает исходные команды владельцев и прав. Поэтому это проверка восстановления данных/структуры, а не проверка точного воспроизведения исходных разрешений.

Успех — все пять восстановлений без ошибок, список таблиц с количеством строк и финальная строка. При ошибке `pg_restore --exit-on-error` останавливает проверку; временный контейнер удаляется при выходе. Исходные дампы и рабочий стенд остаются на месте. Для больших копий память может закончиться: это ограничение данного учебного способа, а не автоматическое доказательство повреждения дампа.

Количество строк помогает заметить пустую базу, но не доказывает равенство содержимого. Для более строгой проверки нужны контрольные суммы содержимого таблиц и состояние последовательностей, снятые в той же точке, что backup. Сравнение с уже работающей и изменившейся базой может дать ожидаемые различия. В выполненной проверке проекта сравнивались все 19 таблиц и последовательности; границы этой проверки описаны в [отчёте о проверках](../verification.md). Она подтверждает PostgreSQL, **не восстановление S3, Kafka или всего сервиса**.

Не восстанавливайте пробную копию в `marketplace-postgres` и не используйте рабочие тома для упражнения. Если терминал был аварийно убит и временный контейнер остался, найдите именно его через `docker ps` и проверьте отсутствие сети/томов перед удалением. Не удаляйте контейнеры по широкому шаблону.

### Полное восстановление после потери окружения

Здесь нет универсальной команды «вернуть всё». Общая последовательность для заранее подготовленного плана:

1. Поднимите отдельные зависимости и загрузите нужную конфигурацию и секреты. Оставьте consumers/publishers и внешнюю отправку почты выключенными.
2. Восстановите базы, объекты S3 и согласованное состояние Kafka. Проверьте версию схемы и совместимость версии приложения с копией.
3. Сопоставьте отправленный outbox, доступные события, offsets и inbox. Если Kafka потеряна, Search можно перестроить из Listings. Для Notifications нужно отдельно решить, какие письма могли уйти и какие требуют повтора.
4. Запускайте приложения по подготовленному плану, выполните [reindex](events.md) при необходимости и затем разрешите фоновые обработки и письма. Стандартные бинарные файлы запускают связанные workers вместе с HTTP: команда `start` сама по себе не разделяет эти роли. Раздельное включение нужно организовать в процедуре восстановления, а не предполагать готовый переключатель.
5. Проверьте открытие фотографий, права на переписку, поиск, новые сообщения, lag и состояния заданий. Одного ответа `200` от Gateway недостаточно.

Хранение копии на том же диске удобно для упражнения, но не защищает от потери компьютера. Копии содержат личные данные и хеши паролей: храните их отдельно с ограниченным доступом, определённым сроком хранения и регулярной проверкой восстановления.

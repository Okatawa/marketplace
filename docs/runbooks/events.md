# События: диагностика, replay и переиндексация

## Проверить lag и outbox

```sh
docker compose --env-file .local/.env exec -T kafka \
  /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server kafka:9092 --all-groups --describe
```

До первой переиндексации Search использует `search-v1`, затем — группу активной проекции. Notifications использует `notifications-v1`. Для publisher посмотрите количество записей `outbox WHERE sent_at IS NULL` в базе Listings или Messaging и метрики возраста записи.

Если outbox растёт, проверьте доступность Kafka, топик и права producer. Если outbox пуст, но lag растёт, проверьте consumer, его БД и обработчик. Повторяющееся событие при ошибке не следует «исправлять» ручным продвижением offset: так можно потерять бизнес-изменение.

## Посмотреть DLQ и повторить обработку

Для стандартных групп топики ошибок:

- `marketplace.listings.v1.search-v1.dlq`;
- `marketplace.messages.v1.notifications-v1.dlq`.

После reindex имя группы и DLQ Search меняется. Посмотрите список топиков:

```sh
docker compose --env-file .local/.env exec -T kafka \
  /opt/kafka/bin/kafka-topics.sh --bootstrap-server kafka:9092 --list
```

Пример чтения одного сообщения:

```sh
docker compose --env-file .local/.env exec -T kafka \
  /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server kafka:9092 \
  --topic marketplace.messages.v1.notifications-v1.dlq \
  --from-beginning --max-messages 1 --timeout-ms 10000 \
  --property print.partition=true --property print.offset=true
```

DLQ содержит исходные `topic`, `partition`, `offset`, `key` и строку `value`. Координаты внутри payload указывают на исходное событие. Для команды replay нужны **координаты самой записи DLQ**, напечатанные console consumer.

Сначала исправьте причину: несовместимый обработчик, неверную схему или ошибочную публикацию. Затем подставьте реальные DLQ partition/offset:

```sh
docker compose --env-file .local/.env --profile tools run --rm --build replay \
  --topic marketplace.messages.v1.notifications-v1.dlq --partition 0 --offset 0
```

Команда читает конкретную запись, проверяет оболочку и отправляет исходное событие в исходный топик с прежним `event_id`. Она не удаляет DLQ и не сбрасывает offsets. Если исходный payload по-прежнему непригоден обработчику, запись опять попадёт в DLQ. Replay не редактирует payload и не создаёт исправленный бизнес-факт; для такого изменения нужна отдельная осмысленная миграция или штатная операция сервиса.

Повтор валидного события безопасен для версии Search и inbox Notifications. Это не гарантия повторной отправки письма: если inbox уже содержит `event_id`, новое задание не создаётся. Для окончательно неотправленного задания нужно разбирать состояние `jobs`, а не бесконечно переигрывать Kafka.

## Полностью пересобрать Search

Search — восстанавливаемая проекция. Источник истины — Listings; доступной истории Kafka может не хватать после семи дней retention.

Перед запуском проверьте доступность Listings, Search DB и Kafka. Оцените время импорта относительно retention; CLI ограничен 30 минутами. Запустите:

```sh
docker compose --env-file .local/.env --profile tools run --rm --build reindex
```

Для воспроизводимого имени можно передать новый UUID:

```sh
docker compose --env-file .local/.env --profile tools run --rm --build reindex \
  --projection 8fe8c96c-547c-4a58-9d37-b3f156e73517
```

Используйте новый ID на каждый запуск. Команда создаёт отдельную проекцию и группу, сохраняет границу Kafka, импортирует текущие публичные объявления со статусами и версиями, догоняет события и **сама активирует** результат после проверки. Предыдущая выдача обслуживает запросы во время построения.

Если событие некорректно или нужная история уже недоступна, перестроение должно завершиться ошибкой с сохранением прежней активной проекции. После исправления причины запустите новую попытку с новым ID. Не переключайте `settings.active_projection` вручную на незавершённый результат.

Проверить результат в Search DB:

```sh
docker compose --env-file .local/.env exec -T postgres psql -U search -d search \
  -c 'SELECT p.id,p.group_id,p.ready FROM projections p JOIN settings s ON s.active_projection=p.id;'
curl -fsS 'http://127.0.0.1:8080/api/v1/search?limit=20'
```

Старые проекции и группы сохраняются для разбора. Автоматическое удаление не реализовано; сначала убедитесь, какая проекция активна и не выполняется ли другой rebuild. Ручной откат на старую проекцию не обеспечивает актуальность: после переключения она может больше не получать события.

## Отказ SMTP

Проверьте Mailpit, доступность внутреннего Accounts API и состояния `jobs`. Тексты технических ошибок умышленно не сохраняют email и секреты. Окончательный `failed` требует разбора оператором.

После устранения причины можно осознанно вернуть **одно выбранное** задание в очередь через административный SQL, учитывая возможность дубликата уже доставленного письма:

```sql
UPDATE jobs
SET state='pending', attempts=0, next_attempt=now(),
    lease_until=NULL, lease_owner=NULL, last_error=NULL
WHERE id='<проверенный UUID задания>' AND state='failed';
```

Не сбрасывайте разом все состояния и не удаляйте inbox: это уничтожает защиту от повторных событий.

# Полный сценарий через HTTP API

[К оглавлению](../README.md)

Этот практикум проходит путь двух пользователей: продавец публикует объявление, покупатель находит его и пишет сообщение, продавец читает переписку и снимает объявление с продажи. По дороге мы проверим ошибки доступа, конфликт версий и безопасный повтор запроса.

Все запросы идут через Gateway по `http://127.0.0.1:8080`. Внутренние адреса сервисов и `SERVICE_TOKEN` для этого не нужны. Точный контракт находится в [`api/openapi.yaml`](../../api/openapi.yaml); ниже показана последовательность работы с ним, а не перечень всех маршрутов.

## 1. Подготовить стенд и рабочую оболочку

Нужны запущенный локальный стенд (`make up` из корня репозитория), `curl`, `python3` и Bash либо zsh. Python использует только стандартную библиотеку; `jq`, Pillow и демонстрационные аккаунты не нужны. `make seed` для этого сценария не требуется. Все следующие блоки выполняйте **по порядку в одной и той же оболочке**: они используют ранее созданные переменные и функции. Можно собрать блоки в один файл и запустить через Bash. Если команда завершилась ошибкой, устраните причину перед продолжением.

Сценарий создаёт двух пользователей, одно объявление, фотографию, диалог, сообщение и локальное письмо в Mailpit. Эти данные остаются в стенде; снятие с продажи не удаляет объявление. Повторный запуск создаёт новые адреса пользователей и новый заголовок.

```sh
set +x
set -eu
set -o pipefail
umask 077
BASE_URL='http://127.0.0.1:8080'
WORK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/marketplace-api.XXXXXX")
RESPONSE="$WORK_DIR/response.json"

api() {
  local expected_code="$1"
  shift
  local actual_code
  actual_code=$(curl --silent --show-error --max-time 20 \
    --output "$RESPONSE" --write-out '%{http_code}' "$@") || return
  if [ "$actual_code" != "$expected_code" ]; then
    printf 'Ожидался HTTP %s, получен HTTP %s\n' "$expected_code" "$actual_code" >&2
    python3 - "$RESPONSE" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
    print(json.dumps({k: data[k] for k in ("error", "request_id") if k in data},
                     ensure_ascii=False), file=sys.stderr)
except (ValueError, OSError):
    print("Ответ не является JSON; проверьте доступность Gateway и его логи.", file=sys.stderr)
PY
    return 1
  fi
  printf 'HTTP %s\n' "$actual_code"
}

field() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[2]))[sys.argv[1]])' \
    "$1" "${2:-$RESPONSE}"
}

listing_summary() {
  python3 - "$RESPONSE" <<'PY'
import json, sys
x = json.load(open(sys.argv[1]))
print(json.dumps({k: x[k] for k in ("id", "title", "status", "price_minor", "version")},
                 ensure_ascii=False, indent=2))
print("Фотографий:", len(x["photos"]))
PY
}

api 200 "$BASE_URL/health/ready"
api 200 "$BASE_URL/api/v1/categories"
python3 -m json.tool "$RESPONSE"
```

Функция `api` сохраняет тело последнего ответа в `$RESPONSE`, выводит только HTTP-статус и проверяет его. При неожиданном статусе она возвращает ошибку и показывает публичное описание ошибки. `set -e` останавливает исполняемый сценарий при необработанной ошибке. Функция `field version` читает одно поле JSON; подстановка `VERSION=$(field version)` сохраняет его в переменную. Следующий запрос перезаписывает файл ответа, поэтому нужные значения извлекаются сразу.

Не включайте `set -x` и не печатайте переменные с токенами: трассировка команд раскрывает заголовок `Authorization`. Ответы авторизации также не выводим целиком. Временные файлы доступны только вашему пользователю благодаря `umask 077`. Читать или выполнять через `source` файл `.local/.env` здесь не требуется.

Категории возвращаются как `{"items":[{"id":"sport","name":"…"},…]}`. В сценарии используется существующая категория `sport`.

## 2. Зарегистрировать продавца

Сначала создадим уникальные регистрационные данные и тестовую картинку. PNG ниже — настоящий файл 2×2 пикселя из [`tests/browser/marketplace.spec.js`](../../tests/browser/marketplace.spec.js). Подмена расширения произвольного файла на `.png` не сработает: сервер декодирует изображение.

```sh
python3 - "$WORK_DIR" <<'PY'
import base64, json, pathlib, secrets, sys, time
root = pathlib.Path(sys.argv[1])
suffix = str(time.time_ns())
(root / "suffix.txt").write_text(suffix)
for role, name in (("seller", "Продавец практикума"), ("buyer", "Покупатель практикума")):
    credentials = {"name": name, "email": f"guide-{role}-{suffix}@example.com",
                   "password": secrets.token_urlsafe(24)}
    (root / f"{role}-register.json").write_text(json.dumps(credentials))
    (root / f"{role}-login.json").write_text(json.dumps(
        {k: credentials[k] for k in ("email", "password")}))
(root / "photo.png").write_bytes(base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGNQKw0FIgYIBQAZkgPBSf4NCgAAAABJRU5ErkJggg=="))
PY
SUFFIX=$(cat "$WORK_DIR/suffix.txt")

api 201 --request POST "$BASE_URL/api/v1/auth/register" \
  --header 'Content-Type: application/json' \
  --cookie-jar "$WORK_DIR/seller.cookies" \
  --data-binary "@$WORK_DIR/seller-register.json"
SELLER_TOKEN=$(field access_token)

api 200 "$BASE_URL/api/v1/me" \
  --header "Authorization: Bearer $SELLER_TOKEN"
SELLER_ID=$(field id)
python3 -m json.tool "$RESPONSE"
```

Регистрация отвечает `201` и JSON с полем `access_token`; это JWT на 15 минут. Отдельно заголовок `Set-Cookie` устанавливает `refresh` на 30 дней. `curl --cookie-jar` сохраняет cookie в файл, но сам по себе не прикладывает его к будущим запросам. Для этого ниже используется `--cookie`.

`GET /me` возвращает `id`, `name`, `email` текущего пользователя. Публичный `GET /users/{id}` возвращает профиль без email. Правила регистрации: имя 1–80 символов, корректный email, пароль 12–72 **байта**. Реализация: [`services/accounts/handler.go`](../../services/accounts/handler.go), [`services/accounts/service.go`](../../services/accounts/service.go).

## 3. Создать черновик и загрузить фото

```sh
python3 - "$SUFFIX" > "$WORK_DIR/listing.json" <<'PY'
import json, sys
print(json.dumps({"title": "Велосипед практикум " + sys.argv[1],
                  "description": "Учебное объявление, созданное через HTTP API",
                  "category_id": "sport", "price_minor": 1500000}))
PY

api 201 --request POST "$BASE_URL/api/v1/listings" \
  --header "Authorization: Bearer $SELLER_TOKEN" \
  --header 'Content-Type: application/json' \
  --data-binary "@$WORK_DIR/listing.json"
LISTING_ID=$(field id)
DRAFT_VERSION=$(field version)
listing_summary

api 404 "$BASE_URL/api/v1/listings/$LISTING_ID"

api 201 --request POST "$BASE_URL/api/v1/listings/$LISTING_ID/photos" \
  --header "Authorization: Bearer $SELLER_TOKEN" \
  --form "file=@$WORK_DIR/photo.png;type=image/png"
PHOTO_ID=$(field id)

api 200 "$BASE_URL/api/v1/listings/$LISTING_ID" \
  --header "Authorization: Bearer $SELLER_TOKEN"
VERSION=$(field version)
listing_summary
```

`price_minor=1500000` означает **15 000 рублей**, потому что API принимает целые копейки. Такой же масштаб у поисковых фильтров `min_price` и `max_price`. Дробное число рублей в это поле передавать нельзя.

Черновик имеет `status=draft` и сначала `version=1`. Владелец видит его с JWT; анонимный запрос получает `404`, чтобы не раскрывать чужие черновики. Объект объявления содержит `id`, `owner_id`, `title`, `description`, `category_id`, `price_minor`, `status`, `version`, `photos`, `created_at`. Время сериализуется в формате RFC3339.

Фото загружается как multipart-форма с полем **`file`**, а не как JSON или Base64-строка. Не задавайте общий `Content-Type: multipart/form-data` вручную: `curl --form` добавит правильную границу частей. Ответ `201` содержит `id` и временную подписанную `url` фотографии; это не всё объявление. Поэтому после загрузки заново читаем карточку и её `version`. Загрузка увеличивает версию, хотя не принимает `expected_version`.

Ограничения: JPEG/PNG, до 5 МиБ, не более 20 млн пикселей и максимум восемь готовых фотографий. Подписанные ссылки действуют 15 минут. Публичный маршрут `/api/v1/listings/{id}/photos/{photo_id}` проверяет доступ и перенаправляет на такую ссылку. Код: [`services/listings/media.go`](../../services/listings/media.go).

## 4. Опубликовать и дождаться поиска

```sh
api 200 --request POST "$BASE_URL/api/v1/listings/$LISTING_ID/publish" \
  --header "Authorization: Bearer $SELLER_TOKEN" \
  --header 'Content-Type: application/json' \
  --data "{\"expected_version\":$VERSION}"
PUBLISHED_VERSION=$(field version)
listing_summary

wait_search() {
  local expected_presence="$1"
  local attempt=0
  while [ "$attempt" -lt 40 ]; do
    api 200 --get "$BASE_URL/api/v1/search" \
      --data-urlencode "q=$SUFFIX" \
      --data-urlencode 'category_id=sport' \
      --data-urlencode 'min_price=1000000' \
      --data-urlencode 'max_price=2000000' \
      --data-urlencode 'limit=20' --data-urlencode 'page=1' || return
    if python3 - "$RESPONSE" "$LISTING_ID" "$expected_presence" <<'PY'
import json, sys
items = json.load(open(sys.argv[1]))["items"]
found = any(x["id"] == sys.argv[2] for x in items)
sys.exit(0 if found == (sys.argv[3] == "present") else 1)
PY
    then
      printf 'Состояние объявления в поиске: %s\n' "$expected_presence"
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  printf 'Поиск не достиг ожидаемого состояния за 40 попыток\n' >&2
  return 1
}
wait_search present
```

Публикация отвечает `200` и обновлённым объявлением: `status=published`, версия ещё увеличилась. Для публикации обязательны непустые название, описание, категория и хотя бы одно готовое фото. Цена может быть нулевой. Полные правила находятся в [`services/listings/models.go`](../../services/listings/models.go).

`expected_version` — версия, которую клиент видел при чтении. Сервер под блокировкой сравнивает её с текущей: если другой запрос успел изменить запись, возвращает `409`, а не затирает изменения. Это называется оптимистической блокировкой со стороны клиента.

Ответ публикации подтверждает транзакцию в Listings. Затем outbox передаёт событие в Kafka, а Search обновляет свою отдельную таблицу. Поэтому публикация может быть успешной раньше появления объявления в поиске. Цикл проверяет именно `id` созданного объявления, допускает нормальную задержку и завершает сценарий ошибкой при превышении числа попыток. С учётом времени самих HTTP-запросов это не строгий таймер на 40 секунд.

Поиск возвращает `items`, `page`, `has_more`; страницы начинаются с 1. `limit` — от 1 до 100, по умолчанию 20; `page` — от 1 до 1000. `has_more=true` означает, что получена полная страница, а не точное знание общего числа результатов. Код: [`services/search/handler.go`](../../services/search/handler.go).

## 5. Проверить конфликт версии

После загрузки фото и публикации сохранённая `DRAFT_VERSION` уже устарела. Попытка снять объявление с этой версией должна завершиться **ожидаемой** ошибкой:

```sh
api 409 --request POST "$BASE_URL/api/v1/listings/$LISTING_ID/withdraw" \
  --header "Authorization: Bearer $SELLER_TOKEN" \
  --header 'Content-Type: application/json' \
  --data "{\"expected_version\":$DRAFT_VERSION}"
python3 -m json.tool "$RESPONSE"
```

Получим `error.code="version"`, человекочитаемое `error.message` и `request_id`. Объявление остаётся опубликованным. Правильное действие клиента — заново прочитать запись, показать актуальные данные и осознанно повторить изменение. Не стоит автоматически заменять версию и повторять произвольное редактирование: это может затереть чужую работу.

`PATCH /listings/{id}` также принимает `expected_version`, но в этой реализации его поля не частичные: передавайте все `title`, `description`, `category_id`, `price_minor`. Пропущенные поля получают нулевые значения Go. Реализация мутаций: [`services/listings/handler.go`](../../services/listings/handler.go).

## 6. Зарегистрировать покупателя и войти заново

```sh
api 201 --request POST "$BASE_URL/api/v1/auth/register" \
  --header 'Content-Type: application/json' \
  --cookie-jar "$WORK_DIR/buyer.cookies" \
  --data-binary "@$WORK_DIR/buyer-register.json"

api 204 --request POST "$BASE_URL/api/v1/auth/logout" \
  --header "Origin: $BASE_URL" \
  --cookie "$WORK_DIR/buyer.cookies" --cookie-jar "$WORK_DIR/buyer.cookies"

api 200 --request POST "$BASE_URL/api/v1/auth/login" \
  --header 'Content-Type: application/json' \
  --cookie-jar "$WORK_DIR/buyer.cookies" \
  --data-binary "@$WORK_DIR/buyer-login.json"
BUYER_TOKEN=$(field access_token)

api 403 --request POST "$BASE_URL/api/v1/listings/$LISTING_ID/withdraw" \
  --header "Authorization: Bearer $BUYER_TOKEN" \
  --header 'Content-Type: application/json' \
  --data "{\"expected_version\":$PUBLISHED_VERSION}"
python3 -m json.tool "$RESPONSE"
```

Продавец и покупатель имеют разные JWT и разные файлы cookie. Промежуточный logout закрывает регистрационную refresh-сессию покупателя, чтобы затем показать обычный вход по email и паролю. `204` означает успешный ответ **без тела**; не разбирайте его как JSON.

Покупатель видит опубликованное объявление, но менять его не может: `403`, `error.code="forbidden"`. Проверка владельца выполняется перед сравнением версии. JWT удостоверяет личность; права на конкретную запись проверяет сервис.

## 7. Открыть диалог и отправить сообщение с повтором

```sh
api 201 --request POST "$BASE_URL/api/v1/conversations" \
  --header "Authorization: Bearer $BUYER_TOKEN" \
  --header 'Content-Type: application/json' \
  --data "{\"listing_id\":\"$LISTING_ID\"}"
CONVERSATION_ID=$(field id)
python3 -m json.tool "$RESPONSE"

MESSAGE_KEY="guide-message-$SUFFIX"
printf '%s' '{"body":"Здравствуйте! Можно посмотреть велосипед завтра?"}' \
  > "$WORK_DIR/message.json"
api 201 --request POST "$BASE_URL/api/v1/conversations/$CONVERSATION_ID/messages" \
  --header "Authorization: Bearer $BUYER_TOKEN" \
  --header 'Content-Type: application/json' \
  --header "Idempotency-Key: $MESSAGE_KEY" \
  --data-binary "@$WORK_DIR/message.json"
MESSAGE_ID=$(field id)

api 200 --request POST "$BASE_URL/api/v1/conversations/$CONVERSATION_ID/messages" \
  --header "Authorization: Bearer $BUYER_TOKEN" \
  --header 'Content-Type: application/json' \
  --header "Idempotency-Key: $MESSAGE_KEY" \
  --data-binary "@$WORK_DIR/message.json"
test "$(field id)" = "$MESSAGE_ID"

api 409 --request POST "$BASE_URL/api/v1/conversations/$CONVERSATION_ID/messages" \
  --header "Authorization: Bearer $BUYER_TOKEN" \
  --header 'Content-Type: application/json' \
  --header "Idempotency-Key: $MESSAGE_KEY" \
  --data '{"body":"Совсем другое сообщение с тем же ключом"}'
python3 -m json.tool "$RESPONSE"
```

Новый диалог отвечает `201`; повторное открытие тем же покупателем для того же объявления вернёт существующий диалог и `200`. Его поля: `id`, `listing_id`, `buyer_id`, `seller_id`, `title`, `updated_at`. Создать диалог с самим собой нельзя. Новый диалог создаётся только для опубликованного объявления; уже существующий остаётся доступен после снятия с продажи.

Первое сообщение отвечает `201` и содержит `id`, `seq`, `conversation_id`, `sender_id`, `body`, `created_at`. Повтор с тем же ключом, участником, диалогом и текстом отвечает `200` с тем же `id`: второй записи и второго события не появляется. Это помогает, когда сервер уже принял сообщение, а клиент не получил ответ из-за разрыва соединения. Сохраните ключ исходного запроса и повторите **его же**. Новый ключ обозначает новое сообщение.

Один ключ с другим текстом даёт `409`, `error.code="idempotency"`. Заголовок обязателен, длина ключа 1–128 байт; сообщение — непустой текст до 4000 символов. Сравнивается исходный текст, поэтому даже изменение пробелов может сделать повтор другим запросом. Код: [`services/messaging/handler.go`](../../services/messaging/handler.go).

## 8. Прочитать историю от лица продавца

```sh
api 200 "$BASE_URL/api/v1/conversations/$CONVERSATION_ID/messages?limit=20&cursor=0" \
  --header "Authorization: Bearer $SELLER_TOKEN"
python3 - "$RESPONSE" "$MESSAGE_ID" <<'PY'
import json, sys
x = json.load(open(sys.argv[1]))
assert len(x["items"]) == 1, "В истории ожидалось ровно одно сообщение"
assert x["items"][0]["id"] == sys.argv[2], "Получено другое сообщение"
print(json.dumps(x, ensure_ascii=False, indent=2))
PY
NEXT_CURSOR=$(field next_cursor)

api 200 "$BASE_URL/api/v1/conversations/$CONVERSATION_ID/messages?limit=20&cursor=$NEXT_CURSOR" \
  --header "Authorization: Bearer $SELLER_TOKEN"
python3 - "$RESPONSE" <<'PY'
import json, sys
assert json.load(open(sys.argv[1]))["items"] == [], "Неожиданные новые сообщения"
print("Новых сообщений после курсора пока нет")
PY
```

История содержит `items`, `next_cursor` и `conversation`. Курсор истории — строка с последним `seq`, а не UUID сообщения и не номер страницы. Следующий запрос читает сообщения с большим `seq`. `seq` не обязан начинаться с 1 в данном диалоге и не обязан идти без пропусков. Пустая выдача сохраняет переданный курсор. Читать и писать могут только покупатель и продавец данного диалога; посторонний авторизованный пользователь получит `403`.

На странице Mailpit [http://127.0.0.1:8025](http://127.0.0.1:8025) спустя небольшую задержку появится письмо продавцу. Адрес текущего продавца можно вывести без пароля: `field email "$WORK_DIR/seller-register.json"`. Письмо содержит ссылку на переписку, но не текст сообщения. Сценарий выше проверяет историю через HTTP; доставку письма автоматически проверяет `make smoke`.

## 9. Обновить access token через cookie

Можно выполнить этот блок раньше, если практикум занял больше 15 минут. Для покупателя аналогично используйте его cookie-файл и переменную `BUYER_TOKEN`.

```sh
api 403 --request POST "$BASE_URL/api/v1/auth/refresh" \
  --cookie "$WORK_DIR/seller.cookies"

api 200 --request POST "$BASE_URL/api/v1/auth/refresh" \
  --header "Origin: $BASE_URL" \
  --cookie "$WORK_DIR/seller.cookies" --cookie-jar "$WORK_DIR/seller.cookies"
SELLER_TOKEN=$(field access_token)
```

Первый запрос намеренно не передаёт `Origin` и получает `403`, `error.code="origin"`. Второй передаёт `Origin: http://127.0.0.1:8080`, точно совпадающий с локальным `PUBLIC_URL`, и возвращает новый JWT. Значение `http://localhost:8080` здесь другое и не подходит. При другой конфигурации стенда нужно согласованно изменить его публичный URL и запросы, а не просто подменить один заголовок.

Refresh использует cookie, а не старый Bearer JWT. `--cookie` отправляет прежнюю cookie, `--cookie-jar` сохраняет новую. Старый refresh token удаляется в той же транзакции, в которой создаётся новый: повторное использование старого даёт `401`. Не запускайте несколько refresh-запросов одной сессии параллельно. `HttpOnly` защищает от чтения cookie браузерным JavaScript; `curl` закономерно может сохранить её в файл.

## 10. Снять объявление с продажи и завершить работу

```sh
api 200 "$BASE_URL/api/v1/listings/$LISTING_ID" \
  --header "Authorization: Bearer $SELLER_TOKEN"
VERSION=$(field version)
api 200 --request POST "$BASE_URL/api/v1/listings/$LISTING_ID/withdraw" \
  --header "Authorization: Bearer $SELLER_TOKEN" \
  --header 'Content-Type: application/json' \
  --data "{\"expected_version\":$VERSION}"
listing_summary
wait_search absent

api 204 --request POST "$BASE_URL/api/v1/auth/logout" \
  --header "Origin: $BASE_URL" \
  --cookie "$WORK_DIR/seller.cookies" --cookie-jar "$WORK_DIR/seller.cookies"
api 204 --request POST "$BASE_URL/api/v1/auth/logout" \
  --header "Origin: $BASE_URL" \
  --cookie "$WORK_DIR/buyer.cookies" --cookie-jar "$WORK_DIR/buyer.cookies"

unset SELLER_TOKEN BUYER_TOKEN
rm -r -- "$WORK_DIR"
unset RESPONSE WORK_DIR
```

Объявление переходит в `withdrawn` и после обработки события пропадает из поиска. Прямое чтение карточки и существующая переписка сохраняются. Состояние `withdrawn` можно снова перевести в `published`, передав актуальную версию и соблюдая условия публикации. Переход `published → published` или `draft → withdrawn` даст `409` с кодом `state`.

Logout удаляет refresh-сессию и очищает cookie; ранее выданный access JWT технически остаётся действительным до своего срока истечения. Мы удаляем переменные и временные файлы, но не серверные данные. Для повторения начните с первого шага в новой оболочке.

## Если запрос завершился ошибкой

| Ответ | Что проверить |
|---|---|
| `400 validation` | JSON, обязательные поля, категорию, размер/содержимое фото, диапазон цены, `Idempotency-Key` |
| `401` | Наличие и срок JWT; для refresh — актуальную cookie. Обновите сессию либо войдите заново |
| `403 origin` | Точное совпадение `Origin` с публичным URL, в примере — `127.0.0.1`, без завершающего `/` |
| `403 forbidden` | Под каким пользователем выполняется действие и кому принадлежит запись |
| `404` | Правильность ID; черновики скрыты от других пользователей |
| `409 version` | Прочитайте свежую версию объявления перед изменением |
| `409 idempotency` | Не использован ли ключ сообщения с другим текстом |
| `429` | Сработал rate limit Gateway; не запускайте практикум многократно в тесном цикле |
| `500`, `502`, `503` или ошибка соединения | Состояние контейнеров и логи нужного сервиса; см. [локальную диагностику](../runbooks/local-development.md) |

Публичные ошибки бизнес-API содержат `error.code`, `error.message`, иногда `error.fields`, и `request_id`. Этот идентификатор также передаётся в `X-Request-ID`; он помогает связать ответ с логами. Реализация общего формата: [`internal/platform/httpx/errors.go`](../../internal/platform/httpx/errors.go). При поиске причин асинхронной задержки также полезен [разбор событий](../runbooks/events.md).

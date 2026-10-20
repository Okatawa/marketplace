# Локальный Kubernetes через kind

Этот стенд показывает запуск шести Go-сервисов из Helm chart и отдельный шаг миграции. PostgreSQL, Kafka, RustFS и Mailpit разворачиваются отдельными локальными манифестами с одной репликой. Для внешнего окружения нужны самостоятельные управляемые зависимости.

## Запуск

Нужны Docker Engine **28+** с API 1.48+ (`docker image save --platform`), Go, Python 3, Make, `kubectl`, свободные порты 18080/19000/18025 и достаточно памяти для ещё одного Kubernetes-узла. Одновременно запущенные Compose и kind содержат независимые копии данных и расходуют дополнительные ресурсы.

Из корня репозитория:

```sh
make setup
make tools
make kind-up
make kind-smoke
```

`make tools` помещает Helm, kind и kubeconform в `.local/bin`. `make kind-up` собирает образы, а `scripts/kind-up.sh` создаёт кластер **marketplace-example** и работает с отдельным `.local/kubeconfig`, context **kind-marketplace-example**. Пользовательский текущий context для операций развёртывания не используется.

Скрипт импортирует образы для архитектуры узла, создаёт локальные Secrets из `.local/.env`, запускает зависимости, ждёт инициализации Kafka, применяет пять migration Jobs и только затем устанавливает/обновляет приложения Helm. Повторный запуск обновляет контейнеры с изменёнными образами `:dev`.

| Что | Адрес |
|---|---|
| Приложение | http://127.0.0.1:18080 |
| Mailpit | http://127.0.0.1:18025 |
| S3 API | http://127.0.0.1:19000 |

Демоданные загружаются отдельно:

```sh
go run ./cmd/seed --base-url http://127.0.0.1:18080
```

Логины те же, что в Compose: `anna@example.com` и `boris@example.com`, пароль — `DEMO_PASSWORD` из `.local/.env`. Аккаунты и объявления kind не синхронизируются с Compose.

## Диагностика

```sh
kubectl --kubeconfig .local/kubeconfig --context kind-marketplace-example \
  -n marketplace get pods,jobs,pvc
kubectl --kubeconfig .local/kubeconfig --context kind-marketplace-example \
  -n marketplace logs deployment/marketplace-listings --tail=100
kubectl --kubeconfig .local/kubeconfig --context kind-marketplace-example \
  -n marketplace get events --sort-by=.lastTimestamp
```

Успешные `kafka-init` и migration Jobs переходят в `Completed`. Если приложение не готово, сначала проверьте статус миграции, подключение к своей БД и доступность зависимостей. При ошибке загрузки образа сравните архитектуру узла с локальным образом и версию Docker.

`make helm-check` отдельно проверяет chart, структуру сгенерированных ресурсов и Kubernetes-схемы. Прохождение рендера не заменяет запуск в кластере; `make kind-smoke` выполняет реальный пользовательский сценарий через NodePort, включая письмо Mailpit.

## Ограничения и очистка

kindnet в этой конфигурации не обеспечивает применение NetworkPolicy, поэтому в `values.yaml` они отключены. Production chart включает входящие политики; проверять их нужно с поддерживающим CNI.

PostgreSQL, Kafka и S3 используют PVC, привязанные к локальному кластеру. Перезапуск pod не равен потере всего узла; **удаление кластера удаляет учебные данные**. Сохраните нужное заранее:

```sh
.local/bin/kind delete cluster --name marketplace-example
```

Команда затрагивает только указанный kind-кластер. `.local/.env` и данные Compose сохраняются. Не удаляйте пользовательские Kubernetes contexts и другие кластеры для очистки этого стенда.

Подробнее об обновлении внешнего окружения и backup — в [runbook](../../docs/runbooks/deployment-and-backups.md).

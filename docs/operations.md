# Эксплуатация

## Docker

Запуск:

```bash
docker compose up --build -d
```

Логи:

```bash
docker compose logs -f app
```

Пересоздать приложение без пересоздания базы:

```bash
docker compose up -d --force-recreate app
```

## Переменные окружения

Основной файл настроек - `.env`.

Актуальный список переменных и комментарии по группам находятся в `.env.example`.

Минимально нужны:

- `DATABASE_URL`
- `POSTGRES_DB`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `VK_GROUP_ID`
- `VK_ACCESS_TOKEN`
- `LLM_PROVIDER`
- `LLM_MODEL`

Для `LLM_PROVIDER=openai_compat` также нужны:

- `LLM_BASE_URL`
- `LLM_API_KEY`

## Сертификаты

Docker-образ устанавливает `ca-certificates`, копирует `certs/*.crt` в `/usr/local/share/ca-certificates/` и запускает `update-ca-certificates`.

В репозитории лежат публичные российские доверенные CA как резерв для HTTPS-вызовов, включая VK.

Переменная:

```env
SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
```

указывает Go/TLS на системный bundle сертификатов внутри контейнера.

## Команда debug

Команда из `DEBUG_COMMAND` доступна пользователям из `MANUAL_TRIGGER_USER_IDS`.

Она показывает:

- текущую LLM-модель;
- ping до VK API;
- краткие расходы за последние 30 дней;
- график input/output за последние 7 дней;
- подробную статистику по callback-кнопке администратора.

## Автопостинг коммитов в VK

Workflow `.github/workflows/vk-wall-post.yml` публикует на стене сообщества список коммитов после push в `main`.

Нужны GitHub repository secret:

- `VK_ACCESS_TOKEN` - ключ сообщества с правом `wall`.

Нужны GitHub repository variables:

- `VK_GROUP_ID` - числовой id сообщества без минуса.
- `VK_API_VERSION` - версия VK API, по умолчанию `5.199`.

Если secret или variable не заданы, workflow пропускает публикацию.

Workflow можно запустить вручную через `workflow_dispatch`.

## Частые проблемы

`invalid access_token`

Проверить, что используется ключ сообщества, а не пользовательский токен.

`Access denied`

Проверить `VK_GROUP_ID`, право `messages`, включенный Long Poll API и сообщения сообщества.

Нет сообщений из беседы

Проверить, что сообщество добавлено в беседу и имеет доступ к сообщениям.

LLM не отвечает

Проверить `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, лимиты провайдера и `LLM_REQUEST_TIMEOUT`.

Нет картинки

Проверить `SUMMARY_IMAGE_ENABLED`, провайдера, модель, ключи и timeout генерации.

## Перед рабочим запуском

- Проверить `.env`.
- Убедиться, что `.env` не попал в Git.
- Добавить сообщество в нужные беседы.
- Проверить ручную команду дайджеста.
- Проверить `/livanda-debug`.
- Посмотреть логи после первого автодайджеста.


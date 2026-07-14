# bot-summary-vk

Рабочий MVP-бот для ВКонтакте на Go: читает сообщения из всех бесед, куда добавлено сообщество, ведет отдельный контекст по каждой беседе в PostgreSQL и автоматически публикует сводку (summary) после каждых `N` осмысленных сообщений. Дополнительно сводку можно выпустить раньше командой управляющего бота. При включенной генерации изображений бот отправляет summary вместе с иллюстрацией и watermark `DDD`.


## Запуск

### 1. Подготовить сообщество VK

Бот работает от имени сообщества через `Bots Long Poll API`.

Что нужно включить в панели VK:
- `Управление -> Сообщения`:
  - включить `Сообщения сообщества`
  - включить `Возможности ботов`
  - включить `Разрешать добавлять сообщество в беседы`
- `Управление -> Дополнительно -> Работа с API`:
  - создать `ключ доступа сообщества`
  - выдать ключу право `messages`
- `Управление -> Дополнительно -> Работа с API -> Long Poll API`:
  - включить `Long Poll API`
  - выбрать актуальную версию API
  - включить событие `message_new`

Важно:
- нужен именно `ключ доступа сообщества`, а не пользовательский токен
- `VK_GROUP_ID` должен относиться к тому же сообществу, для которого выпущен токен
- если бот не видит сообщения из конкретной беседы, проверь права сообщества в этой беседе; на практике для некоторых чатов может понадобиться выдать сообществу расширенные права или админку

### 2. Добавить сообщество в беседы

После включения `Разрешать добавлять сообщество в беседы` у сообщества появляется кнопка `Пригласить в беседу` / `Добавить в беседу`.

Что сделать:
- добавить сообщество в нужные беседы
- отправить в беседы несколько обычных текстовых сообщений
- если нужен ручной запуск, убедиться, что управляющий пользователь или бот тоже есть в этой беседе

Текущая логика бота:
- бот обрабатывает все групповые беседы, куда его добавили
- контекст и счетчик summary ведутся отдельно по каждому `peer_id`

### 3. Получить нужные идентификаторы

#### `VK_GROUP_ID`

Если ссылка на сообщество выглядит как `club237254188`, то:
- `VK_GROUP_ID=237254188`

#### `MANUAL_TRIGGER_USER_IDS`

Это список числовых `user_id` пользователей VK, которые могут вызвать ручной summary.

Пример:
- `MANUAL_TRIGGER_USER_IDS=123456789,227439621`
- `MANUAL_TRIGGER_COMMAND=/livanda`

### 4. Подготовить `.env`

```bash
cd bot-summary-vk
cp .env.example .env
```

Заполнить минимум:
- `VK_GROUP_ID`
- `VK_ACCESS_TOKEN`
- `SUMMARY_BATCH_SIZE`
- если нужен управляющий ручной запуск: `MANUAL_TRIGGER_USER_IDS`, `MANUAL_TRIGGER_COMMAND`
- если нужен внешний LLM: `LLM_PROVIDER`, `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`
- если нужна картинка к summary: `SUMMARY_IMAGE_ENABLED`, `SUMMARY_IMAGE_PROVIDER` и переменные выбранного image-провайдера
- если запускаешь через `docker compose`, проверь `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `DATABASE_URL`

`docker compose` прокидывает `.env` внутрь контейнера через `env_file`, поэтому runtime-переменные тоже оставлены в примере.

Runtime-переменные:
- `PATH` - стандартный путь поиска бинарников внутри контейнера; обычно не требует изменения
- `SSL_CERT_FILE` - явный путь к системному CA bundle, который Go/TLS использует для проверки HTTPS-сертификатов

Практический стартовый набор:
- `VK_GROUP_ID` - id сообщества
- `VK_ACCESS_TOKEN` - ключ доступа сообщества с правом `messages`
- `MANUAL_TRIGGER_USER_IDS` - список `id` управляющих пользователей или ботов через запятую
- `MANUAL_TRIGGER_COMMAND=/livanda`
- `SUMMARY_BATCH_SIZE=200`
- `LLM_PROVIDER=openai_compat` или `stub`
- `SUMMARY_IMAGE_ENABLED=false` для запуска без картинок

Если используешь OpenRouter, Fireworks, Yandex AI Cloud или другой OpenAI-совместимый адрес:
- `LLM_BASE_URL=https://openrouter.ai/api/v1`, `https://api.fireworks.ai/inference/v1` или `https://ai.api.cloud.yandex.net/v1`
- `LLM_API_KEY=...`
- `LLM_MODEL=...`

Если нужна картинка к summary через Cloudflare Workers AI:
- `SUMMARY_IMAGE_ENABLED=true`
- `SUMMARY_IMAGE_PROVIDER=cloudflare`
- `SUMMARY_IMAGE_BASE_URL=https://api.cloudflare.com`
- `SUMMARY_IMAGE_API_KEY=...`
- `SUMMARY_IMAGE_ACCOUNT_ID=...`
- `SUMMARY_IMAGE_MODEL=@cf/black-forest-labs/flux-2-klein-4b`
- `SUMMARY_IMAGE_WIDTH=1024`
- `SUMMARY_IMAGE_HEIGHT=1024`

Если нужна картинка через YandexART:
- `SUMMARY_IMAGE_ENABLED=true`
- `SUMMARY_IMAGE_PROVIDER=yandex_art`
- `SUMMARY_IMAGE_BASE_URL=https://ai.api.cloud.yandex.net`
- `SUMMARY_IMAGE_API_KEY=...`; если не задано, используется `LLM_API_KEY`
- `SUMMARY_IMAGE_FOLDER_ID=...`; если не задано, папка берется из `LLM_MODEL` вида `gpt://<folder_id>/...`
- `SUMMARY_IMAGE_MODEL=yandex-art`
- `SUMMARY_IMAGE_WIDTH_RATIO=1`
- `SUMMARY_IMAGE_HEIGHT_RATIO=1`

### 5. Запуск через Docker Compose

```bash
docker compose up --build
```

Если нужен фоновый режим:

```bash
docker compose up --build -d
```

Проверить логи:

```bash
docker compose logs -f app
```

Ожидаемый признак нормального старта:
- лог `application initialized`

### 6. Локальный запуск без Docker

Нужен доступный PostgreSQL и экспортированные переменные окружения.

```bash
export $(grep -v '^#' .env | xargs)
go run ./cmd/bot
```

### 7. Первая проверка после запуска

1. Убедиться, что контейнер `postgres` поднялся и healthy.
2. Убедиться, что приложение стартовало без ошибок `Access denied` или `invalid access_token`.
3. Написать несколько сообщений в беседу, куда добавлено сообщество.
4. Если включен ручной запуск, отправить команду:

```text
/livanda
```

Ожидаемое поведение:
- если осмысленные сообщения есть, бот публикует summary в эту же беседу
- если сообщений пока мало, бот честно напишет, что summary пока не из чего собирать

### 8. Что делать, если не работает

#### `vk api error 5: invalid access_token`

Обычно это значит:
- токен введен с ошибкой
- токен не от того сообщества
- используется не ключ сообщества

#### `vk api error 15: Access denied`

Обычно это значит:
- `VK_GROUP_ID` не совпадает с сообществом токена
- у ключа нет права `messages`
- не включен `Long Poll API`
- не включены `Сообщения сообщества`

#### Бот не видит сообщения из беседы

Проверь:
- сообщество точно добавлено в эту беседу
- у сообщества есть доступ к этой беседе
- сообщение отправлено уже после того, как бот был добавлен и long poll стартовал
- при необходимости выдай сообществу повышенные права в самой беседе

#### Summary не публикуется

Проверь:
- накопилось ли достаточно осмысленных сообщений для `SUMMARY_BATCH_SIZE`
- не уперся ли LLM в ограничение по частоте запросов
- корректно ли заполнены `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`
- что в логах нет таймаутов чтения ответа от LLM

#### Summary публикуется без картинки

Проверь:
- включен ли `SUMMARY_IMAGE_ENABLED=true`
- корректно ли выбран `SUMMARY_IMAGE_PROVIDER`: `cloudflare` или `yandex_art`
- для Cloudflare заполнены ли `SUMMARY_IMAGE_API_KEY`, `SUMMARY_IMAGE_ACCOUNT_ID`, `SUMMARY_IMAGE_MODEL`, `SUMMARY_IMAGE_WIDTH`, `SUMMARY_IMAGE_HEIGHT`
- для YandexART заполнены ли `SUMMARY_IMAGE_API_KEY` или `LLM_API_KEY`, `SUMMARY_IMAGE_FOLDER_ID`, `SUMMARY_IMAGE_MODEL`, `SUMMARY_IMAGE_WIDTH_RATIO`, `SUMMARY_IMAGE_HEIGHT_RATIO`
- что в логах нет ошибок генерации изображения или загрузки фото в VK

Если генерация изображения или загрузка attachment в VK падает, бот не теряет summary: он отправляет текст без картинки и пишет предупреждение в лог.



## Почему выбран VK Bots Long Poll

Для MVP выбран `Bots Long Poll API` сообщества.

Почему:
- не нужен внешний webhook-адрес
- легко поднимается локально через `docker compose`
- меньше инфраструктурных деталей, чем callback-сервер
- достаточно для чтения `message_new` и публикации summary обратно в чат

Компромисс:
- это один long-poll обработчик внутри приложения; для MVP это проще и надежнее, чем callback + отдельная очередь



## Архитектура

- `cmd/bot` - точка входа
- `internal/config` - конфигурация из переменных окружения
- `internal/storage` - PostgreSQL, миграции, advisory lock, репозиторий
- `internal/vk` - клиент VK, long poll обработчик, публикация сообщений
- `internal/summary` - фильтрация сообщений, сборка prompt, логика формирования summary и prompt для изображения
- `internal/llm` - интерфейс LLM и адаптеры `stub` / `openai_compat`
- `internal/imagegen` - клиенты Cloudflare/YandexART и post-processing изображения с watermark `DDD`
- `internal/app` - сборка приложения и жизненный цикл
- `migrations` - SQL миграции, встроенные в бинарник

## Основные сущности БД

### `messages`
- `source_message_id`
- `conversation_message_id`
- `chat_id`
- `peer_id`
- `sender_id`
- `text`
- `sent_at`
- `received_at`
- `is_outgoing`
- служебные timestamps

Сообщения хранятся отдельно по каждой беседе через `peer_id`.
Идемпотентность приема сообщений обеспечивается уникальностью `(peer_id, source_message_id)`.

### `processed_summary_batches`
- `chat_id`
- `peer_id`
- `first_message_id`
- `last_message_id`
- `raw_message_count`
- `meaningful_message_count`
- `summary_text`
- `issue_number`
- `llm_provider`
- `trigger_source`
- `published_at`

Повторная публикация диапазона блокируется уникальностью `(peer_id, first_message_id, last_message_id)`.

### `summary_issue_counters`
- `peer_id`
- `next_issue_number`
- `updated_at`

Номер выпуска считается отдельно для каждой беседы по `peer_id`. Сейчас номер хранится как метаданные публикации и не рисуется на картинке.

## Сквозной поток данных

1. Приложение поднимает Postgres pool и применяет миграции.
2. VK long poll получает `message_new` события от сообщества для всех бесед, куда оно добавлено.
3. Сообщения каждой беседы сохраняются в таблицу `messages` со своим `peer_id`; для ответов сохраняется короткий reply context: id исходного сообщения, автор и preview текста.
4. После каждого нового сообщения приложение проверяет, накопилось ли `SUMMARY_BATCH_SIZE` осмысленных сообщений после прошлого summary.
5. Сервис summary:
   - определяет следующий необработанный диапазон сообщений
   - берет PostgreSQL advisory lock на диапазон
   - проверяет, не было ли уже успешной публикации
   - читает сообщения диапазона
   - фильтрует мусор, пустые и слишком короткие сообщения
   - для автопубликации ждет, пока накопится `SUMMARY_BATCH_SIZE` осмысленных сообщений
   - для ручной команды управляющего бота может выпустить summary раньше
   - строит prompt с последним опубликованным summary как контекстом непрерывности
   - передает reply context в prompt, чтобы модель понимала, кому и на что отвечали
   - при превышении лимита prompt прореживает строки сообщений, сохраняя хронологический порядок
   - вызывает `GenerateSummary(ctx, input)`
   - резервирует номер выпуска для текущего `peer_id`
   - если включены изображения, строит отдельный безопасный prompt по готовому summary
   - генерирует одну цветную noir-иллюстрацию без текста на самой картинке
   - после получения изображения накладывает watermark `DDD` в правый нижний угол без фоновой плашки
   - публикует summary в чат, по возможности одним сообщением с attachment
   - если изображение не удалось сгенерировать или загрузить в VK, публикует только текст summary
   - после успешной публикации сохраняет диапазон в `processed_summary_batches`
   - сразу после этого забывает обработанные сообщения, удаляя их из `messages`
   - если LLM упирается в лимит, бот пишет сообщение об ограничении, не теряет контекст и сдвигает следующую автопопытку еще на `SUMMARY_BATCH_SIZE` осмысленных сообщений

Если LLM или VK недоступны, диапазон не считается успешно обработанным и будет повторно обработан позже.

## Управляющий бот

Дополнительно поддержан ручной запуск summary по команде в чате.

Отладочная команда `/livanda-debug` доступна тем же пользователям из `MANUAL_TRIGGER_USER_IDS`. Она публикует в чат LLM usage: текущую модель, ping до VK API, текущие сутки с 00:00 МСК, каждый день последних 7 дней и суммарную статистику за последние 30 дней. В ответ также прикладывается PNG-график input/output tokens за последние 7 дней в разбивке по датам.

Как это работает:
- в `.env` задается `MANUAL_TRIGGER_USER_IDS` (список id через запятую)
- в `.env` задается `MANUAL_TRIGGER_COMMAND`; в текущем примере используется `/livanda`
- только пользователи или управляющие боты из этого списка VK `user_id` могут запускать summary вручную
- команда работает в той беседе, где ее отправили
- автоматический summary публикуется отдельно в каждой беседе после каждых `SUMMARY_BATCH_SIZE` осмысленных сообщений
- ручной запуск выпускает summary по текущему необработанному хвосту, даже если до `SUMMARY_BATCH_SIZE` еще не дошли
- если новых осмысленных сообщений нет, бот скажет об этом

Важно:
- это не принудительная публикация
- ручная команда не ломает идемпотентность и не публикует один и тот же диапазон повторно
- после успешного summary обработанные сообщения удаляются из `messages`, но последние опубликованные summary остаются в `processed_summary_batches` для контекста непрерывности
- при почасовом лимите LLM бот пишет об этом в чат, сохраняет контекст и переносит следующую автопопытку на еще один батч сообщений

## Как идентифицировать нужную беседу

Бот обрабатывает все групповые беседы, куда его добавили.

## Фильтрация и ограничение контекста

Без сложного машинного отбора используются простые эвристики:
- убрать пустые сообщения
- убрать слишком короткие сообщения
- убрать сообщения, состоящие только из ссылки
- убрать сообщения без букв и цифр

Ограничение контекста:
- `SUMMARY_MAX_CONTEXT_MESSAGES`
- `SUMMARY_MAX_CONTEXT_CHARS`
- если prompt получается слишком большим, строки сообщений равномерно прореживаются и остаются в хронологическом порядке
- последний опубликованный summary передается в prompt только как контекст непрерывности, а не как источник новых фактов
- reply context передается коротким preview без обязанности цитировать исходное сообщение

Это сознательный компромисс MVP: лучше держать prompt в допустимом размере, чем строить сложную систему ранжирования. Для каждой беседы хранится только ограниченное число последних опубликованных summary.

## LLM интеграция

Интерфейс:

```go
type Client interface {
    GenerateSummary(ctx context.Context, input GenerateSummaryInput) (GenerateSummaryOutput, error)
    Provider() string
}
```

Есть два режима:
- `LLM_PROVIDER=stub` - локальная заглушка по умолчанию
- `LLM_PROVIDER=openai_compat` - адаптер для chat-completions API, включая OpenRouter, Fireworks, Yandex AI Cloud и другие совместимые API

Что нужно задать для рабочей интеграции:
- подставить реальный `LLM_API_KEY`
- подставить реальную модель `LLM_MODEL`
- при необходимости заменить адаптер на конкретного провайдера без переписывания summary-логики

Для Yandex AI Cloud адаптер умеет передавать `reasoning_effort`: для `gpt-oss` используется `medium`, для `qwen` - `none`.

## Генерация изображений

Изображения включаются отдельно через `SUMMARY_IMAGE_ENABLED=true`. Поддерживаются два провайдера:
- `SUMMARY_IMAGE_PROVIDER=cloudflare` - синхронный вызов Cloudflare Workers AI; нужны `SUMMARY_IMAGE_ACCOUNT_ID`, `SUMMARY_IMAGE_MODEL`, `SUMMARY_IMAGE_WIDTH`, `SUMMARY_IMAGE_HEIGHT`
- `SUMMARY_IMAGE_PROVIDER=yandex_art` - асинхронная генерация YandexART; нужны `SUMMARY_IMAGE_FOLDER_ID`, `SUMMARY_IMAGE_MODEL`, `SUMMARY_IMAGE_WIDTH_RATIO`, `SUMMARY_IMAGE_HEIGHT_RATIO`, `SUMMARY_IMAGE_POLL_INTERVAL`

Общие переменные:
- `SUMMARY_IMAGE_BASE_URL` - базовый URL API провайдера
- `SUMMARY_IMAGE_API_KEY` - ключ image-провайдера; если не задан, используется `LLM_API_KEY`
- `SUMMARY_IMAGE_TIMEOUT` - общий timeout генерации
- `SUMMARY_IMAGE_PROMPT_MAX_CHARS` - максимальная длина визуального prompt после сжатия

Пайплайн изображения:
- summary сначала генерируется как текст
- затем LLM делает короткий визуальный prompt по готовому summary
- image-провайдер генерирует одну цельную цветную noir-сцену без текста, заголовков и логотипов
- watermark `DDD` накладывается кодом после генерации: крайние `D` белые, центральная `D` красная, внизу красное подчёркивание
- картинка не сохраняется на диск; байты держатся в памяти только до загрузки в VK


## Идемпотентность и отказоустойчивость

- прием сообщений идемпотентен на уровне БД
- публикация summary использует `advisory lock` в PostgreSQL
- опубликованные диапазоны записываются в отдельную таблицу
- номера выпусков считаются отдельно по каждой беседе через `summary_issue_counters`
- старые опубликованные summary по беседе автоматически обрезаются до небольшого окна хранения
- диапазон считается обработанным только после успешной публикации
- после успешной публикации обработанные сообщения удаляются из `messages`
- long poll при временных сбоях переподключается
- управляющая команда использует ту же summary-логику и те же гарантии

## Автопостинг push в VK

В репозитории есть GitHub Actions workflow `.github/workflows/vk-wall-post.yml`. На каждый `push` в `main` он публикует на стене сообщества короткий digest: короткий hash коммита и название коммита. Ссылки в пост не добавляются.

Workflow можно запустить вручную через `workflow_dispatch`. Для ручного запуска доступны inputs:
- `base_ref` - нижняя граница диапазона, не включается; если пусто, берутся коммиты из всей истории `head_ref`
- `head_ref` - верхняя граница, по умолчанию `main`
- `limit` - максимум строк с коммитами в посте, по умолчанию `50`

Для работы нужно добавить в GitHub repository secret:
- `VK_ACCESS_TOKEN` - ключ сообщества VK с правом `wall`

И GitHub repository variables:
- `VK_GROUP_ID` - числовой id сообщества без минуса
- `VK_API_VERSION` - версия VK API; если не задана, используется `5.199`

Если secret или variable не заданы, workflow завершится успешно и просто пропустит публикацию.

### LLM usage

После успешной публикации summary бот пишет в лог и сохраняет в `processed_summary_batches` счетчики токенов, модель и latency LLM, если провайдер вернул `usage`. Команда `/livanda-debug` дополнительно показывает количество summary и уникальных чатов за период, а также отправляет график input/output за последние 7 дней:
- `llm_model` - модель, использованная для summary
- `llm_prompt_tokens` - input/prompt tokens
- `llm_cached_prompt_tokens` - cached input tokens, если OpenAI-совместимый провайдер вернул это поле
- `llm_completion_tokens` - output/completion tokens, включая reasoning-токены у reasoning-моделей
- `llm_latency_ms` - длительность LLM-запроса

Пример быстрой проверки расхода за последние 7 дней:

```sql
SELECT
    date_trunc('day', published_at) AS day,
    llm_provider,
    COUNT(DISTINCT peer_id) AS chats,
    SUM(llm_prompt_tokens) AS input_tokens,
    SUM(llm_completion_tokens) AS output_tokens
FROM processed_summary_batches
WHERE published_at >= NOW() - INTERVAL '7 days'
GROUP BY 1, 2
ORDER BY 1 DESC, 2;
```

## Безопасность и эксплуатация

- все внешние вызовы используют `context.Context`
- у HTTP и БД есть таймауты
- логи структурированные через `slog`
- токены не логируются
- полный сырой prompt в логах не печатается
- сгенерированные изображения не пишутся в файловую систему приложения
- `.env`, backup-файлы, cache, логи и локальные данные БД/Redis должны оставаться вне Git

## Что проверить перед рабочим запуском

- вставить реальный `VK_ACCESS_TOKEN` сообщества
- проверить, что сообщество добавлено в нужный чат и имеет доступ к `message_new`
- при использовании внешней LLM заполнить `LLM_API_KEY`, `LLM_BASE_URL`, `LLM_MODEL`
- при включении изображений заполнить переменные `SUMMARY_IMAGE_*` для выбранного провайдера
- при необходимости подкрутить prompt и фильтры под конкретный чат

# bot-summary-vk

[![Quality and security](https://github.com/Haeniken/vk_chat_digest_bot/actions/workflows/quality.yml/badge.svg)](https://github.com/Haeniken/vk_chat_digest_bot/actions/workflows/quality.yml)
[![Go version](https://img.shields.io/github/go-mod/go-version/Haeniken/vk_chat_digest_bot?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Бот для бесед ВКонтакте. Он читает сообщения из чатов, куда добавлено сообщество, ведет отдельный контекст по каждой беседе и публикует дайджест после заданного количества осмысленных сообщений. Дайджест можно выпустить вручную командой администратора.

Поддерживаются разные LLM через OpenAI-совместимый API. Иллюстрации к дайджестам можно включить отдельно.

## Оглавление

- [Установка и запуск](#установка-и-запуск)
- [Переменные окружения](#переменные-окружения)
- [Настройка VK](#настройка-vk)
- [Настройка LLM](#настройка-llm)
- [Изображения к дайджестам](#изображения-к-дайджестам)
- [Команды](#команды)
- [Проверка работы](#проверка-работы)
- [Если что-то не работает](#если-что-то-не-работает)
- [Документация проекта](#документация-проекта)
- [Конфиденциальность](#конфиденциальность)
- [Безопасность](#безопасность)
- [Участие в разработке](#участие-в-разработке)
- [История изменений](#история-изменений)
- [Лицензия](#лицензия)

## Установка и запуск

1. Склонировать репозиторий и перейти в директорию проекта:

```bash
git clone https://github.com/Haeniken/vk_chat_digest_bot.git
cd vk_chat_digest_bot
```

2. Создать `.env`:

```bash
cp .env.example .env
```

3. Заполнить обязательные переменные:

```env
VK_GROUP_ID=237254188
VK_ACCESS_TOKEN=...

POSTGRES_DB=vk_digest
POSTGRES_USER=vk_digest
POSTGRES_PASSWORD=...
DATABASE_URL=postgres://vk_digest:...@postgres:5432/vk_digest?sslmode=disable

LLM_PROVIDER=openai_compat
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=...
LLM_MODEL=gpt-5.3-chat-latest
SUMMARY_HISTORY_RETENTION_DAYS=90
MESSAGE_RETENTION_DAYS=90
```

4. Если нужен ручной запуск дайджеста, указать администраторов:

```env
MANUAL_TRIGGER_USER_IDS=123456789,987654321
MANUAL_TRIGGER_COMMAND=/livanda
DEBUG_COMMAND=/livanda-debug
```

5. Запустить:

```bash
docker compose up --build -d
```

6. Проверить логи:

```bash
docker compose logs -f app
```

Нормальный старт сопровождается строкой `application initialized`.

## Переменные окружения

Значения по умолчанию совпадают с `.env.example` и настройками в коде. Если в колонке `По умолчанию` стоит `обязательно`, переменную нужно заполнить для рабочего запуска.

### Application

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `APP_ENV` | `dev` | Имя окружения. Сейчас используется как служебная метка конфигурации. |
| `LOG_LEVEL` | `INFO` | Уровень логов: `DEBUG`, `INFO`, `WARN`, `ERROR`. |
| `HTTP_TIMEOUT` | `20s` | Общий HTTP timeout из старой конфигурации. Для VK/LLM/image есть отдельные timeout-переменные. |
| `PATH` | `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` | Runtime path внутри контейнера. Обычно не меняется. |
| `SSL_CERT_FILE` | `/etc/ssl/certs/ca-certificates.crt` | CA bundle для HTTPS-проверок Go/TLS внутри контейнера. |

### Database

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `DATABASE_URL` | обязательно | PostgreSQL DSN для приложения. |
| `DB_MAX_CONNS` | `10` | Максимум соединений pgx pool приложения. |
| `DB_MIN_CONNS` | `1` | Минимум соединений pgx pool приложения. |
| `DB_CONNECT_TIMEOUT` | `5s` | Timeout подключения к PostgreSQL. |
| `DB_QUERY_TIMEOUT` | `5s` | Timeout обычных запросов к PostgreSQL. |
| `POSTGRES_DB` | `example_db` | Имя БД, которую создает postgres-контейнер. |
| `POSTGRES_USER` | `example_user` | Пользователь postgres-контейнера. |
| `POSTGRES_PASSWORD` | `example_password` | Пароль postgres-контейнера. |

### VK

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `VK_GROUP_ID` | обязательно | Числовой id сообщества без `club` и без минуса. |
| `VK_ACCESS_TOKEN` | обязательно | Ключ доступа сообщества VK с правом `messages`. |
| `VK_API_VERSION` | `5.199` | Версия VK API. |
| `VK_LONGPOLL_WAIT` | `25` | `wait` для VK Long Poll, секунды. |
| `VK_REQUEST_TIMEOUT` | `20s` | Timeout запросов к VK API. |
| `VK_SEND_RANDOM_ID` | `0` | Fallback `random_id` для отправки сообщений. Для summary используется детерминированный id. |

### Commands

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `MANUAL_TRIGGER_USER_IDS` | пусто | Список VK user id администраторов через запятую. |
| `MANUAL_TRIGGER_COMMAND` | `/summary` | Команда ручного выпуска дайджеста. |
| `DEBUG_COMMAND` | `/livanda-debug` | Команда статистики и диагностики. |

### Summary

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `SUMMARY_BATCH_SIZE` | `200` | Сколько осмысленных сообщений нужно для автодайджеста. |
| `SUMMARY_MAX_CONTEXT_CHARS` | `12000` | Максимальный размер текущего контекста сообщений. |
| `SUMMARY_MAX_CONTEXT_MESSAGES` | `200` | Максимум текущих сообщений в prompt. |
| `SUMMARY_MIN_MESSAGE_LENGTH` | `3` | Минимальная длина сообщения для фильтрации мусора. |
| `SUMMARY_HISTORY_RETENTION_DAYS` | `90` | Сколько дней хранить тексты опубликованных дайджестов и диапазоны публикаций. |
| `MESSAGE_RETENTION_DAYS` | `90` | Сколько дней хранить сырые необработанные сообщения, если они не попали в дайджест. |

### Text LLM

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `LLM_PROVIDER` | `stub` | Провайдер текста: `stub` или `openai_compat`. |
| `LLM_MODEL` | `stub-sarcasm-v1` | Модель для дайджеста. |
| `LLM_BASE_URL` | пусто | Base URL OpenAI-совместимого API. Обязателен для `openai_compat`. |
| `LLM_API_KEY` | пусто | API key LLM-провайдера. Обязателен для `openai_compat`. |
| `LLM_REQUEST_TIMEOUT` | `600s` | Timeout LLM-запроса. |
| `LLM_MAX_RETRIES` | `2` | Количество повторов временных ошибок. |
| `LLM_RETRY_BASE_DELAY` | `2s` | Базовая задержка между retry. |
| `LLM_TEMPERATURE` | `0.3` | Температура генерации текста. |
| `LLM_MAX_OUTPUT_TOKENS` | `220` | Максимум output tokens для текста. |
| `LLM_PROMPT_MAX_CHARS` | `12000` | Максимальный размер prompt. |

### Summary Images

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `SUMMARY_IMAGE_ENABLED` | `false` | Включить изображение к дайджесту. |
| `SUMMARY_IMAGE_PROVIDER` | `yandex_art` | Провайдер изображений: `openai`, `cloudflare`, `yandex_art`. |
| `SUMMARY_IMAGE_BASE_URL` | `https://ai.api.cloud.yandex.net` | Base URL image API. |
| `SUMMARY_IMAGE_API_KEY` | `LLM_API_KEY` | API key image-провайдера. Если пусто, используется `LLM_API_KEY`. |
| `SUMMARY_IMAGE_ACCOUNT_ID` | пусто | Cloudflare account id. Обязателен для `cloudflare`. |
| `SUMMARY_IMAGE_MODEL` | `yandex-art` | Модель image-провайдера. |
| `SUMMARY_IMAGE_QUALITY` | `medium` | Качество OpenAI image: `auto`, `low`, `medium`, `high`. |
| `SUMMARY_IMAGE_TIMEOUT` | `90s` | Timeout генерации изображения. |
| `SUMMARY_IMAGE_POLL_INTERVAL` | `3s` | Интервал polling для YandexART. |
| `SUMMARY_IMAGE_WIDTH` | `1024` | Ширина для OpenAI/Cloudflare. |
| `SUMMARY_IMAGE_HEIGHT` | `1024` | Высота для OpenAI/Cloudflare. |
| `SUMMARY_IMAGE_WIDTH_RATIO` | `1` | Соотношение ширины для YandexART. |
| `SUMMARY_IMAGE_HEIGHT_RATIO` | `1` | Соотношение высоты для YandexART. |
| `SUMMARY_IMAGE_FOLDER_ID` | из `LLM_MODEL` | Yandex folder id. Может извлекаться из `gpt://<folder_id>/...`. |
| `SUMMARY_IMAGE_PROMPT_MAX_CHARS` | `1200` | Максимальная длина prompt для image-провайдера. |

### Image Prompt LLM

| Переменная | По умолчанию | Описание |
| --- | --- | --- |
| `SUMMARY_IMAGE_PROMPT_LLM_PROVIDER` | `LLM_PROVIDER` | Отдельный LLM-провайдер для подготовки image prompt. |
| `SUMMARY_IMAGE_PROMPT_LLM_MODEL` | зависит от LLM | Модель для image prompt. Для OpenAI base URL default `gpt-5.4-nano`, иначе основная `LLM_MODEL`. |
| `SUMMARY_IMAGE_PROMPT_LLM_BASE_URL` | `LLM_BASE_URL` | Base URL image-prompt LLM. |
| `SUMMARY_IMAGE_PROMPT_LLM_API_KEY` | `LLM_API_KEY` | API key image-prompt LLM. |
| `SUMMARY_IMAGE_PROMPT_LLM_REQUEST_TIMEOUT` | `120s` | Timeout image-prompt LLM. |
| `SUMMARY_IMAGE_PROMPT_LLM_MAX_RETRIES` | `LLM_MAX_RETRIES` | Количество retry. |
| `SUMMARY_IMAGE_PROMPT_LLM_RETRY_BASE_DELAY` | `LLM_RETRY_BASE_DELAY` | Базовая задержка retry. |
| `SUMMARY_IMAGE_PROMPT_LLM_TEMPERATURE` | `0.4` | Температура image-prompt LLM. |
| `SUMMARY_IMAGE_PROMPT_LLM_MAX_OUTPUT_TOKENS` | `220` | Максимум output tokens для image prompt. |
| `SUMMARY_IMAGE_PROMPT_LLM_PROMPT_MAX_CHARS` | `LLM_PROMPT_MAX_CHARS` | Максимальный размер prompt image-prompt LLM. |

## Настройка VK

Бот работает от имени сообщества через `Bots Long Poll API`.

В панели управления сообществом нужно включить:

- `Сообщения сообщества`
- `Возможности ботов`
- `Разрешать добавлять сообщество в беседы`
- `Long Poll API`
- событие `message_new`

Для ключа доступа сообщества нужно право `messages`.

Важно:

- нужен ключ сообщества, не пользовательский токен;
- `VK_GROUP_ID` должен быть id того же сообщества, для которого выпущен токен;
- сообщество нужно добавить в каждую беседу, где должен работать бот;
- в каждой беседе сообществу обязательно нужно выдать права администратора.

Если ссылка на сообщество выглядит как `club237254188`, то:

```env
VK_GROUP_ID=237254188
```

## Настройка LLM

Для тестового запуска без внешнего API можно оставить заглушку:

```env
LLM_PROVIDER=stub
LLM_MODEL=stub-sarcasm-v1
```

Для реальной работы используется OpenAI-совместимый режим:

```env
LLM_PROVIDER=openai_compat
LLM_BASE_URL=https://api.openai.com/v1
LLM_API_KEY=...
LLM_MODEL=gpt-5.3-chat-latest
LLM_TEMPERATURE=1
LLM_MAX_OUTPUT_TOKENS=10000
LLM_REQUEST_TIMEOUT=600s
```

Можно использовать любой сервис с совместимым chat-completions API: OpenAI, OpenRouter, Yandex AI Cloud и другие. Обычно меняются только `LLM_BASE_URL`, `LLM_API_KEY` и `LLM_MODEL`.

Пример для OpenRouter:

```env
LLM_PROVIDER=openai_compat
LLM_BASE_URL=https://openrouter.ai/api/v1
LLM_API_KEY=...
LLM_MODEL=google/gemma-4-31b-it:free
```

## Изображения к дайджестам

По умолчанию изображения отключены:

```env
SUMMARY_IMAGE_ENABLED=false
```

Чтобы включить генерацию:

```env
SUMMARY_IMAGE_ENABLED=true
SUMMARY_IMAGE_PROVIDER=openai
SUMMARY_IMAGE_MODEL=gpt-image-1-mini
SUMMARY_IMAGE_QUALITY=medium
SUMMARY_IMAGE_WIDTH=1024
SUMMARY_IMAGE_HEIGHT=1024
```

Поддерживаемые провайдеры:

- `openai`
- `cloudflare`
- `yandex_art`

Для подготовки промпта изображения можно использовать отдельную LLM:

```env
SUMMARY_IMAGE_PROMPT_LLM_PROVIDER=openai_compat
SUMMARY_IMAGE_PROMPT_LLM_BASE_URL=https://api.openai.com/v1
SUMMARY_IMAGE_PROMPT_LLM_API_KEY=...
SUMMARY_IMAGE_PROMPT_LLM_MODEL=gpt-5.4-nano
```

Если `SUMMARY_IMAGE_API_KEY` пустой, бот использует `LLM_API_KEY`.

## Команды

Команды доступны только пользователям из `MANUAL_TRIGGER_USER_IDS`.

`/livanda` или значение `MANUAL_TRIGGER_COMMAND`:

- выпускает дайджест по текущей беседе;
- не публикует повторно уже обработанный диапазон сообщений;
- сообщает, если новых сообщений недостаточно.

`/livanda-debug` или значение `DEBUG_COMMAND`:

- показывает текущую LLM-модель;
- показывает ping до VK;
- показывает расходы text/images за месяц;
- отправляет график input/output токенов за последние 7 дней;
- по кнопке администратора раскрывает подробную статистику.

Автоматический дайджест публикуется после `SUMMARY_BATCH_SIZE` осмысленных сообщений в каждой беседе отдельно.

## Проверка работы

1. Убедиться, что `postgres` поднялся и healthy:

```bash
docker compose ps
```

2. Проверить логи приложения:

```bash
docker compose logs -f app
```

3. Написать несколько сообщений в беседу, куда добавлено сообщество.

4. Если настроен ручной запуск, отправить команду:

```text
/livanda
```

5. Для проверки статистики отправить:

```text
/livanda-debug
```

## Если что-то не работает

`vk api error 5: invalid access_token`

Проверь, что токен скопирован без ошибки, выпущен для нужного сообщества и является ключом сообщества.

`vk api error 15: Access denied`

Проверь `VK_GROUP_ID`, право `messages`, включенный Long Poll API и сообщения сообщества.

Бот не видит сообщения из беседы

Проверь, что сообщество добавлено в беседу с правами администратора, а сообщения отправлены уже после запуска бота. Права администратора обязательны в каждой беседе.

Дайджест не публикуется

Проверь `SUMMARY_BATCH_SIZE`, настройки LLM и ошибки в логах. Если LLM вернула лимит или временную ошибку, контекст сохраняется и команду можно повторить позже.

Дайджест выходит без картинки

Проверь `SUMMARY_IMAGE_ENABLED`, `SUMMARY_IMAGE_PROVIDER`, ключи и модель image-провайдера. Если генерация или загрузка изображения в VK падает, бот отправляет текст без картинки.

## Документация проекта

- [Архитектура](docs/architecture.md)
- [Эксплуатация](docs/operations.md)
- [Конфиденциальность и обработка данных](docs/privacy.md)

## Конфиденциальность

Бот сохраняет сообщения бесед в PostgreSQL и может передавать их содержимое
настроенному LLM-провайдеру. При включенной генерации изображений производный
визуальный prompt также отправляется выбранному image-провайдеру. До запуска
оператор должен уведомить участников беседы и определить допустимые сроки
хранения данных.

Состав сохраняемых и передаваемых данных, фактические сроки хранения, логи и
процедура удаления описаны в документе
[«Конфиденциальность и обработка данных»](docs/privacy.md).

## Безопасность

Не публикуйте сведения об уязвимостях в открытых Issue. Используйте инструкции
и приватные контакты из [политики безопасности](.github/SECURITY.md).

## Участие в разработке

Правила подготовки изменений, обязательные проверки и порядок оформления Pull
Request описаны в [руководстве для участников](.github/CONTRIBUTING.md).

## История изменений

Основные этапы развития и связанные коммиты перечислены в
[CHANGELOG.md](CHANGELOG.md).

## Лицензия

Проект распространяется на условиях [MIT License](LICENSE): код можно
использовать, изменять и распространять, сохраняя текст лицензии и уведомление
об авторских правах.

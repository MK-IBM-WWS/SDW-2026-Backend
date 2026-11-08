# Индексы в БД — бэкенд

Go + Gin + GORM, PostgreSQL, Redis, MinIO. Основа — приложенный бэкенд ЛР4.
Фронтенд находится в отдельной папке `SDW-2026-Frontend`.

## Что добавлено

- `GET /api/tariffs?page=1&limit=10&priceLimit=1000` возвращает только одну страницу. Значения по умолчанию: `page=1`, `limit=10`; допустимый `limit`: 1..100. Неверные/повторяющиеся параметры дают HTTP 400. За концом списка возвращается пустой `data` с настоящим `total`.
- Ответ содержит `status`, `data`, `total`, `page`, `limit`, `query_ms`, `index_enabled`. Фильтр применяется и к `COUNT`, и к странице; порядок — цена, затем ID. `is_owner`/`is_liked` рассчитываются для текущего JWT.
- `query_ms` — реальное суммарное время подсчёта и выборки в репозитории, включая загрузку автора. Сетевые расходы браузера, проверка индекса и получение ссылок MinIO сюда не включены. Список не кешируется в Redis.
- Частичный B-tree индекс `idx_cloud_tariffs_published_price` по `(price_per_month, tariff_id)` для опубликованных тарифов. Включение и отключение выполняются отдельно от HTTP API через `cmd/indexes`; приложение продолжает работать.
- `cmd/seed` создаёт учебный набор из **120000 опубликованных тарифов**. Повторный запуск не добавляет существующие `Lab-PAG-...` заново. Рабочие данные не удаляются, учебные тарифы принадлежат `student`. Цена случайная: 100..500000; RAM: 1..128 ГБ. У медиа пустые имена, поэтому используются прежние локальные заглушки. Приблизительно 0,18% набора проходит фильтр «до 1000 ₽».
- Генератор написан на Go и использует PostgreSQL `generate_series`/`random` для пакетной вставки. Это выполняет задачу генерации из PAG.md без дополнительной библиотеки faker и 120000 отдельных INSERT.

## Запуск на Fedora

Нужны Go 1.25+, Node.js 22.12+ или 24+, Docker Engine и Compose v2.
Если Go/Node.js ещё нет: `sudo dnf install golang nodejs`.
Проверьте `go version`, `node --version`, `npm --version`.
Установка Docker для Fedora: https://docs.docker.com/engine/install/fedora/ .
Если он уже установлен, переустанавливать его не нужно.

Из этой папки:

```bash
sudo docker compose up -d --wait postgres redis minio adminer
go mod download
go run ./cmd/migrate
go run ./cmd/seed
go run ./cmd/tariffs
```

`.env` включён и содержит прежние учебные параметры: PostgreSQL `localhost:666`, Redis `localhost:6379`, MinIO `localhost:9000`, API `localhost:8080`.
Второй терминал, из соседней папки `SDW-2026-Frontend`: `npm ci`, затем `npm run dev`.
Открыть http://localhost:3000 . Учебный вход: `student` / `student123` (для новой БД; миграция не меняет настоящий пароль существующего аккаунта).
Swagger: http://localhost:8080/swagger/index.html . Adminer: http://localhost:8081 , сервер `postgres`, БД `cloud_tariffs`, пользователь `cloud_user`, пароль `cloud_password`.
Минимально нужны только `postgres redis minio`; Adminer оставлен для просмотра БД.

Остановка Go/Vite: Ctrl+C. Остановка контейнеров: `sudo docker compose stop`.
Тома с данными сохраняются.

## Индекс и измерения

Миграция создаёт индекс. В отдельном терминале из папки бэкенда:

```bash
go run ./cmd/indexes -action disable
go run ./cmd/indexes -action explain -price 1000 -page 1 -limit 10
go run ./cmd/indexes -action enable
go run ./cmd/indexes -action explain -price 1000 -page 1 -limit 10
```

В React выбрать «до 1000 ₽» и применить фильтр кнопкой лупы. Повторное нажатие лупы повторяет тот же GET. Над плиткой отображаются время запросов и наличие индекса. При переключении индекса Go/Vite перезапускать не нужно. Команда `-action status` показывает определения индексов.

`explain` печатает `EXPLAIN (ANALYZE, BUFFERS)` для COUNT и основной выборки страницы гостя с теми же фильтром, сортировкой, LIMIT/OFFSET и подзапросами лайков, что использует API. Загрузка автора GORM отдельным запросом в EXPLAIN не входит.

SQL, эквивалентный включению/отключению:

```sql
CREATE INDEX IF NOT EXISTS idx_cloud_tariffs_published_price
ON cloud_tariffs (price_per_month, tariff_id)
WHERE tariff_status = 'опубликован';

DROP INDEX IF EXISTS idx_cloud_tariffs_published_price;
ANALYZE cloud_tariffs;
```

Сравнивать нужно одинаковые страницу, лимит и цену; лучше несколько повторов каждого запроса. Первое чтение, кеш ОС/БД и нагрузка влияют на время. При широком фильтре планировщик может предпочесть последовательное сканирование — наличие индекса само по себе не гарантирует его использование. Индексы PK, единственного черновика и лайков не отключаются. Повторная миграция снова включает учебный индекс.

## Лента в Redis

React при загрузке любой страницы вызывает `GET /api/tariffs/feed?init=true`. Сервер устанавливает отдельную сессионную HttpOnly cookie `feed_session`. Это идентификатор браузера, а не авторизация. Повторная загрузка восстанавливает существующий набор, пока он живёт в Redis.

Ключи:

- гость: `feed:guest:<session_uuid>:cards`;
- вошедший: `feed:user:<user_id>:<session_uuid>:cards`;
- метаданные: тот же префикс + `:meta`;
- короткая блокировка генерации: тот же префикс + `:lock`.

`cards` — Redis LIST из 100 JSON-объектов `{position, tariff}`: снимок карточки, ID, название, цена, RAM и имена медиа, а не только список идентификаторов. Внутри каждой сотни ID не повторяются. Следующая сотня выбирается заново случайно; между разными сотнями тарифы могут совпадать. Если опубликованных тарифов меньше 100, набор содержит все доступные. При пустой БД ответ `data: null`.

`GET /api/tariffs/feed?position=1` читает первую карточку. После 100-й `position=101` атомарно заменяет набор на следующие 100 с номерами 101..200. Далее 201..300 и т.д. Повтор одного запроса не создаёт ещё один набор. Redis-lock и транзакция защищают от параллельных обращений. Ранее заменённая сотня уже недоступна; откройте `/tariffs/feed` без параметров, чтобы вернуться к началу текущего набора.

`?id=...` открывает конкретный опубликованный тариф из плитки; `?id=...&next=true` переходит к следующему в Redis-порядке, если карточка входит в набор, иначе — к началу набора. Персональные лайки и статус проверяются в БД при выдаче одной карточки; удалённые тарифы пропускаются. Гость не может ставить лайки. Для текущего пользователя набор отделён от гостевого; разные браузерные сессии также отделены.

TTL набора и метаданных — 24 часа с продлением при чтении. После истечения создаётся новый набор с №1. После F5 авторизация, как в ЛР6, сбрасывается в Redux, а cookie гостевой ленты сохраняется. JWT blacklist хранится отдельно и не очищается при обновлении ленты.

Состав ответа: `status`, `data` и `feed` (`session_id`, `redis_key`, `batch_id`, `batch_start`, `batch_size`, `position`). `position=0` означает отдельно открытую из плитки карточку вне сотни.

Просмотр Redis без дополнительных инструментов на хосте:

```bash
sudo docker compose exec redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --scan --pattern "feed:*:cards"'
```

Скопировать нужный ключ из вывода или поля `feed.redis_key` в Network и заменить `<КЛЮЧ>`:

```bash
sudo docker compose exec redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli LLEN "<КЛЮЧ>"'
sudo docker compose exec redis sh -c 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli LRANGE "<КЛЮЧ>" 0 99'
```

## Разработка и проверка

```bash
go test ./...
go vet ./...
```

Тесты: пагинация, валидация лимитов, фильтрация и стабильный порядок, ровно 100 уникальных карточек, изоляция пользователей/сессий, переход 100→101, повтор запроса, одновременная инициализация, TTL, исключение удалённых и актуальность лайков, прежние сценарии авторизации/медиа/публикации.

После изменения Swagger-комментариев:

```bash
go install github.com/swaggo/swag/cmd/swag@v1.16.6
"$(go env GOPATH)/bin/swag" init -g cmd/tariffs/main.go --output docs
cp docs/swagger.json ../SDW-2026-Frontend/src/api/swagger.json
```

Затем во фронтенде `npm run generate-api`. Готовые описания Swagger и Axios-клиент уже включены в архивы, выполнять генерацию для запуска не нужно.

Официальные справочные материалы: [составные индексы PostgreSQL](https://www.postgresql.org/docs/17/indexes-multicolumn.html), [EXPLAIN](https://www.postgresql.org/docs/17/sql-explain.html), [Redis LIST](https://redis.io/docs/latest/develop/data-types/lists/), [Node.js в Fedora](https://developer.fedoraproject.org/tech/languages/nodejs/nodejs.html).

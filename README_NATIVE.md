# Бэкенд для Tauri + CLIP

API Go из приложенного проекта сохранён; добавлены три публичных GET-метода гостя:

- `GET /api/native/tariffs?q=...&priceLimit=...&demo=true&page=1&limit=100` — опубликованные тарифы с поиском по имени/описанию и ценой. `demo=true` выбирает имена `CLIP-%`; `demo=false` — все опубликованные. Нативный интерфейс использует обычный список без ограничения тестовым набором; `semantic=true` выбирает опубликованные описания ASCII длиной 50–100 для CLIP.
- `GET /api/native/tariffs/:id` — свежая карточка «Подробнее», без Redis-кеша.
- `GET /api/native/tariffs/:id/image` — изображение из MinIO или example.jpg через IP API. Приватные/удалённые услуги недоступны; localhost в ссылках клиенту не передаётся.

Фильтры применяются в SQL, параметры передаются отдельно. Английские описания и эмбеддинги не меняют существующую схему БД. Transformers.js работает во фронтенде.

## Запуск на Fedora

Требуется Go 1.25+, Docker Compose. Сохранены `.env` и настройки исходного архива.

```bash
docker compose up -d postgres minio redis adminer
go run ./cmd/migrate
go run ./cmd/seed-clip
go run ./cmd/tariffs
```

API слушает `0.0.0.0:8080`. Найдите LAN IP: `ip -4 -br address`.
Разрешите 8080/tcp в активной зоне firewalld:

```bash
sudo firewall-cmd --add-port=8080/tcp
sudo firewall-cmd --permanent --add-port=8080/tcp
```

Проверка с клиента: `curl 'http://IP_СЕРВЕРА:8080/api/native/tariffs?demo=true&limit=100'`.
Остальные контейнеры исходного compose для этой лабораторной запускать не требуется.

## 100 тестовых записей

`go run ./cmd/seed-clip` добавляет 100 тарифов облачного ИИ: по 10 планов для cat/dog/horse/tiger/elephant/bird/car/bicycle/flower/football.
Длина каждого описания 50–100 символов, текст английский. Прежние тарифы не удаляются и не изменяются. Повторный запуск пропускает существующие записи по имени; изменения цены и описания не затираются. Если переименовать учебную запись, seed посчитает исходное имя отсутствующим и добавит её повторно.

Фото уже включены в `test-images/`; при их отсутствии скрипт скачивает их с источников из `cmd/seed-clip/records.json`. Фото загружаются в приватный MinIO-бакет под `clip/<category>.jpg`. Все 10 планов одной категории используют одну иллюстрацию. При первом запуске миграции/seed Go скачивает зависимости.

Старый `go run ./cmd/seed` для этой лабораторной не нужен: он создаёт более 100000 записей для проверки индексов.

## Проверить изменения БД

В Adminer (`http://IP:8081`) выберите PostgreSQL, сервер `postgres`, пользователя/пароль и БД из `.env`.

```sql
UPDATE cloud_tariffs SET price_per_month=7777, ram_gb=48,
short_description='A photo of a red car parked on a city street, for cloud image recognition.'
WHERE tariff_name='CLIP-tiger-01 · Облачный ИИ: Тигры';
```

В release Tauri подождите до 15 секунд: автоматически обновятся поля и эмбеддинг изменённого описания. Открытая карточка тоже обновляется раз в 15 секунд.
Весь сценарий и сборка Fedora описаны в `../frontend/README_NATIVE.md`.

## Тесты

```bash
go test ./...
go build ./cmd/tariffs ./cmd/seed-clip
```

В тестах покрыты гостевая фильтрация, пагинация, недоступность неопубликованных услуг, безопасный поиск со спецсимволами, детали, изображения, актуальность после UPDATE и 100 английских описаний.

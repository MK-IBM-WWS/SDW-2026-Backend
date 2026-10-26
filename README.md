# Cloud Tariffs App — лабораторная работа №4

Бэкенд на Go/Gin/GORM: PostgreSQL, MinIO, JWT HS256, Redis blacklist, Swagger.

После заполнения JWT_SECRET в `.env`, из корня проекта:

```bash
docker compose up -d postgres minio redis adminer
go mod download
go run ./cmd/migrate
go run ./cmd/tariffs
```

Swagger: http://localhost:8080/swagger/index.html (также открывается через `/`). Вход — POST `/api/users/login`; JWT передаётся через Authorization, cookie не используется.

API содержит ровно 10 методов: 7 для услуг и 3 для пользователей. Прямые ссылки на медиа MinIO действуют 15 минут. Вход и выход полностью реализованы через JWT и Redis blacklist.

Пояснения к коду ЛР4 и термины для защиты: [LAB4_CODE_EXPLAINED.md](LAB4_CODE_EXPLAINED.md). Простые русские комментарии добавлены непосредственно в исходники.


Правки списка тарифов: `GET /api/tariffs` с фильтром `priceLimit` и без него возвращает ID автора в поле `user_id` на верхнем уровне, без `creator`, `creator_id` и логина автора. Другие поля карточки и правила авторизации сохраняются. Swagger обновлён.

Коллекция для скриншотов: [postman/Lab4_Demo.postman_collection.json](postman/Lab4_Demo.postman_collection.json). О настройке автоматических заголовков и ограничении цвета галочек читайте [postman/README.md](postman/README.md).

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

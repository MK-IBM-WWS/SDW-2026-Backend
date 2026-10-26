package main

import (
	"log"

	"cloud-tariffs-backend/internal/api"
)

// ЛР4. Комментарии @ ниже — общие настройки Swagger.
// Строка @BasePath задаёт /api; BearerAuth описывает заголовок Authorization.
// Swagger покажет кнопку Authorize, а настоящую проверку JWT выполнит middleware.
// @title Cloud Tariffs API
// @version 4.0
// @description Каталог облачных тарифов. Публичное чтение, JWT для методов создателя. Регистрация и вход доступны без JWT.
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Введите Bearer и пробел, затем access_token из data ответа POST /users/login.
// Точка входа программы: запускаем сервер; если запуск не удался, выводим причину в журнал.
func main() {
	log.Println("Application start")

	if err := api.StartServer(); err != nil {
		log.Fatalf("application stopped with an error: %v", err)
	}
}

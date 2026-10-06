package web

import (
	"embed"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
)

//go:embed assets/*
var assets embed.FS

func NewApp(api http.Handler, enabled bool) *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ReadTimeout:           15 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
		BodyLimit:             2 << 20,
	})
	app.Use(func(ctx *fiber.Ctx) error {
		ctx.Set("X-Content-Type-Options", "nosniff")
		ctx.Set("X-Frame-Options", "DENY")
		ctx.Set("Referrer-Policy", "same-origin")
		ctx.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		return ctx.Next()
	})
	if enabled {
		app.Get("/", func(ctx *fiber.Ctx) error {
			data, err := assets.ReadFile("assets/index.html")
			if err != nil {
				return fiber.ErrInternalServerError
			}
			ctx.Type("html", "utf-8")
			return ctx.Send(data)
		})
		app.Get("/assets/app.css", func(ctx *fiber.Ctx) error {
			data, err := assets.ReadFile("assets/app.css")
			if err != nil {
				return fiber.ErrInternalServerError
			}
			ctx.Type("css", "utf-8")
			return ctx.Send(data)
		})
		app.Get("/assets/app.js", func(ctx *fiber.Ctx) error {
			data, err := assets.ReadFile("assets/app.js")
			if err != nil {
				return fiber.ErrInternalServerError
			}
			ctx.Type("js", "utf-8")
			return ctx.Send(data)
		})
	}
	if api != nil {
		apiHandler := adaptor.HTTPHandler(api)
		app.All("/api", apiHandler)
		app.All("/api/*", apiHandler)
	}
	app.Use(func(ctx *fiber.Ctx) error {
		return ctx.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "route not found"})
	})
	return app
}

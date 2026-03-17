package handler

import (
	"github.com/gofiber/fiber/v2"
)

// RegisterStaticRoutes serves the demo UI from the web/ directory.
func RegisterStaticRoutes(app *fiber.App) {
	app.Static("/", "./web", fiber.Static{
		Index: "index.html",
	})
}

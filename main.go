package main

import (
    "log"
    "os"

    "github.com/cristianadrielbraun/qrcreator.link/internal/handlers"
    "github.com/cristianadrielbraun/qrcreator.link/web/pages"
    "github.com/gin-gonic/gin"
)

func main() {
    gin.SetMode(gin.ReleaseMode)
    r := gin.New()
    r.Use(gin.Logger())
    r.Use(gin.Recovery())

    // Static assets
    r.Static("/web/static", "web/static")
    r.Static("/web/assets", "web/assets")

    // API routes
    h := handlers.New()
    api := r.Group("/api")
    {
        api.GET("/qr", h.QRCodeHandler)
        api.POST("/htmx/toast", h.GenericToast)
    }

    // Pages
    r.GET("/", func(c *gin.Context) {
        if err := pages.HomePage().Render(c.Request.Context(), c.Writer); err != nil {
            c.String(500, err.Error())
        }
    })

    addr := getAddr()
    log.Printf("qrcreator.link listening on %s", addr)
    if err := r.Run(addr); err != nil {
        log.Fatal(err)
    }
}

func getAddr() string {
    if port := os.Getenv("PORT"); port != "" {
        return ":" + port
    }
    return ":8080"
}

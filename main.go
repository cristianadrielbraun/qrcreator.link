package main

import (
    "context"
    "log"
    "net/http"
    "os"

    "github.com/gin-gonic/gin"
    "github.com/cristianadrielbraun/qrcreator.link/internal/handlers"
    "github.com/cristianadrielbraun/qrcreator.link/web/pages"
)

func main() {
    mux := http.NewServeMux()

    // Serve static assets (CSS/JS) from web/static and web/assets.
    mux.Handle("/web/static/", http.StripPrefix("/web/static/", http.FileServer(http.Dir("web/static"))))
    mux.Handle("/web/assets/", http.StripPrefix("/web/assets/", http.FileServer(http.Dir("web/assets"))))

    // API router (Gin) mounted under /api
    gin.SetMode(gin.ReleaseMode)
    api := gin.New()
    // Add request logging for better visibility on /api calls
    api.Use(gin.Logger())
    api.Use(gin.Recovery())

    h := handlers.New()
    api.GET("/qr", h.QRCodeHandler)
    api.POST("/htmx/toast", h.GenericToast)

    // Forward /api/* to Gin engine, trimming the /api prefix for routes
    mux.Handle("/api/", http.StripPrefix("/api", api))

    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        // For now, just render the home page with placeholder content.
        ctx := r.Context()
        if err := pages.HomePage().Render(contextWithRequest(ctx, r), w); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
    })

    addr := getAddr()
    log.Printf("qrcreator.link listening on %s", addr)
    if err := http.ListenAndServe(addr, mux); err != nil {
        log.Fatal(err)
    }
}

func getAddr() string {
    if port := os.Getenv("PORT"); port != "" {
        return ":" + port
    }
    return ":8080"
}

// contextWithRequest provides request-specific values to components if needed later.
func contextWithRequest(ctx context.Context, r *http.Request) context.Context {
    return ctx
}

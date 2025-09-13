package main

import (
	"log"
	"net/http"
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

	// API routes
	h := handlers.New()
	api := r.Group("/api")
	{
		api.GET("/qr", h.QRCodeHandler)
		api.POST("/htmx/toast", h.GenericToast)
	}

	// SEO assets
	r.GET("/sitemap.xml", h.SitemapXML)
	r.GET("/robots.txt", func(c *gin.Context) {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(200, "User-agent: *\nAllow: /\nSitemap: "+schemeFromReq(c.Request)+"://"+c.Request.Host+"/sitemap.xml\n")
	})

	// Pages
	r.GET("/privacy", func(c *gin.Context) {
		if err := pages.PrivacyPage().Render(c.Request.Context(), c.Writer); err != nil {
			c.String(500, err.Error())
		}
	})
	r.GET("/about", func(c *gin.Context) {
		if err := pages.AboutPage().Render(c.Request.Context(), c.Writer); err != nil {
			c.String(500, err.Error())
		}
	})
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

// schemeFromReq returns https if TLS present, else http.
func schemeFromReq(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	// honor X-Forwarded-Proto if behind proxy
	if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
		return xf
	}
	return "http"
}

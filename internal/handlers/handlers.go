package handlers

import (
    "github.com/gin-gonic/gin"
)

// Handler is a placeholder for dependencies for HTTP handlers.
// It currently does not hold state, but exists to keep methods organized.
type Handler struct{}

// New returns a new Handler instance.
func New() *Handler { return &Handler{} }

// SitemapXML serves a minimal sitemap for the site.
// Update the URLs if you add more pages.
func (h *Handler) SitemapXML(c *gin.Context) {
    c.Header("Content-Type", "application/xml; charset=utf-8")
    scheme := "https"
    host := c.Request.Host
    if xf := c.Request.Header.Get("X-Forwarded-Proto"); xf != "" {
        scheme = xf
    } else if c.Request.TLS == nil && (host == "localhost:8080" || host == "127.0.0.1:8080") {
        scheme = "http"
    }
    base := scheme + "://" + host
    xml := "" +
        "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
        "<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n" +
        "  <url>\n" +
        "    <loc>" + base + "/" + "</loc>\n" +
        "    <changefreq>weekly</changefreq>\n" +
        "    <priority>1.0</priority>\n" +
        "  </url>\n" +
        "  <url>\n" +
        "    <loc>" + base + "/about" + "</loc>\n" +
        "    <changefreq>monthly</changefreq>\n" +
        "    <priority>0.6</priority>\n" +
        "  </url>\n" +
        "  <url>\n" +
        "    <loc>" + base + "/privacy" + "</loc>\n" +
        "    <changefreq>yearly</changefreq>\n" +
        "    <priority>0.5</priority>\n" +
        "  </url>\n" +
        "</urlset>\n"
    c.String(200, xml)
}

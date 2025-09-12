package handlers

import (
    "net/http"

    "github.com/gin-gonic/gin"
    toast "github.com/cristianadrielbraun/qrcreator.link/web/components/ui/toast"
)

// GenericToast returns a templui Toast component rendered as HTML for HTMX swaps.
func (h *Handler) GenericToast(c *gin.Context) {
    title := c.PostForm("title")
    description := c.PostForm("description")
    variant := c.PostForm("variant")
    dismissible := c.PostForm("dismissible") == "on"

    var v toast.Variant
    switch variant {
    case "error", "destructive":
        v = toast.VariantError
    case "warning":
        v = toast.VariantWarning
    case "info":
        v = toast.VariantInfo
    case "success":
        v = toast.VariantSuccess
    default:
        v = toast.VariantSuccess
    }

    c.Header("Content-Type", "text/html; charset=utf-8")
    c.Status(http.StatusOK)

    _ = toast.Toast(toast.Props{
        Title:         title,
        Description:   description,
        Variant:       v,
        Position:      toast.PositionBottomRight,
        Duration:      2000,
        Dismissible:   dismissible,
        ShowIndicator: false,
        Icon:          true,
    }).Render(c.Request.Context(), c.Writer)
}

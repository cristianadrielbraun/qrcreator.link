package handlers

// Handler is a placeholder for dependencies for HTTP handlers.
// It currently does not hold state, but exists to keep methods organized.
type Handler struct{}

// New returns a new Handler instance.
func New() *Handler { return &Handler{} }

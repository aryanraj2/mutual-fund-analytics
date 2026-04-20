// internal/api/routes.go
package api

import (
	"net/http"

	"github.com/gorilla/mux"
)

func NewRouter(h *Handler) http.Handler {
	r := mux.NewRouter()

	// Fund endpoints
	r.HandleFunc("/funds", h.ListFunds).Methods(http.MethodGet)
	r.HandleFunc("/funds/rank", h.RankFunds).Methods(http.MethodGet)       // must be before /funds/{code}
	r.HandleFunc("/funds/{code}", h.GetFund).Methods(http.MethodGet)
	r.HandleFunc("/funds/{code}/analytics", h.GetAnalytics).Methods(http.MethodGet)

	// Sync endpoints
	r.HandleFunc("/sync/trigger", h.TriggerSync).Methods(http.MethodPost)
	r.HandleFunc("/sync/status", h.SyncStatus).Methods(http.MethodGet)

	return r
}
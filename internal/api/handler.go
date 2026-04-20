// internal/api/handler.go
package api

import (
	"encoding/json"
	"mutual-fund-analytics/internal/analytics"
	"mutual-fund-analytics/internal/pipeline"
	"mutual-fund-analytics/internal/store"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
)

type Handler struct {
	db     *store.DB
	engine *analytics.Engine
	orch   *pipeline.Orchestrator
}

func NewHandler(db *store.DB, engine *analytics.Engine, orch *pipeline.Orchestrator) *Handler {
	return &Handler{db: db, engine: engine, orch: orch}
}

// GET /funds
func (h *Handler) ListFunds(w http.ResponseWriter, r *http.Request) {
	amc := r.URL.Query().Get("amc")
	category := r.URL.Query().Get("category")

	funds, err := h.db.ListFunds(r.Context(), amc, category)
	if err != nil {
		jsonError(w, "failed to list funds", http.StatusInternalServerError)
		return
	}
	jsonOK(w, funds)
}

// GET /funds/{code}
func (h *Handler) GetFund(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]

	fund, err := h.db.GetFund(r.Context(), code)
	if err != nil || fund == nil {
		jsonError(w, "fund not found", http.StatusNotFound)
		return
	}

	nav, err := h.db.GetLatestNAV(r.Context(), code)
	if err != nil {
		jsonError(w, "failed to get NAV", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]interface{}{
		"fund":       fund,
		"latest_nav": nav,
	})
}

// GET /funds/{code}/analytics?window=3Y
func (h *Handler) GetAnalytics(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		code = mux.Vars(r)["code"]
	}
	window := r.URL.Query().Get("window")

	if window == "" {
		jsonError(w, "window is required (1Y|3Y|5Y|10Y)", http.StatusBadRequest)
		return
	}

	fund, err := h.db.GetFund(r.Context(), code)
	if err != nil || fund == nil {
		jsonError(w, "fund not found", http.StatusNotFound)
		return
	}

	a, err := h.db.GetAnalytics(r.Context(), code, window)
	if err != nil || a == nil {
		jsonError(w, "analytics not found for this fund/window", http.StatusNotFound)
		return
	}

	jsonOK(w, map[string]interface{}{
		"fund_code":  code,
		"fund_name":  fund.SchemeName,
		"category":   fund.Category,
		"amc":        fund.AMC,
		"window":     window,
		"data_availability": map[string]interface{}{
			"start_date":      a.DataStart.Format("2006-01-02"),
			"end_date":        a.DataEnd.Format("2006-01-02"),
			"total_days":      a.TotalDays,
			"nav_data_points": a.NAVDataPoints,
		},
		"rolling_periods_analyzed": a.PeriodsAnalyzed,
		"rolling_returns": map[string]interface{}{
			"min":    a.RollingMin,
			"max":    a.RollingMax,
			"median": a.RollingMedian,
			"p25":    a.RollingP25,
			"p75":    a.RollingP75,
		},
		"max_drawdown": a.MaxDrawdown,
		"cagr": map[string]interface{}{
			"min":    a.CAGRMin,
			"max":    a.CAGRMax,
			"median": a.CAGRMedian,
		},
		"computed_at": a.ComputedAt,
	})
}

// GET /funds/rank?category=Mid+Cap&window=3Y&sort_by=median_return&limit=5
func (h *Handler) RankFunds(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	window   := r.URL.Query().Get("window")
	sortBy   := r.URL.Query().Get("sort_by")
	limitStr := r.URL.Query().Get("limit")

	if window == "" {
		jsonError(w, "window is required (1Y|3Y|5Y|10Y)", http.StatusBadRequest)
		return
	}
	if category == "" {
		jsonError(w, "category is required", http.StatusBadRequest)
		return
	}
	if sortBy == "" {
		sortBy = "median_return"
	}

	limit := 5
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	results, err := h.db.GetRankings(r.Context(), category, sortBy, window, limit)
	if err != nil {
		jsonError(w, "failed to get rankings", http.StatusInternalServerError)
		return
	}

	// Build ranked response
	funds := make([]map[string]interface{}, 0, len(results))
	for i, a := range results {
		fund, _ := h.db.GetFund(r.Context(), a.SchemeCode)
		nav, _   := h.db.GetLatestNAV(r.Context(), a.SchemeCode)

		entry := map[string]interface{}{
			"rank":        i + 1,
			"fund_code":   a.SchemeCode,
			"median_return": a.RollingMedian,
			"max_drawdown":  a.MaxDrawdown,
			"computed_at":   a.ComputedAt,
		}
		if fund != nil {
			entry["fund_name"] = fund.SchemeName
			entry["amc"]       = fund.AMC
		}
		if nav != nil {
			entry["current_nav"]  = nav.Value
			entry["last_updated"] = nav.Date.Format("2006-01-02")
		}
		funds = append(funds, entry)
	}

	jsonOK(w, map[string]interface{}{
		"category":    category,
		"window":      window,
		"sorted_by":   sortBy,
		"total_funds": len(results),
		"showing":     len(results),
		"funds":       funds,
	})
}

// POST /sync/trigger
func (h *Handler) TriggerSync(w http.ResponseWriter, r *http.Request) {
	go func() {
		ctx := r.Context()
		if err := h.orch.IncrementalSync(ctx); err != nil {
			return
		}
		if err := h.engine.ComputeAll(ctx); err != nil {
			return
		}
	}()

	jsonOK(w, map[string]string{
		"status":  "triggered",
		"message": "incremental sync started in background",
	})
}

// GET /sync/status
func (h *Handler) SyncStatus(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.orch.SyncStatus(r.Context())
	if err != nil {
		jsonError(w, "failed to get sync status", http.StatusInternalServerError)
		return
	}
	jsonOK(w, map[string]interface{}{
		"jobs": jobs,
	})
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
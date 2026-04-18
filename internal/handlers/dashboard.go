package handlers

import (
	"net/http"

	"github.com/mirceanton/streamarr/internal/db"
)

func DashboardHandler(w http.ResponseWriter, r *http.Request) {
	stats, err := db.GetDashboardStats()
	if err != nil {
		http.Error(w, "Failed to load dashboard stats", http.StatusInternalServerError)
		return
	}

	render(w, "dashboard.html", map[string]interface{}{
		"Page":  "dashboard",
		"Stats": stats,
	})
}

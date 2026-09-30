// dashboard.go serves the product dashboard (item 31): a single embedded
// page that fetches /board for the chosen scope and window. The page is the
// product surface — the JSON endpoints stay the contract.
package intelligence

import (
	_ "embed"
	"net/http"
)

//go:embed dashboard/index.html
var dashboardHTML []byte

func (a *API) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if a.defaultScope != "" && r.URL.Query().Get("scope") == "" {
		http.Redirect(w, r, dashboardScopeURL(a.defaultScope), http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(dashboardHTML)
}

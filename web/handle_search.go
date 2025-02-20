package web

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/steven-cmy/go-evepraisal/typedb"
)

// SearchPage holds the search results to be rendered
type SearchPage struct {
	Results []typedb.EveType `json:"results"`
}

// HandleSearch handles searching for items
func (ctx *Context) HandleSearch(w http.ResponseWriter, r *http.Request) {
	results := ctx.App.TypeDB.Search(r.FormValue("q"))
	if r.Header.Get("format") == formatJSON {
		w.Header().Add("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
		return
	}

	if len(results) == 1 {
		http.Redirect(w, r, fmt.Sprintf("/item/%d", results[0].ID), http.StatusPermanentRedirect)
	}
	_ = ctx.render(r, w, "search.html", SearchPage{Results: results})
}

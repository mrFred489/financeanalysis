package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/mrFred489/financeanalysis/internal/db/dbq"
	"github.com/mrFred489/financeanalysis/internal/viz"
)

// M5 visualization API (plan docs/plans/M5-visualization.md step 3):
// GET /api/viz/timeseries — monthly buckets with per-category/subcategory/account sums
// GET /api/viz/share      — per-subcategory share for the donut/treemap

// parseVizFilters reads the shared viz query params (VIZ-2).
func parseVizFilters(r *http.Request) viz.Filters {
	q := r.URL.Query()
	return viz.Filters{
		From:             q.Get("from"),
		To:               q.Get("to"),
		AccountIDs:       parseIDSlice(q.Get("accounts")),
		CategoryIDs:      parseIDSlice(q.Get("categories")),
		SubcategoryIDs:   parseIDSlice(q.Get("subcategories")),
		IncludeTransfers: q.Get("includeTransfers") == "1" || q.Get("includeTransfers") == "true",
	}
}

// parseIDSlice parses a comma-separated list of int64 ids.
func parseIDSlice(s string) []int64 {
	if s == "" {
		return nil
	}
	var ids []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// subcatMetaMap builds the subcategory metadata map (id → name/category/color)
// used for the share endpoint.
func subcatMetaMap(ctx context.Context, q *dbq.Queries) (map[int64]viz.SubcatMeta, error) {
	cats, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	catKey := map[int64]string{}
	for _, c := range cats {
		catKey[c.ID] = c.Key
	}
	subs, err := q.ListSubcategories(ctx)
	if err != nil {
		return nil, err
	}
	meta := make(map[int64]viz.SubcatMeta, len(subs))
	for _, s := range subs {
		meta[s.ID] = viz.SubcatMeta{
			Name:        s.Name,
			CategoryKey: catKey[s.CategoryID],
			Color:       s.Color,
		}
	}
	return meta, nil
}

// handleVizTimeseries: GET /api/viz/timeseries
func (s *Server) handleVizTimeseries() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		q := dbq.New(d)
		f := parseVizFilters(r)
		lines, err := viz.FetchLines(ctx, q, f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		buckets := viz.Aggregate(lines)
		writeJSON(w, http.StatusOK, map[string]any{"buckets": buckets})
	}
}

// handleVizShare: GET /api/viz/share
func (s *Server) handleVizShare() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		ctx := r.Context()
		q := dbq.New(d)
		f := parseVizFilters(r)
		lines, err := viz.FetchLines(ctx, q, f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		meta, err := subcatMetaMap(ctx, q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		shares := viz.Share(lines, meta)
		writeJSON(w, http.StatusOK, map[string]any{"shares": shares})
	}
}

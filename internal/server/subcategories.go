package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/mrFred489/financeanalysis/internal/colorx"
	"github.com/mrFred489/financeanalysis/internal/db/dbq"
)

// CategoryWithSubcategories is a category with its subcategories nested (GET /api/categories).
type CategoryWithSubcategories struct {
	ID            int64             `json:"id"`
	Key           string            `json:"key"`
	Name          string            `json:"name"`
	Color         string            `json:"color"`
	Subcategories []dbq.Subcategory `json:"subcategories"`
}

func (s *Server) handleListCategories() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		q := dbq.New(d)
		categories, err := q.ListCategories(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		subs, err := q.ListSubcategories(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		result := make([]CategoryWithSubcategories, 0, len(categories))
		for _, c := range categories {
			entry := CategoryWithSubcategories{
				ID:            c.ID,
				Key:           c.Key,
				Name:          c.Name,
				Color:         c.Color,
				Subcategories: []dbq.Subcategory{},
			}
			for _, sub := range subs {
				if sub.CategoryID == c.ID {
					entry.Subcategories = append(entry.Subcategories, *sub)
				}
			}
			result = append(result, entry)
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func (s *Server) handleListSubcategories() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		subs, err := dbq.New(d).ListSubcategories(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, orEmpty(subs))
	}
}

// subcategoryRequest is the body for POST/PUT /api/subcategories. Nil fields use defaults / stay unchanged.
type subcategoryRequest struct {
	CategoryID *int64  `json:"category_id"`
	Name       *string `json:"name"`
	Color      *string `json:"color"`
	Icon       *string `json:"icon"`
	Hotkey     *string `json:"hotkey"`
	SortOrder  *int64  `json:"sort_order"`
}

func (s *Server) handleCreateSubcategory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		var req subcategoryRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_json", Message: err.Error()})
			return
		}
		if req.CategoryID == nil || req.Name == nil || *req.Name == "" {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "category_id and name are required"})
			return
		}
		q := dbq.New(d)
		category, err := findCategory(r.Context(), q, *req.CategoryID)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_category", Message: "category not found"})
			return
		}

		// CAT-5: auto-generate a shade of the category base hue when no color is given
		color := ""
		if req.Color != nil {
			color = *req.Color
		} else {
			existing, err := q.ListSubcategoriesByCategory(r.Context(), category.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
				return
			}
			color, err = colorx.Shade(category.Color, len(existing), len(existing)+1)
			if err != nil {
				writeError(w, http.StatusInternalServerError, APIError{Code: "color_error", Message: err.Error()})
				return
			}
		}

		sortOrder := int64(0)
		if req.SortOrder != nil {
			sortOrder = *req.SortOrder
		}
		sub, err := q.CreateSubcategory(r.Context(), dbq.CreateSubcategoryParams{
			CategoryID: category.ID,
			Name:       *req.Name,
			Color:      color,
			Icon:       nullString(req.Icon),
			Hotkey:     nullString(req.Hotkey),
			SortOrder:  sortOrder,
		})
		if err != nil {
			writeError(w, http.StatusConflict, APIError{Code: "create_failed", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, sub)
	}
}

func (s *Server) handleUpdateSubcategory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid subcategory id"})
			return
		}
		var req subcategoryRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_json", Message: err.Error()})
			return
		}
		sub, err := dbq.New(d).UpdateSubcategory(r.Context(), dbq.UpdateSubcategoryParams{
			CategoryID: nullInt64(req.CategoryID),
			Name:       nullString(req.Name),
			Color:      nullString(req.Color),
			Icon:       nullString(req.Icon),
			Hotkey:     nullString(req.Hotkey),
			SortOrder:  nullInt64(req.SortOrder),
			ID:         id,
		})
		if err != nil {
			writeError(w, http.StatusNotFound, APIError{Code: "not_found", Message: "subcategory not found or update failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, sub)
	}
}

// handleDeleteSubcategory implements merge-delete (CAT-4): the query parameter
// mergeIntoId names the subcategory that receives the deleted one's lines.
func (s *Server) handleDeleteSubcategory() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d := s.requireDB(w, r)
		if d == nil {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_id", Message: "invalid subcategory id"})
			return
		}
		mergeInto := r.URL.Query().Get("mergeIntoId")
		if mergeInto == "" {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "mergeIntoId query parameter is required when deleting a subcategory"})
			return
		}
		mergeIntoID, err := strconv.ParseInt(mergeInto, 10, 64)
		if err != nil || mergeIntoID == id {
			writeError(w, http.StatusBadRequest, APIError{Code: "validation", Message: "mergeIntoId must be a different, existing subcategory"})
			return
		}

		q := dbq.New(d)
		if _, err := q.GetSubcategory(r.Context(), mergeIntoID); err != nil {
			writeError(w, http.StatusBadRequest, APIError{Code: "bad_merge_target", Message: "merge target subcategory not found"})
			return
		}

		tx, err := d.BeginTx(r.Context(), nil)
		if err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		defer tx.Rollback()
		qt := dbq.New(d).WithTx(tx)
		if err := qt.ReassignLinesToSubcategory(r.Context(), dbq.ReassignLinesToSubcategoryParams{
			SubcategoryID:   sql.NullInt64{Int64: mergeIntoID, Valid: true},
			SubcategoryID_2: sql.NullInt64{Int64: id, Valid: true},
		}); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := qt.DeleteSubcategory(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		if err := tx.Commit(); err != nil {
			writeError(w, http.StatusInternalServerError, APIError{Code: "db_error", Message: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

func findCategory(ctx context.Context, q *dbq.Queries, id int64) (*dbq.Category, error) {
	categories, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range categories {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, errors.New("category not found")
}

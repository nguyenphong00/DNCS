package prescription

import (
	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/problem"
	"log/slog"
	"net/http"
	"strconv"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(s *Service, l *slog.Logger) *Handler { return &Handler{s, l} }
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	page, size := httpx.Pagination(r)
	eid, _ := strconv.ParseInt(r.URL.Query().Get("encounter_id"), 10, 64)
	x, e := h.service.ListPage(r.Context(), p, eid, page, size)
	if e != nil {
		problem.Write(w, r, h.logger, "list prescriptions", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) { h.List(w, r) }
func (h *Handler) Encounter(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	page, size := httpx.Pagination(r)
	x, e := h.service.ListPage(r.Context(), p, id, page, size)
	if e != nil {
		problem.Write(w, r, h.logger, "encounter prescriptions", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) GetSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "seriesId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.GetSeries(r.Context(), p, id)
	if e != nil {
		problem.Write(w, r, h.logger, "get prescription series", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) { h.GetSeries(w, r) }
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.Get(r.Context(), p, id)
	if e != nil {
		problem.Write(w, r, h.logger, "get prescription", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q struct {
		OverallNote string `json:"overall_note"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.CreateSeries(r.Context(), p, id, q.OverallNote)
	if e != nil {
		problem.Write(w, r, h.logger, "create prescription", e)
		return
	}
	httpx.WriteJSON(w, 201, x)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64  `json:"expected_version"`
		OverallNote     string `json:"overall_note"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.Update(r.Context(), p, id, q.ExpectedVersion, q.OverallNote)
	if e != nil {
		problem.Write(w, r, h.logger, "update prescription", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	var q struct {
		MedicineID int64  `json:"medicine_id"`
		Quantity   int16  `json:"quantity"`
		Usage      string `json:"usage_instruction"`
		Notes      string `json:"notes"`
		SortOrder  int16  `json:"sort_order"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.AddItem(r.Context(), p, id, q.MedicineID, q.Quantity, q.Usage, q.Notes, q.SortOrder)
	if e != nil {
		problem.Write(w, r, h.logger, "add prescription item", e)
		return
	}
	httpx.WriteJSON(w, 201, x)
}
func (h *Handler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	pid, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	iid, ok := httpx.PathInt64(w, r, "itemId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64  `json:"expected_version"`
		Quantity        int16  `json:"quantity"`
		Usage           string `json:"usage_instruction"`
		Notes           string `json:"notes"`
		SortOrder       int16  `json:"sort_order"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.UpdateItem(r.Context(), p, pid, iid, q.ExpectedVersion, q.Quantity, q.Usage, q.Notes, q.SortOrder)
	if e != nil {
		problem.Write(w, r, h.logger, "update prescription item", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) RemoveItem(w http.ResponseWriter, r *http.Request) {
	pid, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	iid, ok := httpx.PathInt64(w, r, "itemId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64  `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.RemoveItem(r.Context(), p, pid, iid, q.ExpectedVersion, q.Reason)
	if e != nil {
		problem.Write(w, r, h.logger, "remove prescription item", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Issue(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.Issue(r.Context(), p, id, q.ExpectedVersion)
	if e != nil {
		problem.Write(w, r, h.logger, "issue prescription", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Discard(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "prescriptionId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64  `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.Discard(r.Context(), p, id, q.ExpectedVersion, q.Reason)
	if e != nil {
		problem.Write(w, r, h.logger, "discard prescription", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Replacement(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "seriesId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.Replacement(r.Context(), p, id)
	if e != nil {
		problem.Write(w, r, h.logger, "replace prescription", e)
		return
	}
	httpx.WriteJSON(w, 201, x)
}

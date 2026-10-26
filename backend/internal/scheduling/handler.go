package scheduling

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

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var q struct {
		DoctorProfileID     int64  `json:"doctor_profile_id"`
		RoomID              int64  `json:"room_id"`
		StartAt             string `json:"start_at"`
		EndAt               string `json:"end_at"`
		SlotDurationMinutes int16  `json:"slot_duration_minutes"`
		InitialStatus       string `json:"initial_status"`
		Notes               string `json:"notes"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Create(r.Context(), p, CreateInput(q))
	if err != nil {
		problem.Write(w, r, h.logger, "create schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "scheduleId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Get(r.Context(), p, id)
	if err != nil {
		problem.Write(w, r, h.logger, "get schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	page, size := httpx.Pagination(r)
	doctor, _ := strconv.ParseInt(r.URL.Query().Get("doctor_id"), 10, 64)
	room, _ := strconv.ParseInt(r.URL.Query().Get("room_id"), 10, 64)
	items, err := h.service.ListPage(r.Context(), p, r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.URL.Query().Get("status"), doctor, room, page, size)
	if err != nil {
		problem.Write(w, r, h.logger, "list schedules", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "scheduleId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion     int64   `json:"expected_version"`
		RoomID              *int64  `json:"room_id"`
		StartAt             *string `json:"start_at"`
		EndAt               *string `json:"end_at"`
		SlotDurationMinutes *int16  `json:"slot_duration_minutes"`
		Notes               *string `json:"notes"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Update(r.Context(), p, id, UpdateInput(q))
	if err != nil {
		problem.Write(w, r, h.logger, "update schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "scheduleId")
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
	item, err := h.service.Activate(r.Context(), p, id, q.ExpectedVersion)
	if err != nil {
		problem.Write(w, r, h.logger, "activate schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "scheduleId")
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
	item, err := h.service.Cancel(r.Context(), p, id, q.ExpectedVersion, q.Reason)
	if err != nil {
		problem.Write(w, r, h.logger, "cancel schedule", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "scheduleId")
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
	if err := h.service.Archive(r.Context(), p, id, q.ExpectedVersion); err != nil {
		problem.Write(w, r, h.logger, "archive schedule", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

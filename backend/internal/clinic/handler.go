package clinic

import (
	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/problem"
	"log/slog"
	"net/http"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}
func (h *Handler) ListSpecialties(w http.ResponseWriter, r *http.Request) {
	page, size := httpx.Pagination(r)
	items, err := h.service.ListSpecialtiesPage(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("active_only") != "false", page, size)
	if err != nil {
		problem.Write(w, r, h.logger, "list specialties", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
func (h *Handler) GetSpecialty(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "specialtyId")
	if !ok {
		return
	}
	item, err := h.service.GetSpecialty(r.Context(), id, false)
	if err != nil {
		problem.Write(w, r, h.logger, "get specialty", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) CreateSpecialty(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.CreateSpecialty(r.Context(), p, q.Code, q.Name, q.Description)
	if err != nil {
		problem.Write(w, r, h.logger, "create specialty", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) UpdateSpecialty(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "specialtyId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64   `json:"expected_version"`
		Name            *string `json:"name"`
		Description     *string `json:"description"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.UpdateSpecialty(r.Context(), p, id, q.ExpectedVersion, q.Name, q.Description)
	if err != nil {
		problem.Write(w, r, h.logger, "update specialty", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) ChangeSpecialty(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "specialtyId")
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
		item, err := h.service.ChangeSpecialty(r.Context(), p, id, q.ExpectedVersion, action, q.Reason)
		if err != nil {
			problem.Write(w, r, h.logger, "change specialty", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}
func (h *Handler) ListDoctors(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "specialtyId")
	if !ok {
		return
	}
	items, err := h.service.ListDoctors(r.Context(), id)
	if err != nil {
		problem.Write(w, r, h.logger, "list specialty doctors", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *Handler) ListRooms(w http.ResponseWriter, r *http.Request) {
	page, size := httpx.Pagination(r)
	items, err := h.service.ListRoomsPage(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("room_type"), r.URL.Query().Get("active_only") != "false", page, size)
	if err != nil {
		problem.Write(w, r, h.logger, "list rooms", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
func (h *Handler) GetRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "roomId")
	if !ok {
		return
	}
	item, err := h.service.GetRoom(r.Context(), id, false)
	if err != nil {
		problem.Write(w, r, h.logger, "get room", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Code         string `json:"code"`
		Name         string `json:"name"`
		RoomType     string `json:"room_type"`
		Floor        string `json:"floor"`
		LocationNote string `json:"location_note"`
		Description  string `json:"description"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.CreateRoom(r.Context(), p, q.Code, q.Name, q.RoomType, q.Floor, q.LocationNote, q.Description)
	if err != nil {
		problem.Write(w, r, h.logger, "create room", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) UpdateRoom(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "roomId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64   `json:"expected_version"`
		Name            *string `json:"name"`
		RoomType        *string `json:"room_type"`
		Floor           *string `json:"floor"`
		LocationNote    *string `json:"location_note"`
		Description     *string `json:"description"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.UpdateRoom(r.Context(), p, id, RoomUpdate{q.ExpectedVersion, q.Name, q.RoomType, q.Floor, q.LocationNote, q.Description})
	if err != nil {
		problem.Write(w, r, h.logger, "update room", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) ChangeRoom(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "roomId")
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
		item, err := h.service.ChangeRoom(r.Context(), p, id, q.ExpectedVersion, action, q.Reason)
		if err != nil {
			problem.Write(w, r, h.logger, "change room", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}
func (h *Handler) DoctorSpecialties(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "doctorId")
	if !ok {
		return
	}
	items, err := h.service.DoctorSpecialties(r.Context(), id)
	if err != nil {
		problem.Write(w, r, h.logger, "list doctor specialties", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *Handler) AssignDoctor(w http.ResponseWriter, r *http.Request) {
	doctor, ok := httpx.PathInt64(w, r, "doctorId")
	if !ok {
		return
	}
	specialty, ok := httpx.PathInt64(w, r, "specialtyId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	if err := h.service.AssignDoctor(r.Context(), p, doctor, specialty); err != nil {
		problem.Write(w, r, h.logger, "assign doctor specialty", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) RemoveDoctor(w http.ResponseWriter, r *http.Request) {
	doctor, ok := httpx.PathInt64(w, r, "doctorId")
	if !ok {
		return
	}
	specialty, ok := httpx.PathInt64(w, r, "specialtyId")
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
	if err := h.service.RemoveDoctor(r.Context(), p, doctor, specialty, q.ExpectedVersion, q.Reason); err != nil {
		problem.Write(w, r, h.logger, "remove doctor specialty", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

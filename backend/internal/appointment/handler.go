package appointment

import (
	"log/slog"
	"net/http"
	"strconv"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/problem"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}
func (h *Handler) Book(w http.ResponseWriter, r *http.Request) {
	var q struct {
		DoctorScheduleID int64  `json:"doctor_schedule_id"`
		PatientID        int64  `json:"patient_id"`
		SpecialtyID      int64  `json:"specialty_id"`
		StartAt          string `json:"start_at"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, replayed, err := h.service.Book(r.Context(), p, BookInput{q.DoctorScheduleID, q.PatientID, q.SpecialtyID, q.StartAt, r.Header.Get("Idempotency-Key")})
	if err != nil {
		problem.Write(w, r, h.logger, "book appointment", err)
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "appointmentId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Get(r.Context(), p, id)
	if err != nil {
		problem.Write(w, r, h.logger, "get appointment", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) List(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFromContext(r.Context())
		page, size := httpx.Pagination(r)
		patient, _ := strconv.ParseInt(r.URL.Query().Get("patient_id"), 10, 64)
		doctor, _ := strconv.ParseInt(r.URL.Query().Get("doctor_id"), 10, 64)
		specialty, _ := strconv.ParseInt(r.URL.Query().Get("specialty_id"), 10, 64)
		room, _ := strconv.ParseInt(r.URL.Query().Get("room_id"), 10, 64)
		items, err := h.service.ListPage(r.Context(), p, own, r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.URL.Query().Get("status"), patient, doctor, specialty, room, page, size)
		if err != nil {
			problem.Write(w, r, h.logger, "list appointments", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, items)
	}
}
func (h *Handler) Transition(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "appointmentId")
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
		item, err := h.service.Transition(r.Context(), p, id, action, TransitionInput(q))
		if err != nil {
			problem.Write(w, r, h.logger, "transition appointment", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}
func (h *Handler) Slots(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	specialty, _ := strconv.ParseInt(r.URL.Query().Get("specialty_id"), 10, 64)
	doctor, _ := strconv.ParseInt(r.URL.Query().Get("doctor_id"), 10, 64)
	patient, _ := strconv.ParseInt(r.URL.Query().Get("patient_id"), 10, 64)
	items, err := h.service.Slots(r.Context(), principal, specialty, doctor, patient, r.URL.Query().Get("date"))
	if err != nil {
		problem.Write(w, r, h.logger, "search appointment slots", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) FollowUp(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q struct {
		DoctorScheduleID int64  `json:"doctor_schedule_id"`
		StartAt          string `json:"start_at"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, replayed, err := h.service.FollowUp(r.Context(), p, id, FollowUpInput{DoctorScheduleID: q.DoctorScheduleID, StartAt: q.StartAt, IdempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		problem.Write(w, r, h.logger, "create follow-up appointment", err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, status, item)
}

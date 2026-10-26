package encounter

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

func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "appointmentId")
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
	item, replayed, err := h.service.Start(r.Context(), p, id, q.ExpectedVersion)
	if err != nil {
		problem.Write(w, r, h.logger, "start encounter", err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, status, item)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Get(r.Context(), p, id)
	if err != nil {
		problem.Write(w, r, h.logger, "get encounter", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) List(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFromContext(r.Context())
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		doctor, _ := strconv.ParseInt(r.URL.Query().Get("doctor_profile_id"), 10, 64)
		patient, _ := strconv.ParseInt(r.URL.Query().Get("patient_id"), 10, 64)
		specialty, _ := strconv.ParseInt(r.URL.Query().Get("specialty_id"), 10, 64)
		appointment, _ := strconv.ParseInt(r.URL.Query().Get("appointment_id"), 10, 64)
		result, err := h.service.List(r.Context(), p, own, ListFilter{From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to"), Status: r.URL.Query().Get("status"), DoctorProfileID: doctor, PatientID: patient, SpecialtyID: specialty, AppointmentID: appointment, EncounterCode: r.URL.Query().Get("encounter_code"), Cursor: r.URL.Query().Get("cursor"), Limit: limit})
		if err != nil {
			problem.Write(w, r, h.logger, "list encounters", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
	}
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion        int64   `json:"expected_version"`
		ChiefComplaint         *string `json:"chief_complaint"`
		Symptoms               *string `json:"symptoms"`
		Diagnosis              *string `json:"diagnosis"`
		Conclusion             *string `json:"conclusion"`
		ClinicalNotes          *string `json:"clinical_notes"`
		FollowUpRecommendation *string `json:"follow_up_recommendation"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Update(r.Context(), p, id, UpdateInput{q.ExpectedVersion, q.ChiefComplaint, q.Symptoms, q.Diagnosis, q.Conclusion, q.ClinicalNotes, q.FollowUpRecommendation})
	if err != nil {
		problem.Write(w, r, h.logger, "update encounter", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

type vitalRequest struct {
	ExpectedVersion    int64    `json:"expected_version"`
	TemperatureC       *float64 `json:"temperature_c"`
	SystolicBP         *int16   `json:"systolic_bp"`
	DiastolicBP        *int16   `json:"diastolic_bp"`
	HeartRateBPM       *int16   `json:"heart_rate_bpm"`
	RespiratoryRateBPM *int16   `json:"respiratory_rate_bpm"`
	SpO2Percent        *float64 `json:"spo2_percent"`
	WeightKG           *float64 `json:"weight_kg"`
	HeightCM           *float64 `json:"height_cm"`
	PainScore          *int16   `json:"pain_score"`
	Notes              *string  `json:"notes"`
	MeasuredAt         string   `json:"measured_at"`
}

func (q vitalRequest) input() VitalInput {
	return VitalInput{q.ExpectedVersion, q.TemperatureC, q.SystolicBP, q.DiastolicBP, q.HeartRateBPM, q.RespiratoryRateBPM, q.SpO2Percent, q.WeightKG, q.HeightCM, q.PainScore, q.Notes, q.MeasuredAt}
}
func (h *Handler) CreateVital(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q vitalRequest
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.CreateVital(r.Context(), p, id, q.input())
	if err != nil {
		problem.Write(w, r, h.logger, "create vital signs", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) UpdateVital(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	vital, ok := httpx.PathInt64(w, r, "vitalSignId")
	if !ok {
		return
	}
	var q vitalRequest
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.UpdateVital(r.Context(), p, id, vital, q.input())
	if err != nil {
		problem.Write(w, r, h.logger, "update vital signs", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "appointmentId")
	if !ok {
		return
	}
	var q struct {
		AppointmentExpectedVersion int64 `json:"appointment_expected_version"`
		EncounterExpectedVersion   int64 `json:"encounter_expected_version"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, replayed, err := h.service.Finalize(r.Context(), p, id, q.AppointmentExpectedVersion, q.EncounterExpectedVersion)
	if err != nil {
		problem.Write(w, r, h.logger, "complete encounter", err)
		return
	}
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ListServices(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListServices(r.Context(), p, id)
	if err != nil {
		problem.Write(w, r, h.logger, "list encounter services", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *Handler) AddService(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q struct {
		ServiceID int64 `json:"service_id"`
		Quantity  int16 `json:"quantity"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.AddService(r.Context(), p, id, q.ServiceID, q.Quantity)
	if err != nil {
		problem.Write(w, r, h.logger, "assign encounter service", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) UpdateService(w http.ResponseWriter, r *http.Request) {
	encounterID, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	orderID, ok := httpx.PathInt64(w, r, "serviceOrderId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64 `json:"expected_version"`
		Quantity        int16 `json:"quantity"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.UpdateService(r.Context(), p, encounterID, orderID, q.Quantity, q.ExpectedVersion)
	if err != nil {
		problem.Write(w, r, h.logger, "update encounter service", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) RemoveService(w http.ResponseWriter, r *http.Request) {
	encounterID, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	orderID, ok := httpx.PathInt64(w, r, "serviceOrderId")
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
	item, err := h.service.RemoveService(r.Context(), p, encounterID, orderID, q.ExpectedVersion, q.Reason)
	if err != nil {
		problem.Write(w, r, h.logger, "remove encounter service", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ListAmendments(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	items, err := h.service.ListAmendments(r.Context(), p, id)
	if err != nil {
		problem.Write(w, r, h.logger, "list encounter amendments", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (h *Handler) AddAmendment(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q struct {
		AmendmentType string `json:"amendment_type"`
		Reason        string `json:"reason"`
		Content       string `json:"content"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.AddAmendment(r.Context(), p, id, q.AmendmentType, q.Reason, q.Content)
	if err != nil {
		problem.Write(w, r, h.logger, "add encounter amendment", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

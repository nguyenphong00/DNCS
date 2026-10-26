package patient

import (
	"log/slog"
	"net/http"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/problem"
)

type Handler struct {
	service     *Service
	authService *auth.Service
	logger      *slog.Logger
}

func NewHandler(service *Service, authService *auth.Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, authService: authService, logger: logger}
}

type createRequest struct {
	PhoneNumber           string `json:"phone_number"`
	Password              string `json:"password"`
	FullName              string `json:"full_name"`
	DateOfBirth           string `json:"date_of_birth"`
	GenderCode            string `json:"gender_code"`
	Address               string `json:"address"`
	CCCD                  string `json:"cccd"`
	HealthInsuranceNumber string `json:"health_insurance_number"`
	MedicalHistory        string `json:"medical_history"`
	Allergies             string `json:"allergies"`
}

func (r createRequest) input() CreateInput {
	return CreateInput{PhoneNumber: r.PhoneNumber, Password: r.Password, FullName: r.FullName, DateOfBirth: r.DateOfBirth, GenderCode: r.GenderCode, Address: r.Address, CCCD: r.CCCD, HealthInsuranceNumber: r.HealthInsuranceNumber, MedicalHistory: r.MedicalHistory, Allergies: r.Allergies}
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var request createRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	created, err := h.service.Create(r.Context(), nil, request.input())
	if err != nil {
		problem.Write(w, r, h.logger, "register patient", err)
		return
	}
	pair, err := h.authService.StartSessionForUser(r.Context(), created.UserID, auth.ClientInfoFromRequest(r))
	if err != nil {
		problem.Write(w, r, h.logger, "start patient session", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"patient": created.Profile, "tokens": pair})
}
func (h *Handler) CreateAtCounter(w http.ResponseWriter, r *http.Request) {
	var request createRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	created, err := h.service.Create(r.Context(), &principal, request.input())
	if err != nil {
		problem.Write(w, r, h.logger, "create patient", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, created)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	page, size := httpx.Pagination(r)
	keyword := r.URL.Query().Get("q")
	for _, key := range []string{"patient_code", "phone_number", "cccd", "full_name"} {
		if value := r.URL.Query().Get(key); value != "" {
			keyword = value
			break
		}
	}
	items, err := h.service.ListPage(r.Context(), principal, keyword, page, size)
	if err != nil {
		problem.Write(w, r, h.logger, "list patients", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "patientId")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.GetScoped(r.Context(), principal, id)
	if err != nil {
		problem.Write(w, r, h.logger, "get patient", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.GetOwn(r.Context(), principal.UserID)
	if err != nil {
		problem.Write(w, r, h.logger, "get own patient", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

type updateRequest struct {
	ExpectedVersion       int64   `json:"expected_version"`
	FullName              *string `json:"full_name"`
	DateOfBirth           *string `json:"date_of_birth"`
	GenderCode            *string `json:"gender_code"`
	Address               *string `json:"address"`
	CCCD                  *string `json:"cccd"`
	HealthInsuranceNumber *string `json:"health_insurance_number"`
	MedicalHistory        *string `json:"medical_history"`
	Allergies             *string `json:"allergies"`
}

func (r updateRequest) input() UpdateInput {
	return UpdateInput{ExpectedVersion: r.ExpectedVersion, FullName: r.FullName, DateOfBirth: r.DateOfBirth, GenderCode: r.GenderCode, Address: r.Address, CCCD: r.CCCD, HealthInsuranceNumber: r.HealthInsuranceNumber, MedicalHistory: r.MedicalHistory, Allergies: r.Allergies}
}
func (h *Handler) Update(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		var ok bool
		principal, _ := auth.PrincipalFromContext(r.Context())
		if own {
			profile, err := h.service.GetOwn(r.Context(), principal.UserID)
			if err != nil {
				problem.Write(w, r, h.logger, "resolve own patient", err)
				return
			}
			id = profile.ID
		} else {
			id, ok = httpx.PathInt64(w, r, "patientId")
			if !ok {
				return
			}
		}
		var request updateRequest
		if !httpx.DecodeJSON(w, r, &request) {
			return
		}
		item, err := h.service.Update(r.Context(), principal, id, request.input(), own)
		if err != nil {
			problem.Write(w, r, h.logger, "update patient", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}
func (h *Handler) PhoneChange(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "patientId")
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion int64  `json:"expected_version"`
		PhoneNumber     string `json:"phone_number"`
		Reason          string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.ChangePhone(r.Context(), principal, id, request.ExpectedVersion, request.PhoneNumber, request.Reason)
	if err != nil {
		problem.Write(w, r, h.logger, "change patient phone", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "patientId")
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion int64  `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	if err := h.service.Archive(r.Context(), principal, id, request.ExpectedVersion, request.Reason); err != nil {
		problem.Write(w, r, h.logger, "archive patient", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "patientId")
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion int64 `json:"expected_version"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Restore(r.Context(), principal, id, request.ExpectedVersion)
	if err != nil {
		problem.Write(w, r, h.logger, "restore patient", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

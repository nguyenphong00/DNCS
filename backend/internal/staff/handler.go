package staff

import (
	"log/slog"
	"net/http"
	"strings"

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

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Username            string `json:"username"`
		RoleID              int64  `json:"role_id"`
		FullName            string `json:"full_name"`
		PhoneNumber         string `json:"phone_number"`
		Email               string `json:"email"`
		DateOfBirth         string `json:"date_of_birth"`
		GenderCode          string `json:"gender_code"`
		Address             string `json:"address"`
		CreateDoctorProfile bool   `json:"create_doctor_profile"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	result, err := h.service.Create(r.Context(), principal, CreateInput(request))
	if err != nil {
		problem.Write(w, r, h.logger, "create staff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, size := httpx.Pagination(r)
	items, err := h.service.ListPage(r.Context(), r.URL.Query().Get("keyword"), r.URL.Query().Get("employment_status"), r.URL.Query().Get("include_deleted") == "true", page, size)
	if err != nil {
		problem.Write(w, r, h.logger, "list staff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "staffId")
	if !ok {
		return
	}
	item, err := h.service.Get(r.Context(), id, r.URL.Query().Get("include_deleted") == "true")
	if err != nil {
		problem.Write(w, r, h.logger, "get staff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.GetByUser(r.Context(), principal.UserID)
	if err != nil {
		problem.Write(w, r, h.logger, "get own staff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "staffId")
	if !ok {
		return
	}
	var request struct {
		ExpectedVersion int64   `json:"expected_version"`
		FullName        *string `json:"full_name"`
		PhoneNumber     *string `json:"phone_number"`
		Email           *string `json:"email"`
		DateOfBirth     *string `json:"date_of_birth"`
		GenderCode      *string `json:"gender_code"`
		Address         *string `json:"address"`
	}
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Update(r.Context(), principal, id, UpdateInput(request))
	if err != nil {
		problem.Write(w, r, h.logger, "update staff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}
func (h *Handler) ChangeStatus(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "staffId")
		if !ok {
			return
		}
		var request struct {
			ExpectedVersion          int64  `json:"expected_version"`
			Reason                   string `json:"reason"`
			ConfirmExistingOverrides bool   `json:"confirm_existing_overrides"`
		}
		if !httpx.DecodeJSON(w, r, &request) {
			return
		}
		principal, _ := auth.PrincipalFromContext(r.Context())
		item, err := h.service.ChangeEmploymentStatus(r.Context(), principal, id, action, StatusInput(request))
		if err != nil {
			problem.Write(w, r, h.logger, "change staff status", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}
func (h *Handler) CreateDoctor(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "staffId")
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
	item, err := h.service.CreateDoctorProfile(r.Context(), principal, id, request.ExpectedVersion)
	if err != nil {
		problem.Write(w, r, h.logger, "create doctor profile", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}
func (h *Handler) ChangeDoctorStatus(activate bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "staffId")
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
		item, err := h.service.ChangeDoctorStatus(r.Context(), principal, id, activate, StatusInput{ExpectedVersion: request.ExpectedVersion, Reason: strings.TrimSpace(request.Reason)})
		if err != nil {
			problem.Write(w, r, h.logger, "change doctor status", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) Archive(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "staffId")
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
		problem.Write(w, r, h.logger, "archive staff", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Restore(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "staffId")
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
	item, err := h.service.Restore(r.Context(), principal, id, request.ExpectedVersion, request.Reason)
	if err != nil {
		problem.Write(w, r, h.logger, "restore staff", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

package servicecatalog

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

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	principal, _ := auth.PrincipalFromContext(r.Context())
	page, size := httpx.Pagination(r)
	specialty, _ := strconv.ParseInt(r.URL.Query().Get("specialty_id"), 10, 64)
	items, err := h.service.ListPage(r.Context(), principal, ListFilter{Query: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), ServiceType: r.URL.Query().Get("type"), SpecialtyID: specialty, Page: page, PageSize: size})
	if err != nil {
		problem.Write(w, r, h.logger, "list services", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "serviceId")
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Get(r.Context(), principal, id)
	if err != nil {
		problem.Write(w, r, h.logger, "get service", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Code                 string  `json:"code"`
		Name                 string  `json:"name"`
		ServiceType          string  `json:"service_type"`
		SpecialtyID          *int64  `json:"specialty_id"`
		UnitLabel            string  `json:"unit_label"`
		Description          string  `json:"description"`
		Status               string  `json:"status"`
		InitialUnitPriceVND  *int64  `json:"initial_unit_price_vnd"`
		InitialEffectiveFrom *string `json:"initial_effective_from"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Create(r.Context(), principal, CreateInput{q.Code, q.Name, q.ServiceType, q.SpecialtyID, q.UnitLabel, q.Description, q.Status, q.InitialUnitPriceVND, q.InitialEffectiveFrom})
	if err != nil {
		problem.Write(w, r, h.logger, "create service", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "serviceId")
	if !ok {
		return
	}
	var q struct {
		ExpectedVersion int64   `json:"expected_version"`
		Name            *string `json:"name"`
		UnitLabel       *string `json:"unit_label"`
		Description     *string `json:"description"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.Update(r.Context(), principal, id, UpdateInput{q.ExpectedVersion, q.Name, q.UnitLabel, q.Description})
	if err != nil {
		problem.Write(w, r, h.logger, "update service", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) ChangeState(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "serviceId")
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
		principal, _ := auth.PrincipalFromContext(r.Context())
		item, err := h.service.ChangeState(r.Context(), principal, id, q.ExpectedVersion, action, q.Reason)
		if err != nil {
			problem.Write(w, r, h.logger, "change service state", err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) ListPrices(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "serviceId")
	if !ok {
		return
	}
	items, err := h.service.ListPrices(r.Context(), id)
	if err != nil {
		problem.Write(w, r, h.logger, "list service prices", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handler) CreatePrice(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "serviceId")
	if !ok {
		return
	}
	var q struct {
		UnitPriceVND  int64  `json:"unit_price_vnd"`
		EffectiveFrom string `json:"effective_from"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.CreatePrice(r.Context(), principal, id, q.UnitPriceVND, q.EffectiveFrom)
	if err != nil {
		problem.Write(w, r, h.logger, "create service price", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) CancelPrice(w http.ResponseWriter, r *http.Request) {
	serviceID, ok := httpx.PathInt64(w, r, "serviceId")
	if !ok {
		return
	}
	priceID, ok := httpx.PathInt64(w, r, "priceVersionId")
	if !ok {
		return
	}
	var q struct {
		Reason string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	item, err := h.service.CancelPrice(r.Context(), principal, serviceID, priceID, q.Reason)
	if err != nil {
		problem.Write(w, r, h.logger, "cancel service price", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

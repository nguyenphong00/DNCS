package inventory

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
func (h *Handler) ListMedicines(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFromContext(r.Context())
	page, size := httpx.Pagination(r)
	manage, e := h.service.CanManage(r.Context(), p)
	if e != nil {
		problem.Write(w, r, h.logger, "project medicines", e)
		return
	}
	items, e := h.service.ListMedicinesPage(r.Context(), p, manage, r.URL.Query().Get("q"), r.URL.Query().Get("status"), page, size)
	if e != nil {
		problem.Write(w, r, h.logger, "list medicines", e)
		return
	}
	httpx.WriteJSON(w, 200, items)
}
func (h *Handler) GetMedicine(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "medicineId")
	if !ok {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	manage, e := h.service.CanManage(r.Context(), p)
	if e != nil {
		problem.Write(w, r, h.logger, "project medicine", e)
		return
	}
	m, e := h.service.GetMedicine(r.Context(), p, id, manage)
	if e != nil {
		problem.Write(w, r, h.logger, "get medicine", e)
		return
	}
	httpx.WriteJSON(w, 200, m)
}
func (h *Handler) CreateMedicine(w http.ResponseWriter, r *http.Request) {
	var q struct {
		Code                 string  `json:"code"`
		Name                 string  `json:"name"`
		ActiveIngredient     string  `json:"active_ingredient"`
		DosageForm           string  `json:"dosage_form"`
		Strength             string  `json:"strength"`
		DispenseUnit         string  `json:"dispense_unit"`
		Description          string  `json:"description"`
		LowStockThreshold    float64 `json:"low_stock_threshold"`
		Status               string  `json:"status"`
		InitialPrice         *int64  `json:"initial_unit_sale_price_vnd"`
		InitialEffectiveFrom *string `json:"initial_effective_from"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	m, e := h.service.CreateMedicine(r.Context(), p, MedicineInput{q.Code, q.Name, q.ActiveIngredient, q.DosageForm, q.Strength, q.DispenseUnit, q.Description, q.Status, q.LowStockThreshold, q.InitialPrice, q.InitialEffectiveFrom})
	if e != nil {
		problem.Write(w, r, h.logger, "create medicine", e)
		return
	}
	httpx.WriteJSON(w, 201, m)
}
func (h *Handler) UpdateMedicine(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "medicineId")
	if !ok {
		return
	}
	var q MedicineUpdate
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	m, e := h.service.UpdateMedicine(r.Context(), p, id, q)
	if e != nil {
		problem.Write(w, r, h.logger, "update medicine", e)
		return
	}
	httpx.WriteJSON(w, 200, m)
}
func (h *Handler) State(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "medicineId")
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
		m, e := h.service.ChangeState(r.Context(), p, id, q.ExpectedVersion, action, q.Reason)
		if e != nil {
			problem.Write(w, r, h.logger, "medicine state", e)
			return
		}
		httpx.WriteJSON(w, 200, m)
	}
}
func (h *Handler) Prices(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "medicineId")
	if !ok {
		return
	}
	x, e := h.service.ListPrices(r.Context(), id)
	if e != nil {
		problem.Write(w, r, h.logger, "medicine prices", e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": x})
}
func (h *Handler) CreatePrice(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "medicineId")
	if !ok {
		return
	}
	var q struct {
		Amount    int64  `json:"unit_sale_price_vnd"`
		Effective string `json:"effective_from"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.CreatePrice(r.Context(), p, id, q.Amount, q.Effective)
	if e != nil {
		problem.Write(w, r, h.logger, "create medicine price", e)
		return
	}
	httpx.WriteJSON(w, 201, x)
}
func (h *Handler) CancelPrice(w http.ResponseWriter, r *http.Request) {
	mid, ok := httpx.PathInt64(w, r, "medicineId")
	if !ok {
		return
	}
	pid, ok := httpx.PathInt64(w, r, "priceVersionId")
	if !ok {
		return
	}
	var q struct {
		Reason string `json:"reason"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	p, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.CancelPrice(r.Context(), p, mid, pid, q.Reason)
	if e != nil {
		problem.Write(w, r, h.logger, "cancel medicine price", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Receipt(w http.ResponseWriter, r *http.Request) {
	var q ReceiptInput
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	q.IdempotencyKey = r.Header.Get("Idempotency-Key")
	p, _ := auth.PrincipalFromContext(r.Context())
	d, e := h.service.CreateReceipt(r.Context(), p, q)
	if e != nil {
		problem.Write(w, r, h.logger, "create receipt", e)
		return
	}
	if d.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, 201, d)
}
func (h *Handler) Adjustment(w http.ResponseWriter, r *http.Request) {
	var q AdjustmentInput
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	q.IdempotencyKey = r.Header.Get("Idempotency-Key")
	p, _ := auth.PrincipalFromContext(r.Context())
	d, e := h.service.CreateAdjustment(r.Context(), p, q)
	if e != nil {
		problem.Write(w, r, h.logger, "create adjustment", e)
		return
	}
	if d.Replayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, 201, d)
}
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	x, e := h.service.Summary(r.Context())
	if e != nil {
		problem.Write(w, r, h.logger, "inventory summary", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Batches(w http.ResponseWriter, r *http.Request) {
	idValue := r.URL.Query().Get("id")
	if v := r.PathValue("batchId"); v != "" {
		idValue = v
	}
	id, e1 := strconv.ParseInt(idValue, 10, 64)
	medicineValue := r.URL.Query().Get("medicine_id")
	medicine, e2 := strconv.ParseInt(medicineValue, 10, 64)
	if (idValue != "" && (e1 != nil || id <= 0)) || (medicineValue != "" && (e2 != nil || medicine <= 0)) {
		httpx.WriteError(w, 422, "INVENTORY_FILTER_INVALID", "Bộ lọc lô thuốc không hợp lệ.")
		return
	}
	page, size := httpx.Pagination(r)
	x, e := h.service.ListBatchesPage(r.Context(), id, medicine, r.URL.Query().Get("q"), page, size)
	if e != nil {
		problem.Write(w, r, h.logger, "inventory batches", e)
		return
	}
	httpx.WriteJSON(w, 200, x)
}
func (h *Handler) Documents(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var id int64
		if v := r.PathValue(map[string]string{"receipt": "receiptId", "adjustment": "adjustmentId"}[kind]); v != "" {
			id, _ = strconv.ParseInt(v, 10, 64)
		}
		x, e := h.service.ListDocuments(r.Context(), kind, id)
		if e != nil {
			problem.Write(w, r, h.logger, "inventory documents", e)
			return
		}
		httpx.WriteJSON(w, 200, map[string]any{"items": x})
	}
}
func (h *Handler) Movements(w http.ResponseWriter, r *http.Request) {
	mid, _ := strconv.ParseInt(r.URL.Query().Get("medicine_id"), 10, 64)
	bid, _ := strconv.ParseInt(r.URL.Query().Get("batch_id"), 10, 64)
	x, e := h.service.ListMovements(r.Context(), mid, bid)
	if e != nil {
		problem.Write(w, r, h.logger, "stock movements", e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": x})
}

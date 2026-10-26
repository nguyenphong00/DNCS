package billing

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/platform/problem"
)

type Handler struct {
	service *Service
	logger  *slog.Logger
}

func NewHandler(service *Service, logger *slog.Logger) *Handler { return &Handler{service, logger} }
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "encounterId")
	if !ok {
		return
	}
	var q struct {
		Method string `json:"payment_method"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	a, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.CreateInvoice(r.Context(), a, id, q.Method, r.Header.Get("Idempotency-Key"))
	if e != nil {
		problem.Write(w, r, h.logger, "create invoice", e)
		return
	}
	if x.IdempotentlyReplayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, 201, x)
}
func (h *Handler) List(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, _ := auth.PrincipalFromContext(r.Context())
		page, size := httpx.Pagination(r)
		q := r.URL.Query()
		patient, _ := strconv.ParseInt(q.Get("patient_id"), 10, 64)
		encounter, _ := strconv.ParseInt(q.Get("encounter_id"), 10, 64)
		to, toErr := parseDate(q.Get("date_to"), true)
		from, fromErr := parseDate(q.Get("date_from"), false)
		if fromErr != nil || toErr != nil {
			httpx.WriteError(w, http.StatusUnprocessableEntity, "INVOICE_FILTER_INVALID", "Khoảng ngày không hợp lệ.")
			return
		}
		items, e := h.service.ListPage(r.Context(), a, own, ListFilter{Status: q.Get("status"), PaymentMethod: q.Get("payment_method"), Query: q.Get("q"), PatientID: patient, EncounterID: encounter, DateFrom: from, DateTo: to, Page: page, PageSize: size})
		if e != nil {
			problem.Write(w, r, h.logger, "list invoices", e)
			return
		}
		httpx.WriteJSON(w, 200, items)
	}
}
func parseDate(v string, end bool) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	t, e := time.ParseInLocation("2006-01-02", v, time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60))
	if e != nil {
		return time.Time{}, e
	}
	if end {
		return t.Add(24*time.Hour - time.Nanosecond), nil
	}
	return t, nil
}
func (h *Handler) Get(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "invoiceId")
		if !ok {
			return
		}
		a, _ := auth.PrincipalFromContext(r.Context())
		x, e := h.service.Invoice(r.Context(), a, id, own)
		if e != nil {
			problem.Write(w, r, h.logger, "get invoice", e)
			return
		}
		httpx.WriteJSON(w, 200, x)
	}
}
func (h *Handler) QR(own bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "invoiceId")
		if !ok {
			return
		}
		a, _ := auth.PrincipalFromContext(r.Context())
		x, e := h.service.PaymentQR(r.Context(), a, id, own)
		if e != nil {
			problem.Write(w, r, h.logger, "get payment qr", e)
			return
		}
		httpx.WriteJSON(w, 200, x)
	}
}
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "invoiceId")
	if !ok {
		return
	}
	var q struct {
		Amount        int64  `json:"confirmed_amount_vnd"`
		External      string `json:"external_reference"`
		BankReference string `json:"bank_transaction_reference"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	a, _ := auth.PrincipalFromContext(r.Context())
	x, e := h.service.Confirm(r.Context(), a, id, ConfirmInput{q.Amount, q.External, q.BankReference, r.Header.Get("Idempotency-Key")})
	if e != nil {
		problem.Write(w, r, h.logger, "confirm payment", e)
		return
	}
	if x.IdempotentlyReplayed {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	httpx.WriteJSON(w, 200, x)
}

func (h *Handler) Pending(w http.ResponseWriter, r *http.Request) {
	a, _ := auth.PrincipalFromContext(r.Context())
	q := r.URL.Query()
	page, size := httpx.Pagination(r)
	from, e1 := parseDate(q.Get("from"), false)
	to, e2 := parseDate(q.Get("to"), true)
	doctor, e3 := strconv.ParseInt(q.Get("doctor_id"), 10, 64)
	if q.Get("doctor_id") == "" {
		e3 = nil
	}
	if e1 != nil || e2 != nil || e3 != nil || doctor < 0 || (!from.IsZero() && !to.IsZero() && from.After(to)) {
		httpx.WriteError(w, 422, "BILLING_FILTER_INVALID", "Bộ lọc chờ thu tiền không hợp lệ.")
		return
	}
	filter := PendingFilter{Query: q.Get("q"), InvoiceState: q.Get("invoice_state"), DateFrom: from, DateTo: to, DoctorID: doctor, Page: page, PageSize: size}
	if q.Get("with_page") == "true" {
		result, err := h.service.PendingPage(r.Context(), a, filter)
		if err != nil {
			problem.Write(w, r, h.logger, "pending billing", err)
			return
		}
		httpx.WriteJSON(w, 200, result)
		return
	}
	items, err := h.service.Pending(r.Context(), a, filter)
	if err != nil {
		problem.Write(w, r, h.logger, "pending billing", err)
		return
	}
	httpx.WriteJSON(w, 200, items)
}

package settings

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

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	return &Handler{service: service, logger: logger}
}
func (h *Handler) writeErr(w http.ResponseWriter, r *http.Request, op string, err error) {
	problem.Write(w, r, h.logger, op, err)
}

func (h *Handler) Appointment(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		v, e := h.service.Appointment(r.Context())
		if e != nil {
			h.writeErr(w, r, "get appointment settings", e)
			return
		}
		httpx.WriteJSON(w, 200, map[string]any{"items": v})
		return
	}
	var q struct {
		Auto     bool             `json:"appointment_auto_approval_enabled"`
		Min      int              `json:"appointment_booking_min_lead_minutes"`
		Max      int              `json:"appointment_booking_max_advance_days"`
		Early    int              `json:"appointment_check_in_early_minutes"`
		Late     int              `json:"appointment_check_in_late_minutes"`
		Grace    int              `json:"appointment_no_show_grace_minutes"`
		Holding  int              `json:"appointment_max_holding_future_per_patient"`
		Versions map[string]int64 `json:"expected_versions"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	a, _ := auth.PrincipalFromContext(r.Context())
	v, e := h.service.UpdateAppointment(r.Context(), a, AppointmentInput{q.Auto, q.Min, q.Max, q.Early, q.Late, q.Grace, q.Holding, q.Versions})
	if e != nil {
		h.writeErr(w, r, "update appointment settings", e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": v})
}
func (h *Handler) single(key string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			v, e := h.service.Settings(r.Context(), []string{key})
			if e != nil {
				h.writeErr(w, r, "get setting", e)
				return
			}
			httpx.WriteJSON(w, 200, v[0])
			return
		}
		var q struct {
			Value    int   `json:"value"`
			Expected int64 `json:"expected_version"`
		}
		if !httpx.DecodeJSON(w, r, &q) {
			return
		}
		a, _ := auth.PrincipalFromContext(r.Context())
		v, e := h.service.UpdateInteger(r.Context(), a, key, q.Value, q.Expected)
		if e != nil {
			h.writeErr(w, r, "update setting", e)
			return
		}
		httpx.WriteJSON(w, 200, v)
	}
}
func (h *Handler) Scheduling() http.HandlerFunc     { return h.single("default_slot_duration_minutes") }
func (h *Handler) InventoryAlert() http.HandlerFunc { return h.single("inventory_expiry_alert_days") }

func (h *Handler) Banks(w http.ResponseWriter, r *http.Request) {
	page, size := httpx.Pagination(r)
	include := r.URL.Query().Get("include_archived") == "true"
	items, e := h.service.Banks(r.Context(), include, page, size)
	if e != nil {
		h.writeErr(w, r, "list bank accounts", e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items, "page": page, "page_size": size})
}
func (h *Handler) Bank(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "bankAccountId")
	if !ok {
		return
	}
	b, e := h.service.Bank(r.Context(), id, true)
	if e != nil {
		h.writeErr(w, r, "get bank account", e)
		return
	}
	httpx.WriteJSON(w, 200, b)
}
func (h *Handler) CreateBank(w http.ResponseWriter, r *http.Request) {
	var q struct {
		BIN      string `json:"bank_bin"`
		BankName string `json:"bank_name"`
		Number   string `json:"account_number"`
		Name     string `json:"account_name"`
	}
	if !httpx.DecodeJSON(w, r, &q) {
		return
	}
	a, _ := auth.PrincipalFromContext(r.Context())
	b, e := h.service.CreateBank(r.Context(), a, q.BIN, q.BankName, q.Number, q.Name)
	if e != nil {
		h.writeErr(w, r, "create bank account", e)
		return
	}
	httpx.WriteJSON(w, 201, b)
}
func (h *Handler) BankCommand(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathInt64(w, r, "bankAccountId")
		if !ok {
			return
		}
		var q struct {
			Expected int64  `json:"expected_version"`
			Reason   string `json:"reason"`
		}
		if !httpx.DecodeJSON(w, r, &q) {
			return
		}
		a, _ := auth.PrincipalFromContext(r.Context())
		var b BankAccount
		var e error
		switch action {
		case "activate":
			b, e = h.service.ActivateBank(r.Context(), a, id, q.Expected)
		case "deactivate":
			b, e = h.service.DeactivateBank(r.Context(), a, id, q.Expected, q.Reason)
		default:
			b, e = h.service.ArchiveBank(r.Context(), a, id, q.Expected, q.Reason)
		}
		if e != nil {
			h.writeErr(w, r, "change bank account", e)
			return
		}
		httpx.WriteJSON(w, 200, b)
	}
}

func parseDate(v string, end bool) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, e := time.Parse(time.RFC3339, v); e == nil {
		return t, nil
	}
	t, e := time.Parse("2006-01-02", v)
	if e != nil {
		return time.Time{}, e
	}
	if end {
		return t.Add(24*time.Hour - time.Nanosecond), nil
	}
	return t, nil
}
func (h *Handler) AuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	actor, actorErr := strconv.ParseInt(q.Get("actor_user_id"), 10, 64)
	target, targetErr := strconv.ParseInt(q.Get("target_id"), 10, 64)
	if (q.Get("actor_user_id") != "" && (actorErr != nil || actor <= 0)) || (q.Get("target_id") != "" && (targetErr != nil || target <= 0)) {
		httpx.WriteError(w, 422, "AUDIT_FILTER_INVALID", "Người thực hiện hoặc đối tượng không hợp lệ.")
		return
	}
	size, _ := strconv.Atoi(q.Get("page_size"))
	from, e1 := parseDate(q.Get("date_from"), false)
	to, e2 := parseDate(q.Get("date_to"), true)
	if e1 != nil || e2 != nil {
		httpx.WriteError(w, 422, "AUDIT_FILTER_INVALID", "Khoảng ngày audit không hợp lệ.")
		return
	}
	items, next, e := h.service.AuditLogs(r.Context(), AuditFilter{Module: q.Get("module"), Action: q.Get("action_code"), Outcome: q.Get("outcome"), TargetType: q.Get("target_type"), CorrelationID: q.Get("correlation_id"), Cursor: q.Get("cursor"), ActorUserID: actor, TargetID: target, From: from, To: to, PageSize: size})
	if e != nil {
		h.writeErr(w, r, "list audit logs", e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}
func (h *Handler) AuditLog(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.PathInt64(w, r, "auditLogId")
	if !ok {
		return
	}
	a, e := h.service.AuditLog(r.Context(), id)
	if e != nil {
		h.writeErr(w, r, "get audit log", e)
		return
	}
	httpx.WriteJSON(w, 200, a)
}

func (h *Handler) AuditActors(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.AuditActors(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		h.writeErr(w, r, "audit actor lookup", err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items})
}
func (h *Handler) AuditTargets(w http.ResponseWriter, r *http.Request) {
	items, types, err := h.service.AuditTargets(r.Context(), r.URL.Query().Get("target_type"), r.URL.Query().Get("q"))
	if err != nil {
		h.writeErr(w, r, "audit target lookup", err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items, "target_types": types})
}

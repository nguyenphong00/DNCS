package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"dncs/backend/internal/appointment"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/billing"
	"dncs/backend/internal/clinic"
	"dncs/backend/internal/database"
	"dncs/backend/internal/encounter"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/patient"
	"dncs/backend/internal/platform/httpx"
	"dncs/backend/internal/prescription"
	"dncs/backend/internal/scheduling"
	"dncs/backend/internal/servicecatalog"
	"dncs/backend/internal/settings"
	"dncs/backend/internal/staff"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(
	pool *pgxpool.Pool,
	authHandler *auth.Handler,
	authenticator *auth.Authenticator,
	authorizationStore *authorization.Store,
	logger *slog.Logger,
	options ...Options,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", readinessHandler(func(ctx context.Context) error {
		return database.CheckReadiness(ctx, pool)
	}))

	mux.HandleFunc("POST /api/v1/auth/staff/login", authHandler.StaffLogin)
	mux.HandleFunc("POST /api/v1/auth/patient/login", authHandler.PatientLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)

	return newApplicationRouter(mux, pool, authHandler, authenticator, authorizationStore, nil, logger, resolveOptions(options))
}

type Handlers struct {
	Staff        *staff.Handler
	Patient      *patient.Handler
	Clinic       *clinic.Handler
	Scheduling   *scheduling.Handler
	Appointment  *appointment.Handler
	Service      *servicecatalog.Handler
	Encounter    *encounter.Handler
	Inventory    *inventory.Handler
	Prescription *prescription.Handler
	Settings     *settings.Handler
	Billing      *billing.Handler
}

func NewApplicationRouter(
	pool *pgxpool.Pool,
	authHandler *auth.Handler,
	authenticator *auth.Authenticator,
	authorizationStore *authorization.Store,
	handlers Handlers,
	logger *slog.Logger,
	options ...Options,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /health/ready", readinessHandler(func(ctx context.Context) error {
		return database.CheckReadiness(ctx, pool)
	}))
	mux.HandleFunc("POST /api/v1/auth/staff/login", authHandler.StaffLogin)
	mux.HandleFunc("POST /api/v1/auth/patient/login", authHandler.PatientLogin)
	mux.HandleFunc("POST /api/v1/auth/patient/register", handlers.Patient.Register)
	mux.HandleFunc("POST /api/v1/auth/refresh", authHandler.Refresh)
	mux.HandleFunc("POST /api/v1/auth/logout", authHandler.Logout)
	return newApplicationRouter(mux, pool, authHandler, authenticator, authorizationStore, &handlers, logger, resolveOptions(options))
}

func readinessHandler(check func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := check(ctx); err != nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "NOT_READY", "Database chưa sẵn sàng.")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func newApplicationRouter(mux *http.ServeMux, pool *pgxpool.Pool, authHandler *auth.Handler, authenticator *auth.Authenticator, authorizationStore *authorization.Store, handlers *Handlers, logger *slog.Logger, options Options) http.Handler {
	authorizer := authorization.NewMiddleware(authorizationStore, logger)
	secure := func(handler http.Handler) http.Handler { return authenticator.Middleware(handler) }
	require := func(permission string, handler http.Handler) http.Handler {
		return secure(authorizer.Require(permission, handler))
	}
	requireAny := func(permissions []string, handler http.Handler) http.Handler {
		return secure(authorizer.RequireAny(permissions, handler))
	}
	requireAll := func(permissions []string, handler http.Handler) http.Handler {
		return secure(authorizer.RequireAll(permissions, handler))
	}

	mux.Handle("POST /api/v1/auth/logout-all", authenticator.Middleware(http.HandlerFunc(authHandler.LogoutAll)))
	mux.Handle("POST /api/v1/auth/change-password", authenticator.Middleware(http.HandlerFunc(authHandler.ChangePassword)))
	mux.Handle("POST /api/v1/accounts/{userId}/reset-password", require("account.password.reset", http.HandlerFunc(authHandler.ResetPassword)))
	mux.Handle("POST /api/v1/accounts/{userId}/lock", require("account.status.manage", http.HandlerFunc(authHandler.LockAccount)))
	mux.Handle("POST /api/v1/accounts/{userId}/unlock", require("account.status.manage", http.HandlerFunc(authHandler.UnlockAccount)))
	mux.Handle("POST /api/v1/accounts/{userId}/deactivate", require("account.status.manage", http.HandlerFunc(authHandler.DeactivateAccount)))
	mux.Handle("GET /api/v1/roles", require("user.role.assign", authorizationStore.RolesHandler(logger)))
	mux.Handle("GET /api/v1/me", authenticator.Middleware(auth.RequirePasswordChanged(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			principal, _ := auth.PrincipalFromContext(r.Context())
			permissions, err := authorizationStore.ListEffective(r.Context(), principal)
			if err != nil {
				logger.ErrorContext(r.Context(), "load current user permissions", "error", err)
				httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Không thể xử lý yêu cầu.")
				return
			}
			httpx.WriteJSON(w, http.StatusOK, struct {
				Principal   auth.Principal `json:"principal"`
				Permissions []string       `json:"permissions"`
			}{Principal: principal, Permissions: permissions})
		},
	))))

	if handlers != nil {
		// P1 — staff and doctor profiles.
		mux.Handle("GET /api/v1/staff", require("staff.view.all", http.HandlerFunc(handlers.Staff.List)))
		mux.Handle("GET /api/v1/staff/{staffId}", require("staff.view.all", http.HandlerFunc(handlers.Staff.Get)))
		mux.Handle("GET /api/v1/me/staff-profile", require("staff.view.own", http.HandlerFunc(handlers.Staff.GetMe)))
		mux.Handle("POST /api/v1/staff/accounts", requireAll([]string{"staff.account.create", "user.role.assign"}, http.HandlerFunc(handlers.Staff.Create)))
		mux.Handle("PATCH /api/v1/staff/{staffId}", require("staff.manage", http.HandlerFunc(handlers.Staff.Update)))
		mux.Handle("POST /api/v1/staff/{staffId}/activate", requireAll([]string{"staff.manage", "account.status.manage"}, handlers.Staff.ChangeStatus("activate")))
		mux.Handle("POST /api/v1/staff/{staffId}/deactivate", requireAll([]string{"staff.manage", "account.status.manage"}, handlers.Staff.ChangeStatus("deactivate")))
		mux.Handle("POST /api/v1/staff/{staffId}/terminate", requireAll([]string{"staff.manage", "account.status.manage"}, handlers.Staff.ChangeStatus("terminate")))
		mux.Handle("DELETE /api/v1/staff/{staffId}", requireAll([]string{"staff.manage", "account.status.manage"}, http.HandlerFunc(handlers.Staff.Archive)))
		mux.Handle("POST /api/v1/staff/{staffId}/restore", requireAll([]string{"staff.manage", "account.status.manage"}, http.HandlerFunc(handlers.Staff.Restore)))
		mux.Handle("POST /api/v1/staff/{staffId}/doctor-profile", require("staff.manage", http.HandlerFunc(handlers.Staff.CreateDoctor)))
		mux.Handle("POST /api/v1/staff/{staffId}/doctor-profile/activate", require("staff.manage", handlers.Staff.ChangeDoctorStatus(true)))
		mux.Handle("POST /api/v1/staff/{staffId}/doctor-profile/deactivate", require("staff.manage", handlers.Staff.ChangeDoctorStatus(false)))

		// P1 — patient aggregate.
		mux.Handle("POST /api/v1/patients/accounts", require("patient.account.create_at_counter", http.HandlerFunc(handlers.Patient.CreateAtCounter)))
		mux.Handle("GET /api/v1/patients", requireAny([]string{"patient.view.all", "patient.manage.all", "patient.view.assigned_or_history"}, http.HandlerFunc(handlers.Patient.List)))
		mux.Handle("GET /api/v1/patients/{patientId}", requireAny([]string{"patient.view.all", "patient.manage.all", "patient.view.assigned_or_history", "patient.view.own"}, http.HandlerFunc(handlers.Patient.Get)))
		mux.Handle("PATCH /api/v1/patients/{patientId}", require("patient.manage.all", handlers.Patient.Update(false)))
		mux.Handle("POST /api/v1/patients/{patientId}/phone-change", require("patient.phone.change.at_counter", http.HandlerFunc(handlers.Patient.PhoneChange)))
		mux.Handle("GET /api/v1/me/patient-profile", require("patient.view.own", http.HandlerFunc(handlers.Patient.GetMe)))
		mux.Handle("PATCH /api/v1/me/patient-profile", require("patient.update.own", handlers.Patient.Update(true)))
		mux.Handle("POST /api/v1/patients/{patientId}/archive", requireAll([]string{"patient.manage.all", "account.status.manage"}, http.HandlerFunc(handlers.Patient.Archive)))
		mux.Handle("POST /api/v1/patients/{patientId}/restore", requireAll([]string{"patient.manage.all", "account.status.manage"}, http.HandlerFunc(handlers.Patient.Restore)))

		// P1 — clinic master data.
		mux.Handle("GET /api/v1/specialties", requireAny([]string{"specialty.view", "appointment.slot.search"}, http.HandlerFunc(handlers.Clinic.ListSpecialties)))
		mux.Handle("GET /api/v1/specialties/{specialtyId}", require("specialty.view", http.HandlerFunc(handlers.Clinic.GetSpecialty)))
		mux.Handle("POST /api/v1/specialties", require("specialty.manage", http.HandlerFunc(handlers.Clinic.CreateSpecialty)))
		mux.Handle("PATCH /api/v1/specialties/{specialtyId}", require("specialty.manage", http.HandlerFunc(handlers.Clinic.UpdateSpecialty)))
		mux.Handle("POST /api/v1/specialties/{specialtyId}/activate", require("specialty.manage", handlers.Clinic.ChangeSpecialty("activate")))
		mux.Handle("POST /api/v1/specialties/{specialtyId}/deactivate", require("specialty.manage", handlers.Clinic.ChangeSpecialty("deactivate")))
		mux.Handle("DELETE /api/v1/specialties/{specialtyId}", require("specialty.manage", handlers.Clinic.ChangeSpecialty("archive")))
		mux.Handle("GET /api/v1/specialties/{specialtyId}/doctors", requireAny([]string{"specialty.view", "appointment.slot.search"}, http.HandlerFunc(handlers.Clinic.ListDoctors)))
		mux.Handle("GET /api/v1/rooms", require("room.view", http.HandlerFunc(handlers.Clinic.ListRooms)))
		mux.Handle("GET /api/v1/rooms/{roomId}", require("room.view", http.HandlerFunc(handlers.Clinic.GetRoom)))
		mux.Handle("POST /api/v1/rooms", require("room.manage", http.HandlerFunc(handlers.Clinic.CreateRoom)))
		mux.Handle("PATCH /api/v1/rooms/{roomId}", require("room.manage", http.HandlerFunc(handlers.Clinic.UpdateRoom)))
		mux.Handle("POST /api/v1/rooms/{roomId}/activate", require("room.manage", handlers.Clinic.ChangeRoom("activate")))
		mux.Handle("POST /api/v1/rooms/{roomId}/deactivate", require("room.manage", handlers.Clinic.ChangeRoom("deactivate")))
		mux.Handle("DELETE /api/v1/rooms/{roomId}", require("room.manage", handlers.Clinic.ChangeRoom("archive")))
		mux.Handle("GET /api/v1/doctors/{doctorId}/specialties", require("specialty.view", http.HandlerFunc(handlers.Clinic.DoctorSpecialties)))
		mux.Handle("POST /api/v1/doctors/{doctorId}/specialties/{specialtyId}", require("doctor_specialty.manage", http.HandlerFunc(handlers.Clinic.AssignDoctor)))
		mux.Handle("DELETE /api/v1/doctors/{doctorId}/specialties/{specialtyId}", require("doctor_specialty.manage", http.HandlerFunc(handlers.Clinic.RemoveDoctor)))

		// P2 — schedules and appointments.
		mux.Handle("GET /api/v1/doctor-schedules", requireAny([]string{"doctor_schedule.view.all", "doctor_schedule.view.own"}, http.HandlerFunc(handlers.Scheduling.List)))
		mux.Handle("GET /api/v1/doctor-schedules/{scheduleId}", requireAny([]string{"doctor_schedule.view.all", "doctor_schedule.view.own"}, http.HandlerFunc(handlers.Scheduling.Get)))
		mux.Handle("POST /api/v1/doctor-schedules", require("doctor_schedule.manage", http.HandlerFunc(handlers.Scheduling.Create)))
		mux.Handle("PATCH /api/v1/doctor-schedules/{scheduleId}", require("doctor_schedule.manage", http.HandlerFunc(handlers.Scheduling.Update)))
		mux.Handle("POST /api/v1/doctor-schedules/{scheduleId}/activate", require("doctor_schedule.manage", http.HandlerFunc(handlers.Scheduling.Activate)))
		mux.Handle("POST /api/v1/doctor-schedules/{scheduleId}/cancel", require("doctor_schedule.manage", http.HandlerFunc(handlers.Scheduling.Cancel)))
		mux.Handle("POST /api/v1/doctor-schedules/{scheduleId}/archive", require("doctor_schedule.manage", http.HandlerFunc(handlers.Scheduling.Archive)))
		mux.Handle("GET /api/v1/appointment-slots", require("appointment.slot.search", http.HandlerFunc(handlers.Appointment.Slots)))
		mux.Handle("POST /api/v1/appointments", requireAny([]string{"appointment.book.patient_own", "appointment.book.at_counter"}, http.HandlerFunc(handlers.Appointment.Book)))
		mux.Handle("GET /api/v1/appointments", requireAny([]string{"appointment.view.all", "appointment.view.doctor_own"}, handlers.Appointment.List(false)))
		mux.Handle("GET /api/v1/me/appointments", require("appointment.view.patient_own", handlers.Appointment.List(true)))
		mux.Handle("GET /api/v1/appointments/{appointmentId}", requireAny([]string{"appointment.view.all", "appointment.view.doctor_own", "appointment.view.patient_own"}, http.HandlerFunc(handlers.Appointment.Get)))
		mux.Handle("POST /api/v1/appointments/{appointmentId}/approve", requireAny([]string{"appointment.approve.all", "appointment.approve.doctor_own"}, handlers.Appointment.Transition("approve")))
		mux.Handle("POST /api/v1/appointments/{appointmentId}/reject", requireAny([]string{"appointment.approve.all", "appointment.approve.doctor_own"}, handlers.Appointment.Transition("reject")))
		mux.Handle("POST /api/v1/appointments/{appointmentId}/cancel", requireAny([]string{"appointment.cancel.all", "appointment.cancel.patient_own"}, handlers.Appointment.Transition("cancel")))
		mux.Handle("POST /api/v1/appointments/{appointmentId}/check-in", require("appointment.check_in", handlers.Appointment.Transition("check-in")))
		mux.Handle("POST /api/v1/appointments/{appointmentId}/no-show", require("appointment.no_show.mark", handlers.Appointment.Transition("no-show")))

		// P3 — service catalog and clinical encounter.
		mux.Handle("GET /api/v1/services", require("service.view", http.HandlerFunc(handlers.Service.List)))
		mux.Handle("GET /api/v1/services/{serviceId}", require("service.view", http.HandlerFunc(handlers.Service.Get)))
		mux.Handle("POST /api/v1/services", require("service.manage", http.HandlerFunc(handlers.Service.Create)))
		mux.Handle("PATCH /api/v1/services/{serviceId}", require("service.manage", http.HandlerFunc(handlers.Service.Update)))
		mux.Handle("POST /api/v1/services/{serviceId}/activate", require("service.manage", handlers.Service.ChangeState("activate")))
		mux.Handle("POST /api/v1/services/{serviceId}/deactivate", require("service.manage", handlers.Service.ChangeState("deactivate")))
		mux.Handle("POST /api/v1/services/{serviceId}/archive", require("service.manage", handlers.Service.ChangeState("archive")))
		mux.Handle("GET /api/v1/services/{serviceId}/price-versions", require("service.manage", http.HandlerFunc(handlers.Service.ListPrices)))
		mux.Handle("POST /api/v1/services/{serviceId}/price-versions", require("service.manage", http.HandlerFunc(handlers.Service.CreatePrice)))
		mux.Handle("POST /api/v1/services/{serviceId}/price-versions/{priceVersionId}/cancel", require("service.manage", http.HandlerFunc(handlers.Service.CancelPrice)))

		mux.Handle("POST /api/v1/appointments/{appointmentId}/start", require("encounter.create.assigned", http.HandlerFunc(handlers.Encounter.Start)))
		mux.Handle("POST /api/v1/appointments/{appointmentId}/complete", require("encounter.finalize.assigned", http.HandlerFunc(handlers.Encounter.Complete)))
		mux.Handle("GET /api/v1/encounters", requireAny([]string{"encounter.view.all", "encounter.view.assigned_or_history"}, handlers.Encounter.List(false)))
		mux.Handle("GET /api/v1/me/encounters", require("encounter.view.patient_own", handlers.Encounter.List(true)))
		mux.Handle("GET /api/v1/encounters/{encounterId}", requireAny([]string{"encounter.view.all", "encounter.view.assigned_or_history", "encounter.view.patient_own"}, http.HandlerFunc(handlers.Encounter.Get)))
		mux.Handle("PATCH /api/v1/encounters/{encounterId}", require("encounter.update.assigned", http.HandlerFunc(handlers.Encounter.Update)))
		mux.Handle("POST /api/v1/encounters/{encounterId}/vital-signs", require("encounter.update.assigned", http.HandlerFunc(handlers.Encounter.CreateVital)))
		mux.Handle("PATCH /api/v1/encounters/{encounterId}/vital-signs/{vitalSignId}", require("encounter.update.assigned", http.HandlerFunc(handlers.Encounter.UpdateVital)))
		mux.Handle("GET /api/v1/encounters/{encounterId}/services", requireAny([]string{"encounter.view.all", "encounter.view.assigned_or_history", "encounter.view.patient_own"}, http.HandlerFunc(handlers.Encounter.ListServices)))
		mux.Handle("POST /api/v1/encounters/{encounterId}/services", require("encounter.service.assign", http.HandlerFunc(handlers.Encounter.AddService)))
		mux.Handle("PATCH /api/v1/encounters/{encounterId}/services/{serviceOrderId}", require("encounter.service.assign", http.HandlerFunc(handlers.Encounter.UpdateService)))
		mux.Handle("DELETE /api/v1/encounters/{encounterId}/services/{serviceOrderId}", require("encounter.service.assign", http.HandlerFunc(handlers.Encounter.RemoveService)))
		mux.Handle("GET /api/v1/encounters/{encounterId}/amendments", requireAny([]string{"encounter.view.all", "encounter.view.assigned_or_history", "encounter.view.patient_own"}, http.HandlerFunc(handlers.Encounter.ListAmendments)))
		mux.Handle("POST /api/v1/encounters/{encounterId}/amendments", requireAny([]string{"encounter.amend.assigned", "encounter.amend.all"}, http.HandlerFunc(handlers.Encounter.AddAmendment)))
		mux.Handle("POST /api/v1/encounters/{encounterId}/follow-up-appointments", require("appointment.follow_up.create", http.HandlerFunc(handlers.Appointment.FollowUp)))

		// P4 — medicine inventory and prescription.
		mux.Handle("GET /api/v1/medicines", require("medicine.view", http.HandlerFunc(handlers.Inventory.ListMedicines)))
		mux.Handle("GET /api/v1/medicines/{medicineId}", require("medicine.view", http.HandlerFunc(handlers.Inventory.GetMedicine)))
		mux.Handle("POST /api/v1/medicines", require("medicine.manage", http.HandlerFunc(handlers.Inventory.CreateMedicine)))
		mux.Handle("PATCH /api/v1/medicines/{medicineId}", require("medicine.manage", http.HandlerFunc(handlers.Inventory.UpdateMedicine)))
		mux.Handle("POST /api/v1/medicines/{medicineId}/activate", require("medicine.manage", handlers.Inventory.State("activate")))
		mux.Handle("POST /api/v1/medicines/{medicineId}/deactivate", require("medicine.manage", handlers.Inventory.State("deactivate")))
		mux.Handle("POST /api/v1/medicines/{medicineId}/archive", require("medicine.manage", handlers.Inventory.State("archive")))
		mux.Handle("GET /api/v1/medicines/{medicineId}/price-versions", require("medicine.manage", http.HandlerFunc(handlers.Inventory.Prices)))
		mux.Handle("POST /api/v1/medicines/{medicineId}/price-versions", require("medicine.manage", http.HandlerFunc(handlers.Inventory.CreatePrice)))
		mux.Handle("POST /api/v1/medicines/{medicineId}/price-versions/{priceVersionId}/cancel", require("medicine.manage", http.HandlerFunc(handlers.Inventory.CancelPrice)))
		mux.Handle("GET /api/v1/inventory/summary", require("inventory.view", http.HandlerFunc(handlers.Inventory.Summary)))
		mux.Handle("GET /api/v1/inventory/batches", require("inventory.view", http.HandlerFunc(handlers.Inventory.Batches)))
		mux.Handle("GET /api/v1/inventory/batches/{batchId}", require("inventory.view", http.HandlerFunc(handlers.Inventory.Batches)))
		mux.Handle("POST /api/v1/inventory/receipts", require("inventory.receipt.manage", http.HandlerFunc(handlers.Inventory.Receipt)))
		mux.Handle("GET /api/v1/inventory/receipts", require("inventory.receipt.manage", handlers.Inventory.Documents("receipt")))
		mux.Handle("GET /api/v1/inventory/receipts/{receiptId}", require("inventory.receipt.manage", handlers.Inventory.Documents("receipt")))
		mux.Handle("POST /api/v1/inventory/adjustments", require("inventory.adjust", http.HandlerFunc(handlers.Inventory.Adjustment)))
		mux.Handle("GET /api/v1/inventory/adjustments", require("inventory.adjust", handlers.Inventory.Documents("adjustment")))
		mux.Handle("GET /api/v1/inventory/movements", require("inventory.movement.view", http.HandlerFunc(handlers.Inventory.Movements)))

		mux.Handle("GET /api/v1/prescription-series", requireAny([]string{"prescription.view.all", "prescription.view.assigned_or_history"}, http.HandlerFunc(handlers.Prescription.List)))
		mux.Handle("GET /api/v1/prescription-series/{seriesId}", requireAny([]string{"prescription.view.all", "prescription.view.assigned_or_history", "prescription.view.patient_own"}, http.HandlerFunc(handlers.Prescription.GetSeries)))
		mux.Handle("GET /api/v1/prescription-series/{seriesId}/versions", requireAny([]string{"prescription.view.all", "prescription.view.assigned_or_history", "prescription.view.patient_own"}, http.HandlerFunc(handlers.Prescription.Versions)))
		mux.Handle("GET /api/v1/prescriptions/{prescriptionId}", requireAny([]string{"prescription.view.all", "prescription.view.assigned_or_history", "prescription.view.patient_own"}, http.HandlerFunc(handlers.Prescription.Get)))
		mux.Handle("GET /api/v1/me/prescriptions", require("prescription.view.patient_own", http.HandlerFunc(handlers.Prescription.Me)))
		mux.Handle("GET /api/v1/encounters/{encounterId}/prescription-series", requireAny([]string{"prescription.view.all", "prescription.view.assigned_or_history", "prescription.view.patient_own"}, http.HandlerFunc(handlers.Prescription.Encounter)))
		mux.Handle("POST /api/v1/encounters/{encounterId}/prescription-series", require("prescription.draft.manage.assigned", http.HandlerFunc(handlers.Prescription.Create)))
		mux.Handle("PATCH /api/v1/prescriptions/{prescriptionId}", require("prescription.draft.manage.assigned", http.HandlerFunc(handlers.Prescription.Update)))
		mux.Handle("POST /api/v1/prescriptions/{prescriptionId}/items", require("prescription.draft.manage.assigned", http.HandlerFunc(handlers.Prescription.AddItem)))
		mux.Handle("PATCH /api/v1/prescriptions/{prescriptionId}/items/{itemId}", require("prescription.draft.manage.assigned", http.HandlerFunc(handlers.Prescription.UpdateItem)))
		mux.Handle("DELETE /api/v1/prescriptions/{prescriptionId}/items/{itemId}", require("prescription.draft.manage.assigned", http.HandlerFunc(handlers.Prescription.RemoveItem)))
		mux.Handle("POST /api/v1/prescriptions/{prescriptionId}/issue", require("prescription.issue.assigned", http.HandlerFunc(handlers.Prescription.Issue)))
		mux.Handle("POST /api/v1/prescriptions/{prescriptionId}/discard", require("prescription.draft.manage.assigned", http.HandlerFunc(handlers.Prescription.Discard)))
		mux.Handle("POST /api/v1/prescription-series/{seriesId}/versions", require("prescription.version.create.own", http.HandlerFunc(handlers.Prescription.Replacement)))

		// P5 — typed settings, receiving bank accounts and audit explorer.
		mux.Handle("GET /api/v1/settings/appointment", require("settings.appointment.manage", http.HandlerFunc(handlers.Settings.Appointment)))
		mux.Handle("PUT /api/v1/settings/appointment", require("settings.appointment.manage", http.HandlerFunc(handlers.Settings.Appointment)))
		mux.Handle("GET /api/v1/settings/scheduling", require("settings.appointment.manage", handlers.Settings.Scheduling()))
		mux.Handle("PATCH /api/v1/settings/scheduling", require("settings.appointment.manage", handlers.Settings.Scheduling()))
		mux.Handle("GET /api/v1/settings/inventory-alert", require("settings.inventory_alert.manage", handlers.Settings.InventoryAlert()))
		mux.Handle("PATCH /api/v1/settings/inventory-alert", require("settings.inventory_alert.manage", handlers.Settings.InventoryAlert()))
		mux.Handle("GET /api/v1/settings/bank-accounts", require("settings.bank_account.manage", http.HandlerFunc(handlers.Settings.Banks)))
		mux.Handle("GET /api/v1/settings/bank-accounts/{bankAccountId}", require("settings.bank_account.manage", http.HandlerFunc(handlers.Settings.Bank)))
		mux.Handle("POST /api/v1/settings/bank-accounts", require("settings.bank_account.manage", http.HandlerFunc(handlers.Settings.CreateBank)))
		mux.Handle("POST /api/v1/settings/bank-accounts/{bankAccountId}/activate", require("settings.bank_account.manage", handlers.Settings.BankCommand("activate")))
		mux.Handle("POST /api/v1/settings/bank-accounts/{bankAccountId}/deactivate", require("settings.bank_account.manage", handlers.Settings.BankCommand("deactivate")))
		mux.Handle("POST /api/v1/settings/bank-accounts/{bankAccountId}/archive", require("settings.bank_account.manage", handlers.Settings.BankCommand("archive")))
		mux.Handle("GET /api/v1/audit-log-actors", require("audit_log.view", http.HandlerFunc(handlers.Settings.AuditActors)))
		mux.Handle("GET /api/v1/audit-log-targets", require("audit_log.view", http.HandlerFunc(handlers.Settings.AuditTargets)))
		mux.Handle("GET /api/v1/audit-logs", require("audit_log.view", http.HandlerFunc(handlers.Settings.AuditLogs)))
		mux.Handle("GET /api/v1/audit-logs/{auditLogId}", require("audit_log.view", http.HandlerFunc(handlers.Settings.AuditLog)))

		// P5 — immutable invoice/payment snapshots and payment-time FEFO deduction.
		mux.Handle("GET /api/v1/billing/pending-encounters", require("billing.pending_encounter.view", http.HandlerFunc(handlers.Billing.Pending)))
		mux.Handle("GET /api/v1/invoices", require("invoice.view.all", handlers.Billing.List(false)))
		mux.Handle("GET /api/v1/invoices/{invoiceId}", requireAny([]string{"invoice.view.all", "invoice.view.patient_own"}, handlers.Billing.Get(false)))
		mux.Handle("GET /api/v1/me/invoices", require("invoice.view.patient_own", handlers.Billing.List(true)))
		mux.Handle("GET /api/v1/me/invoices/{invoiceId}", require("invoice.view.patient_own", handlers.Billing.Get(true)))
		mux.Handle("POST /api/v1/encounters/{encounterId}/invoices", require("invoice.create", http.HandlerFunc(handlers.Billing.Create)))
		mux.Handle("GET /api/v1/invoices/{invoiceId}/payment-qr", requireAll([]string{"payment.qr.generate", "invoice.view.all"}, handlers.Billing.QR(false)))
		mux.Handle("GET /api/v1/me/invoices/{invoiceId}/payment-qr", require("payment.qr.view.patient_own", handlers.Billing.QR(true)))
		mux.Handle("POST /api/v1/invoices/{invoiceId}/payments/confirm", require("payment.confirm", http.HandlerFunc(handlers.Billing.Confirm)))
	}

	return wrapHardening(mux, logger, options)
}

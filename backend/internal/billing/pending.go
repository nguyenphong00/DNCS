package billing

import (
	"context"
	"dncs/backend/internal/platform/pagination"
	"math"
	"strings"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/platform/problem"
	"github.com/jackc/pgx/v5"
)

type PendingFilter struct {
	Query, InvoiceState string
	DateFrom, DateTo    time.Time
	DoctorID            int64
	Page, PageSize      int
}
type PendingInvoice struct {
	ID            int64  `json:"id"`
	InvoiceCode   string `json:"invoice_code"`
	Status        string `json:"status"`
	PaymentMethod string `json:"payment_method"`
}
type PendingEncounter struct {
	EncounterID              int64           `json:"encounter_id"`
	EncounterCode            string          `json:"encounter_code"`
	AppointmentCode          string          `json:"appointment_code"`
	PatientID                int64           `json:"patient_id"`
	PatientCode              string          `json:"patient_code"`
	PatientName              string          `json:"patient_name"`
	PatientPhone             string          `json:"patient_phone"`
	DoctorName               string          `json:"doctor_name"`
	SpecialtyName            string          `json:"specialty_name"`
	FinalizedAt              time.Time       `json:"finalized_at"`
	ExpectedTotalVND         int64           `json:"expected_total_vnd"`
	ExpectedServiceTotalVND  int64           `json:"expected_service_total_vnd"`
	ExpectedMedicineTotalVND int64           `json:"expected_medicine_total_vnd"`
	ExistingInvoice          *PendingInvoice `json:"existing_invoice,omitempty"`
	BillingError             string          `json:"billing_error,omitempty"`
}

func sourceTotals(services []serviceSource, medicines []medicineSource) (total, service, medicine int64, err error) {
	add := func(price int64, q int16, subtotal *int64) error {
		if q <= 0 || price < 0 || price > math.MaxInt64/int64(q) || total > math.MaxInt64-price*int64(q) {
			return problem.New(422, "INVOICE_TOTAL_INVALID", "Tổng hóa đơn vượt giới hạn.")
		}
		value := price * int64(q)
		total += value
		*subtotal += value
		return nil
	}
	for _, x := range services {
		if err = add(x.price, x.quantity, &service); err != nil {
			return
		}
	}
	for _, x := range medicines {
		if err = add(x.price, x.quantity, &medicine); err != nil {
			return
		}
	}
	return
}

// Pending uses a read-only consistent snapshot. Preview never locks or allocates stock.
func (s *Service) Pending(ctx context.Context, actor auth.Principal, f PendingFilter) ([]PendingEncounter, error) {
	return s.pending(ctx, actor, f, false)
}

func (s *Service) PendingPage(ctx context.Context, actor auth.Principal, f PendingFilter) (pagination.Page[PendingEncounter], error) {
	// Preserve the legacy invalid-page rejection instead of clamping its offset.
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	items, err := s.pending(ctx, actor, f, true)
	return pagination.From(items, f.Page, f.PageSize), err
}

func (s *Service) pending(ctx context.Context, actor auth.Principal, f PendingFilter, extra bool) ([]PendingEncounter, error) {
	d, err := s.authz.Allowed(ctx, actor, "billing.pending_encounter.view")
	if err != nil {
		return nil, err
	}
	if !d.Allowed {
		return nil, problem.New(403, "FORBIDDEN", "Không có quyền xem chờ thu tiền.")
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 100
	}
	if f.Page-1 > math.MaxInt/f.PageSize {
		return nil, problem.New(422, "BILLING_FILTER_INVALID", "Trang vượt giới hạn.")
	}
	state := strings.ToLower(f.InvoiceState)
	if state == "" {
		state = "all"
	}
	if state != "all" && state != "none" && state != "unpaid" {
		return nil, problem.New(422, "BILLING_FILTER_INVALID", "Trạng thái hóa đơn không hợp lệ.")
	}
	fetch := f.PageSize
	if extra {
		fetch++
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT e.id,e.encounter_code,a.appointment_code,e.patient_id,e.patient_code_at_start,e.patient_name_at_start,COALESCE(u.phone_number,''),e.doctor_name_at_start,e.specialty_name_at_start,e.finalized_at,i.id,i.invoice_code,i.status::text,pay.payment_method::text
 FROM medical_encounters e JOIN patients p ON p.id=e.patient_id JOIN users u ON u.id=p.user_id
 JOIN appointments a ON a.id=e.appointment_id
 LEFT JOIN invoices i ON i.encounter_id=e.id AND i.status='UNPAID'
 LEFT JOIN payments pay ON pay.invoice_id=i.id
 WHERE e.status='FINALIZED' AND NOT EXISTS(SELECT 1 FROM invoices paid WHERE paid.encounter_id=e.id AND paid.status='PAID')
 AND ($1='' OR e.patient_name_at_start ILIKE '%'||$1||'%' OR e.patient_code_at_start ILIKE '%'||$1||'%' OR e.encounter_code ILIKE '%'||$1||'%' OR u.phone_number ILIKE '%'||$1||'%')
 AND ($2::timestamptz IS NULL OR e.finalized_at >= $2) AND ($3::timestamptz IS NULL OR e.finalized_at <= $3)
 AND ($4::bigint=0 OR e.doctor_profile_id=$4) AND ($5='all' OR ($5='none' AND i.id IS NULL) OR ($5='unpaid' AND i.id IS NOT NULL))
 ORDER BY e.finalized_at ASC,e.id ASC LIMIT $6 OFFSET $7`, strings.TrimSpace(f.Query), timeOrNil(f.DateFrom), timeOrNil(f.DateTo), f.DoctorID, state, fetch, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, err
	}
	out := []PendingEncounter{}
	for rows.Next() {
		var x PendingEncounter
		var id *int64
		var code, status, method *string
		if err = rows.Scan(&x.EncounterID, &x.EncounterCode, &x.AppointmentCode, &x.PatientID, &x.PatientCode, &x.PatientName, &x.PatientPhone, &x.DoctorName, &x.SpecialtyName, &x.FinalizedAt, &id, &code, &status, &method); err != nil {
			rows.Close()
			return nil, err
		}
		if id != nil {
			x.ExistingInvoice = &PendingInvoice{*id, *code, *status, *method}
		}
		out = append(out, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		// The lookahead row is metadata only; avoid pricing it.
		if extra && i >= f.PageSize {
			break
		}
		x := &out[i]
		if x.ExistingInvoice != nil {
			err = tx.QueryRow(ctx, `SELECT COALESCE(sum(line_total_vnd)FILTER(WHERE item_type='SERVICE'),0)::bigint,COALESCE(sum(line_total_vnd)FILTER(WHERE item_type='MEDICINE'),0)::bigint FROM invoice_items WHERE invoice_id=$1`, x.ExistingInvoice.ID).Scan(&x.ExpectedServiceTotalVND, &x.ExpectedMedicineTotalVND)
			if err != nil {
				return nil, err
			}
			x.ExpectedTotalVND = x.ExpectedServiceTotalVND + x.ExpectedMedicineTotalVND
			continue
		}
		services := []serviceSource{}
		medicines := []medicineSource{}
		sr, err := tx.Query(ctx, billableServiceQuery, x.EncounterID)
		if err != nil {
			return nil, err
		}
		for sr.Next() {
			var v serviceSource
			if err = sr.Scan(&v.id, &v.serviceID, &v.priceID, &v.code, &v.name, &v.unit, &v.quantity, &v.price); err != nil {
				sr.Close()
				return nil, err
			}
			services = append(services, v)
		}
		sr.Close()
		if err = sr.Err(); err != nil {
			return nil, err
		}
		mr, err := tx.Query(ctx, billableMedicineQuery, x.EncounterID)
		if err != nil {
			return nil, err
		}
		for mr.Next() {
			var v medicineSource
			var sid int64
			if err = mr.Scan(&v.itemID, &v.prescriptionID, &v.medicineID, &v.code, &v.name, &v.dosage, &v.strength, &v.unit, &v.quantity, &sid); err != nil {
				mr.Close()
				return nil, err
			}
			medicines = append(medicines, v)
		}
		mr.Close()
		if err = mr.Err(); err != nil {
			return nil, err
		}
		medicineIDs := make([]int64, 0, len(medicines))
		for _, v := range medicines {
			medicineIDs = append(medicineIDs, v.medicineID)
		}
		sales, priceErr := s.inventory.PreviewSalesForInvoice(ctx, tx, medicineIDs)
		if priceErr != nil {
			if p, ok := problem.As(priceErr); ok && (p.Code == "MEDICINE_INACTIVE" || p.Code == "MEDICINE_PRICE_REQUIRED") {
				x.BillingError = "INVOICE_MEDICINE_NOT_ELIGIBLE"
			} else {
				return nil, priceErr
			}
		}
		prices := map[int64]int64{}
		for _, sale := range sales {
			prices[sale.MedicineID] = sale.UnitSalePriceVND
		}
		for j := range medicines {
			medicines[j].price = prices[medicines[j].medicineID]
		}
		if x.BillingError != "" {
			continue
		}
		if len(services) == 0 && len(medicines) == 0 {
			x.BillingError = "INVOICE_NO_BILLABLE_ITEMS"
			continue
		}
		x.ExpectedTotalVND, x.ExpectedServiceTotalVND, x.ExpectedMedicineTotalVND, err = sourceTotals(services, medicines)
		if err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

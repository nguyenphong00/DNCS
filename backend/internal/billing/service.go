package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/inventory"
	"dncs/backend/internal/platform/pagination"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/settings"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"
)

type Service struct {
	pool      *pgxpool.Pool
	authz     *authorization.Store
	inventory *inventory.Service
	settings  *settings.Service
}

func NewService(pool *pgxpool.Pool, authz *authorization.Store, inv *inventory.Service, cfg *settings.Service) *Service {
	return &Service{pool: pool, authz: authz, inventory: inv, settings: cfg}
}

type Invoice struct {
	ID                   int64         `json:"id"`
	InvoiceCode          string        `json:"invoice_code"`
	EncounterID          int64         `json:"encounter_id"`
	PatientID            int64         `json:"patient_id"`
	PatientCode          string        `json:"patient_code_at_invoice"`
	PatientName          string        `json:"patient_name_at_invoice"`
	Status               string        `json:"status"`
	SubtotalVND          int64         `json:"subtotal_vnd"`
	TotalVND             int64         `json:"total_vnd"`
	CreatedByUserID      int64         `json:"created_by_user_id"`
	CreatedAt            time.Time     `json:"created_at"`
	PaidAt               *time.Time    `json:"paid_at,omitempty"`
	Version              int64         `json:"version"`
	Items                []InvoiceItem `json:"items,omitempty"`
	Payment              *Payment      `json:"payment,omitempty"`
	IdempotentlyReplayed bool          `json:"-"`
}

type InvoiceItem struct {
	ID                     int64   `json:"id"`
	InvoiceID              int64   `json:"invoice_id"`
	ItemType               string  `json:"item_type"`
	EncounterServiceID     *int64  `json:"encounter_service_id,omitempty"`
	PrescriptionItemID     *int64  `json:"prescription_item_id,omitempty"`
	PrescriptionID         *int64  `json:"prescription_id,omitempty"`
	ServicePriceVersionID  *int64  `json:"service_price_version_id,omitempty"`
	MedicinePriceVersionID *int64  `json:"medicine_price_version_id,omitempty"`
	ItemCode               string  `json:"item_code_at_invoice"`
	ItemName               string  `json:"item_name_at_invoice"`
	DosageForm             *string `json:"dosage_form_at_invoice,omitempty"`
	Strength               *string `json:"strength_at_invoice,omitempty"`
	UnitLabel              string  `json:"unit_label_at_invoice"`
	Quantity               int16   `json:"quantity"`
	UnitPriceVND           int64   `json:"unit_price_snapshot_vnd"`
	LineTotalVND           int64   `json:"line_total_vnd"`
}

type Payment struct {
	ID                       int64      `json:"id"`
	PaymentCode              string     `json:"payment_code"`
	InvoiceID                int64      `json:"invoice_id"`
	PaymentMethod            string     `json:"payment_method"`
	Status                   string     `json:"status"`
	ExpectedAmountVND        int64      `json:"expected_amount_vnd"`
	BankAccountIDAtCreation  *int64     `json:"bank_account_id_at_creation,omitempty"`
	BankBINAtCreation        *string    `json:"bank_bin_at_creation,omitempty"`
	AccountNumberAtCreation  *string    `json:"account_number_at_creation,omitempty"`
	AccountNameAtCreation    *string    `json:"account_name_at_creation,omitempty"`
	TransferContent          *string    `json:"transfer_content,omitempty"`
	QRPayload                *string    `json:"qr_payload,omitempty"`
	BankTransactionReference *string    `json:"bank_transaction_reference,omitempty"`
	ExternalReference        *string    `json:"external_reference,omitempty"`
	ConfirmedAmountVND       *int64     `json:"confirmed_amount_vnd,omitempty"`
	ConfirmedByUserID        *int64     `json:"confirmed_by_user_id,omitempty"`
	ConfirmedAt              *time.Time `json:"confirmed_at,omitempty"`
	Version                  int64      `json:"version"`
}

type ListFilter struct {
	Status, PaymentMethod, Query string
	PatientID, EncounterID       int64
	DateFrom, DateTo             time.Time
	Page, PageSize               int
}

type ConfirmInput struct {
	ConfirmedAmountVND       int64
	ExternalReference        string
	BankTransactionReference string
	IdempotencyKey           string
}

const invoiceSelect = `SELECT i.id,i.invoice_code,i.encounter_id,i.patient_id,i.patient_code_at_invoice,i.patient_name_at_invoice,i.status::text,i.subtotal_vnd::bigint,i.total_vnd::bigint,i.created_by_user_id,i.created_at,i.paid_at,i.version FROM invoices i`

func scanInvoice(row interface{ Scan(...any) error }) (Invoice, error) {
	var x Invoice
	err := row.Scan(&x.ID, &x.InvoiceCode, &x.EncounterID, &x.PatientID, &x.PatientCode, &x.PatientName, &x.Status, &x.SubtotalVND, &x.TotalVND, &x.CreatedByUserID, &x.CreatedAt, &x.PaidAt, &x.Version)
	return x, err
}

func validateKey(key string) error {
	if len(key) < 16 || len(key) > 128 || key != strings.TrimSpace(key) {
		return problem.New(400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key hợp lệ là bắt buộc.")
	}
	for _, r := range key {
		if r < 32 || r > 126 {
			return problem.New(400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key hợp lệ là bắt buộc.")
		}
	}
	return nil
}

func requestHash(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func beginIdempotency(ctx context.Context, tx pgx.Tx, actor auth.Principal, scope, key, hash string) (int64, *int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO idempotency_records(principal_key,actor_user_id,operation_scope,idempotency_key,request_hash,status,expires_at) VALUES($1,$2,$3,$4,$5,'PROCESSING',now()+interval '30 days') ON CONFLICT DO NOTHING RETURNING id`, fmt.Sprintf("USER:%d", actor.UserID), actor.UserID, scope, key, hash).Scan(&id)
	if err == nil {
		return id, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, err
	}
	var oldHash, state string
	var resourceID *int64
	err = tx.QueryRow(ctx, `SELECT request_hash,status::text,resource_id FROM idempotency_records WHERE principal_key=$1 AND operation_scope=$2 AND idempotency_key=$3 FOR UPDATE`, fmt.Sprintf("USER:%d", actor.UserID), scope, key).Scan(&oldHash, &state, &resourceID)
	if err != nil {
		return 0, nil, err
	}
	if oldHash != hash {
		return 0, nil, problem.New(409, "IDEMPOTENCY_KEY_REUSED", "Idempotency-Key đã được dùng với payload khác.")
	}
	if state != "COMPLETED" || resourceID == nil {
		return 0, nil, problem.New(409, "BILLING_CONCURRENT_UPDATE", "Yêu cầu cùng idempotency key đang được xử lý.")
	}
	return 0, resourceID, nil
}

type serviceSource struct {
	id, serviceID, priceID int64
	code, name, unit       string
	quantity               int16
	price                  int64
}

type medicineSource struct {
	itemID, prescriptionID, medicineID int64
	code, name, dosage, strength, unit string
	quantity                           int16
	priceID, price                     int64
}

func uniqueSorted(values []int64) []int64 {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := values[:0]
	for _, v := range values {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}

func lockIDs(ctx context.Context, tx pgx.Tx, table string, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	allowed := map[string]bool{"services": true, "service_price_versions": true, "encounter_services": true, "prescription_series": true, "prescriptions": true, "prescription_items": true}
	if !allowed[table] {
		return errors.New("unsupported billing lock group")
	}
	rows, err := tx.Query(ctx, `SELECT id FROM `+table+` WHERE id=ANY($1) ORDER BY id FOR SHARE`, uniqueSorted(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) CreateInvoice(ctx context.Context, actor auth.Principal, encounterID int64, method, key string) (Invoice, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method != "CASH" && method != "BANK_TRANSFER" {
		return Invoice{}, problem.New(422, "PAYMENT_METHOD_MISMATCH", "Phương thức thanh toán không hợp lệ.")
	}
	if err := validateKey(key); err != nil {
		return Invoice{}, err
	}
	var patientID, patientUserID, appointmentID int64
	err := s.pool.QueryRow(ctx, `SELECT e.patient_id,p.user_id,e.appointment_id FROM medical_encounters e JOIN patients p ON p.id=e.patient_id WHERE e.id=$1`, encounterID).Scan(&patientID, &patientUserID, &appointmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, problem.New(404, "ENCOUNTER_NOT_FOUND", "Không tìm thấy encounter.")
	}
	if err != nil {
		return Invoice{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invoice{}, err
	}
	defer tx.Rollback(ctx)
	recordID, replayID, err := beginIdempotency(ctx, tx, actor, fmt.Sprintf("invoice.create:%d", encounterID), key, requestHash(map[string]any{"encounter_id": encounterID, "payment_method": method}))
	if err != nil {
		return Invoice{}, err
	}
	if replayID != nil {
		x, err := s.invoice(ctx, actor, *replayID, false)
		x.IdempotentlyReplayed = true
		return x, err
	}
	var bank settings.ActiveBankAccount
	if method == "BANK_TRANSFER" {
		bank, err = s.settings.LockActiveBankAccount(ctx, tx)
		if err != nil {
			if p, ok := problem.As(err); ok && p.Code == "BANK_ACCOUNT_ACTIVE_REQUIRED" {
				return Invoice{}, problem.New(409, "PAYMENT_BANK_ACCOUNT_NOT_CONFIGURED", "Chưa cấu hình tài khoản nhận chuyển khoản.")
			}
			return Invoice{}, err
		}
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR SHARE`, patientUserID); err != nil {
		return Invoice{}, err
	}
	var patientCode, patientName string
	if err = tx.QueryRow(ctx, `SELECT patient_code,full_name FROM patients WHERE id=$1 AND user_id=$2 AND NOT is_deleted FOR SHARE`, patientID, patientUserID).Scan(&patientCode, &patientName); err != nil {
		return Invoice{}, problem.New(409, "INVOICE_PATIENT_INVALID", "Patient của encounter không còn hợp lệ.")
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM appointments WHERE id=$1 FOR SHARE`, appointmentID); err != nil {
		return Invoice{}, err
	}
	var encounterStatus string
	if err = tx.QueryRow(ctx, `SELECT status::text FROM medical_encounters WHERE id=$1 AND patient_id=$2 AND appointment_id=$3 FOR UPDATE`, encounterID, patientID, appointmentID).Scan(&encounterStatus); err != nil {
		return Invoice{}, err
	}
	if encounterStatus != "FINALIZED" {
		return Invoice{}, problem.New(409, "INVOICE_ENCOUNTER_NOT_FINALIZED", "Encounter chưa được finalized.")
	}
	var unpaid bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM invoices WHERE encounter_id=$1 AND status='UNPAID')`, encounterID).Scan(&unpaid); err != nil {
		return Invoice{}, err
	}
	if unpaid {
		return Invoice{}, problem.New(409, "INVOICE_UNPAID_ALREADY_EXISTS", "Encounter đã có hóa đơn chưa thanh toán.")
	}

	serviceRows, err := tx.Query(ctx, billableServiceQuery, encounterID)
	if err != nil {
		return Invoice{}, err
	}
	services := []serviceSource{}
	serviceIDs, priceIDs, encounterServiceIDs := []int64{}, []int64{}, []int64{}
	for serviceRows.Next() {
		var x serviceSource
		if err = serviceRows.Scan(&x.id, &x.serviceID, &x.priceID, &x.code, &x.name, &x.unit, &x.quantity, &x.price); err != nil {
			serviceRows.Close()
			return Invoice{}, err
		}
		services = append(services, x)
		serviceIDs = append(serviceIDs, x.serviceID)
		priceIDs = append(priceIDs, x.priceID)
		encounterServiceIDs = append(encounterServiceIDs, x.id)
	}
	serviceRows.Close()
	if err = serviceRows.Err(); err != nil {
		return Invoice{}, err
	}

	medicineRows, err := tx.Query(ctx, billableMedicineQuery, encounterID)
	if err != nil {
		return Invoice{}, err
	}
	medicines := []medicineSource{}
	medicineIDs, seriesIDs, prescriptionIDs, prescriptionItemIDs := []int64{}, []int64{}, []int64{}, []int64{}
	for medicineRows.Next() {
		var x medicineSource
		var sid int64
		if err = medicineRows.Scan(&x.itemID, &x.prescriptionID, &x.medicineID, &x.code, &x.name, &x.dosage, &x.strength, &x.unit, &x.quantity, &sid); err != nil {
			medicineRows.Close()
			return Invoice{}, err
		}
		medicines = append(medicines, x)
		medicineIDs = append(medicineIDs, x.medicineID)
		seriesIDs = append(seriesIDs, sid)
		prescriptionIDs = append(prescriptionIDs, x.prescriptionID)
		prescriptionItemIDs = append(prescriptionItemIDs, x.itemID)
	}
	medicineRows.Close()
	if err = medicineRows.Err(); err != nil {
		return Invoice{}, err
	}
	if len(services) == 0 && len(medicines) == 0 {
		return Invoice{}, problem.New(409, "INVOICE_NO_BILLABLE_ITEMS", "Không còn nguồn hợp lệ để lập hóa đơn.")
	}

	if err = lockIDs(ctx, tx, "services", serviceIDs); err != nil {
		return Invoice{}, err
	}
	if err = lockIDs(ctx, tx, "service_price_versions", priceIDs); err != nil {
		return Invoice{}, err
	}
	sales, err := s.inventory.ResolveSalesForInvoice(ctx, tx, medicineIDs)
	if err != nil {
		if p, ok := problem.As(err); ok && (p.Code == "MEDICINE_INACTIVE" || p.Code == "MEDICINE_PRICE_REQUIRED") {
			return Invoice{}, problem.New(409, "INVOICE_MEDICINE_NOT_ELIGIBLE", "Có thuốc không đủ điều kiện lập hóa đơn.")
		}
		return Invoice{}, err
	}
	byMedicine := map[int64]inventory.SaleSnapshot{}
	for _, x := range sales {
		byMedicine[x.MedicineID] = x
	}
	if err = lockIDs(ctx, tx, "encounter_services", encounterServiceIDs); err != nil {
		return Invoice{}, err
	}
	if err = lockIDs(ctx, tx, "prescription_series", seriesIDs); err != nil {
		return Invoice{}, err
	}
	if err = lockIDs(ctx, tx, "prescriptions", prescriptionIDs); err != nil {
		return Invoice{}, err
	}
	if err = lockIDs(ctx, tx, "prescription_items", prescriptionItemIDs); err != nil {
		return Invoice{}, err
	}
	for i := range medicines {
		x, ok := byMedicine[medicines[i].medicineID]
		if !ok {
			return Invoice{}, problem.New(409, "INVOICE_MEDICINE_NOT_ELIGIBLE", "Có thuốc không đủ điều kiện lập hóa đơn.")
		}
		medicines[i].priceID = x.PriceVersionID
		medicines[i].price = x.UnitSalePriceVND
	}

	total, _, _, err := sourceTotals(services, medicines)
	if err != nil {
		return Invoice{}, err
	}
	var invoiceID int64
	err = tx.QueryRow(ctx, `INSERT INTO invoices(invoice_code,encounter_id,patient_id,patient_code_at_invoice,patient_name_at_invoice,subtotal_vnd,total_vnd,created_by_user_id)VALUES('', $1,$2,$3,$4,$5,$5,$6)RETURNING id`, encounterID, patientID, patientCode, patientName, total, actor.UserID).Scan(&invoiceID)
	if err != nil {
		return Invoice{}, mapErr(err)
	}
	for _, x := range services {
		_, err = tx.Exec(ctx, `INSERT INTO invoice_items(invoice_id,item_type,encounter_service_id,service_price_version_id,item_code_at_invoice,item_name_at_invoice,unit_label_at_invoice,quantity,unit_price_snapshot_vnd,line_total_vnd)VALUES($1,'SERVICE',$2,$3,$4,$5,$6,$7,$8,($8::numeric)*($7::smallint))`, invoiceID, x.id, x.priceID, x.code, x.name, x.unit, x.quantity, x.price)
		if err != nil {
			return Invoice{}, mapErr(err)
		}
	}
	for _, x := range medicines {
		dosage := nullableText(x.dosage)
		strength := nullableText(x.strength)
		_, err = tx.Exec(ctx, `INSERT INTO invoice_items(invoice_id,item_type,prescription_item_id,prescription_id,medicine_price_version_id,item_code_at_invoice,item_name_at_invoice,dosage_form_at_invoice,strength_at_invoice,unit_label_at_invoice,quantity,unit_price_snapshot_vnd,line_total_vnd)VALUES($1,'MEDICINE',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,($11::numeric)*($10::smallint))`, invoiceID, x.itemID, x.prescriptionID, x.priceID, x.code, x.name, dosage, strength, x.unit, x.quantity, x.price)
		if err != nil {
			return Invoice{}, mapErr(err)
		}
	}
	var invoiceCode string
	if err = tx.QueryRow(ctx, `SELECT invoice_code FROM invoices WHERE id=$1`, invoiceID).Scan(&invoiceCode); err != nil {
		return Invoice{}, err
	}
	if method == "CASH" {
		_, err = tx.Exec(ctx, `INSERT INTO payments(payment_code,invoice_id,payment_method,expected_amount_vnd)VALUES('',$1,'CASH',$2)`, invoiceID, total)
	} else {
		content := transferContent(invoiceID)
		payload := vietQRPayload(bank, total, content)
		_, err = tx.Exec(ctx, `INSERT INTO payments(payment_code,invoice_id,payment_method,expected_amount_vnd,bank_account_id_at_creation,bank_bin_at_creation,account_number_at_creation,account_name_at_creation,transfer_content,qr_payload)VALUES('',$1,'BANK_TRANSFER',$2,$3,$4,$5,$6,$7,$8)`, invoiceID, total, bank.ID, bank.BankBIN, bank.AccountNumber, bank.AccountName, content, payload)
	}
	if err != nil {
		return Invoice{}, mapErr(err)
	}
	ev := audit.UserEvent(actor.UserID, "BILLING_PAYMENT", "INVOICE_CREATED", "INVOICE", invoiceID)
	ev.TargetCode = invoiceCode
	ev.Metadata = map[string]any{"encounter_id": encounterID, "patient_id": patientID, "item_count": len(services) + len(medicines), "total_vnd": total, "payment_method": method}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return Invoice{}, err
	}
	ev = audit.UserEvent(actor.UserID, "BILLING_PAYMENT", "INVOICE_ITEM_SNAPSHOTTED", "INVOICE", invoiceID)
	ev.Metadata = map[string]any{"service_count": len(services), "medicine_count": len(medicines)}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return Invoice{}, err
	}
	ev = audit.UserEvent(actor.UserID, "BILLING_PAYMENT", "PAYMENT_PENDING_CREATED", "INVOICE", invoiceID)
	ev.Metadata = map[string]any{"payment_method": method, "bank_account_id": func() any {
		if method == "BANK_TRANSFER" {
			return bank.ID
		}
		return nil
	}()}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return Invoice{}, err
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',resource_type='INVOICE',resource_id=$2,http_status=201,response_body=jsonb_build_object('invoice_id',$2::bigint),completed_at=now(),updated_at=now() WHERE id=$1`, recordID, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Invoice{}, mapErr(err)
	}
	return s.invoice(ctx, actor, invoiceID, false)
}

func nullableText(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
func tlv(id, value string) string { return id + fmt.Sprintf("%02d", len([]byte(value))) + value }
func qrSafe(v string, max int) string {
	v = strings.ReplaceAll(strings.ReplaceAll(v, "Đ", "D"), "đ", "d")
	var b strings.Builder
	for _, r := range norm.NFD.String(norm.NFKC.String(strings.ToUpper(v))) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' {
			b.WriteRune(r)
		}
	}
	value := strings.Join(strings.Fields(b.String()), " ")
	if len(value) > max {
		value = value[:max]
	}
	if value == "" {
		value = "DNCS"
	}
	return value
}

// A bigint invoice ID has at most 19 decimal digits: DNCS + ID stays <= 25.
func transferContent(invoiceID int64) string { return "DNCS" + strconv.FormatInt(invoiceID, 10) }
func crc16(v string) uint16 {
	crc := uint16(0xffff)
	for _, b := range []byte(v) {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
func vietQRPayload(bank settings.ActiveBankAccount, amount int64, content string) string {
	beneficiary := tlv("00", bank.BankBIN) + tlv("01", bank.AccountNumber)
	merchant := tlv("00", "A000000727") + tlv("01", beneficiary) + tlv("02", "QRIBFTTA")
	additional := tlv("08", content)
	base := tlv("00", "01") + tlv("01", "12") + tlv("38", merchant) + tlv("53", "704") + tlv("54", strconv.FormatInt(amount, 10)) + tlv("58", "VN") + tlv("59", qrSafe(bank.AccountName, 25)) + tlv("62", additional) + "6304"
	return base + fmt.Sprintf("%04X", crc16(base))
}

func (s *Service) canViewAll(ctx context.Context, actor auth.Principal) (bool, error) {
	d, err := s.authz.Allowed(ctx, actor, "invoice.view.all")
	return d.Allowed, err
}
func (s *Service) patientID(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM patients WHERE user_id=$1 AND NOT is_deleted`, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, problem.New(404, "INVOICE_NOT_FOUND", "Không tìm thấy hóa đơn.")
	}
	return id, err
}

func (s *Service) Invoice(ctx context.Context, actor auth.Principal, id int64, forceOwn bool) (Invoice, error) {
	return s.invoice(ctx, actor, id, forceOwn)
}
func (s *Service) invoice(ctx context.Context, actor auth.Principal, id int64, forceOwn bool) (Invoice, error) {
	own := forceOwn
	if !forceOwn {
		all, err := s.canViewAll(ctx, actor)
		if err != nil {
			return Invoice{}, err
		}
		own = !all
	}
	var patient int64
	if own {
		var err error
		patient, err = s.patientID(ctx, actor.UserID)
		if err != nil {
			return Invoice{}, err
		}
	}
	x, err := scanInvoice(s.pool.QueryRow(ctx, invoiceSelect+` WHERE i.id=$1 AND ($2=0 OR i.patient_id=$2)`, id, patient))
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, problem.New(404, "INVOICE_NOT_FOUND", "Không tìm thấy hóa đơn.")
	}
	if err != nil {
		return Invoice{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT id,invoice_id,item_type::text,encounter_service_id,prescription_item_id,prescription_id,service_price_version_id,medicine_price_version_id,item_code_at_invoice,item_name_at_invoice,dosage_form_at_invoice,strength_at_invoice,unit_label_at_invoice,quantity,unit_price_snapshot_vnd::bigint,line_total_vnd::bigint FROM invoice_items WHERE invoice_id=$1 ORDER BY id`, id)
	if err != nil {
		return Invoice{}, err
	}
	defer rows.Close()
	x.Items = []InvoiceItem{}
	for rows.Next() {
		var item InvoiceItem
		if err = rows.Scan(&item.ID, &item.InvoiceID, &item.ItemType, &item.EncounterServiceID, &item.PrescriptionItemID, &item.PrescriptionID, &item.ServicePriceVersionID, &item.MedicinePriceVersionID, &item.ItemCode, &item.ItemName, &item.DosageForm, &item.Strength, &item.UnitLabel, &item.Quantity, &item.UnitPriceVND, &item.LineTotalVND); err != nil {
			return Invoice{}, err
		}
		x.Items = append(x.Items, item)
	}
	if err = rows.Err(); err != nil {
		return Invoice{}, err
	}
	p, err := scanPayment(s.pool.QueryRow(ctx, paymentSelect+` WHERE invoice_id=$1`, id))
	if err != nil {
		return Invoice{}, err
	}
	if own {
		p.BankTransactionReference = nil
		p.ExternalReference = nil
	}
	x.Payment = &p
	return x, nil
}

const paymentSelect = `SELECT id,payment_code,invoice_id,payment_method::text,status::text,expected_amount_vnd::bigint,bank_account_id_at_creation,btrim(bank_bin_at_creation),account_number_at_creation,account_name_at_creation,transfer_content,qr_payload,bank_transaction_reference,external_reference,confirmed_amount_vnd::bigint,confirmed_by_user_id,confirmed_at,version FROM payments`

func scanPayment(row interface{ Scan(...any) error }) (Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.PaymentCode, &p.InvoiceID, &p.PaymentMethod, &p.Status, &p.ExpectedAmountVND, &p.BankAccountIDAtCreation, &p.BankBINAtCreation, &p.AccountNumberAtCreation, &p.AccountNameAtCreation, &p.TransferContent, &p.QRPayload, &p.BankTransactionReference, &p.ExternalReference, &p.ConfirmedAmountVND, &p.ConfirmedByUserID, &p.ConfirmedAt, &p.Version)
	return p, err
}

func (s *Service) List(ctx context.Context, actor auth.Principal, own bool, f ListFilter) ([]Invoice, error) {
	return s.list(ctx, actor, own, f, false)
}
func (s *Service) ListPage(ctx context.Context, actor auth.Principal, own bool, f ListFilter) (pagination.Page[Invoice], error) {
	f.Page, f.PageSize = pagination.Normalize(f.Page, f.PageSize)
	items, err := s.list(ctx, actor, own, f, true)
	return pagination.From(items, f.Page, f.PageSize), err
}
func (s *Service) list(ctx context.Context, actor auth.Principal, own bool, f ListFilter, extra bool) ([]Invoice, error) {
	patient := f.PatientID
	if own {
		var err error
		patient, err = s.patientID(ctx, actor.UserID)
		if err != nil {
			return nil, err
		}
		f.EncounterID = 0
		f.Query = ""
	} else {
		all, err := s.canViewAll(ctx, actor)
		if err != nil {
			return nil, err
		}
		if !all {
			return nil, problem.New(403, "AUTHZ_RESOURCE_SCOPE_DENIED", "Không có quyền xem hóa đơn.")
		}
	}
	f.Page, f.PageSize = pagination.Normalize(f.Page, f.PageSize)
	fetch := f.PageSize
	if extra {
		fetch++
	}
	if !f.DateFrom.IsZero() && !f.DateTo.IsZero() && (f.DateTo.Before(f.DateFrom) || f.DateTo.Sub(f.DateFrom) > 366*24*time.Hour) {
		return nil, problem.New(422, "INVOICE_FILTER_INVALID", "Khoảng ngày không hợp lệ.")
	}
	rows, err := s.pool.Query(ctx, invoiceSelect+` JOIN payments p ON p.invoice_id=i.id WHERE ($1=0 OR i.patient_id=$1) AND ($2=0 OR i.encounter_id=$2) AND ($3='' OR i.status::text=$3) AND ($4='' OR p.payment_method::text=$4) AND ($5='' OR i.invoice_code ILIKE '%'||$5||'%' OR i.patient_code_at_invoice ILIKE '%'||$5||'%') AND ($6::timestamptz IS NULL OR i.created_at >= $6) AND ($7::timestamptz IS NULL OR i.created_at <= $7) ORDER BY i.created_at DESC,i.id DESC LIMIT $8 OFFSET $9`, patient, f.EncounterID, strings.ToUpper(f.Status), strings.ToUpper(f.PaymentMethod), strings.TrimSpace(f.Query), timeOrNil(f.DateFrom), timeOrNil(f.DateTo), fetch, (f.Page-1)*f.PageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invoice{}
	for rows.Next() {
		x, e := scanInvoice(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func timeOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func (s *Service) PaymentQR(ctx context.Context, actor auth.Principal, invoiceID int64, own bool) (map[string]any, error) {
	x, err := s.invoice(ctx, actor, invoiceID, own)
	if err != nil {
		return nil, err
	}
	if x.Payment == nil || x.Payment.PaymentMethod != "BANK_TRANSFER" || x.Payment.QRPayload == nil {
		return nil, problem.New(409, "PAYMENT_QR_NOT_AVAILABLE", "Hóa đơn không có QR chuyển khoản.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	hash := sha256.Sum256([]byte(*x.Payment.QRPayload))
	ev := audit.UserEvent(actor.UserID, "BILLING_PAYMENT", "PAYMENT_QR_VIEWED", "PAYMENT", x.Payment.ID)
	ev.Metadata = map[string]any{"invoice_id": x.ID, "bank_account_id": x.Payment.BankAccountIDAtCreation, "qr_sha256": hex.EncodeToString(hash[:])}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"invoice_id": x.ID, "invoice_code": x.InvoiceCode, "payment_id": x.Payment.ID, "expected_amount_vnd": x.Payment.ExpectedAmountVND, "bank_bin": x.Payment.BankBINAtCreation, "account_number": x.Payment.AccountNumberAtCreation, "account_name": x.Payment.AccountNameAtCreation, "transfer_content": x.Payment.TransferContent, "qr_payload": x.Payment.QRPayload}, nil
}

func normalizeOptional(v string, max int) (*string, error) {
	v = strings.Join(strings.Fields(norm.NFKC.String(v)), " ")
	if v == "" {
		return nil, nil
	}
	if len([]rune(v)) > max {
		return nil, problem.New(422, "PAYMENT_METHOD_MISMATCH", "Tham chiếu thanh toán quá dài.")
	}
	return &v, nil
}
func normalizeBankReference(v string) (string, string, error) {
	display := strings.Join(strings.Fields(norm.NFKC.String(v)), " ")
	if display == "" || len([]rune(display)) > 100 {
		return "", "", problem.New(422, "PAYMENT_BANK_REFERENCE_REQUIRED", "Mã giao dịch ngân hàng là bắt buộc.")
	}
	return display, strings.ToUpper(display), nil
}

func (s *Service) Confirm(ctx context.Context, actor auth.Principal, invoiceID int64, in ConfirmInput) (Invoice, error) {
	if err := validateKey(in.IdempotencyKey); err != nil {
		return Invoice{}, err
	}
	ext, err := normalizeOptional(in.ExternalReference, 100)
	if err != nil {
		return Invoice{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invoice{}, err
	}
	defer tx.Rollback(ctx)
	recordID, replayID, err := beginIdempotency(ctx, tx, actor, fmt.Sprintf("payment.confirm:%d", invoiceID), in.IdempotencyKey, requestHash(map[string]any{"invoice_id": invoiceID, "amount": in.ConfirmedAmountVND, "external_reference": ext, "bank_transaction_reference": strings.Join(strings.Fields(norm.NFKC.String(in.BankTransactionReference)), " ")}))
	if err != nil {
		return Invoice{}, err
	}
	if replayID != nil {
		x, e := s.invoice(ctx, actor, *replayID, false)
		x.IdempotentlyReplayed = true
		return x, e
	}
	rows, err := tx.Query(ctx, `SELECT ii.id,pi.medicine_id,ii.quantity::float8 FROM invoice_items ii JOIN prescription_items pi ON pi.id=ii.prescription_item_id WHERE ii.invoice_id=$1 AND ii.item_type='MEDICINE' ORDER BY pi.medicine_id,ii.id`, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	requirements := []inventory.StockRequirement{}
	for rows.Next() {
		var x inventory.StockRequirement
		if err = rows.Scan(&x.ReferenceID, &x.MedicineID, &x.Quantity); err != nil {
			rows.Close()
			return Invoice{}, err
		}
		requirements = append(requirements, x)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return Invoice{}, err
	}
	allocations, err := s.inventory.AllocateStockForPayment(ctx, tx, requirements)
	if err != nil {
		return Invoice{}, err
	}
	var status, invoiceCode string
	var expected int64
	err = tx.QueryRow(ctx, `SELECT status::text,total_vnd::bigint,invoice_code FROM invoices WHERE id=$1 FOR UPDATE`, invoiceID).Scan(&status, &expected, &invoiceCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, problem.New(404, "INVOICE_NOT_FOUND", "Không tìm thấy hóa đơn.")
	}
	if err != nil {
		return Invoice{}, err
	}
	if status == "PAID" {
		return Invoice{}, problem.New(409, "PAYMENT_ALREADY_CONFIRMED", "Thanh toán đã được xác nhận.")
	}
	if expected != in.ConfirmedAmountVND {
		return Invoice{}, problem.New(422, "PAYMENT_AMOUNT_MISMATCH", "Số tiền xác nhận không khớp hóa đơn.")
	}
	var paymentID int64
	var method, paymentStatus string
	var bankID *int64
	err = tx.QueryRow(ctx, `SELECT id,payment_method::text,status::text,bank_account_id_at_creation FROM payments WHERE invoice_id=$1 FOR UPDATE`, invoiceID).Scan(&paymentID, &method, &paymentStatus, &bankID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, problem.New(404, "PAYMENT_NOT_FOUND", "Không tìm thấy thanh toán.")
	}
	if err != nil {
		return Invoice{}, err
	}
	if paymentStatus == "CONFIRMED" {
		return Invoice{}, problem.New(409, "PAYMENT_ALREADY_CONFIRMED", "Thanh toán đã được xác nhận.")
	}
	var bankDisplay, bankNormalized *string
	if method == "BANK_TRANSFER" {
		d, n, e := normalizeBankReference(in.BankTransactionReference)
		if e != nil {
			return Invoice{}, e
		}
		bankDisplay = &d
		bankNormalized = &n
	} else if strings.TrimSpace(in.BankTransactionReference) != "" {
		return Invoice{}, problem.New(409, "PAYMENT_METHOD_MISMATCH", "CASH không nhận mã giao dịch ngân hàng.")
	}
	_, err = tx.Exec(ctx, `UPDATE payments SET status='CONFIRMED',bank_transaction_reference=$2,bank_transaction_reference_normalized=$3,external_reference=$4,confirmed_amount_vnd=$5,confirmed_by_user_id=$6,confirmed_at=clock_timestamp(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, paymentID, bankDisplay, bankNormalized, ext, expected, actor.UserID)
	if err != nil {
		return Invoice{}, mapErr(err)
	}
	_, err = tx.Exec(ctx, `UPDATE invoices SET status='PAID',paid_at=clock_timestamp(),version=version+1 WHERE id=$1`, invoiceID)
	if err != nil {
		return Invoice{}, mapErr(err)
	}
	ev := audit.UserEvent(actor.UserID, "BILLING_PAYMENT", "PAYMENT_CONFIRMED", "PAYMENT", paymentID)
	ev.Metadata = map[string]any{"invoice_id": invoiceID, "invoice_code": invoiceCode, "payment_method": method, "amount_vnd": expected, "bank_account_id": bankID, "allocation_count": len(allocations)}
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return Invoice{}, err
	}
	if len(allocations) > 0 {
		movementIDs := make([]int64, 0, len(allocations))
		for _, a := range allocations {
			movementIDs = append(movementIDs, a.MovementID)
		}
		ev = audit.UserEvent(actor.UserID, "INVENTORY", "INVENTORY_PAYMENT_STOCK_ALLOCATED", "INVOICE", invoiceID)
		ev.Metadata = map[string]any{"movement_ids": movementIDs}
		if err = audit.Insert(ctx, tx, ev); err != nil {
			return Invoice{}, err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',resource_type='INVOICE',resource_id=$2,http_status=200,response_body=jsonb_build_object('invoice_id',$2::bigint),completed_at=now(),updated_at=now() WHERE id=$1`, recordID, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Invoice{}, mapErr(err)
	}
	return s.invoice(ctx, actor, invoiceID, false)
}

func mapErr(err error) error {
	switch pgutil.Constraint(err) {
	case "uq_invoices_one_unpaid_encounter":
		return problem.New(409, "INVOICE_UNPAID_ALREADY_EXISTS", "Encounter đã có hóa đơn chưa thanh toán.")
	case "uq_invoice_items_service_source", "uq_invoice_items_medicine_source":
		return problem.New(409, "INVOICE_SOURCE_ALREADY_BILLED", "Nguồn hóa đơn đã được sử dụng.")
	case "uq_payments_confirmed_bank_reference":
		return problem.New(409, "PAYMENT_BANK_REFERENCE_EXISTS", "Mã giao dịch ngân hàng đã được sử dụng.")
	}
	return fmt.Errorf("billing persistence: %w", err)
}

const billableServiceQuery = `SELECT es.id,es.service_id,es.service_price_version_id_at_order,es.service_code_at_order,es.service_name_at_order,es.unit_label_at_order,es.quantity,sp.unit_price_vnd::bigint FROM encounter_services es JOIN service_price_versions sp ON sp.id=es.service_price_version_id_at_order WHERE es.encounter_id=$1 AND es.status='ACTIVE' AND NOT EXISTS(SELECT 1 FROM invoice_items ii WHERE ii.encounter_service_id=es.id) ORDER BY es.id`

const billableMedicineQuery = `SELECT pi.id,p.id,pi.medicine_id,pi.medicine_code_at_prescription,pi.medicine_name_at_prescription,COALESCE(pi.dosage_form_at_prescription,''),COALESCE(pi.strength_at_prescription,''),pi.dispense_unit_at_prescription,pi.quantity,ps.id FROM prescription_series ps JOIN prescriptions p ON p.id=ps.current_issued_prescription_id AND p.status='ISSUED' JOIN prescription_items pi ON pi.prescription_id=p.id AND NOT pi.is_removed WHERE ps.encounter_id=$1 AND ps.status='ISSUED' AND NOT EXISTS(SELECT 1 FROM invoice_items ii WHERE ii.prescription_item_id=pi.id) ORDER BY ps.id,p.id,pi.id`

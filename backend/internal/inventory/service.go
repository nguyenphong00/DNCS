package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/platform/pagination"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/platform/textnorm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool  *pgxpool.Pool
	authz *authorization.Store
	now   func() time.Time
}

func NewService(pool *pgxpool.Pool, stores ...*authorization.Store) *Service {
	s := &Service{pool: pool, now: time.Now}
	if len(stores) > 0 {
		s.authz = stores[0]
	}
	return s
}
func (s *Service) CanManage(ctx context.Context, actor auth.Principal) (bool, error) {
	if s.authz == nil {
		return actor.RoleCode == "ADMIN" || actor.RoleCode == "SUPER_ADMIN", nil
	}
	d, err := s.authz.Allowed(ctx, actor, "medicine.manage")
	return d.Allowed, err
}

type Medicine struct {
	ID                int64     `json:"id"`
	Code              string    `json:"code"`
	Name              string    `json:"name"`
	ActiveIngredient  string    `json:"active_ingredient"`
	DosageForm        string    `json:"dosage_form"`
	Strength          string    `json:"strength"`
	DispenseUnit      string    `json:"dispense_unit"`
	Description       *string   `json:"description,omitempty"`
	LowStockThreshold float64   `json:"low_stock_threshold,omitempty"`
	Status            string    `json:"status"`
	Version           int64     `json:"version"`
	IsDeleted         bool      `json:"is_deleted,omitempty"`
	CurrentPrice      *Price    `json:"current_price,omitempty"`
	StockAdvisory     string    `json:"stock_advisory"`
	PhysicalQuantity  float64   `json:"physical_quantity,omitempty"`
	SellableQuantity  float64   `json:"sellable_quantity,omitempty"`
	ExpiredQuantity   float64   `json:"expired_quantity,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
type Price struct {
	ID                 int64      `json:"id"`
	MedicineID         int64      `json:"medicine_id"`
	EffectiveFrom      time.Time  `json:"effective_from"`
	UnitSalePriceVND   int64      `json:"unit_sale_price_vnd"`
	Currency           string     `json:"currency"`
	Phase              string     `json:"phase"`
	CancellationReason *string    `json:"cancellation_reason,omitempty"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`
}
type MedicineInput struct {
	Code, Name, ActiveIngredient, DosageForm, Strength, DispenseUnit, Description, Status string
	LowStockThreshold                                                                     float64
	InitialPrice                                                                          *int64
	InitialEffectiveFrom                                                                  *string
}
type MedicineUpdate struct {
	ExpectedVersion   int64    `json:"expected_version"`
	Name              *string  `json:"name"`
	ActiveIngredient  *string  `json:"active_ingredient"`
	DosageForm        *string  `json:"dosage_form"`
	Strength          *string  `json:"strength"`
	DispenseUnit      *string  `json:"dispense_unit"`
	Description       *string  `json:"description"`
	LowStockThreshold *float64 `json:"low_stock_threshold"`
}
type ReceiptItem struct {
	MedicineID      int64   `json:"medicine_id"`
	LotNumber       string  `json:"lot_number"`
	ManufactureDate *string `json:"manufacture_date"`
	ExpiresOn       string  `json:"expires_on"`
	Quantity        float64 `json:"received_quantity"`
	UnitCostVND     int64   `json:"unit_purchase_cost_vnd"`
}
type ReceiptInput struct {
	SupplierName        string        `json:"supplier_name"`
	SupplierDocumentRef string        `json:"supplier_document_ref"`
	ReceivedAt          string        `json:"received_at"`
	Note                string        `json:"note"`
	IdempotencyKey      string        `json:"-"`
	Items               []ReceiptItem `json:"items"`
}
type AdjustmentItem struct {
	BatchID   int64   `json:"medicine_batch_id"`
	Direction string  `json:"direction"`
	Quantity  float64 `json:"quantity"`
}
type AdjustmentInput struct {
	ReasonCode     string           `json:"reason_code"`
	ReasonDetail   string           `json:"reason_detail"`
	IdempotencyKey string           `json:"-"`
	CorrectionOf   *int64           `json:"correction_of_adjustment_id"`
	Items          []AdjustmentItem `json:"items"`
}
type Document struct {
	ID        int64     `json:"id"`
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"created_at"`
	Replayed  bool      `json:"-"`
}
type SaleSnapshot struct {
	MedicineID, PriceVersionID, UnitSalePriceVND int64
	Code, Name, DispenseUnit                     string
}
type StockRequirement struct {
	MedicineID, ReferenceID int64
	Quantity                float64
}
type StockAllocation struct {
	MedicineID, BatchID, ReferenceID, MovementID int64
	Quantity                                     float64
}

var medCode = regexp.MustCompile(`^MED-[A-Z0-9-]{2,30}$`)

func cleanRequired(v string, max int) (string, error) {
	v = strings.Join(strings.Fields(v), " ")
	if v == "" || len([]rune(v)) > max {
		return "", errors.New("invalid")
	}
	return v, nil
}
func invalid(msg string) error { return problem.New(422, "MEDICINE_FIELD_INVALID", msg) }

func (s *Service) CreateMedicine(ctx context.Context, actor auth.Principal, in MedicineInput) (Medicine, error) {
	in.Code = textnorm.UpperCode(in.Code)
	in.Status = strings.ToUpper(strings.TrimSpace(in.Status))
	if in.Status == "" {
		in.Status = "INACTIVE"
	}
	var err error
	if !medCode.MatchString(in.Code) {
		return Medicine{}, invalid("Mã thuốc không hợp lệ.")
	}
	for _, p := range []*string{&in.Name, &in.ActiveIngredient, &in.DosageForm, &in.Strength, &in.DispenseUnit} {
		*p, err = cleanRequired(*p, 200)
		if err != nil {
			return Medicine{}, invalid("Thông tin snapshot thuốc không hợp lệ.")
		}
	}
	if len([]rune(in.DispenseUnit)) > 50 || in.LowStockThreshold < 0 || in.LowStockThreshold > 9999999999.999 {
		return Medicine{}, invalid("Đơn vị hoặc ngưỡng tồn không hợp lệ.")
	}
	desc, err := textnorm.Optional(in.Description, 1000)
	if err != nil {
		return Medicine{}, invalid("Mô tả quá dài.")
	}
	if in.Status != "ACTIVE" && in.Status != "INACTIVE" {
		return Medicine{}, invalid("Trạng thái không hợp lệ.")
	}
	if in.Status == "ACTIVE" && in.InitialPrice == nil {
		return Medicine{}, problem.New(409, "MEDICINE_PRICE_REQUIRED", "Thuốc ACTIVE cần giá đang hiệu lực.")
	}
	effective := s.now()
	if in.InitialEffectiveFrom != nil {
		effective, err = time.Parse(time.RFC3339, *in.InitialEffectiveFrom)
		if err != nil || effective.Before(s.now().Add(-time.Second)) {
			return Medicine{}, problem.New(422, "MEDICINE_PRICE_EFFECTIVE_FROM_PAST", "Mốc giá không hợp lệ.")
		}
	}
	if in.InitialPrice != nil && *in.InitialPrice < 0 {
		return Medicine{}, problem.New(422, "MEDICINE_PRICE_INVALID", "Giá thuốc không hợp lệ.")
	}
	if in.Status == "ACTIVE" && effective.After(s.now()) {
		return Medicine{}, problem.New(409, "MEDICINE_PRICE_REQUIRED", "Giá ban đầu chưa hiệu lực.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Medicine{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO medicines(code,name,normalized_name,active_ingredient,dosage_form,strength,dispense_unit,description,low_stock_threshold,status,created_by_user_id,updated_by_user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11) RETURNING id`, in.Code, in.Name, textnorm.Search(in.Name), in.ActiveIngredient, in.DosageForm, in.Strength, in.DispenseUnit, desc, in.LowStockThreshold, in.Status, actor.UserID).Scan(&id)
	if err != nil {
		return Medicine{}, mapErr(err)
	}
	if in.InitialPrice != nil {
		if _, err = tx.Exec(ctx, `INSERT INTO medicine_price_versions(medicine_id,effective_from,unit_sale_price_vnd,created_by_user_id) VALUES($1,$2,$3,$4)`, id, effective, *in.InitialPrice, actor.UserID); err != nil {
			return Medicine{}, mapErr(err)
		}
	}
	ev := audit.UserEvent(actor.UserID, "INVENTORY", "MEDICINE_CREATED", "MEDICINE", id)
	ev.TargetCode = in.Code
	if err = audit.Insert(ctx, tx, ev); err != nil {
		return Medicine{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Medicine{}, err
	}
	return s.GetMedicine(ctx, actor, id, true)
}

const medicineSelect = `SELECT m.id,m.code,m.name,m.active_ingredient,m.dosage_form,m.strength,m.dispense_unit,m.description,m.low_stock_threshold,m.status::text,m.version,m.is_deleted,m.created_at,m.updated_at, p.id,p.effective_from,p.unit_sale_price_vnd, COALESCE(sum(b.quantity_on_hand),0)::float8,COALESCE(sum(b.quantity_on_hand) FILTER(WHERE b.expires_on>=CURRENT_DATE),0)::float8,COALESCE(sum(b.quantity_on_hand) FILTER(WHERE b.expires_on<CURRENT_DATE),0)::float8 FROM medicines m LEFT JOIN LATERAL(SELECT * FROM medicine_price_versions x WHERE x.medicine_id=m.id AND x.cancelled_at IS NULL AND x.effective_from<=now() ORDER BY x.effective_from DESC,x.id DESC LIMIT 1)p ON true LEFT JOIN medicine_batches b ON b.medicine_id=m.id`

func scanMedicine(row interface{ Scan(...any) error }) (Medicine, error) {
	var m Medicine
	var pid, pamt *int64
	var peff *time.Time
	err := row.Scan(&m.ID, &m.Code, &m.Name, &m.ActiveIngredient, &m.DosageForm, &m.Strength, &m.DispenseUnit, &m.Description, &m.LowStockThreshold, &m.Status, &m.Version, &m.IsDeleted, &m.CreatedAt, &m.UpdatedAt, &pid, &peff, &pamt, &m.PhysicalQuantity, &m.SellableQuantity, &m.ExpiredQuantity)
	if pid != nil {
		m.CurrentPrice = &Price{ID: *pid, MedicineID: m.ID, EffectiveFrom: *peff, UnitSalePriceVND: *pamt, Currency: "VND", Phase: "EFFECTIVE"}
	}
	if m.SellableQuantity > 0 {
		m.StockAdvisory = "IN_STOCK"
	} else if m.ExpiredQuantity > 0 {
		m.StockAdvisory = "EXPIRED_ONLY"
	} else {
		m.StockAdvisory = "OUT_OF_STOCK"
	}
	return m, err
}
func (s *Service) GetMedicine(ctx context.Context, actor auth.Principal, id int64, manage bool) (Medicine, error) {
	m, err := scanMedicine(s.pool.QueryRow(ctx, medicineSelect+` WHERE m.id=$1 GROUP BY m.id,p.id,p.effective_from,p.unit_sale_price_vnd`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Medicine{}, problem.New(404, "MEDICINE_NOT_FOUND", "Không tìm thấy thuốc.")
	}
	if err != nil {
		return m, err
	}
	if !manage {
		m.Description = nil
		m.LowStockThreshold = 0
		m.PhysicalQuantity = 0
		m.SellableQuantity = 0
		m.ExpiredQuantity = 0
		m.CurrentPrice = nil
		if m.Status != "ACTIVE" || m.IsDeleted {
			return Medicine{}, problem.New(404, "MEDICINE_NOT_FOUND", "Không tìm thấy thuốc.")
		}
	}
	return m, nil
}
func (s *Service) ListMedicines(ctx context.Context, actor auth.Principal, manage bool, q, status string, page, size int) ([]Medicine, error) {
	page, size = pagination.Normalize(page, size)
	return s.listMedicines(ctx, actor, manage, q, status, page, size, size)
}
func (s *Service) ListMedicinesPage(ctx context.Context, actor auth.Principal, manage bool, q, status string, page, size int) (pagination.Page[Medicine], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.listMedicines(ctx, actor, manage, q, status, page, size, size+1)
	return pagination.From(items, page, size), err
}
func (s *Service) listMedicines(ctx context.Context, actor auth.Principal, manage bool, q, status string, page, size, fetch int) ([]Medicine, error) {
	where := ` WHERE NOT m.is_deleted AND ($1 OR m.status='ACTIVE') AND ($2='' OR m.status::text=$2) AND ($3='' OR m.code ILIKE '%'||$3||'%' OR m.name ILIKE '%'||$3||'%' OR m.active_ingredient ILIKE '%'||$3||'%')`
	rows, err := s.pool.Query(ctx, medicineSelect+where+` GROUP BY m.id,p.id,p.effective_from,p.unit_sale_price_vnd ORDER BY m.code,m.id LIMIT $4 OFFSET $5`, manage, strings.ToUpper(status), strings.TrimSpace(q), fetch, (page-1)*size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Medicine
	for rows.Next() {
		m, e := scanMedicine(rows)
		if e != nil {
			return nil, e
		}
		if !manage {
			m.Description = nil
			m.LowStockThreshold = 0
			m.PhysicalQuantity = 0
			m.SellableQuantity = 0
			m.ExpiredQuantity = 0
			m.CurrentPrice = nil
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Service) UpdateMedicine(ctx context.Context, a auth.Principal, id int64, in MedicineUpdate) (Medicine, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Medicine{}, e
	}
	defer tx.Rollback(ctx)
	var m Medicine
	e = tx.QueryRow(ctx, `SELECT name,active_ingredient,dosage_form,strength,dispense_unit,description,low_stock_threshold,version FROM medicines WHERE id=$1 AND NOT is_deleted FOR UPDATE`, id).Scan(&m.Name, &m.ActiveIngredient, &m.DosageForm, &m.Strength, &m.DispenseUnit, &m.Description, &m.LowStockThreshold, &m.Version)
	if errors.Is(e, pgx.ErrNoRows) {
		return Medicine{}, problem.New(404, "MEDICINE_NOT_FOUND", "Không tìm thấy thuốc.")
	}
	if e != nil {
		return Medicine{}, e
	}
	if m.Version != in.ExpectedVersion {
		return Medicine{}, problem.New(409, "MEDICINE_CONCURRENT_UPDATE", "Dữ liệu thuốc đã đổi.")
	}
	if in.Name != nil {
		m.Name = *in.Name
	}
	if in.ActiveIngredient != nil {
		m.ActiveIngredient = *in.ActiveIngredient
	}
	if in.DosageForm != nil {
		m.DosageForm = *in.DosageForm
	}
	if in.Strength != nil {
		m.Strength = *in.Strength
	}
	if in.DispenseUnit != nil {
		m.DispenseUnit = *in.DispenseUnit
	}
	if in.Description != nil {
		m.Description, e = textnorm.Optional(*in.Description, 1000)
		if e != nil {
			return Medicine{}, invalid("Mô tả quá dài.")
		}
	}
	if in.LowStockThreshold != nil {
		m.LowStockThreshold = *in.LowStockThreshold
	}
	if m.LowStockThreshold < 0 || m.LowStockThreshold > 9999999999.999 {
		return Medicine{}, invalid("Ngưỡng tồn không hợp lệ.")
	}
	for _, v := range []string{m.Name, m.ActiveIngredient, m.DosageForm, m.Strength, m.DispenseUnit} {
		if _, e = cleanRequired(v, 200); e != nil {
			return Medicine{}, invalid("Thông tin thuốc không hợp lệ.")
		}
	}
	_, e = tx.Exec(ctx, `UPDATE medicines SET name=$2,normalized_name=$3,active_ingredient=$4,dosage_form=$5,strength=$6,dispense_unit=$7,description=$8,low_stock_threshold=$9,updated_by_user_id=$10,updated_at=now(),version=version+1 WHERE id=$1`, id, m.Name, textnorm.Search(m.Name), m.ActiveIngredient, m.DosageForm, m.Strength, m.DispenseUnit, m.Description, m.LowStockThreshold, a.UserID)
	if e != nil {
		return Medicine{}, mapErr(e)
	}
	ev := audit.UserEvent(a.UserID, "INVENTORY", "MEDICINE_METADATA_UPDATED", "MEDICINE", id)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Medicine{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Medicine{}, e
	}
	return s.GetMedicine(ctx, a, id, true)
}
func (s *Service) ChangeState(ctx context.Context, a auth.Principal, id, expected int64, action, reason string) (Medicine, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Medicine{}, e
	}
	defer tx.Rollback(ctx)
	var st string
	var ver int64
	var del bool
	e = tx.QueryRow(ctx, `SELECT status::text,version,is_deleted FROM medicines WHERE id=$1 FOR UPDATE`, id).Scan(&st, &ver, &del)
	if errors.Is(e, pgx.ErrNoRows) {
		return Medicine{}, problem.New(404, "MEDICINE_NOT_FOUND", "Không tìm thấy thuốc.")
	}
	if e != nil {
		return Medicine{}, e
	}
	if del || ver != expected {
		return Medicine{}, problem.New(409, "MEDICINE_CONCURRENT_UPDATE", "Dữ liệu thuốc đã đổi.")
	}
	action = strings.ToLower(action)
	code := ""
	switch action {
	case "activate":
		var ok bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM medicine_price_versions WHERE medicine_id=$1 AND effective_from<=now() AND cancelled_at IS NULL)`, id).Scan(&ok)
		if e != nil {
			return Medicine{}, e
		}
		if st != "INACTIVE" || !ok {
			return Medicine{}, problem.New(409, "MEDICINE_PRICE_REQUIRED", "Thuốc cần giá hiệu lực để kích hoạt.")
		}
		code = "MEDICINE_ACTIVATED"
		st = "ACTIVE"
	case "deactivate":
		if st != "ACTIVE" {
			return Medicine{}, problem.New(409, "MEDICINE_CONCURRENT_UPDATE", "Trạng thái không hợp lệ.")
		}
		code = "MEDICINE_DEACTIVATED"
		st = "INACTIVE"
	case "archive":
		reason = strings.TrimSpace(reason)
		if st != "INACTIVE" || reason == "" {
			return Medicine{}, problem.New(409, "MEDICINE_ARCHIVED", "Chỉ lưu trữ thuốc INACTIVE có lý do.")
		}
		code = "MEDICINE_ARCHIVED"
		_, e = tx.Exec(ctx, `UPDATE medicines SET is_deleted=true,deleted_at=now(),deleted_by_user_id=$2,archive_reason=$3,updated_at=now(),updated_by_user_id=$2,version=version+1 WHERE id=$1`, id, a.UserID, reason)
	default:
		return Medicine{}, invalid("Action không hợp lệ.")
	}
	if action != "archive" {
		_, e = tx.Exec(ctx, `UPDATE medicines SET status=$2,updated_at=now(),updated_by_user_id=$3,version=version+1 WHERE id=$1`, id, st, a.UserID)
	}
	if e != nil {
		return Medicine{}, mapErr(e)
	}
	ev := audit.UserEvent(a.UserID, "INVENTORY", code, "MEDICINE", id)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Medicine{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Medicine{}, e
	}
	return s.GetMedicine(ctx, a, id, true)
}

func (s *Service) CreatePrice(ctx context.Context, a auth.Principal, id, amount int64, when string) (Price, error) {
	t, e := time.Parse(time.RFC3339, when)
	if e != nil || t.Before(s.now()) || amount < 0 {
		return Price{}, problem.New(422, "MEDICINE_PRICE_INVALID", "Giá hoặc mốc hiệu lực không hợp lệ.")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Price{}, e
	}
	defer tx.Rollback(ctx)
	var del bool
	if e = tx.QueryRow(ctx, `SELECT is_deleted FROM medicines WHERE id=$1 FOR UPDATE`, id).Scan(&del); e != nil {
		return Price{}, e
	}
	if del {
		return Price{}, problem.New(409, "MEDICINE_ARCHIVED", "Thuốc đã lưu trữ.")
	}
	var pid int64
	e = tx.QueryRow(ctx, `INSERT INTO medicine_price_versions(medicine_id,effective_from,unit_sale_price_vnd,created_by_user_id)VALUES($1,$2,$3,$4)RETURNING id`, id, t, amount, a.UserID).Scan(&pid)
	if e != nil {
		return Price{}, mapErr(e)
	}
	ev := audit.UserEvent(a.UserID, "INVENTORY", "MEDICINE_PRICE_VERSION_CREATED", "MEDICINE_PRICE_VERSION", pid)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Price{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Price{}, e
	}
	return Price{ID: pid, MedicineID: id, EffectiveFrom: t, UnitSalePriceVND: amount, Currency: "VND", Phase: map[bool]string{true: "SCHEDULED", false: "EFFECTIVE"}[t.After(s.now())]}, nil
}
func (s *Service) ListPrices(ctx context.Context, id int64) ([]Price, error) {
	var exists bool
	if e := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM medicines WHERE id=$1)`, id).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, problem.New(404, "MEDICINE_NOT_FOUND", "Không tìm thấy thuốc.")
	}
	rows, e := s.pool.Query(ctx, `SELECT id,medicine_id,effective_from,unit_sale_price_vnd,currency,CASE WHEN cancelled_at IS NOT NULL THEN 'CANCELLED' WHEN effective_from>now() THEN 'SCHEDULED' ELSE 'EFFECTIVE' END,cancellation_reason,cancelled_at FROM medicine_price_versions WHERE medicine_id=$1 ORDER BY effective_from DESC,id DESC`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Price
	for rows.Next() {
		var p Price
		if e = rows.Scan(&p.ID, &p.MedicineID, &p.EffectiveFrom, &p.UnitSalePriceVND, &p.Currency, &p.Phase, &p.CancellationReason, &p.CancelledAt); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (s *Service) CancelPrice(ctx context.Context, a auth.Principal, mid, pid int64, reason string) (Price, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Price{}, problem.New(422, "MEDICINE_PRICE_INVALID", "Lý do hủy không hợp lệ.")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Price{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT id FROM medicines WHERE id=$1 FOR UPDATE`, mid); e != nil {
		return Price{}, e
	}
	tag, e := tx.Exec(ctx, `UPDATE medicine_price_versions SET cancellation_reason=$3,cancelled_by_user_id=$4,cancelled_at=now() WHERE id=$1 AND medicine_id=$2 AND cancelled_at IS NULL AND effective_from>now()`, pid, mid, reason, a.UserID)
	if e != nil {
		return Price{}, mapErr(e)
	}
	if tag.RowsAffected() != 1 {
		return Price{}, problem.New(409, "MEDICINE_PRICE_VERSION_IMMUTABLE", "Chỉ hủy được giá SCHEDULED.")
	}
	ev := audit.UserEvent(a.UserID, "INVENTORY", "MEDICINE_PRICE_VERSION_CANCELLED", "MEDICINE_PRICE_VERSION", pid)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Price{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Price{}, e
	}
	list, e := s.ListPrices(ctx, mid)
	for _, p := range list {
		if p.ID == pid {
			return p, e
		}
	}
	return Price{}, e
}

func hash(v any) string {
	b, _ := json.Marshal(v)
	x := sha256.Sum256(b)
	return hex.EncodeToString(x[:])
}
func validateKey(k string) error {
	if k != strings.TrimSpace(k) || len(k) < 16 || len(k) > 128 {
		return problem.New(400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key 16-128 ký tự là bắt buộc.")
	}
	return nil
}
func beginIdem(ctx context.Context, tx pgx.Tx, a auth.Principal, scope, key, h string) (int64, *int64, error) {
	var id int64
	e := tx.QueryRow(ctx, `INSERT INTO idempotency_records(principal_key,actor_user_id,operation_scope,idempotency_key,request_hash,status,expires_at)VALUES($1,$2,$3,$4,$5,'PROCESSING',now()+interval '30 days')ON CONFLICT DO NOTHING RETURNING id`, fmt.Sprintf("user:%d", a.UserID), a.UserID, scope, key, h).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		var old, state string
		var rid *int64
		e = tx.QueryRow(ctx, `SELECT request_hash,status::text,resource_id FROM idempotency_records WHERE principal_key=$1 AND operation_scope=$2 AND idempotency_key=$3 FOR UPDATE`, fmt.Sprintf("user:%d", a.UserID), scope, key).Scan(&old, &state, &rid)
		if e != nil {
			return 0, nil, e
		}
		if old != h {
			return 0, nil, problem.New(409, "IDEMPOTENCY_KEY_REUSED", "Idempotency-Key đã dùng với payload khác.")
		}
		if state == "COMPLETED" {
			return 0, rid, nil
		}
		return 0, nil, problem.New(409, "IDEMPOTENCY_IN_PROGRESS", "Command đang xử lý.")
	}
	return id, nil, e
}

func (s *Service) CreateReceipt(ctx context.Context, a auth.Principal, in ReceiptInput) (Document, error) {
	if e := validateKey(in.IdempotencyKey); e != nil {
		return Document{}, e
	}
	if len(in.Items) < 1 || len(in.Items) > 100 {
		return Document{}, problem.New(422, "INVENTORY_RECEIPT_INVALID", "Receipt cần 1-100 item.")
	}
	received, e := time.Parse(time.RFC3339, in.ReceivedAt)
	if e != nil || received.After(s.now()) {
		return Document{}, problem.New(422, "INVENTORY_RECEIPT_INVALID", "received_at không hợp lệ.")
	}
	// Column limits (varchar 200/100/1000): reject over-length input as 422
	// instead of letting PostgreSQL raise 22001 (surfaced as 500).
	if len([]rune(strings.TrimSpace(in.SupplierName))) > 200 || len([]rune(strings.TrimSpace(in.SupplierDocumentRef))) > 100 || len([]rune(strings.TrimSpace(in.Note))) > 1000 {
		return Document{}, problem.New(422, "INVENTORY_RECEIPT_INVALID", "Nhà cung cấp, số chứng từ hoặc ghi chú quá dài.")
	}
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].MedicineID < in.Items[j].MedicineID })
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Document{}, e
	}
	defer tx.Rollback(ctx)
	recID, replay, e := beginIdem(ctx, tx, a, "inventory.receipt.create", in.IdempotencyKey, hash(in))
	if e != nil {
		return Document{}, e
	}
	if replay != nil {
		tx.Commit(ctx)
		d, e := s.getDocument(ctx, "stock_receipts", "receipt_code", *replay)
		d.Replayed = true
		return d, e
	}
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO stock_receipts(supplier_name,supplier_document_ref,received_at,note,received_by_user_id)VALUES(NULLIF($1,''),NULLIF($2,''),$3,NULLIF($4,''),$5)RETURNING id`, strings.TrimSpace(in.SupplierName), strings.TrimSpace(in.SupplierDocumentRef), received, strings.TrimSpace(in.Note), a.UserID).Scan(&id)
	if e != nil {
		return Document{}, e
	}
	for _, it := range in.Items {
		lot := strings.TrimSpace(it.LotNumber)
		location := time.FixedZone("Asia/Ho_Chi_Minh", 7*3600)
		exp, x := time.ParseInLocation("2006-01-02", it.ExpiresOn, location)
		now := s.now().In(location)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)
		if x != nil || !exp.After(today) || it.Quantity <= 0 || it.UnitCostVND < 0 || lot == "" {
			return Document{}, problem.New(422, "INVENTORY_RECEIPT_INVALID", "Receipt item không hợp lệ.")
		}
		var active bool
		if e = tx.QueryRow(ctx, `SELECT NOT is_deleted FROM medicines WHERE id=$1 FOR SHARE`, it.MedicineID).Scan(&active); e != nil || !active {
			return Document{}, problem.New(409, "MEDICINE_ARCHIVED", "Medicine không nhận lô mới.")
		}
		var mfg *time.Time
		if it.ManufactureDate != nil {
			x, xerr := time.ParseInLocation("2006-01-02", *it.ManufactureDate, location)
			if xerr != nil || x.After(exp) {
				return Document{}, problem.New(422, "INVENTORY_RECEIPT_INVALID", "Ngày sản xuất không hợp lệ.")
			}
			mfg = &x
		}
		var bid, iid int64
		e = tx.QueryRow(ctx, `INSERT INTO medicine_batches(medicine_id,lot_number,manufacture_date,expires_on,initial_received_quantity,unit_purchase_cost_vnd,quantity_on_hand)VALUES($1,$2,$3,$4,$5,$6,$5)RETURNING id`, it.MedicineID, lot, mfg, exp, it.Quantity, it.UnitCostVND).Scan(&bid)
		if e != nil {
			return Document{}, mapErr(e)
		}
		e = tx.QueryRow(ctx, `INSERT INTO stock_receipt_items(stock_receipt_id,medicine_id,medicine_batch_id,lot_number,manufacture_date,expires_on,received_quantity,unit_purchase_cost_vnd)VALUES($1,$2,$3,$4,$5,$6,$7,$8)RETURNING id`, id, it.MedicineID, bid, lot, mfg, exp, it.Quantity, it.UnitCostVND).Scan(&iid)
		if e != nil {
			return Document{}, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO stock_movements(medicine_batch_id,medicine_id,movement_type,quantity_delta,quantity_before,quantity_after,reference_type,reference_id,created_by_user_id)VALUES($1,$2,'RECEIPT_IN',$3,0,$3,'RECEIPT_ITEM',$4,$5)`, bid, it.MedicineID, it.Quantity, iid, a.UserID)
		if e != nil {
			return Document{}, e
		}
	}
	ev := audit.UserEvent(a.UserID, "INVENTORY", "INVENTORY_RECEIPT_CREATED", "STOCK_RECEIPT", id)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Document{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',resource_type='STOCK_RECEIPT',resource_id=$2,http_status=201,response_body=jsonb_build_object('id',$2::bigint),completed_at=now(),updated_at=now() WHERE id=$1`, recID, id)
	if e != nil {
		return Document{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Document{}, e
	}
	return s.getDocument(ctx, "stock_receipts", "receipt_code", id)
}

func (s *Service) CreateAdjustment(ctx context.Context, a auth.Principal, in AdjustmentInput) (Document, error) {
	if e := validateKey(in.IdempotencyKey); e != nil {
		return Document{}, e
	}
	in.ReasonCode = strings.ToUpper(strings.TrimSpace(in.ReasonCode))
	in.ReasonDetail = strings.TrimSpace(in.ReasonDetail)
	validReason := map[string]bool{"COUNT_CORRECTION": true, "DAMAGE": true, "EXPIRED_DISPOSAL": true, "OTHER": true}
	if len(in.Items) < 1 || len(in.Items) > 100 || in.ReasonDetail == "" || !validReason[in.ReasonCode] {
		return Document{}, problem.New(422, "INVENTORY_ADJUSTMENT_INVALID", "Adjustment không hợp lệ.")
	}
	sort.Slice(in.Items, func(i, j int) bool { return in.Items[i].BatchID < in.Items[j].BatchID })
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Document{}, e
	}
	defer tx.Rollback(ctx)
	batchIDs := make([]int64, 0, len(in.Items))
	for _, item := range in.Items {
		batchIDs = append(batchIDs, item.BatchID)
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT medicine_id FROM medicine_batches WHERE id=ANY($1) ORDER BY medicine_id`, batchIDs)
	if e != nil {
		return Document{}, e
	}
	var medicineIDs []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return Document{}, e
		}
		medicineIDs = append(medicineIDs, id)
	}
	rows.Close()
	for _, id := range medicineIDs {
		if _, e = tx.Exec(ctx, `SELECT id FROM medicines WHERE id=$1 FOR SHARE`, id); e != nil {
			return Document{}, e
		}
	}
	recID, replay, e := beginIdem(ctx, tx, a, "inventory.adjustment.create", in.IdempotencyKey, hash(in))
	if e != nil {
		return Document{}, e
	}
	if replay != nil {
		tx.Commit(ctx)
		d, e := s.getDocument(ctx, "inventory_adjustments", "adjustment_code", *replay)
		d.Replayed = true
		return d, e
	}
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO inventory_adjustments(reason_code,reason_detail,correction_of_adjustment_id,adjusted_by_user_id)VALUES($1,$2,$3,$4)RETURNING id`, in.ReasonCode, in.ReasonDetail, in.CorrectionOf, a.UserID).Scan(&id)
	if e != nil {
		return Document{}, e
	}
	for _, it := range in.Items {
		it.Direction = strings.ToUpper(it.Direction)
		if it.Quantity <= 0 || (it.Direction != "IN" && it.Direction != "OUT") {
			return Document{}, problem.New(422, "INVENTORY_ADJUSTMENT_INVALID", "Adjustment item không hợp lệ.")
		}
		var mid int64
		var before float64
		var exp time.Time
		e = tx.QueryRow(ctx, `SELECT medicine_id,quantity_on_hand::float8,expires_on FROM medicine_batches WHERE id=$1 FOR UPDATE`, it.BatchID).Scan(&mid, &before, &exp)
		if errors.Is(e, pgx.ErrNoRows) {
			return Document{}, problem.New(404, "INVENTORY_BATCH_NOT_FOUND", "Không tìm thấy lô.")
		}
		if e != nil {
			return Document{}, e
		}
		if in.ReasonCode == "EXPIRED_DISPOSAL" && !exp.Before(s.now()) {
			return Document{}, problem.New(422, "INVENTORY_ADJUSTMENT_INVALID", "Lô chưa hết hạn.")
		}
		delta := it.Quantity
		typ := "ADJUSTMENT_IN"
		if it.Direction == "OUT" {
			delta = -delta
			typ = "ADJUSTMENT_OUT"
		}
		after := before + delta
		if after < 0 {
			return Document{}, problem.New(409, "INVENTORY_ADJUSTMENT_OUT_EXCEEDS_STOCK", "Điều chỉnh vượt tồn.")
		}
		var iid int64
		e = tx.QueryRow(ctx, `INSERT INTO inventory_adjustment_items(inventory_adjustment_id,medicine_batch_id,direction,quantity)VALUES($1,$2,$3,$4)RETURNING id`, id, it.BatchID, it.Direction, it.Quantity).Scan(&iid)
		if e != nil {
			return Document{}, e
		}
		_, e = tx.Exec(ctx, `UPDATE medicine_batches SET quantity_on_hand=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, it.BatchID, after)
		if e != nil {
			return Document{}, e
		}
		_, e = tx.Exec(ctx, `INSERT INTO stock_movements(medicine_batch_id,medicine_id,movement_type,quantity_delta,quantity_before,quantity_after,reference_type,reference_id,created_by_user_id)VALUES($1,$2,$3,$4,$5,$6,'ADJUSTMENT_ITEM',$7,$8)`, it.BatchID, mid, typ, delta, before, after, iid, a.UserID)
		if e != nil {
			return Document{}, e
		}
	}
	ev := audit.UserEvent(a.UserID, "INVENTORY", "INVENTORY_ADJUSTMENT_CREATED", "INVENTORY_ADJUSTMENT", id)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Document{}, e
	}
	_, e = tx.Exec(ctx, `UPDATE idempotency_records SET status='COMPLETED',resource_type='INVENTORY_ADJUSTMENT',resource_id=$2,http_status=201,response_body=jsonb_build_object('id',$2::bigint),completed_at=now(),updated_at=now() WHERE id=$1`, recID, id)
	if e != nil {
		return Document{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Document{}, e
	}
	return s.getDocument(ctx, "inventory_adjustments", "adjustment_code", id)
}
func (s *Service) getDocument(ctx context.Context, table, code string, id int64) (Document, error) {
	q := fmt.Sprintf(`SELECT id,%s,created_at FROM %s WHERE id=$1`, code, table)
	var d Document
	e := s.pool.QueryRow(ctx, q, id).Scan(&d.ID, &d.Code, &d.CreatedAt)
	return d, e
}
func (s *Service) Summary(ctx context.Context) (map[string]any, error) {
	var p, sell, exp float64
	var low, soon int
	e := s.pool.QueryRow(ctx, `WITH q AS(SELECT m.id,m.low_stock_threshold,COALESCE(sum(b.quantity_on_hand),0) physical,COALESCE(sum(b.quantity_on_hand)FILTER(WHERE b.expires_on>=CURRENT_DATE),0)sell,COALESCE(sum(b.quantity_on_hand)FILTER(WHERE b.expires_on<CURRENT_DATE),0)expired FROM medicines m LEFT JOIN medicine_batches b ON b.medicine_id=m.id GROUP BY m.id),cfg AS(SELECT integer_value days FROM system_settings WHERE setting_key='inventory_expiry_alert_days') SELECT COALESCE(sum(physical),0)::float8,COALESCE(sum(sell),0)::float8,COALESCE(sum(expired),0)::float8,count(*)FILTER(WHERE low_stock_threshold>0 AND sell<=low_stock_threshold),(SELECT count(*) FROM medicine_batches,cfg WHERE quantity_on_hand>0 AND expires_on BETWEEN CURRENT_DATE AND CURRENT_DATE+days) FROM q`).Scan(&p, &sell, &exp, &low, &soon)
	return map[string]any{"physical_quantity": p, "sellable_quantity": sell, "expired_quantity": exp, "low_stock_count": low, "expiry_soon_count": soon}, e
}

func (s *Service) ListBatches(ctx context.Context, id int64, medicineFilter ...int64) ([]map[string]any, error) {
	var medicineID int64
	if len(medicineFilter) > 0 {
		medicineID = medicineFilter[0]
	}
	return s.listBatches(ctx, id, medicineID, "", 0, 0)
}
func (s *Service) ListBatchesPage(ctx context.Context, id, medicineID int64, search string, page, size int) (pagination.Page[map[string]any], error) {
	page, size = pagination.Normalize(page, size)
	items, err := s.listBatches(ctx, id, medicineID, search, size+1, (page-1)*size)
	return pagination.From(items, page, size), err
}
func (s *Service) listBatches(ctx context.Context, id, medicineID int64, search string, limit, offset int) ([]map[string]any, error) {
	q := `SELECT b.id,b.medicine_id,m.code,m.name,b.lot_number,b.manufacture_date,b.expires_on,b.initial_received_quantity::float8,b.unit_purchase_cost_vnd::bigint,b.quantity_on_hand::float8,b.version,b.created_at,b.updated_at,CASE WHEN b.quantity_on_hand=0 THEN 'OUT_OF_STOCK' WHEN b.expires_on<CURRENT_DATE THEN 'EXPIRED' ELSE 'AVAILABLE' END FROM medicine_batches b JOIN medicines m ON m.id=b.medicine_id WHERE ($1=0 OR b.id=$1) AND ($2=0 OR b.medicine_id=$2) AND ($3='' OR b.lot_number ILIKE '%'||$3||'%' OR m.code ILIKE '%'||$3||'%' OR m.name ILIKE '%'||$3||'%') ORDER BY m.code,b.expires_on,b.id`
	args := []any{id, medicineID, strings.TrimSpace(search)}
	if limit > 0 {
		q += ` LIMIT $4 OFFSET $5`
		args = append(args, limit, offset)
	}
	rows, e := s.pool.Query(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var bid, mid, ver, cost int64
		var code, name, lot, state string
		var mfg *time.Time
		var exp, ca, ua time.Time
		var initial, onhand float64
		if e = rows.Scan(&bid, &mid, &code, &name, &lot, &mfg, &exp, &initial, &cost, &onhand, &ver, &ca, &ua, &state); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": bid, "medicine_id": mid, "medicine_code": code, "medicine_name": name, "lot_number": lot, "manufacture_date": mfg, "expires_on": exp, "initial_received_quantity": initial, "unit_purchase_cost_vnd": cost, "quantity_on_hand": onhand, "version": ver, "state": state, "created_at": ca, "updated_at": ua})
	}
	if id > 0 && len(out) == 0 {
		return nil, problem.New(404, "INVENTORY_BATCH_NOT_FOUND", "Không tìm thấy lô.")
	}
	return out, rows.Err()
}
func (s *Service) ListDocuments(ctx context.Context, kind string, id int64) ([]map[string]any, error) {
	table, code, actor, at := "stock_receipts", "receipt_code", "received_by_user_id", "received_at"
	if kind == "adjustment" {
		table, code, actor, at = "inventory_adjustments", "adjustment_code", "adjusted_by_user_id", "adjusted_at"
	}
	q := fmt.Sprintf(`SELECT id,%s,%s,%s,created_at FROM %s WHERE ($1=0 OR id=$1) ORDER BY %s DESC,id DESC`, code, actor, at, table, at)
	rows, e := s.pool.Query(ctx, q, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var rid, uid int64
		var c string
		var t, created time.Time
		if e = rows.Scan(&rid, &c, &uid, &t, &created); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": rid, "code": c, "actor_user_id": uid, "occurred_at": t, "created_at": created})
	}
	return out, rows.Err()
}
func (s *Service) ListMovements(ctx context.Context, medicineID, batchID int64) ([]map[string]any, error) {
	rows, e := s.pool.Query(ctx, `SELECT id,medicine_batch_id,medicine_id,movement_type::text,quantity_delta::float8,quantity_before::float8,quantity_after::float8,reference_type::text,reference_id,occurred_at,created_by_user_id FROM stock_movements WHERE ($1=0 OR medicine_id=$1)AND($2=0 OR medicine_batch_id=$2)ORDER BY occurred_at DESC,id DESC LIMIT 100`, medicineID, batchID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, bid, mid, ref int64
		var typ, rt string
		var d, b, a float64
		var at time.Time
		var uid *int64
		if e = rows.Scan(&id, &bid, &mid, &typ, &d, &b, &a, &rt, &ref, &at, &uid); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": id, "medicine_batch_id": bid, "medicine_id": mid, "movement_type": typ, "quantity_delta": d, "quantity_before": b, "quantity_after": a, "reference_type": rt, "reference_id": ref, "occurred_at": at, "created_by_user_id": uid})
	}
	return out, rows.Err()
}

// ResolveSalesForInvoice locks catalog and current prices in the global order. Caller owns the transaction.
func (s *Service) ResolveSalesForInvoice(ctx context.Context, tx pgx.Tx, medicineIDs []int64) ([]SaleSnapshot, error) {
	return s.resolveSales(ctx, tx, medicineIDs, true)
}

// PreviewSalesForInvoice applies identical eligibility and current-price rules without row locks.
func (s *Service) PreviewSalesForInvoice(ctx context.Context, tx pgx.Tx, medicineIDs []int64) ([]SaleSnapshot, error) {
	return s.resolveSales(ctx, tx, medicineIDs, false)
}
func (s *Service) resolveSales(ctx context.Context, tx pgx.Tx, medicineIDs []int64, lock bool) ([]SaleSnapshot, error) {
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	sort.Slice(medicineIDs, func(i, j int) bool { return medicineIDs[i] < medicineIDs[j] })
	out := make([]SaleSnapshot, 0, len(medicineIDs))
	seen := map[int64]bool{}
	for _, id := range medicineIDs {
		if seen[id] {
			continue
		}
		seen[id] = true
		var x SaleSnapshot
		var status string
		var deleted bool
		e := tx.QueryRow(ctx, `SELECT id,code,name,dispense_unit,status::text,is_deleted FROM medicines WHERE id=$1`+suffix, id).Scan(&x.MedicineID, &x.Code, &x.Name, &x.DispenseUnit, &status, &deleted)
		if errors.Is(e, pgx.ErrNoRows) || deleted || status != "ACTIVE" {
			return nil, problem.New(409, "MEDICINE_INACTIVE", "Thuốc không còn đủ điều kiện lập hóa đơn.")
		}
		if e != nil {
			return nil, e
		}
		e = tx.QueryRow(ctx, `SELECT id,unit_sale_price_vnd::bigint FROM medicine_price_versions WHERE medicine_id=$1 AND effective_from<=now() AND cancelled_at IS NULL ORDER BY effective_from DESC,id DESC LIMIT 1`+suffix, id).Scan(&x.PriceVersionID, &x.UnitSalePriceVND)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, problem.New(409, "MEDICINE_PRICE_REQUIRED", "Thuốc chưa có giá hiệu lực.")
		}
		if e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, nil
}

// AllocateStockForPayment performs full-invoice precheck and FEFO deduction. Caller must rollback on error and commit financial state in the same transaction.
func (s *Service) AllocateStockForPayment(ctx context.Context, tx pgx.Tx, requirements []StockRequirement) ([]StockAllocation, error) {
	if len(requirements) == 0 {
		return nil, nil
	}
	sort.Slice(requirements, func(i, j int) bool {
		if requirements[i].MedicineID == requirements[j].MedicineID {
			return requirements[i].ReferenceID < requirements[j].ReferenceID
		}
		return requirements[i].MedicineID < requirements[j].MedicineID
	})
	totals := map[int64]float64{}
	ids := []int64{}
	for _, r := range requirements {
		if r.MedicineID < 1 || r.ReferenceID < 1 || r.Quantity <= 0 {
			return nil, problem.New(409, "INVENTORY_SALE_ALLOCATION_INVALID", "Yêu cầu phân bổ kho không hợp lệ.")
		}
		if _, ok := totals[r.MedicineID]; !ok {
			ids = append(ids, r.MedicineID)
		}
		totals[r.MedicineID] += r.Quantity
	}
	for _, id := range ids {
		if _, e := tx.Exec(ctx, `SELECT id FROM medicines WHERE id=$1 FOR SHARE`, id); e != nil {
			return nil, e
		}
	}
	type batch struct {
		id  int64
		qty float64
	}
	batches := map[int64][]batch{}
	for _, id := range ids {
		rows, e := tx.Query(ctx, `SELECT id,quantity_on_hand::float8 FROM medicine_batches WHERE medicine_id=$1 AND expires_on>=CURRENT_DATE AND quantity_on_hand>0 ORDER BY expires_on,id FOR UPDATE`, id)
		if e != nil {
			return nil, e
		}
		var total float64
		for rows.Next() {
			var b batch
			if e = rows.Scan(&b.id, &b.qty); e != nil {
				rows.Close()
				return nil, e
			}
			batches[id] = append(batches[id], b)
			total += b.qty
		}
		rows.Close()
		if total+1e-9 < totals[id] {
			return nil, problem.New(409, "INVENTORY_INSUFFICIENT_STOCK", "Không đủ thuốc còn hạn cho toàn bộ hóa đơn.")
		}
	}
	alloc := []StockAllocation{}
	offset := map[int64]int{}
	for _, r := range requirements {
		remaining := r.Quantity
		for remaining > 1e-9 {
			idx := offset[r.MedicineID]
			b := &batches[r.MedicineID][idx]
			take := remaining
			if b.qty < take {
				take = b.qty
			}
			before := b.qty
			after := before - take
			var movementID int64
			if _, e := tx.Exec(ctx, `UPDATE medicine_batches SET quantity_on_hand=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, b.id, after); e != nil {
				return nil, e
			}
			e := tx.QueryRow(ctx, `INSERT INTO stock_movements(medicine_batch_id,medicine_id,movement_type,quantity_delta,quantity_before,quantity_after,reference_type,reference_id)VALUES($1,$2,'SALE_OUT',$3,$4,$5,'INVOICE_ITEM',$6)RETURNING id`, b.id, r.MedicineID, -take, before, after, r.ReferenceID).Scan(&movementID)
			if e != nil {
				return nil, e
			}
			alloc = append(alloc, StockAllocation{MedicineID: r.MedicineID, BatchID: b.id, ReferenceID: r.ReferenceID, MovementID: movementID, Quantity: take})
			b.qty = after
			remaining -= take
			if b.qty < 1e-9 {
				offset[r.MedicineID]++
			}
		}
	}
	return alloc, nil
}
func mapErr(e error) error {
	switch pgutil.Constraint(e) {
	case "uq_medicines_code":
		return problem.New(409, "MEDICINE_CODE_EXISTS", "Mã thuốc đã tồn tại.")
	case "uq_medicines_normalized_name":
		return problem.New(409, "MEDICINE_NAME_EXISTS", "Tên thuốc đã tồn tại.")
	case "uq_medicine_price_effective_from":
		return problem.New(409, "MEDICINE_PRICE_VERSION_EXISTS", "Trùng mốc giá.")
	case "uq_medicine_batches_lot":
		return problem.New(409, "INVENTORY_RECEIPT_LOT_EXISTS", "Lô thuốc đã tồn tại.")
	}
	return fmt.Errorf("inventory persistence: %w", e)
}

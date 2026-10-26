package servicecatalog

import (
	"context"
	"dncs/backend/internal/platform/pagination"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/platform/textnorm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var serviceCodePattern = regexp.MustCompile(`^SVC-[A-Z0-9-]{2,30}$`)

type Service struct {
	pool  *pgxpool.Pool
	authz *authorization.Store
	now   func() time.Time
}

func NewService(pool *pgxpool.Pool, authz *authorization.Store) *Service {
	return &Service{pool: pool, authz: authz, now: time.Now}
}

type CatalogService struct {
	ID           int64         `json:"id"`
	Code         string        `json:"code,omitempty"`
	Name         string        `json:"name"`
	ServiceType  string        `json:"service_type"`
	SpecialtyID  *int64        `json:"specialty_id,omitempty"`
	UnitLabel    string        `json:"unit_label"`
	Description  *string       `json:"description,omitempty"`
	Status       string        `json:"status"`
	CurrentPrice *CurrentPrice `json:"current_price,omitempty"`
	NextPriceAt  *time.Time    `json:"next_price_at,omitempty"`
	Version      int64         `json:"version"`
	IsDeleted    bool          `json:"is_deleted"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

type CurrentPrice struct {
	EffectiveFrom time.Time `json:"effective_from"`
	UnitPriceVND  int64     `json:"unit_price_vnd"`
	Currency      string    `json:"currency"`
}

type PriceVersion struct {
	ID                 int64      `json:"id"`
	ServiceID          int64      `json:"service_id"`
	EffectiveFrom      time.Time  `json:"effective_from"`
	UnitPriceVND       int64      `json:"unit_price_vnd"`
	Currency           string     `json:"currency"`
	Phase              string     `json:"phase"`
	CreatedByUserID    int64      `json:"created_by_user_id"`
	CreatedAt          time.Time  `json:"created_at"`
	CancellationReason *string    `json:"cancellation_reason,omitempty"`
	CancelledByUserID  *int64     `json:"cancelled_by_user_id,omitempty"`
	CancelledAt        *time.Time `json:"cancelled_at,omitempty"`
}

type CreateInput struct {
	Code                 string
	Name                 string
	ServiceType          string
	SpecialtyID          *int64
	UnitLabel            string
	Description          string
	Status               string
	InitialUnitPriceVND  *int64
	InitialEffectiveFrom *string
}

type UpdateInput struct {
	ExpectedVersion int64
	Name            *string
	UnitLabel       *string
	Description     *string
}

type ListFilter struct {
	Query       string
	Status      string
	ServiceType string
	SpecialtyID int64
	Page        int
	PageSize    int
}

func (s *Service) Create(ctx context.Context, actor auth.Principal, input CreateInput) (CatalogService, error) {
	code := textnorm.UpperCode(input.Code)
	name, err := textnorm.HumanName(input.Name, 200)
	serviceType := strings.ToUpper(strings.TrimSpace(input.ServiceType))
	status := strings.ToUpper(strings.TrimSpace(input.Status))
	if status == "" {
		status = "INACTIVE"
	}
	unit := strings.TrimSpace(input.UnitLabel)
	if unit == "" {
		unit = "Lần"
	}
	description, descErr := textnorm.Optional(input.Description, 1000)
	if err != nil || descErr != nil || !serviceCodePattern.MatchString(code) || len([]rune(unit)) > 50 ||
		(serviceType != "CONSULTATION" && serviceType != "PROCEDURE" && serviceType != "OTHER") ||
		(status != "ACTIVE" && status != "INACTIVE") || input.ExpectedSpecialtyInvalid(serviceType) {
		return CatalogService{}, invalid("Thông tin dịch vụ không hợp lệ.")
	}
	price, effective, err := s.validateInitialPrice(input)
	if err != nil {
		return CatalogService{}, err
	}
	if status == "ACTIVE" && (price == nil || effective.After(s.now())) {
		return CatalogService{}, problem.New(409, "SERVICE_PRICE_REQUIRED", "Dịch vụ hoạt động cần giá đang hiệu lực.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CatalogService{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if serviceType == "CONSULTATION" {
		var eligible bool
		if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM specialties WHERE id=$1 FOR SHARE`, *input.SpecialtyID).Scan(&eligible); errors.Is(err, pgx.ErrNoRows) {
			return CatalogService{}, problem.New(409, "SERVICE_SPECIALTY_NOT_ELIGIBLE", "Chuyên khoa không hợp lệ cho phí khám.")
		} else if err != nil {
			return CatalogService{}, err
		}
		if !eligible {
			return CatalogService{}, problem.New(409, "SERVICE_SPECIALTY_NOT_ELIGIBLE", "Chuyên khoa không hoạt động.")
		}
	}
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO services(code,name,normalized_name,service_type,specialty_id,unit_label,description,status,created_by_user_id,updated_by_user_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) RETURNING id`, code, name, textnorm.Search(name), serviceType,
		input.SpecialtyID, unit, description, status, actor.UserID).Scan(&id)
	if err != nil {
		return CatalogService{}, mapConstraint(err)
	}
	created := audit.UserEvent(actor.UserID, "SERVICE", "SERVICE_CREATED", "SERVICE", id)
	created.TargetCode = code
	created.Metadata = map[string]any{"service_type": serviceType, "status": status, "specialty_id": input.SpecialtyID}
	if err := audit.Insert(ctx, tx, created); err != nil {
		return CatalogService{}, err
	}
	if price != nil {
		var priceID int64
		if err := tx.QueryRow(ctx, `INSERT INTO service_price_versions(service_id,effective_from,unit_price_vnd,created_by_user_id) VALUES($1,$2,$3,$4) RETURNING id`, id, effective, *price, actor.UserID).Scan(&priceID); err != nil {
			return CatalogService{}, mapConstraint(err)
		}
		event := audit.UserEvent(actor.UserID, "SERVICE", "SERVICE_PRICE_VERSION_CREATED", "SERVICE_PRICE_VERSION", priceID)
		event.Metadata = map[string]any{"service_id": id, "effective_from": effective, "unit_price_vnd": *price}
		if err := audit.Insert(ctx, tx, event); err != nil {
			return CatalogService{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return CatalogService{}, mapConstraint(err)
	}
	return s.getRaw(ctx, id, true)
}

func (input CreateInput) ExpectedSpecialtyInvalid(serviceType string) bool {
	return (serviceType == "CONSULTATION" && (input.SpecialtyID == nil || *input.SpecialtyID < 1)) ||
		(serviceType != "CONSULTATION" && input.SpecialtyID != nil)
}

func (s *Service) validateInitialPrice(input CreateInput) (*int64, time.Time, error) {
	if input.InitialUnitPriceVND == nil && input.InitialEffectiveFrom == nil {
		return nil, time.Time{}, nil
	}
	if input.InitialUnitPriceVND == nil || *input.InitialUnitPriceVND < 0 {
		return nil, time.Time{}, problem.New(422, "SERVICE_PRICE_INVALID", "Giá ban đầu không hợp lệ.")
	}
	effective := s.now()
	if input.InitialEffectiveFrom != nil && strings.TrimSpace(*input.InitialEffectiveFrom) != "" {
		parsed, err := time.Parse(time.RFC3339, *input.InitialEffectiveFrom)
		if err != nil {
			return nil, time.Time{}, problem.New(422, "SERVICE_PRICE_INVALID", "Mốc hiệu lực giá không hợp lệ.")
		}
		effective = parsed
		if effective.Before(s.now()) {
			return nil, time.Time{}, problem.New(422, "SERVICE_PRICE_EFFECTIVE_FROM_PAST", "Không thể tạo giá có hiệu lực trong quá khứ.")
		}
	}
	return input.InitialUnitPriceVND, effective, nil
}

func (s *Service) Update(ctx context.Context, actor auth.Principal, id int64, input UpdateInput) (CatalogService, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CatalogService{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var name, unit string
	var description *string
	var version int64
	if err := tx.QueryRow(ctx, `SELECT name,unit_label,description,version FROM services WHERE id=$1 AND NOT is_deleted FOR UPDATE`, id).Scan(&name, &unit, &description, &version); errors.Is(err, pgx.ErrNoRows) {
		return CatalogService{}, notFound()
	} else if err != nil {
		return CatalogService{}, err
	}
	if version != input.ExpectedVersion {
		return CatalogService{}, concurrent()
	}
	if input.Name != nil {
		name = *input.Name
	}
	if input.UnitLabel != nil {
		unit = *input.UnitLabel
	}
	if input.Description != nil {
		description, err = textnorm.Optional(*input.Description, 1000)
		if err != nil {
			return CatalogService{}, invalid("Mô tả dịch vụ quá dài.")
		}
	}
	name, err = textnorm.HumanName(name, 200)
	unit = strings.TrimSpace(unit)
	if err != nil || unit == "" || len([]rune(unit)) > 50 {
		return CatalogService{}, invalid("Tên hoặc đơn vị dịch vụ không hợp lệ.")
	}
	if _, err := tx.Exec(ctx, `UPDATE services SET name=$2,normalized_name=$3,unit_label=$4,description=$5,updated_by_user_id=$6,updated_at=now(),version=version+1 WHERE id=$1`, id, name, textnorm.Search(name), unit, description, actor.UserID); err != nil {
		return CatalogService{}, mapConstraint(err)
	}
	event := audit.UserEvent(actor.UserID, "SERVICE", "SERVICE_METADATA_UPDATED", "SERVICE", id)
	event.Metadata = map[string]any{"from_version": version, "to_version": version + 1, "fields": []string{"name", "unit_label", "description"}}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return CatalogService{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CatalogService{}, err
	}
	return s.getRaw(ctx, id, true)
}

func (s *Service) ChangeState(ctx context.Context, actor auth.Principal, id, expectedVersion int64, action, reason string) (CatalogService, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	reason = strings.TrimSpace(reason)
	if action == "archive" && (reason == "" || len([]rune(reason)) > 1000) {
		return CatalogService{}, invalid("Lý do lưu trữ là bắt buộc và tối đa 1.000 ký tự.")
	}
	var preliminaryType string
	var preliminarySpecialtyID *int64
	if err := s.pool.QueryRow(ctx, `SELECT service_type::text,specialty_id FROM services WHERE id=$1`, id).Scan(&preliminaryType, &preliminarySpecialtyID); errors.Is(err, pgx.ErrNoRows) {
		return CatalogService{}, notFound()
	} else if err != nil {
		return CatalogService{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CatalogService{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if preliminaryType == "CONSULTATION" && preliminarySpecialtyID != nil {
		if _, err := tx.Exec(ctx, `SELECT id FROM specialties WHERE id=$1 FOR SHARE`, *preliminarySpecialtyID); err != nil {
			return CatalogService{}, err
		}
	}
	var status, serviceType string
	var specialtyID *int64
	var deleted bool
	var version int64
	if err := tx.QueryRow(ctx, `SELECT status::text,service_type::text,specialty_id,is_deleted,version FROM services WHERE id=$1 FOR UPDATE`, id).Scan(&status, &serviceType, &specialtyID, &deleted, &version); errors.Is(err, pgx.ErrNoRows) {
		return CatalogService{}, notFound()
	} else if err != nil {
		return CatalogService{}, err
	}
	if deleted {
		return CatalogService{}, problem.New(409, "SERVICE_ARCHIVED", "Dịch vụ đã được lưu trữ.")
	}
	if version != expectedVersion {
		return CatalogService{}, concurrent()
	}
	eventCode, targetStatus := "", status
	switch action {
	case "activate":
		if status != "INACTIVE" {
			return CatalogService{}, problem.New(409, "SERVICE_CONCURRENT_UPDATE", "Dịch vụ không ở trạng thái có thể kích hoạt.")
		}
		if serviceType == "CONSULTATION" {
			var eligible bool
			if err := tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM specialties WHERE id=$1`, specialtyID).Scan(&eligible); err != nil {
				return CatalogService{}, err
			}
			if !eligible {
				return CatalogService{}, problem.New(409, "SERVICE_SPECIALTY_NOT_ELIGIBLE", "Chuyên khoa không hoạt động.")
			}
		}
		var hasPrice bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM service_price_versions WHERE service_id=$1 AND effective_from<=now() AND cancelled_at IS NULL)`, id).Scan(&hasPrice); err != nil {
			return CatalogService{}, err
		}
		if !hasPrice {
			return CatalogService{}, problem.New(409, "SERVICE_PRICE_REQUIRED", "Dịch vụ cần giá đang hiệu lực trước khi kích hoạt.")
		}
		targetStatus, eventCode = "ACTIVE", "SERVICE_ACTIVATED"
	case "deactivate":
		if status != "ACTIVE" {
			return CatalogService{}, problem.New(409, "SERVICE_CONCURRENT_UPDATE", "Dịch vụ không ở trạng thái có thể vô hiệu hóa.")
		}
		targetStatus, eventCode = "INACTIVE", "SERVICE_DEACTIVATED"
	case "archive":
		if status != "INACTIVE" {
			return CatalogService{}, problem.New(409, "SERVICE_MUST_BE_INACTIVE", "Phải vô hiệu hóa dịch vụ trước khi lưu trữ.")
		}
		eventCode = "SERVICE_ARCHIVED"
	default:
		return CatalogService{}, invalid("Thao tác trạng thái dịch vụ không hợp lệ.")
	}
	if action == "archive" {
		_, err = tx.Exec(ctx, `UPDATE services SET is_deleted=true,deleted_at=now(),deleted_by_user_id=$2,archive_reason=$3,updated_by_user_id=$2,updated_at=now(),version=version+1 WHERE id=$1`, id, actor.UserID, reason)
	} else {
		_, err = tx.Exec(ctx, `UPDATE services SET status=$2,updated_by_user_id=$3,updated_at=now(),version=version+1 WHERE id=$1`, id, targetStatus, actor.UserID)
	}
	if err != nil {
		return CatalogService{}, mapConstraint(err)
	}
	event := audit.UserEvent(actor.UserID, "SERVICE", eventCode, "SERVICE", id)
	event.Metadata = map[string]any{"from_status": status, "to_status": targetStatus, "reason": reason}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return CatalogService{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CatalogService{}, mapConstraint(err)
	}
	return s.getRaw(ctx, id, true)
}

func (s *Service) CreatePrice(ctx context.Context, actor auth.Principal, serviceID, amount int64, effectiveFrom string) (PriceVersion, error) {
	if amount < 0 {
		return PriceVersion{}, problem.New(422, "SERVICE_PRICE_INVALID", "Đơn giá không hợp lệ.")
	}
	effective, err := time.Parse(time.RFC3339, effectiveFrom)
	if err != nil {
		return PriceVersion{}, problem.New(422, "SERVICE_PRICE_INVALID", "Mốc hiệu lực không hợp lệ.")
	}
	if effective.Before(s.now()) {
		return PriceVersion{}, problem.New(422, "SERVICE_PRICE_EFFECTIVE_FROM_PAST", "Không thể tạo giá có hiệu lực trong quá khứ.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PriceVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var deleted bool
	if err := tx.QueryRow(ctx, `SELECT is_deleted FROM services WHERE id=$1 FOR UPDATE`, serviceID).Scan(&deleted); errors.Is(err, pgx.ErrNoRows) {
		return PriceVersion{}, notFound()
	} else if err != nil {
		return PriceVersion{}, err
	}
	if deleted {
		return PriceVersion{}, problem.New(409, "SERVICE_ARCHIVED", "Dịch vụ đã được lưu trữ.")
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO service_price_versions(service_id,effective_from,unit_price_vnd,created_by_user_id) VALUES($1,$2,$3,$4) RETURNING id`, serviceID, effective, amount, actor.UserID).Scan(&id); err != nil {
		return PriceVersion{}, mapConstraint(err)
	}
	event := audit.UserEvent(actor.UserID, "SERVICE", "SERVICE_PRICE_VERSION_CREATED", "SERVICE_PRICE_VERSION", id)
	event.Metadata = map[string]any{"service_id": serviceID, "effective_from": effective, "unit_price_vnd": amount}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return PriceVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PriceVersion{}, err
	}
	return s.getPrice(ctx, serviceID, id)
}

func (s *Service) CancelPrice(ctx context.Context, actor auth.Principal, serviceID, priceID int64, reason string) (PriceVersion, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return PriceVersion{}, invalid("Lý do hủy giá là bắt buộc và tối đa 500 ký tự.")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return PriceVersion{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT id FROM services WHERE id=$1 FOR UPDATE`, serviceID); err != nil {
		return PriceVersion{}, err
	}
	var effective time.Time
	var cancelled *time.Time
	if err := tx.QueryRow(ctx, `SELECT effective_from,cancelled_at FROM service_price_versions WHERE id=$1 AND service_id=$2 FOR UPDATE`, priceID, serviceID).Scan(&effective, &cancelled); errors.Is(err, pgx.ErrNoRows) {
		return PriceVersion{}, problem.New(404, "SERVICE_PRICE_VERSION_NOT_FOUND", "Không tìm thấy phiên bản giá.")
	} else if err != nil {
		return PriceVersion{}, err
	}
	var referenced bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM encounter_services WHERE service_price_version_id_at_order=$1)`, priceID).Scan(&referenced); err != nil {
		return PriceVersion{}, err
	}
	if cancelled != nil || !effective.After(s.now()) || referenced {
		return PriceVersion{}, problem.New(409, "SERVICE_PRICE_VERSION_NOT_CANCELLABLE", "Phiên bản giá không còn có thể hủy.")
	}
	if _, err := tx.Exec(ctx, `UPDATE service_price_versions SET cancellation_reason=$2,cancelled_by_user_id=$3,cancelled_at=now() WHERE id=$1`, priceID, reason, actor.UserID); err != nil {
		return PriceVersion{}, mapConstraint(err)
	}
	event := audit.UserEvent(actor.UserID, "SERVICE", "SERVICE_PRICE_VERSION_CANCELLED", "SERVICE_PRICE_VERSION", priceID)
	event.Metadata = map[string]any{"service_id": serviceID, "reason": reason}
	if err := audit.Insert(ctx, tx, event); err != nil {
		return PriceVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PriceVersion{}, err
	}
	return s.getPrice(ctx, serviceID, priceID)
}

func (s *Service) Get(ctx context.Context, actor auth.Principal, id int64) (CatalogService, error) {
	manage, err := s.authz.Allowed(ctx, actor, "service.manage")
	if err != nil {
		return CatalogService{}, err
	}
	item, err := s.getRaw(ctx, id, manage.Allowed)
	if err != nil {
		return CatalogService{}, err
	}
	return projectCatalog(item, actor, manage.Allowed), nil
}

func (s *Service) List(ctx context.Context, actor auth.Principal, filter ListFilter) ([]CatalogService, error) {
	return s.list(ctx, actor, filter, false)
}

func (s *Service) ListPage(ctx context.Context, actor auth.Principal, filter ListFilter) (pagination.Page[CatalogService], error) {
	filter.Page, filter.PageSize = pagination.Normalize(filter.Page, filter.PageSize)
	items, err := s.list(ctx, actor, filter, true)
	return pagination.From(items, filter.Page, filter.PageSize), err
}

func (s *Service) list(ctx context.Context, actor auth.Principal, filter ListFilter, extra bool) ([]CatalogService, error) {
	fetch := filter.PageSize
	if extra {
		fetch++
	}
	manage, err := s.authz.Allowed(ctx, actor, "service.manage")
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, catalogQuery+`
		WHERE ($1 OR (s.status='ACTIVE' AND NOT s.is_deleted AND (s.service_type<>'CONSULTATION' OR sp.status='ACTIVE' AND NOT sp.is_deleted) AND cp.id IS NOT NULL))
		  AND (NOT $1 OR ($2='' OR s.status::text=$2) AND ($3='' OR s.service_type::text=$3) AND ($4=0 OR s.specialty_id=$4))
		  AND ($5='' OR s.code=$6 OR s.normalized_name LIKE '%'||$5||'%')
		  AND NOT s.is_deleted
		ORDER BY s.code,s.id LIMIT $7 OFFSET $8`, manage.Allowed,
		strings.ToUpper(strings.TrimSpace(filter.Status)), strings.ToUpper(strings.TrimSpace(filter.ServiceType)), filter.SpecialtyID,
		textnorm.Search(filter.Query), textnorm.UpperCode(filter.Query), fetch, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CatalogService, 0)
	for rows.Next() {
		item, err := scanCatalog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	for i := range items {
		items[i] = projectCatalog(items[i], actor, manage.Allowed)
	}
	return items, rows.Err()
}

func (s *Service) ListPrices(ctx context.Context, serviceID int64) ([]PriceVersion, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM services WHERE id=$1)`, serviceID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, notFound()
	}
	rows, err := s.pool.Query(ctx, priceQuery+` WHERE service_id=$1 ORDER BY effective_from DESC,id DESC`, serviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PriceVersion, 0)
	for rows.Next() {
		item, err := scanPrice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const catalogQuery = `
	SELECT s.id,s.code,s.name,s.service_type::text,s.specialty_id,s.unit_label,s.description,s.status::text,s.version,s.is_deleted,s.created_at,s.updated_at,
	       cp.id,cp.effective_from,cp.unit_price_vnd,cp.currency,cp.created_by_user_id,cp.created_at,
	       (SELECT min(f.effective_from) FROM service_price_versions f WHERE f.service_id=s.id AND f.cancelled_at IS NULL AND f.effective_from>now())
	FROM services s
	LEFT JOIN specialties sp ON sp.id=s.specialty_id
	LEFT JOIN LATERAL (
		SELECT p.* FROM service_price_versions p
		WHERE p.service_id=s.id AND p.cancelled_at IS NULL AND p.effective_from<=now()
		ORDER BY p.effective_from DESC,p.id DESC LIMIT 1
	) cp ON true`

func (s *Service) getRaw(ctx context.Context, id int64, includeInactive bool) (CatalogService, error) {
	item, err := scanCatalog(s.pool.QueryRow(ctx, catalogQuery+`
		WHERE s.id=$1 AND ($2 OR (s.status='ACTIVE' AND NOT s.is_deleted
		AND (s.service_type<>'CONSULTATION' OR sp.status='ACTIVE' AND NOT sp.is_deleted) AND cp.id IS NOT NULL))`, id, includeInactive))
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogService{}, notFound()
	}
	return item, err
}

type scanner interface{ Scan(...any) error }

func scanCatalog(row scanner) (CatalogService, error) {
	var item CatalogService
	var priceID, priceAmount, priceActor *int64
	var priceEffective, priceCreated *time.Time
	var currency *string
	err := row.Scan(&item.ID, &item.Code, &item.Name, &item.ServiceType, &item.SpecialtyID, &item.UnitLabel, &item.Description,
		&item.Status, &item.Version, &item.IsDeleted, &item.CreatedAt, &item.UpdatedAt,
		&priceID, &priceEffective, &priceAmount, &currency, &priceActor, &priceCreated, &item.NextPriceAt)
	if err != nil {
		return CatalogService{}, err
	}
	if priceID != nil {
		item.CurrentPrice = &CurrentPrice{EffectiveFrom: *priceEffective, UnitPriceVND: *priceAmount, Currency: *currency}
	}
	return item, nil
}

func projectCatalog(item CatalogService, actor auth.Principal, manage bool) CatalogService {
	if !manage {
		item.NextPriceAt = nil
	}
	if actor.AccountType == auth.AccountTypePatient {
		item.Code = ""
		item.Description = nil
	}
	return item
}

const priceQuery = `SELECT id,service_id,effective_from,unit_price_vnd,currency,created_by_user_id,created_at,cancellation_reason,cancelled_by_user_id,cancelled_at,
	CASE WHEN cancelled_at IS NOT NULL THEN 'CANCELLED' WHEN effective_from>now() THEN 'SCHEDULED' ELSE 'EFFECTIVE' END FROM service_price_versions`

func scanPrice(row scanner) (PriceVersion, error) {
	var item PriceVersion
	err := row.Scan(&item.ID, &item.ServiceID, &item.EffectiveFrom, &item.UnitPriceVND, &item.Currency, &item.CreatedByUserID,
		&item.CreatedAt, &item.CancellationReason, &item.CancelledByUserID, &item.CancelledAt, &item.Phase)
	return item, err
}

func (s *Service) getPrice(ctx context.Context, serviceID, id int64) (PriceVersion, error) {
	item, err := scanPrice(s.pool.QueryRow(ctx, priceQuery+` WHERE id=$1 AND service_id=$2`, id, serviceID))
	if errors.Is(err, pgx.ErrNoRows) {
		return PriceVersion{}, problem.New(404, "SERVICE_PRICE_VERSION_NOT_FOUND", "Không tìm thấy phiên bản giá.")
	}
	return item, err
}

func invalid(message string) error { return problem.New(422, "SERVICE_VALIDATION_FAILED", message) }
func notFound() error {
	return problem.New(404, "SERVICE_NOT_FOUND", "Không tìm thấy dịch vụ.")
}
func concurrent() error {
	return problem.New(409, "SERVICE_CONCURRENT_UPDATE", "Dữ liệu dịch vụ đã thay đổi; hãy tải lại.")
}
func mapConstraint(err error) error {
	switch pgutil.Constraint(err) {
	case "uq_services_code":
		return problem.New(409, "SERVICE_CODE_EXISTS", "Mã dịch vụ đã tồn tại.")
	case "uq_services_normalized_name":
		return problem.New(409, "SERVICE_NAME_EXISTS", "Tên dịch vụ đã tồn tại.")
	case "uq_services_active_consultation_specialty":
		return problem.New(409, "SERVICE_CONSULTATION_ALREADY_EXISTS", "Chuyên khoa đã có phí khám hoạt động.")
	case "uq_service_price_effective_from":
		return problem.New(409, "SERVICE_PRICE_VERSION_EXISTS", "Đã có phiên bản giá tại mốc hiệu lực này.")
	default:
		return fmt.Errorf("service catalog persistence: %w", err)
	}
}

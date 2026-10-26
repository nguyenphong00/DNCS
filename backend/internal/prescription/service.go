package prescription

import (
	"context"
	"dncs/backend/internal/audit"
	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/platform/pagination"
	"dncs/backend/internal/platform/pgutil"
	"dncs/backend/internal/platform/problem"
	"dncs/backend/internal/platform/textnorm"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
	"time"
)

type Service struct {
	pool  *pgxpool.Pool
	authz *authorization.Store
}

func NewService(p *pgxpool.Pool, a *authorization.Store) *Service { return &Service{p, a} }

type Series struct {
	ID                          int64          `json:"id"`
	PrescriptionCode            string         `json:"prescription_code"`
	EncounterID                 int64          `json:"encounter_id"`
	EncounterCode               string         `json:"encounter_code"`
	PatientCode                 string         `json:"patient_code"`
	PatientName                 string         `json:"patient_name"`
	Status                      string         `json:"status"`
	CurrentIssuedPrescriptionID *int64         `json:"current_issued_prescription_id,omitempty"`
	Version                     int64          `json:"version"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
	Versions                    []Prescription `json:"versions,omitempty"`
}
type Prescription struct {
	ID                   int64      `json:"id"`
	SeriesID             int64      `json:"series_id"`
	VersionNumber        int        `json:"version_number"`
	VersionCode          string     `json:"version_code"`
	SourcePrescriptionID *int64     `json:"source_prescription_id,omitempty"`
	Status               string     `json:"status"`
	OverallNote          *string    `json:"overall_note,omitempty"`
	CreatedByUserID      int64      `json:"created_by_user_id"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	IssuedAt             *time.Time `json:"issued_at,omitempty"`
	SupersededAt         *time.Time `json:"superseded_at,omitempty"`
	DiscardedAt          *time.Time `json:"discarded_at,omitempty"`
	DiscardReason        *string    `json:"discard_reason,omitempty"`
	Version              int64      `json:"version"`
	Items                []Item     `json:"items"`
}
type Item struct {
	ID               int64     `json:"id"`
	PrescriptionID   int64     `json:"prescription_id"`
	MedicineID       int64     `json:"medicine_id"`
	MedicineCode     string    `json:"medicine_code_at_prescription"`
	MedicineName     string    `json:"medicine_name_at_prescription"`
	DosageForm       string    `json:"dosage_form_at_prescription"`
	Strength         string    `json:"strength_at_prescription"`
	DispenseUnit     string    `json:"dispense_unit_at_prescription"`
	Quantity         int16     `json:"quantity"`
	UsageInstruction string    `json:"usage_instruction"`
	Notes            *string   `json:"notes,omitempty"`
	SortOrder        int16     `json:"sort_order"`
	IsRemoved        bool      `json:"is_removed"`
	RemovalReason    *string   `json:"removal_reason,omitempty"`
	Version          int64     `json:"version"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func perr(status int, code, msg string) error { return problem.New(status, code, msg) }
func (s *Service) doctorID(ctx context.Context, uid int64) (int64, error) {
	var id int64
	e := s.pool.QueryRow(ctx, `SELECT d.id FROM doctor_profiles d JOIN staff_profiles sp ON sp.id=d.staff_profile_id JOIN users u ON u.id=sp.user_id WHERE sp.user_id=$1 AND d.status='ACTIVE' AND NOT d.is_deleted AND sp.employment_status='ACTIVE' AND NOT sp.is_deleted AND u.status='ACTIVE' AND NOT u.is_deleted`, uid).Scan(&id)
	return id, e
}

type ownership struct {
	AppointmentID, EncounterID, DoctorID, SeriesID, PrescriptionID int64
	EncounterStatus, PrescriptionStatus                            string
	FinalizedAt                                                    *time.Time
}

func (s *Service) ownPrescription(ctx context.Context, id int64) (ownership, error) {
	var o ownership
	e := s.pool.QueryRow(ctx, `SELECT e.appointment_id,e.id,e.doctor_profile_id,ps.id,p.id,e.status::text,p.status::text,e.finalized_at FROM prescriptions p JOIN prescription_series ps ON ps.id=p.series_id JOIN medical_encounters e ON e.id=ps.encounter_id WHERE p.id=$1`, id).Scan(&o.AppointmentID, &o.EncounterID, &o.DoctorID, &o.SeriesID, &o.PrescriptionID, &o.EncounterStatus, &o.PrescriptionStatus, &o.FinalizedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return o, perr(404, "PRESCRIPTION_NOT_FOUND", "Không tìm thấy đơn thuốc.")
	}
	return o, e
}
func lockScope(ctx context.Context, tx pgx.Tx, o ownership, doctor int64, requireEncounterDraft bool) error {
	if _, e := tx.Exec(ctx, `SELECT id FROM appointments WHERE id=$1 FOR UPDATE`, o.AppointmentID); e != nil {
		return e
	}
	var status string
	var did int64
	var finalized *time.Time
	if e := tx.QueryRow(ctx, `SELECT status::text,doctor_profile_id,finalized_at FROM medical_encounters WHERE id=$1 FOR UPDATE`, o.EncounterID).Scan(&status, &did, &finalized); e != nil {
		return e
	}
	if did != doctor {
		return perr(403, "PRESCRIPTION_ASSIGNED_DOCTOR_MISMATCH", "Không phải bác sĩ phụ trách.")
	}
	if requireEncounterDraft && status != "DRAFT" {
		return perr(409, "PRESCRIPTION_ENCOUNTER_NOT_DRAFT", "Encounter không còn DRAFT.")
	}
	return nil
}
func (s *Service) CreateSeries(ctx context.Context, a auth.Principal, encounterID int64, note string) (Series, error) {
	doctor, e := s.doctorID(ctx, a.UserID)
	if e != nil {
		return Series{}, perr(403, "PRESCRIPTION_ASSIGNED_DOCTOR_MISMATCH", "Không phải bác sĩ phụ trách.")
	}
	var o ownership
	o.EncounterID = encounterID
	o.DoctorID = doctor
	e = s.pool.QueryRow(ctx, `SELECT appointment_id FROM medical_encounters WHERE id=$1`, encounterID).Scan(&o.AppointmentID)
	if errors.Is(e, pgx.ErrNoRows) {
		return Series{}, perr(404, "ENCOUNTER_NOT_FOUND", "Không tìm thấy encounter.")
	}
	if e != nil {
		return Series{}, e
	}
	n, er := textnorm.Optional(note, 1000)
	if er != nil {
		return Series{}, perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Ghi chú quá dài.")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Series{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockScope(ctx, tx, o, doctor, true); e != nil {
		return Series{}, e
	}
	var sid int64
	e = tx.QueryRow(ctx, `INSERT INTO prescription_series(encounter_id,created_by_user_id)VALUES($1,$2)RETURNING id`, encounterID, a.UserID).Scan(&sid)
	if e != nil {
		return Series{}, e
	}
	var code string
	if e = tx.QueryRow(ctx, `SELECT prescription_code FROM prescription_series WHERE id=$1`, sid).Scan(&code); e != nil {
		return Series{}, e
	}
	var pid int64
	e = tx.QueryRow(ctx, `INSERT INTO prescriptions(series_id,version_number,version_code,overall_note,created_by_user_id)VALUES($1,1,$2||'-V1',$3,$4)RETURNING id`, sid, code, n, a.UserID).Scan(&pid)
	if e != nil {
		return Series{}, e
	}
	for _, ac := range []string{"PRESCRIPTION_SERIES_CREATED", "PRESCRIPTION_DRAFT_CREATED"} {
		ev := audit.UserEvent(a.UserID, "PRESCRIPTION", ac, "PRESCRIPTION_SERIES", sid)
		if e = audit.Insert(ctx, tx, ev); e != nil {
			return Series{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Series{}, e
	}
	return s.GetSeries(ctx, a, sid)
}
func (s *Service) Update(ctx context.Context, a auth.Principal, id, expected int64, note string) (Prescription, error) {
	return s.mutateDraft(ctx, a, id, func(ctx context.Context, tx pgx.Tx, o ownership) error {
		n, e := textnorm.Optional(note, 1000)
		if e != nil {
			return perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Ghi chú quá dài.")
		}
		tag, e := tx.Exec(ctx, `UPDATE prescriptions SET overall_note=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND status='DRAFT' AND version=$3`, id, n, expected)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return perr(409, "PRESCRIPTION_CONCURRENT_UPDATE", "Draft đã thay đổi.")
		}
		ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_DRAFT_UPDATED", "PRESCRIPTION", id)
		return audit.Insert(ctx, tx, ev)
	})
}
func (s *Service) mutateDraft(ctx context.Context, a auth.Principal, id int64, fn func(context.Context, pgx.Tx, ownership) error) (Prescription, error) {
	doctor, e := s.doctorID(ctx, a.UserID)
	if e != nil {
		return Prescription{}, perr(403, "PRESCRIPTION_ASSIGNED_DOCTOR_MISMATCH", "Không phải bác sĩ phụ trách.")
	}
	o, e := s.ownPrescription(ctx, id)
	if e != nil {
		return Prescription{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Prescription{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockScope(ctx, tx, o, doctor, false); e != nil {
		return Prescription{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT id FROM prescription_series WHERE id=$1 FOR UPDATE`, o.SeriesID); e != nil {
		return Prescription{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT id FROM prescriptions WHERE id=$1 FOR UPDATE`, id); e != nil {
		return Prescription{}, e
	}
	if e = fn(ctx, tx, o); e != nil {
		return Prescription{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Prescription{}, mapErr(e)
	}
	return s.Get(ctx, a, id)
}
func validItem(q int16, instruction string) (string, error) {
	instruction = strings.TrimSpace(instruction)
	if q < 1 || q > 999 {
		return "", perr(422, "PRESCRIPTION_QUANTITY_INVALID", "Số lượng hoặc hướng dẫn không hợp lệ.")
	}
	if instruction == "" || len([]rune(instruction)) > 1000 {
		return "", perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Hướng dẫn phải có từ 1 đến 1000 ký tự.")
	}
	return instruction, nil
}
func (s *Service) AddItem(ctx context.Context, a auth.Principal, id, medicineID int64, quantity int16, instruction, notes string, sortOrder int16) (Prescription, error) {
	instruction, e := validItem(quantity, instruction)
	if e != nil {
		return Prescription{}, e
	}
	doctor, e := s.doctorID(ctx, a.UserID)
	if e != nil {
		return Prescription{}, perr(403, "PRESCRIPTION_ASSIGNED_DOCTOR_MISMATCH", "Không phải bác sĩ phụ trách.")
	}
	o, e := s.ownPrescription(ctx, id)
	if e != nil {
		return Prescription{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Prescription{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockScope(ctx, tx, o, doctor, false); e != nil {
		return Prescription{}, e
	}
	var code, name, form, strength, unit, status string
	var del bool
	e = tx.QueryRow(ctx, `SELECT code,name,dosage_form,strength,dispense_unit,status::text,is_deleted FROM medicines WHERE id=$1 FOR SHARE`, medicineID).Scan(&code, &name, &form, &strength, &unit, &status, &del)
	if errors.Is(e, pgx.ErrNoRows) || status != "ACTIVE" || del {
		return Prescription{}, perr(409, "PRESCRIPTION_MEDICINE_NOT_ELIGIBLE", "Thuốc không đủ điều kiện kê.")
	}
	if e != nil {
		return Prescription{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT id FROM prescription_series WHERE id=$1 FOR UPDATE`, o.SeriesID); e != nil {
		return Prescription{}, e
	}
	var st string
	if e = tx.QueryRow(ctx, `SELECT status::text FROM prescriptions WHERE id=$1 FOR UPDATE`, id).Scan(&st); e != nil {
		return Prescription{}, e
	}
	if st != "DRAFT" {
		return Prescription{}, perr(409, "PRESCRIPTION_DRAFT_IMMUTABLE", "Đơn không còn DRAFT.")
	}
	if sortOrder < 1 {
		e = tx.QueryRow(ctx, `SELECT COALESCE(max(sort_order),0)+1 FROM prescription_items WHERE prescription_id=$1 AND NOT is_removed`, id).Scan(&sortOrder)
		if e != nil {
			return Prescription{}, e
		}
	}
	n, noteErr := textnorm.Optional(notes, 500)
	if noteErr != nil {
		return Prescription{}, perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Ghi chú tối đa 500 ký tự.")
	}
	var iid int64
	e = tx.QueryRow(ctx, `INSERT INTO prescription_items(prescription_id,medicine_id,medicine_code_at_prescription,medicine_name_at_prescription,dosage_form_at_prescription,strength_at_prescription,dispense_unit_at_prescription,quantity,usage_instruction,notes,sort_order)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)RETURNING id`, id, medicineID, code, name, form, strength, unit, quantity, instruction, n, sortOrder).Scan(&iid)
	if e != nil {
		return Prescription{}, mapErr(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE prescriptions SET updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id); e != nil {
		return Prescription{}, e
	}
	ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_ITEM_ADDED", "PRESCRIPTION_ITEM", iid)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Prescription{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Prescription{}, mapErr(e)
	}
	return s.Get(ctx, a, id)
}
func (s *Service) UpdateItem(ctx context.Context, a auth.Principal, pid, iid int64, expected int64, quantity int16, instruction, notes string, sortOrder int16) (Prescription, error) {
	n, noteErr := textnorm.Optional(notes, 500)
	if noteErr != nil {
		return Prescription{}, perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Ghi chú tối đa 500 ký tự.")
	}
	instruction, e := validItem(quantity, instruction)
	if e != nil {
		return Prescription{}, e
	}
	if sortOrder < 1 {
		return Prescription{}, perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Thứ tự item không hợp lệ.")
	}
	return s.mutateDraft(ctx, a, pid, func(ctx context.Context, tx pgx.Tx, o ownership) error {
		tag, e := tx.Exec(ctx, `UPDATE prescription_items SET quantity=$3,usage_instruction=$4,notes=$5,sort_order=$6,updated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND prescription_id=$2 AND NOT is_removed AND version=$7`, iid, pid, quantity, instruction, n, sortOrder, expected)
		if e != nil {
			return mapErr(e)
		}
		if tag.RowsAffected() != 1 {
			return perr(409, "PRESCRIPTION_CONCURRENT_UPDATE", "Item đã thay đổi.")
		}
		_, e = tx.Exec(ctx, `UPDATE prescriptions SET updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, pid)
		if e != nil {
			return e
		}
		ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_ITEM_UPDATED", "PRESCRIPTION_ITEM", iid)
		return audit.Insert(ctx, tx, ev)
	})
}
func (s *Service) RemoveItem(ctx context.Context, a auth.Principal, pid, iid, expected int64, reason string) (Prescription, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Prescription{}, perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Lý do không hợp lệ.")
	}
	return s.mutateDraft(ctx, a, pid, func(ctx context.Context, tx pgx.Tx, o ownership) error {
		tag, e := tx.Exec(ctx, `UPDATE prescription_items SET is_removed=true,removed_by_user_id=$3,removed_at=now(),removal_reason=$4,updated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND prescription_id=$2 AND NOT is_removed AND version=$5`, iid, pid, a.UserID, reason, expected)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return perr(409, "PRESCRIPTION_CONCURRENT_UPDATE", "Item đã thay đổi.")
		}
		_, e = tx.Exec(ctx, `UPDATE prescriptions SET updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, pid)
		if e != nil {
			return e
		}
		ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_ITEM_REMOVED", "PRESCRIPTION_ITEM", iid)
		return audit.Insert(ctx, tx, ev)
	})
}
func (s *Service) Issue(ctx context.Context, a auth.Principal, id, expected int64) (Prescription, error) {
	doctor, e := s.doctorID(ctx, a.UserID)
	if e != nil {
		return Prescription{}, perr(403, "PRESCRIPTION_ASSIGNED_DOCTOR_MISMATCH", "Không phải bác sĩ phụ trách.")
	}
	o, e := s.ownPrescription(ctx, id)
	if e != nil {
		return Prescription{}, e
	}
	if o.PrescriptionStatus == "ISSUED" {
		return s.Get(ctx, a, id)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Prescription{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockScope(ctx, tx, o, doctor, false); e != nil {
		return Prescription{}, e
	}
	rows, e := tx.Query(ctx, `SELECT DISTINCT medicine_id FROM prescription_items WHERE prescription_id=$1 AND NOT is_removed ORDER BY medicine_id`, id)
	if e != nil {
		return Prescription{}, e
	}
	var mids []int64
	for rows.Next() {
		var x int64
		rows.Scan(&x)
		mids = append(mids, x)
	}
	rows.Close()
	if len(mids) == 0 {
		return Prescription{}, perr(422, "PRESCRIPTION_EMPTY_CANNOT_ISSUE", "Đơn không có thuốc.")
	}
	for _, mid := range mids {
		var ok bool
		if e = tx.QueryRow(ctx, `SELECT status='ACTIVE' AND NOT is_deleted FROM medicines WHERE id=$1 FOR SHARE`, mid).Scan(&ok); e != nil || !ok {
			return Prescription{}, perr(409, "PRESCRIPTION_MEDICINE_NOT_ELIGIBLE", "Thuốc không còn đủ điều kiện.")
		}
	}
	var current *int64
	var seriesStatus string
	if e = tx.QueryRow(ctx, `SELECT current_issued_prescription_id,status::text FROM prescription_series WHERE id=$1 FOR UPDATE`, o.SeriesID).Scan(&current, &seriesStatus); e != nil {
		return Prescription{}, e
	}
	var st string
	var ver int64
	var created time.Time
	if e = tx.QueryRow(ctx, `SELECT status::text,version,created_at FROM prescriptions WHERE id=$1 FOR UPDATE`, id).Scan(&st, &ver, &created); e != nil {
		return Prescription{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT id FROM prescription_items WHERE prescription_id=$1 AND NOT is_removed ORDER BY id FOR UPDATE`, id); e != nil {
		return Prescription{}, e
	}
	if st != "DRAFT" || ver != expected {
		return Prescription{}, perr(409, "PRESCRIPTION_CONCURRENT_UPDATE", "Draft đã thay đổi.")
	}
	if o.FinalizedAt != nil && created.After(*o.FinalizedAt) {
		return Prescription{}, perr(409, "PRESCRIPTION_POST_FINALIZE_DRAFT_NOT_ALLOWED", "Draft được tạo sau finalize.")
	}
	if current != nil {
		if _, e = tx.Exec(ctx, `UPDATE prescriptions SET status='SUPERSEDED',superseded_at=now(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND status='ISSUED'`, *current); e != nil {
			return Prescription{}, e
		}
	}
	if _, e = tx.Exec(ctx, `UPDATE prescriptions SET status='ISSUED',issued_by_user_id=$2,issued_at=now(),updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, id, a.UserID); e != nil {
		return Prescription{}, e
	}
	if _, e = tx.Exec(ctx, `UPDATE prescription_series SET status='ISSUED',current_issued_prescription_id=$2,updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, o.SeriesID, id); e != nil {
		return Prescription{}, e
	}
	ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_ISSUED", "PRESCRIPTION", id)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Prescription{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Prescription{}, mapErr(e)
	}
	return s.Get(ctx, a, id)
}
func (s *Service) Replacement(ctx context.Context, a auth.Principal, sid int64) (Prescription, error) {
	doctor, e := s.doctorID(ctx, a.UserID)
	if e != nil {
		return Prescription{}, perr(403, "PRESCRIPTION_ASSIGNED_DOCTOR_MISMATCH", "Không phải bác sĩ phụ trách.")
	}
	var o ownership
	var cur int64
	var code string
	e = s.pool.QueryRow(ctx, `SELECT e.appointment_id,e.id,e.doctor_profile_id,ps.current_issued_prescription_id,ps.prescription_code FROM prescription_series ps JOIN medical_encounters e ON e.id=ps.encounter_id WHERE ps.id=$1`, sid).Scan(&o.AppointmentID, &o.EncounterID, &o.DoctorID, &cur, &code)
	if errors.Is(e, pgx.ErrNoRows) {
		return Prescription{}, perr(404, "PRESCRIPTION_SERIES_NOT_FOUND", "Không tìm thấy series.")
	}
	if e != nil {
		return Prescription{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Prescription{}, e
	}
	defer tx.Rollback(ctx)
	if e = lockScope(ctx, tx, o, doctor, true); e != nil {
		return Prescription{}, e
	}
	if e = tx.QueryRow(ctx, `SELECT current_issued_prescription_id FROM prescription_series WHERE id=$1 AND status='ISSUED' FOR UPDATE`, sid).Scan(&cur); e != nil {
		return Prescription{}, e
	}
	var next int
	if e = tx.QueryRow(ctx, `SELECT COALESCE(max(version_number),0)+1 FROM prescriptions WHERE series_id=$1`, sid).Scan(&next); e != nil {
		return Prescription{}, e
	}
	var id int64
	e = tx.QueryRow(ctx, `INSERT INTO prescriptions(series_id,version_number,version_code,source_prescription_id,overall_note,created_by_user_id)SELECT $1,$2,$3||'-V'||$2::integer::text,$4,overall_note,$5 FROM prescriptions WHERE id=$4 AND status='ISSUED' RETURNING id`, sid, next, code, cur, a.UserID).Scan(&id)
	if e != nil {
		return Prescription{}, mapErr(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO prescription_items(prescription_id,medicine_id,medicine_code_at_prescription,medicine_name_at_prescription,dosage_form_at_prescription,strength_at_prescription,dispense_unit_at_prescription,quantity,usage_instruction,notes,sort_order)SELECT $1,medicine_id,medicine_code_at_prescription,medicine_name_at_prescription,dosage_form_at_prescription,strength_at_prescription,dispense_unit_at_prescription,quantity,usage_instruction,notes,sort_order FROM prescription_items WHERE prescription_id=$2 AND NOT is_removed ORDER BY sort_order,id`, id, cur)
	if e != nil {
		return Prescription{}, e
	}
	ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_REPLACEMENT_DRAFT_CREATED", "PRESCRIPTION", id)
	if e = audit.Insert(ctx, tx, ev); e != nil {
		return Prescription{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Prescription{}, mapErr(e)
	}
	return s.Get(ctx, a, id)
}
func (s *Service) Discard(ctx context.Context, a auth.Principal, id, expected int64, reason string) (Prescription, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len([]rune(reason)) > 500 {
		return Prescription{}, perr(422, "PRESCRIPTION_INSTRUCTION_INVALID", "Lý do không hợp lệ.")
	}
	return s.mutateDraft(ctx, a, id, func(ctx context.Context, tx pgx.Tx, o ownership) error {
		tag, e := tx.Exec(ctx, `UPDATE prescriptions SET status='DISCARDED',discarded_by_user_id=$2,discarded_at=now(),discard_reason=$3,updated_at=clock_timestamp(),version=version+1 WHERE id=$1 AND status='DRAFT' AND version=$4`, id, a.UserID, reason, expected)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return perr(409, "PRESCRIPTION_CONCURRENT_UPDATE", "Draft đã đổi.")
		}
		var cur *int64
		if e = tx.QueryRow(ctx, `SELECT current_issued_prescription_id FROM prescription_series WHERE id=$1`, o.SeriesID).Scan(&cur); e != nil {
			return e
		}
		if cur == nil {
			_, e = tx.Exec(ctx, `UPDATE prescription_series SET status='DISCARDED',updated_at=clock_timestamp(),version=version+1 WHERE id=$1`, o.SeriesID)
		}
		if e != nil {
			return e
		}
		ev := audit.UserEvent(a.UserID, "PRESCRIPTION", "PRESCRIPTION_DRAFT_DISCARDED", "PRESCRIPTION", id)
		return audit.Insert(ctx, tx, ev)
	})
}

func (s *Service) canView(ctx context.Context, a auth.Principal, encounterID, doctorID int64, status string) (bool, error) {
	d, e := s.authz.Allowed(ctx, a, "prescription.view.all")
	if e != nil {
		return false, e
	}
	if d.Allowed {
		return true, nil
	}
	if a.AccountType == auth.AccountTypePatient {
		var ok bool
		e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM medical_encounters e JOIN patients p ON p.id=e.patient_id WHERE e.id=$1 AND p.user_id=$2)`, encounterID, a.UserID).Scan(&ok)
		return ok && (status == "ISSUED" || status == "SUPERSEDED"), e
	}
	did, e := s.doctorID(ctx, a.UserID)
	return e == nil && did == doctorID, nil
}

const rxq = `SELECT p.id,p.series_id,p.version_number,p.version_code,p.source_prescription_id,p.status::text,p.overall_note,p.created_by_user_id,p.created_at,p.updated_at,p.issued_at,p.superseded_at,p.discarded_at,p.discard_reason,p.version,ps.encounter_id,e.doctor_profile_id FROM prescriptions p JOIN prescription_series ps ON ps.id=p.series_id JOIN medical_encounters e ON e.id=ps.encounter_id`

func scanRx(row interface{ Scan(...any) error }) (Prescription, int64, int64, error) {
	var p Prescription
	var eid, did int64
	e := row.Scan(&p.ID, &p.SeriesID, &p.VersionNumber, &p.VersionCode, &p.SourcePrescriptionID, &p.Status, &p.OverallNote, &p.CreatedByUserID, &p.CreatedAt, &p.UpdatedAt, &p.IssuedAt, &p.SupersededAt, &p.DiscardedAt, &p.DiscardReason, &p.Version, &eid, &did)
	return p, eid, did, e
}
func (s *Service) Get(ctx context.Context, a auth.Principal, id int64) (Prescription, error) {
	p, eid, did, e := scanRx(s.pool.QueryRow(ctx, rxq+` WHERE p.id=$1`, id))
	if errors.Is(e, pgx.ErrNoRows) {
		return p, perr(404, "PRESCRIPTION_NOT_FOUND", "Không tìm thấy đơn thuốc.")
	}
	if e != nil {
		return p, e
	}
	ok, e := s.canView(ctx, a, eid, did, p.Status)
	if e != nil {
		return p, e
	}
	if !ok {
		return p, perr(404, "PRESCRIPTION_NOT_FOUND", "Không tìm thấy đơn thuốc.")
	}
	rows, e := s.pool.Query(ctx, `SELECT id,prescription_id,medicine_id,medicine_code_at_prescription,medicine_name_at_prescription,dosage_form_at_prescription,strength_at_prescription,dispense_unit_at_prescription,quantity,usage_instruction,notes,sort_order,is_removed,removal_reason,version,created_at,updated_at FROM prescription_items WHERE prescription_id=$1 ORDER BY is_removed,sort_order,id`, id)
	if e != nil {
		return p, e
	}
	defer rows.Close()
	for rows.Next() {
		var x Item
		if e = rows.Scan(&x.ID, &x.PrescriptionID, &x.MedicineID, &x.MedicineCode, &x.MedicineName, &x.DosageForm, &x.Strength, &x.DispenseUnit, &x.Quantity, &x.UsageInstruction, &x.Notes, &x.SortOrder, &x.IsRemoved, &x.RemovalReason, &x.Version, &x.CreatedAt, &x.UpdatedAt); e != nil {
			return p, e
		}
		if a.AccountType != auth.AccountTypePatient || !x.IsRemoved {
			p.Items = append(p.Items, x)
		}
	}
	return p, rows.Err()
}
func (s *Service) GetSeries(ctx context.Context, a auth.Principal, id int64) (Series, error) {
	var x Series
	var did int64
	e := s.pool.QueryRow(ctx, `SELECT ps.id,ps.prescription_code,ps.encounter_id,ps.status::text,ps.current_issued_prescription_id,ps.version,ps.created_at,ps.updated_at,e.doctor_profile_id,e.encounter_code,e.patient_code_at_start,e.patient_name_at_start FROM prescription_series ps JOIN medical_encounters e ON e.id=ps.encounter_id WHERE ps.id=$1`, id).Scan(&x.ID, &x.PrescriptionCode, &x.EncounterID, &x.Status, &x.CurrentIssuedPrescriptionID, &x.Version, &x.CreatedAt, &x.UpdatedAt, &did, &x.EncounterCode, &x.PatientCode, &x.PatientName)
	if errors.Is(e, pgx.ErrNoRows) {
		return x, perr(404, "PRESCRIPTION_SERIES_NOT_FOUND", "Không tìm thấy series.")
	}
	if e != nil {
		return x, e
	}
	rows, e := s.pool.Query(ctx, `SELECT id FROM prescriptions WHERE series_id=$1 ORDER BY version_number`, id)
	if e != nil {
		return x, e
	}
	ids := []int64{}
	for rows.Next() {
		var pid int64
		if e = rows.Scan(&pid); e != nil {
			rows.Close()
			return x, e
		}
		ids = append(ids, pid)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return x, e
	}
	for _, pid := range ids {
		p, ge := s.Get(ctx, a, pid)
		if ge == nil {
			x.Versions = append(x.Versions, p)
			continue
		}
		if domain, ok := problem.As(ge); ok && domain.HTTPStatus == 404 {
			continue
		}
		return x, ge
	}
	if len(x.Versions) == 0 {
		return Series{}, perr(404, "PRESCRIPTION_SERIES_NOT_FOUND", "Không tìm thấy series.")
	}
	return x, nil
}
func (s *Service) List(ctx context.Context, a auth.Principal, encounterID int64, page, size int) ([]Series, error) {
	result, err := s.ListPage(ctx, a, encounterID, page, size)
	return result.Items, err
}
func (s *Service) ListPage(ctx context.Context, a auth.Principal, encounterID int64, page, size int) (pagination.Page[Series], error) {
	page, size = pagination.Normalize(page, size)
	empty := pagination.From([]Series{}, page, size)
	decision, err := s.authz.Allowed(ctx, a, "prescription.view.all")
	if err != nil {
		return empty, err
	}
	patient := a.AccountType == auth.AccountTypePatient
	var doctor int64
	if !decision.Allowed && !patient {
		doctor, err = s.doctorID(ctx, a.UserID)
		if err != nil {
			return empty, err
		}
	}
	// Apply resource scope before pagination, so another user's rows cannot create gaps.
	rows, err := s.pool.Query(ctx, `SELECT ps.id FROM prescription_series ps
 JOIN medical_encounters e ON e.id=ps.encounter_id JOIN patients p ON p.id=e.patient_id
 WHERE ($1=0 OR ps.encounter_id=$1) AND ($2 OR
 ($3 AND p.user_id=$4 AND EXISTS(SELECT 1 FROM prescriptions rx WHERE rx.series_id=ps.id AND rx.status IN ('ISSUED','SUPERSEDED'))) OR
 (NOT $3 AND e.doctor_profile_id=$5))
 ORDER BY ps.created_at DESC,ps.id DESC LIMIT $6 OFFSET $7`, encounterID, decision.Allowed, patient, a.UserID, doctor, size+1, (page-1)*size)
	if err != nil {
		return empty, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return empty, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, err
	}
	more := len(ids) > size
	if more {
		ids = ids[:size]
	}
	out := []Series{}
	for _, id := range ids {
		x, err := s.GetSeries(ctx, a, id)
		if err != nil {
			return empty, err
		}
		out = append(out, x)
	}
	result := pagination.From(out, page, size)
	result.HasMore = more
	return result, nil
}
func (s *Service) GetBillableVersions(ctx context.Context, encounterID int64) ([]Prescription, error) {
	var status string
	if e := s.pool.QueryRow(ctx, `SELECT status::text FROM medical_encounters WHERE id=$1`, encounterID).Scan(&status); e != nil {
		return nil, e
	}
	if status != "FINALIZED" {
		return nil, perr(409, "PRESCRIPTION_BILLING_NOT_ELIGIBLE", "Encounter chưa finalized.")
	}
	rows, e := s.pool.Query(ctx, `SELECT current_issued_prescription_id FROM prescription_series WHERE encounter_id=$1 AND status='ISSUED' ORDER BY id`, encounterID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Prescription
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		p, _, _, e := scanRx(s.pool.QueryRow(ctx, rxq+` WHERE p.id=$1 AND p.status='ISSUED'`, id))
		if e != nil {
			return nil, e
		}
		items, e := s.pool.Query(ctx, `SELECT id,prescription_id,medicine_id,medicine_code_at_prescription,medicine_name_at_prescription,dosage_form_at_prescription,strength_at_prescription,dispense_unit_at_prescription,quantity,usage_instruction,notes,sort_order,is_removed,removal_reason,version,created_at,updated_at FROM prescription_items WHERE prescription_id=$1 AND NOT is_removed ORDER BY sort_order,id`, id)
		if e != nil {
			return nil, e
		}
		for items.Next() {
			var x Item
			if e = items.Scan(&x.ID, &x.PrescriptionID, &x.MedicineID, &x.MedicineCode, &x.MedicineName, &x.DosageForm, &x.Strength, &x.DispenseUnit, &x.Quantity, &x.UsageInstruction, &x.Notes, &x.SortOrder, &x.IsRemoved, &x.RemovalReason, &x.Version, &x.CreatedAt, &x.UpdatedAt); e != nil {
				items.Close()
				return nil, e
			}
			p.Items = append(p.Items, x)
		}
		items.Close()
		out = append(out, p)
	}
	return out, rows.Err()
}
func mapErr(e error) error {
	switch pgutil.Constraint(e) {
	case "uq_prescriptions_one_draft":
		return perr(409, "PRESCRIPTION_DRAFT_ALREADY_EXISTS", "Series đã có draft.")
	case "uq_prescriptions_one_issued":
		return perr(409, "PRESCRIPTION_INVALID_STATE_TRANSITION", "Series đã có version ISSUED.")
	case "uq_prescription_items_active_medicine":
		return perr(409, "PRESCRIPTION_ITEM_DUPLICATE_MEDICINE", "Medicine đã có trong draft.")
	case "uq_prescription_items_active_sort":
		return perr(409, "PRESCRIPTION_CONCURRENT_UPDATE", "Thứ tự item bị trùng.")
	}
	return fmt.Errorf("prescription persistence: %w", e)
}

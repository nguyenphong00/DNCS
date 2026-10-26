# Constraint & Integrity Design — DNCS MVP

## 1. Nguyên tắc

Mỗi invariant được xếp vào một trong ba lớp:

| Lớp | Dùng khi | Ví dụ |
| --- | --- | --- |
| `DB` | Chỉ phụ thuộc row hiện tại hoặc key/range mà PostgreSQL bảo vệ nguyên tử được | Unique phone, exclusion overlap, check state metadata |
| `TRIGGER` | Cần defense-in-depth cho immutability hoặc kiểm tra parent state trong cùng database | Cấm update audit, cấm sửa price đã effective |
| `SERVICE/TRANSACTION` | Phụ thuộc clock, permission, nhiều aggregate, số lượng child hoặc lock order | Encounter finalized phải có vital sign; stock FEFO; role đang active |

Không dùng trigger để thay thế toàn bộ domain service. Trigger phải nhỏ, deterministic, có test và trả exception/error name ổn định.

## 2. Naming convention

| Object | Prefix | Ví dụ |
| --- | --- | --- |
| Primary key | `pk_` | `pk_users` |
| Foreign key | `fk_` | `fk_users_role` |
| Unique constraint/index | `uq_` | `uq_users_normalized_phone` |
| Check constraint | `ck_` | `ck_users_identifier_by_type` |
| Exclusion constraint | `ex_` | `ex_appointments_doctor_overlap` |
| Non-unique index | `ix_` | `ix_appointments_patient_start` |
| Trigger | `trg_` | `trg_audit_logs_immutable` |
| Trigger function | `fn_` | `fn_reject_mutation` |
| Sequence | `_seq` suffix | `invoice_code_seq` |

Tên constraint là API nội bộ giữa persistence và business error mapper; không tự đổi tên sau khi đã phát hành.

## 3. Foreign key và delete policy

- Mọi FK domain dùng `ON DELETE RESTRICT`/`NO ACTION`.
- Không cấp `DELETE` cho application role trên dữ liệu lịch sử, clinical, inventory hoặc financial.
- Soft delete không làm FK mất hiệu lực và không giải phóng unique key trừ relation có lịch sử nhiều lần như `doctor_specialties`.
- FK actor tới `users` vẫn giữ khi user inactive/deleted.
- `audit_logs.target_*`, `stock_movements.reference_*` và `idempotency_records.resource_*` là polymorphic logical reference, không tạo generic FK.

Các composite key hỗ trợ invariant:

| Parent unique key | Child composite FK | Mục đích |
| --- | --- | --- |
| `doctor_schedules(id, doctor_profile_id, room_id)` | `appointments(doctor_schedule_id, doctor_profile_id, room_id)` | Doctor/room appointment khớp schedule |
| `appointments(id, patient_id, doctor_profile_id, specialty_id)` | `medical_encounters(appointment_id, patient_id, doctor_profile_id, specialty_id)` | Encounter khớp appointment nguồn |
| `service_price_versions(id, service_id)` | `encounter_services(service_price_version_id_at_order, service_id)` | Giá thuộc đúng service |
| `prescriptions(id, series_id)` | `prescription_series(current_issued_prescription_id, id)` | Current issued thuộc đúng series |
| `prescriptions(id, series_id)` | `prescriptions(source_prescription_id, series_id)` | Replacement source cùng series |
| `prescription_items(id, prescription_id)` | `invoice_items(prescription_item_id, prescription_id)` | Medicine invoice source đúng version |
| `encounter_services(id, service_price_version_id_at_order)` | `invoice_items(encounter_service_id, service_price_version_id)` | Service invoice giữ đúng giá order |
| `medical_encounters(id, patient_id)` | `invoices(encounter_id, patient_id)` | Invoice patient khớp encounter |
| `medicine_batches(id, medicine_id)` | Receipt item và stock movement `(medicine_batch_id, medicine_id)` | Batch thuộc đúng medicine |

FK vòng được thêm sau khi cả hai bảng tồn tại. FK current prescription nên `DEFERRABLE INITIALLY DEFERRED` để issue/supersede có thể cập nhật nguyên tử. FK follow-up appointment không cần deferred khi encounter nguồn đã tồn tại trước lịch tái khám.

## 4. Identity và authorization

### 4.1. DB constraints

| Tên | Loại | Biểu thức/ý nghĩa |
| --- | --- | --- |
| `uq_roles_code` | UQ | `roles(code)` trên toàn bảng |
| `ck_roles_code` | CHECK | Upper snake case, không rỗng |
| `uq_permissions_code` | UQ | `permissions(code)` |
| `ck_permissions_code` | CHECK | Lower dot-separated code hợp lệ |
| `uq_users_normalized_username` | UQ | `normalized_username` khi khác null; unique thường đã bỏ qua null |
| `uq_users_normalized_phone` | UQ | `normalized_phone_number` khi khác null |
| `ck_users_identifier_by_type` | CHECK | STAFF có username pair và không có phone pair; PATIENT có phone pair và không có username pair |
| `ck_users_phone_e164` | CHECK | Null hoặc `^\+84[0-9]{9,10}$` theo normalizer baseline |
| `ck_users_login_state` | CHECK | failed count `>=0`, token/version `>=1`; LOCKED có `locked_until` hoặc là administrative lock được service phân biệt |
| `uq_auth_sessions_refresh_hash` | UQ | Không có hai session dùng cùng refresh hash |
| `ck_auth_sessions_period` | CHECK | `expires_at > issued_at`; revoked metadata đồng nhất |
| `pk_role_permissions` | PK | `(role_id, permission_id)` |
| `uq_user_permission_override_active` | partial UQ | `(user_id, permission_id) WHERE is_deleted=false` |
| `ck_user_override_reason` | CHECK | `char_length(trim(reason)) BETWEEN 1 AND 1000` |

### 4.2. Service/transaction guard

- Role gán cho user phải active, chưa deleted và `account_scope = users.account_type`.
- User STAFF/PATIENT phải được tạo cùng đúng profile tương ứng; không được có cả hai.
- Protected role/permission/super admin không bị thay đổi bởi command thông thường.
- Permission inactive không được gán mới.
- `user_role_histories`, `users.role_id`, `users.token_version` và audit phải commit cùng transaction.
- Refresh rotation dùng conditional update/row lock; cùng raw token chỉ một request thắng.

## 5. Profile và clinic master

### 5.1. DB constraints

| Table | Constraint chính |
| --- | --- |
| `staff_profiles` | UQ `user_id`, `staff_code`, normalized phone/email; full name không rỗng; DOB được type bảo vệ |
| `doctor_profiles` | UQ `staff_profile_id`, `doctor_code`; `version>=1` |
| `patients` | UQ `user_id`, `patient_code`, `cccd`, `health_insurance_number`; CCCD `^[0-9]{12}$`; BHYT `^[A-Z0-9]{15}$`; text length theo catalog |
| `specialties` | UQ `code`, `normalized_name`; code `^SPC_[A-Z0-9_]{2,30}$` |
| `rooms` | UQ `code`, `normalized_name`; code `^RM-[A-Z0-9-]{2,20}$` |
| `doctor_specialties` | Partial UQ `(doctor_profile_id,specialty_id) WHERE is_deleted=false`; removal metadata all-null hoặc all-present |

`staff_code`, `doctor_code`, `patient_code` không tái sử dụng và bất biến. Có thể sinh bằng application sau `INSERT ... RETURNING id` trong cùng transaction hoặc bằng database function; không dùng `MAX+1`.

### 5.2. Service/transaction guard

- DOB không ở tương lai theo `Asia/Ho_Chi_Minh`; `CURRENT_DATE` không đặt trong immutable CHECK.
- User/profile account type phải khớp.
- Doctor/staff/user phải cùng active eligibility trước phân công mới.
- Specialty/room inactive/deleted không được dùng cho relation/schedule/booking mới.
- Không deactivate/archive resource còn dependency active theo feature spec.

## 6. Scheduling

### 6.1. Checks

```text
ck_doctor_schedules_time: start_at < end_at
ck_doctor_schedules_duration: slot_duration_minutes IN (15,20,30,45,60)
ck_doctor_schedules_divisible:
  extract(epoch FROM end_at-start_at)::bigint % (slot_duration_minutes*60) = 0
ck_doctor_schedules_cancel_metadata:
  CANCELLED <=> cancelled_at/cancelled_by/reason all present
ck_doctor_schedules_delete_metadata:
  is_deleted=false <=> deleted_at/deleted_by both null
ck_doctor_schedules_active_not_deleted:
  status <> ACTIVE OR is_deleted=false
```

“Cùng ngày địa phương” được service validate với timezone `Asia/Ho_Chi_Minh`; không dùng session timezone hoặc volatile timezone rule trong CHECK.

### 6.2. Exclusion constraints

Yêu cầu extension `btree_gist`:

```sql
CONSTRAINT ex_doctor_schedules_doctor_overlap
EXCLUDE USING gist (
  doctor_profile_id WITH =,
  schedule_range WITH &&
) WHERE (status = 'ACTIVE');

CONSTRAINT ex_doctor_schedules_room_overlap
EXCLUDE USING gist (
  room_id WITH =,
  schedule_range WITH &&
) WHERE (status = 'ACTIVE');
```

Predicate không dựa vào `is_deleted`. SQLSTATE `23P01` được map theo tên constraint.

### 6.3. Transaction guard

- Activate/create ACTIVE lock doctor, room, schedule theo global order, revalidate eligibility rồi để exclusion constraint quyết định cuối.
- Schedule ACTIVE bất biến về doctor/room/range/duration; chỉ notes được sửa trong policy cho phép.
- Cancel chặn nếu còn appointment holding; soft delete chỉ DRAFT/CANCELLED.

## 7. Appointment

### 7.1. Checks và unique

| Tên | Loại | Ý nghĩa |
| --- | --- | --- |
| `uq_appointments_code` | UQ | Business code bất biến |
| `uq_appointments_booking_idempotency` | UQ | `(booked_by_user_id,idempotency_key)` |
| `ck_appointments_time` | CHECK | `start_at < end_at` |
| `ck_appointments_source` | CHECK | FOLLOW_UP iff `follow_up_from_encounter_id` non-null |
| `ck_appointments_window_snapshot` | CHECK | early/late/grace trong range và grace `<= late` |
| `ck_appointments_holding_not_deleted` | CHECK | Holding status yêu cầu `is_deleted=false` |
| `ck_appointments_idempotency_key` | CHECK | 16..128 ASCII, hash 64 hex |
| `uq_appointments_follow_up_holding` | partial UQ | `follow_up_from_encounter_id` với holding statuses |
| `ck_appointment_history_actor` | CHECK | USER cần actor ID; SYSTEM actor ID null |

### 7.2. Exclusion constraints

Ba constraint dùng cùng predicate holding:

```text
status IN (PENDING_APPROVAL, CONFIRMED, CHECKED_IN, IN_PROGRESS)
```

| Tên | Keys |
| --- | --- |
| `ex_appointments_doctor_overlap` | `doctor_profile_id =`, `appointment_range &&` |
| `ex_appointments_patient_overlap` | `patient_id =`, `appointment_range &&` |
| `ex_appointments_room_overlap` | `room_id =`, `appointment_range &&` |

### 7.3. Service/transaction guard

- Appointment nằm hoàn toàn trong schedule, đúng duration và aligned slot boundary.
- Specialty relation của doctor đang hiệu lực tại booking.
- Min lead/max advance/max holding lấy setting server-side.
- Status transition, actor scope và time guard hợp lệ; status current và history insert cùng transaction.
- Snapshot code/name lấy từ row đã lock, không lấy client.
- Retry cùng key/hash trả resource cũ; key khác hash trả conflict.

## 8. Service và Medical Encounter

### 8.1. Service DB constraints

| Tên | Ý nghĩa |
| --- | --- |
| `uq_services_code`, `uq_services_normalized_name` | Không tái sử dụng catalog identity |
| `ck_services_type_specialty` | CONSULTATION có specialty; type khác không có |
| `uq_services_active_consultation_specialty` | Partial UQ specialty khi CONSULTATION + ACTIVE + not deleted |
| `uq_service_price_effective_from` | `(service_id,effective_from)` |
| `ck_service_price_value` | price `>=0`, currency `VND`; cancellation metadata all-null/all-present |
| `uq_encounter_services_active` | `(encounter_id,service_id) WHERE status=ACTIVE` |
| `ck_encounter_services_source` | SYSTEM_CONSULTATION quantity 1/actor null; DOCTOR_ORDER actor present |
| `ck_encounter_services_removal` | ACTIVE removal fields null; REMOVED fields present |

Price `effective_from >= server_now` lúc create và chỉ SCHEDULED được cancel là transaction/trigger guard, không phải CHECK.

### 8.2. Encounter DB constraints

| Tên | Ý nghĩa |
| --- | --- |
| `uq_medical_encounters_appointment` | Một appointment tối đa một encounter |
| `uq_medical_encounters_code` | Encounter code bất biến |
| `ck_medical_encounters_finalize` | DRAFT có finalize fields null; FINALIZED có fields present và `started_at<=finalized_at` |
| `ck_medical_encounters_not_deleted` | `is_deleted=false` |
| `ck_medical_encounters_text_length` | Giới hạn 1k/4k/4k/4k/8k/2k |
| `ck_vital_signs_values` | Non-negative cơ bản; SpO2 0..100; pain 0..10; systolic >= diastolic nếu đủ cặp |
| `uq_encounter_amendments_sequence` | `(encounter_id,sequence_number)` |
| `ck_encounter_amendment_text` | reason 1..1,000; content 1..8,000 |

### 8.3. Service/transaction guard

- Start encounter chỉ từ appointment CHECKED_IN và đồng thời chuyển appointment IN_PROGRESS.
- Snapshot patient/doctor/specialty lấy từ parent/profile đã lock.
- Vital sign/encounter service mutation chỉ khi encounter DRAFT.
- Finalize yêu cầu chief complaint, diagnosis, conclusion non-empty, ít nhất một vital sign và consultation service hợp lệ; transition encounter FINALIZED + appointment COMPLETED cùng transaction.
- Amendment chỉ trên FINALIZED, đúng actor/type và trong 10 ngày.
- Clinical state/history/audit cùng transaction.

## 9. Prescription

### 9.1. DB constraints

| Tên | Loại | Ý nghĩa |
| --- | --- | --- |
| `uq_prescription_series_code` | UQ | Series code |
| `uq_prescriptions_series_version` | UQ | `(series_id,version_number)` |
| `uq_prescriptions_version_code` | UQ | Version display code |
| `uq_prescriptions_one_draft` | partial UQ | `(series_id) WHERE status=DRAFT` |
| `uq_prescriptions_one_issued` | partial UQ | `(series_id) WHERE status=ISSUED` |
| `ck_prescription_series_state` | CHECK | ISSUED có current ID; DRAFT/DISCARDED không có current ID |
| `ck_prescriptions_source` | CHECK | V1 source null; V>1 source present |
| `ck_prescriptions_state_metadata` | CHECK | DRAFT/ISSUED/SUPERSEDED/DISCARDED metadata đúng tổ hợp |
| `uq_prescription_items_active_medicine` | partial UQ | `(prescription_id,medicine_id) WHERE is_removed=false` |
| `uq_prescription_items_active_sort` | partial UQ | `(prescription_id,sort_order) WHERE is_removed=false` |
| `ck_prescription_items_removal` | CHECK | Removal metadata all-null/all-present |

### 9.2. Deferred/composite guard

- `prescription_series(current_issued_prescription_id,id)` → `prescriptions(id,series_id)` là deferred FK.
- `prescriptions(source_prescription_id,series_id)` → `prescriptions(id,series_id)` giữ source cùng series.
- Trigger/service kiểm tra current row có status ISSUED; FK chỉ kiểm tra ownership.

### 9.3. Service/transaction guard

- Mọi mutation version/item lock encounter → series → versions → items theo thứ tự.
- Series DRAFT có đúng V1 DRAFT; DISCARDED không còn DRAFT/ISSUED là transaction guard vì cần đếm child row.
- Chỉ DRAFT mutable; issue yêu cầu ít nhất một active item và medicine eligibility.
- Replacement issue chuyển old ISSUED → SUPERSEDED, new DRAFT → ISSUED và update series current trong một transaction.
- Draft trên encounter FINALIZED chỉ hợp lệ nếu được tạo trước/equal finalized time theo policy đã chốt.

## 10. Medicine và inventory

### 10.1. Catalog/batch DB constraints

| Tên | Ý nghĩa |
| --- | --- |
| `uq_medicines_code`, `uq_medicines_normalized_name` | Catalog identity không tái sử dụng |
| `ck_medicines_threshold` | `0 <= low_stock_threshold <= 9999999999.999` |
| `uq_medicine_price_effective_from` | `(medicine_id,effective_from)` |
| `ck_medicine_price_value` | price `>=0`, currency `VND`, cancel metadata đồng nhất |
| `uq_medicine_batches_lot` | `(medicine_id,lot_number)` |
| `ck_medicine_batches_dates` | manufacture null hoặc `<= expires_on` |
| `ck_medicine_batches_quantity` | initial `>0`, on hand `>=0`, cost `>=0`, version `>=1` |
| `uq_stock_receipt_item_batch` | Mỗi batch đúng một receipt item |
| `ck_stock_receipt_item_values` | received qty `>0`, cost `>=0`, date snapshot hợp lệ |
| `uq_inventory_codes` | receipt/adjustment code unique |
| `ck_adjustment_item_quantity` | quantity `>0` |

Receipt/adjustment có 1..100 item là transaction guard; không thể CHECK trên header.

### 10.2. Ledger DB constraints

```text
ck_stock_movements_sign:
  RECEIPT_IN/ADJUSTMENT_IN => quantity_delta > 0
  SALE_OUT/ADJUSTMENT_OUT  => quantity_delta < 0

ck_stock_movements_balance:
  quantity_after = quantity_before + quantity_delta
  AND quantity_before >= 0 AND quantity_after >= 0

ck_stock_movements_reference_type:
  RECEIPT_IN -> RECEIPT_ITEM
  SALE_OUT -> INVOICE_ITEM
  ADJUSTMENT_* -> ADJUSTMENT_ITEM
```

UQ `uq_stock_movements_allocation(medicine_batch_id,reference_type,reference_id)` ngăn cùng source phân bổ hai lần vào cùng batch. Ledger là append-only.

### 10.3. Service/transaction guard

- Receipt: medicine active/eligible, batch mới, receipt item, batch balance và RECEIPT_IN commit cùng nhau.
- Expiry/date so với business date được kiểm tra bằng server clock.
- Adjustment OUT lock batch và không vượt tồn.
- Payment confirm lập FEFO plan theo `expires_on ASC,id ASC`, lock đủ batch, revalidate rồi update balance + movement.
- Mọi batch update tăng version và khớp movement before/after; không có generic PATCH quantity.
- Tổng movement của batch phải reconcile với `quantity_on_hand`; verification test kiểm tra định kỳ.

## 11. Billing và payment

### 11.1. Invoice/item DB constraints

| Tên | Ý nghĩa |
| --- | --- |
| `uq_invoices_code`, `uq_payments_code` | Business code unique |
| `uq_invoices_one_unpaid` | `(encounter_id) WHERE status=UNPAID` |
| `ck_invoices_amount` | totals non-negative và `total_vnd=subtotal_vnd` |
| `ck_invoices_state` | UNPAID paid_at null; PAID paid_at present |
| `ck_invoice_items_source` | SERVICE/MEDICINE có đúng nhóm FK tương ứng |
| `ck_invoice_items_amount` | quantity 1..999, prices non-negative, line total = unit price × quantity |
| `uq_invoice_items_encounter_service` | Source service khác null chỉ xuất hiện một lần |
| `uq_invoice_items_prescription_item` | Source medicine khác null chỉ xuất hiện một lần |
| `uq_payments_invoice` | Một invoice tối đa một payment |

Invoice có ít nhất một item và đúng một payment được bảo vệ bởi command transaction; FK/UQ chỉ bảo vệ at-most-one.

### 11.2. Payment DB constraints

`ck_payments_method_state` bảo vệ:

- CASH: toàn bộ bank account/QR/reference field null.
- BANK_TRANSFER: bank snapshot + transfer content + QR present ngay lúc create.
- PENDING_CONFIRMATION: confirmation actor/time/amount/reference null.
- CONFIRMED: confirmed amount/actor/time present; amount bằng expected; BANK cần bank reference.

Partial UQ:

```sql
CREATE UNIQUE INDEX uq_payments_bank_reference_confirmed
ON payments(bank_account_id_at_creation, bank_transaction_reference_normalized)
WHERE status='CONFIRMED'
  AND bank_transaction_reference_normalized IS NOT NULL;
```

### 11.3. Service/transaction guard

- Invoice chỉ từ encounter FINALIZED; lấy toàn bộ source chưa billed và tạo immutable items + pending payment + audit cùng transaction.
- Medicine price version phải thuộc medicine của prescription item; vì invoice item không lặp `medicine_id`, command lock/revalidate hai source trước insert.
- Invoice subtotal là sum item server-side; client không truyền quantity/price/total.
- Confirm assert invoice UNPAID + payment PENDING; service-only hoặc FEFO branch commit financial/inventory/audit nguyên tử.
- PAID/CONFIRMED và snapshot financial không sửa; refund/void là chứng từ mới ngoài MVP.

## 12. Settings, bank, audit và idempotency

### 12.1. Settings

`ck_system_settings_value_type` yêu cầu đúng một value field theo type. `ck_system_settings_catalog_value` giới hạn đúng 9 key baseline và range:

| Key | Type/range |
| --- | --- |
| `appointment_auto_approval_enabled` | BOOLEAN |
| `appointment_booking_min_lead_minutes` | INTEGER 0..1440 |
| `appointment_booking_max_advance_days` | INTEGER 1..365 |
| `appointment_check_in_early_minutes` | INTEGER 0..240 |
| `appointment_check_in_late_minutes` | INTEGER 0..240 |
| `appointment_no_show_grace_minutes` | INTEGER 0..240; cross-row group rule `<= late` |
| `appointment_max_holding_future_per_patient` | INTEGER 1..20 |
| `default_slot_duration_minutes` | INTEGER IN 15/20/30/45/60 |
| `inventory_expiry_alert_days` | INTEGER 1..365 |

Cross-row group update lock key ASC và validate grace ≤ late trước commit.

### 12.2. Bank account

- UQ `(bank_bin,account_number)` trên mọi row.
- Regex bank BIN/account number và state/archive metadata checks.
- At most one active row:

```sql
CREATE UNIQUE INDEX uq_bank_accounts_single_active
ON bank_accounts ((1))
WHERE status='ACTIVE' AND is_deleted=false;
```

Activation handover lock current/candidate ID ASC; archive chỉ INACTIVE.

### 12.3. Audit

| Constraint | Ý nghĩa |
| --- | --- |
| `ck_audit_logs_actor` | USER có user ID; SERVICE có service; SYSTEM không có hai field |
| `ck_audit_logs_json_size` | Mỗi JSONB metadata/snapshot `<= 16 KiB` serialized |
| `ck_audit_logs_retention` | `retention_until >= occurred_at + interval '7 years'` |
| `ck_audit_logs_success_target` | SUCCESS mutation cần target trừ action allowlist system aggregate |

Trigger `trg_audit_logs_immutable` từ chối mọi UPDATE/DELETE. Application role chỉ có INSERT và SELECT projection.

### 12.4. Idempotency

- UQ `(principal_key,operation_scope,idempotency_key)`.
- Key length 16..128 ASCII; request hash đúng 64 hex.
- PROCESSING không có response/completed; COMPLETED có `http_status`, `completed_at`, resource/response phù hợp.
- `expires_at > created_at`; financial record có expiry tối thiểu 30 ngày do command validate.
- `response_body` chỉ lưu projection replay tối thiểu, không lưu raw token, password, full QR/bank account hoặc clinical content.

## 13. Immutability trigger/policy matrix

| Table | Policy |
| --- | --- |
| `user_role_histories` | Reject UPDATE/DELETE |
| `appointment_status_histories` | Reject UPDATE/DELETE |
| `medical_encounter_status_histories` | Reject UPDATE/DELETE |
| `medical_encounter_amendments` | Reject UPDATE/DELETE |
| `audit_logs` | Reject UPDATE/DELETE |
| `stock_movements` | Reject UPDATE/DELETE |
| `stock_receipts`, `stock_receipt_items` | Reject UPDATE/DELETE sau insert |
| `inventory_adjustments`, `inventory_adjustment_items` | Reject UPDATE/DELETE sau insert |
| `invoice_items` | Reject UPDATE/DELETE |
| `service_price_versions`, `medicine_price_versions` | Chỉ cho set cancellation metadata khi version còn future và chưa referenced |
| `prescriptions` | Chỉ DRAFT update content/version; state transition qua command function/repository |
| `prescription_items` | Chỉ khi parent DRAFT; removed row không restore |
| `encounter_vital_signs`, `encounter_services` | Chỉ khi parent encounter DRAFT |
| `invoices`, `payments` | Chỉ state/confirmation columns qua guarded command; snapshot/amount immutable |

Nếu application dùng database role tách theo writer, ưu tiên revoke quyền trước trigger; trigger là defense-in-depth và giúp test migration độc lập.

## 14. Global lock order

Mọi command chỉ lock nhóm mình dùng nhưng không đảo thứ tự:

```text
idempotency claim (nếu command yêu cầu; insert/lookup trước domain locks)
  -> system_setting
  -> bank_account
  -> specialty
  -> room
  -> doctor user
  -> staff_profile
  -> doctor_profile
  -> doctor_specialty
  -> doctor_schedule
  -> patient user
  -> patient
  -> appointment
  -> medical_encounter
  -> service
  -> service_price_version
  -> medicine
  -> medicine_price_version
  -> medicine_batch (medicine, expires_on ASC, id ASC)
  -> encounter_service
  -> prescription_series
  -> prescription (id ASC)
  -> prescription_item (id ASC)
  -> invoice / invoice_item / payment (id ASC)
  -> append required audit row
```

- Nhiều row cùng model lock primary key ASC, trừ FEFO batch theo `expires_on ASC,id ASC` trong từng medicine.
- Audit insert là bước cuối, append-only, không tạo lock group mới.
- Idempotency record không được dùng để giữ transaction mở trong khi gọi network/external service.
- Không gọi ngân hàng ngoài transaction; QR được sinh từ snapshot nội bộ.

## 15. Constraint → business error mapping tối thiểu

| Constraint | Business error |
| --- | --- |
| `ex_doctor_schedules_doctor_overlap` | `DOCTOR_SCHEDULE_DOCTOR_CONFLICT` |
| `ex_doctor_schedules_room_overlap` | `DOCTOR_SCHEDULE_ROOM_CONFLICT` |
| `ex_appointments_doctor_overlap` | `APPOINTMENT_DOCTOR_TIME_CONFLICT` |
| `ex_appointments_patient_overlap` | `APPOINTMENT_PATIENT_TIME_CONFLICT` |
| `ex_appointments_room_overlap` | `APPOINTMENT_ROOM_TIME_CONFLICT` |
| `uq_appointments_booking_idempotency` | Replay hoặc `APPOINTMENT_IDEMPOTENCY_CONFLICT` sau hash check |
| `uq_appointments_follow_up_holding` | `APPOINTMENT_FOLLOW_UP_ACTIVE_EXISTS` |
| `uq_bank_accounts_single_active` | `BANK_ACCOUNT_ACTIVE_CONFLICT` |
| `uq_prescriptions_one_draft` | `PRESCRIPTION_DRAFT_ALREADY_EXISTS` |
| `uq_prescriptions_one_issued` | `PRESCRIPTION_ISSUED_CONFLICT` |
| `uq_invoices_one_unpaid` | `INVOICE_UNPAID_ALREADY_EXISTS` |
| `uq_invoice_items_encounter_service` / `...prescription_item` | `INVOICE_SOURCE_ALREADY_BILLED` |
| `uq_payments_bank_reference_confirmed` | `PAYMENT_BANK_REFERENCE_ALREADY_USED` |
| `uq_stock_movements_allocation` | `INVENTORY_ALLOCATION_ALREADY_RECORDED` |
| `uq_idempotency_scope_key` | Replay hoặc `IDEMPOTENCY_KEY_REUSED` sau hash check |

Raw SQLSTATE, constraint text, schema/table name và stack trace không trả cho client.

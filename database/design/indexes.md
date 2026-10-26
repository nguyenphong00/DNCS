# Index Design — DNCS MVP

## 1. Nguyên tắc

1. PostgreSQL tự tạo index cho PK, unique constraint và exclusion constraint; không tạo index trùng.
2. PostgreSQL không tự tạo index cho phía child của FK. Mọi FK dùng cho dependency check/join phải được đánh giá riêng.
3. Composite index đặt equality/filter column trước, sau đó sort/cursor column.
4. Sort ổn định luôn có `id` làm tie-breaker.
5. Không dùng `CURRENT_DATE`, `now()` hoặc expression volatile trong partial-index predicate.
6. Index write-heavy như ledger/audit chỉ tạo khi có query MVP cụ thể.
7. Sau khi có dữ liệu đại diện phải chạy `EXPLAIN (ANALYZE, BUFFERS)` và loại index không được dùng/trùng prefix.

## 2. Index đã có từ constraint

Không tạo lại các nhóm sau:

- PK của 42 bảng.
- Unique business code/normalized identifier.
- Composite unique source/idempotency/version.
- Partial unique active relation/current state.
- GiST index do exclusion constraint schedule/appointment tạo.

Danh sách dưới đây là index bổ sung hoặc unique index cần tạo tường minh vì có predicate/expression.

## 3. Identity và authorization

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_roles_scope_status` | `roles(account_scope,status,id) WHERE is_deleted=false` | Chọn role hợp lệ khi tạo/gán account |
| `ix_permissions_module_status` | `permissions(module,status,code) WHERE is_deleted=false` | Ma trận permission theo module |
| `ix_users_role_active` | `users(role_id,status,id) WHERE is_deleted=false` | Dependency trước deactivate role, list account |
| `ix_users_status_created` | `users(status,created_at DESC,id DESC) WHERE is_deleted=false` | Admin account list |
| `ix_auth_sessions_user_active` | `auth_sessions(user_id,expires_at DESC,id DESC) WHERE revoked_at IS NULL` | Validate/revoke current sessions |
| `ix_auth_sessions_expiry` | `auth_sessions(expires_at,id) WHERE revoked_at IS NULL` | Cleanup session hết hạn |
| `ix_role_permissions_permission` | `role_permissions(permission_id,role_id)` | Reverse lookup role theo permission |
| `ix_user_overrides_permission_active` | `user_permission_overrides(permission_id,user_id) WHERE is_deleted=false` | Dependency permission catalog |
| `ix_user_role_history_user_time` | `user_role_histories(user_id,created_at DESC,id DESC)` | Lịch sử role của user |

Unique `users(normalized_username)` và `users(normalized_phone_number)` phục vụ login exact; không tạo thêm index identifier.

## 4. Platform

| Index | Definition | Use case |
| --- | --- | --- |
| `uq_bank_accounts_single_active` | Unique `((1)) WHERE status='ACTIVE' AND is_deleted=false` | At-most-one receiving account |
| `ix_bank_accounts_status` | `bank_accounts(status,id) WHERE is_deleted=false` | Admin list/handover |
| `ix_audit_logs_time` | `audit_logs(occurred_at DESC,id DESC)` | Audit cursor mặc định |
| `ix_audit_logs_module_time` | `audit_logs(module,occurred_at DESC,id DESC)` | Filter module |
| `ix_audit_logs_action_time` | `audit_logs(action_code,occurred_at DESC,id DESC)` | Filter action |
| `ix_audit_logs_actor_time` | `audit_logs(actor_user_id,occurred_at DESC,id DESC) WHERE actor_user_id IS NOT NULL` | Filter actor |
| `ix_audit_logs_target_time` | `audit_logs(target_type,target_id,occurred_at DESC,id DESC) WHERE target_id IS NOT NULL` | Audit aggregate |
| `ix_audit_logs_outcome_time` | `audit_logs(outcome,occurred_at DESC,id DESC)` | Security/failure review |
| `ix_audit_logs_retention` | `audit_logs(retention_until,id)` | Future reviewed retention job |
| `ix_idempotency_expiry` | `idempotency_records(expires_at,id)` | Cleanup expired records |
| `ix_idempotency_resource` | `idempotency_records(resource_type,resource_id) WHERE resource_id IS NOT NULL` | Recovery/support lookup |
| `ix_idempotency_processing` | `idempotency_records(created_at,id) WHERE status='PROCESSING'` | Phát hiện record bất thường |

`system_settings(setting_key)` unique đủ cho mọi consumer read và lock-by-key.

## 5. Staff, patient và clinic master

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_staff_profiles_status_created` | `staff_profiles(employment_status,created_at DESC,id DESC) WHERE is_deleted=false` | Danh sách nhân sự |
| `ix_doctor_profiles_status` | `doctor_profiles(status,id) WHERE is_deleted=false` | Doctor eligibility list |
| `ix_patients_list` | `patients(is_deleted,created_at DESC,id DESC)` | Patient list ổn định |
| `ix_patients_name_trgm` | `USING gin(normalized_full_name gin_trgm_ops)` | Search tên không dấu `ILIKE '%...%'` |
| `ix_patients_date_of_birth` | `patients(date_of_birth,id) WHERE date_of_birth IS NOT NULL` | Đối chiếu tại quầy |
| `ix_specialties_active_name` | `specialties(status,is_deleted,normalized_name,id)` | Catalog/list/search |
| `ix_rooms_active_type_name` | `rooms(status,is_deleted,room_type,normalized_name,id)` | Room admin/filter |
| `ix_doctor_specialties_specialty_active` | `doctor_specialties(specialty_id,doctor_profile_id) WHERE is_deleted=false` | Specialty → doctor |
| `ix_doctor_specialties_history` | `doctor_specialties(doctor_profile_id,assigned_at DESC,id DESC)` | Relation history |

## 6. Scheduling và appointment

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_doctor_schedules_doctor_start` | `doctor_schedules(doctor_profile_id,start_at,id)` | Lịch ngày của doctor |
| `ix_doctor_schedules_room_start` | `doctor_schedules(room_id,start_at,id)` | Phân bổ phòng |
| `ix_doctor_schedules_admin` | `doctor_schedules(status,is_deleted,start_at,end_at,id)` | Admin filter/date range |
| `ix_appointments_patient_start` | `appointments(patient_id,start_at DESC,id DESC)` | Patient upcoming/history |
| `ix_appointments_doctor_start` | `appointments(doctor_profile_id,start_at,id)` | Doctor worklist |
| `ix_appointments_schedule_start` | `appointments(doctor_schedule_id,start_at,id)` | Dependency/slot occupancy |
| `ix_appointments_status_start` | `appointments(status,start_at,id)` | Dashboard pending/check-in/no-show |
| `ix_appointments_room_start` | `appointments(room_id,start_at,id)` | Room operational list |
| `ix_appointments_specialty_start` | `appointments(specialty_id,start_at,id)` | Specialty filter/report |
| `ix_appointments_follow_up_source` | `appointments(follow_up_from_encounter_id,id) WHERE follow_up_from_encounter_id IS NOT NULL` | Follow-up history |
| `ix_appointment_history_timeline` | `appointment_status_histories(appointment_id,created_at,id)` | Status timeline |

Các exclusion constraint đã cung cấp GiST index cho overlap; B-tree vẫn cần cho list/date range.

## 7. Service và encounter

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_services_active_type` | `services(status,service_type,id) WHERE is_deleted=false` | Catalog theo type |
| `ix_services_specialty_active` | `services(specialty_id,id) WHERE is_deleted=false AND specialty_id IS NOT NULL` | Consultation fee lookup |
| `ix_service_prices_current` | `service_price_versions(service_id,effective_from DESC,id DESC) WHERE cancelled_at IS NULL` | Resolve current price |
| `ix_encounters_patient_started` | `medical_encounters(patient_id,started_at DESC,id DESC)` | Patient history/cursor |
| `ix_encounters_doctor_started` | `medical_encounters(doctor_profile_id,started_at DESC,id DESC)` | Doctor history |
| `ix_encounters_status_started` | `medical_encounters(status,started_at DESC,id DESC)` | Clinical dashboard |
| `ix_encounters_specialty_started` | `medical_encounters(specialty_id,started_at DESC,id DESC)` | Filter/report |
| `ix_vital_signs_timeline` | `encounter_vital_signs(encounter_id,measured_at,id)` | Vital timeline/finalize existence |
| `ix_encounter_status_timeline` | `medical_encounter_status_histories(encounter_id,created_at,id)` | Status timeline |
| `ix_encounter_services_list` | `encounter_services(encounter_id,status,id)` | Encounter service projection |
| `ix_encounter_services_service_history` | `encounter_services(service_id,assigned_at DESC,id DESC)` | Service usage/report |

Unique `(encounter_id,sequence_number)` của amendment phục vụ trực tiếp timeline; không cần index khác trong MVP.

## 8. Medicine và inventory

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_medicines_active` | `medicines(status,id) WHERE is_deleted=false` | Catalog eligible |
| `ix_medicine_prices_current` | `medicine_price_versions(medicine_id,effective_from DESC,id DESC) WHERE cancelled_at IS NULL` | Resolve sale price |
| `ix_batches_fefo` | `medicine_batches(medicine_id,expires_on,id) WHERE quantity_on_hand>0` | FEFO allocation; expiry filter runtime |
| `ix_batches_expiry` | `medicine_batches(expires_on,id) WHERE quantity_on_hand>0` | Expiry dashboard |
| `ix_receipts_time` | `stock_receipts(received_at DESC,id DESC)` | Receipt history |
| `ix_receipt_items_header` | `stock_receipt_items(stock_receipt_id,id)` | Receipt detail |
| `ix_receipt_items_medicine_expiry` | `stock_receipt_items(medicine_id,expires_on,id)` | Medicine receipt trace |
| `ix_adjustments_time` | `inventory_adjustments(adjusted_at DESC,id DESC)` | Adjustment history |
| `ix_adjustment_items_header` | `inventory_adjustment_items(inventory_adjustment_id,id)` | Adjustment detail |
| `ix_adjustment_items_batch` | `inventory_adjustment_items(medicine_batch_id,id)` | Batch trace |
| `ix_stock_movements_batch_time` | `stock_movements(medicine_batch_id,occurred_at DESC,id DESC)` | Batch ledger |
| `ix_stock_movements_medicine_time` | `stock_movements(medicine_id,occurred_at DESC,id DESC)` | Medicine ledger |
| `ix_stock_movements_reference` | `stock_movements(reference_type,reference_id,id)` | Source → allocation lookup |

`uq_stock_movements_allocation(medicine_batch_id,reference_type,reference_id)` không thay thế `ix_stock_movements_reference` vì thứ tự leading column khác.

## 9. Prescription

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_prescription_series_encounter` | `prescription_series(encounter_id,created_at DESC,id DESC)` | Series của encounter |
| `ix_prescription_series_status` | `prescription_series(status,created_at DESC,id DESC)` | Draft/issued worklist |
| `ix_prescriptions_status_issued` | `prescriptions(status,issued_at DESC,id DESC)` | Issued history |
| `ix_prescription_items_list` | `prescription_items(prescription_id,is_removed,sort_order,id)` | Render version |
| `ix_prescription_items_medicine_history` | `prescription_items(medicine_id,created_at DESC,id DESC)` | Medicine trace/report |

Partial unique one-DRAFT/one-ISSUED và active medicine/sort đã cover current-state lookup theo series/prescription.

## 10. Billing và payment

| Index | Definition | Use case |
| --- | --- | --- |
| `ix_invoices_patient_created` | `invoices(patient_id,created_at DESC,id DESC)` | Patient invoice history |
| `ix_invoices_encounter_created` | `invoices(encounter_id,created_at DESC,id DESC)` | Encounter billing history |
| `ix_invoices_status_created` | `invoices(status,created_at DESC,id DESC)` | Cashier worklist |
| `ix_invoice_items_invoice` | `invoice_items(invoice_id,id)` | Invoice detail |
| `ix_payments_status_created` | `payments(status,created_at DESC,id DESC)` | Pending confirmation list |
| `ix_payments_confirmed_time` | `payments(confirmed_at DESC,id DESC) WHERE status='CONFIRMED'` | Payment history/reconciliation support |

Unique partial source indexes trên `invoice_items` và unique `payments(invoice_id)` đã phục vụ source dedup/exact payment lookup.

## 11. Index rollout và verification

### Trước migration

- Kiểm tra tên index <= 63 byte.
- Kiểm tra không trùng exact definition với PK/UQ/FK-support index khác.
- Gắn mỗi index với endpoint/query trong feature spec.

### Sau migration trên database rỗng

- Query `pg_indexes`, `pg_constraint` để snapshot catalog.
- Xác minh `btree_gist`, `pg_trgm` đã được cài.
- Chạy test exclusion/partial unique với hai transaction đồng thời.

### Sau khi có dữ liệu đại diện

- Dùng `EXPLAIN (ANALYZE, BUFFERS)` cho slot search, patient search, doctor worklist, encounter history, current price, FEFO, unpaid invoice và audit explorer.
- Theo dõi `pg_stat_user_indexes.idx_scan` và write amplification.
- Chỉ drop index bằng forward migration riêng sau khi chứng minh không dùng; không chỉnh migration đã phát hành.

# Table Catalog — DNCS MVP

## 1. Quy ước

- `PK`: primary key; `FK`: foreign key; `UQ`: unique key.
- Mọi `FK users` dùng `ON DELETE RESTRICT` và có thể tham chiếu user inactive/deleted để giữ lịch sử.
- `created_at`/`updated_at` dùng `timestamptz`; default timestamp do database đặt.
- `version` mặc định `1` và luôn `>= 1`.
- Các độ dài dưới đây là physical baseline. Chuỗi nullable chỉ chứa whitespace được application chuẩn hóa thành `NULL`.
- Constraint chi tiết, tên constraint và guard xuyên bảng nằm tại [`constraints.md`](constraints.md).

## 2. Enum và lookup

### 2.1. PostgreSQL enum

| Type | Values |
| --- | --- |
| `account_type_enum` | `STAFF`, `PATIENT` |
| `user_status_enum` | `ACTIVE`, `INACTIVE`, `LOCKED` |
| `role_type_enum` | `SYSTEM`, `CUSTOM` |
| `account_scope_enum` | `STAFF`, `PATIENT` |
| `role_status_enum` | `ACTIVE`, `INACTIVE` |
| `permission_risk_enum` | `NORMAL`, `SENSITIVE`, `PROTECTED` |
| `permission_status_enum` | `ACTIVE`, `INACTIVE` |
| `permission_effect_enum` | `ALLOW`, `DENY` |
| `employment_status_enum` | `ACTIVE`, `INACTIVE`, `TERMINATED` |
| `doctor_status_enum` | `ACTIVE`, `INACTIVE` |
| `catalog_status_enum` | `ACTIVE`, `INACTIVE` |
| `room_type_enum` | `CONSULTATION`, `FUNCTIONAL`, `OTHER` |
| `doctor_schedule_status_enum` | `DRAFT`, `ACTIVE`, `CANCELLED` |
| `appointment_status_enum` | `PENDING_APPROVAL`, `CONFIRMED`, `REJECTED`, `CANCELLED`, `CHECKED_IN`, `NO_SHOW`, `IN_PROGRESS`, `COMPLETED` |
| `appointment_source_enum` | `PATIENT_SELF`, `COUNTER`, `FOLLOW_UP` |
| `appointment_approval_mode_enum` | `AUTO`, `MANUAL`, `DIRECT_FOLLOW_UP` |
| `appointment_actor_type_enum` | `USER`, `SYSTEM` |
| `patient_created_source_enum` | `SELF_REGISTRATION`, `COUNTER_ADMIN`, `COUNTER_CASHIER` |
| `encounter_status_enum` | `DRAFT`, `FINALIZED` |
| `encounter_amendment_type_enum` | `CLINICAL_ADDENDUM`, `CLINICAL_CORRECTION`, `ADMINISTRATIVE_NOTE` |
| `service_type_enum` | `CONSULTATION`, `PROCEDURE`, `OTHER` |
| `encounter_service_source_enum` | `SYSTEM_CONSULTATION`, `DOCTOR_ORDER` |
| `encounter_service_status_enum` | `ACTIVE`, `REMOVED` |
| `inventory_adjustment_reason_enum` | `COUNT_CORRECTION`, `DAMAGE`, `EXPIRED_DISPOSAL`, `OTHER` |
| `inventory_direction_enum` | `IN`, `OUT` |
| `stock_movement_type_enum` | `RECEIPT_IN`, `SALE_OUT`, `ADJUSTMENT_IN`, `ADJUSTMENT_OUT` |
| `stock_reference_type_enum` | `RECEIPT_ITEM`, `INVOICE_ITEM`, `ADJUSTMENT_ITEM` |
| `prescription_series_status_enum` | `DRAFT`, `ISSUED`, `DISCARDED` |
| `prescription_status_enum` | `DRAFT`, `ISSUED`, `SUPERSEDED`, `DISCARDED` |
| `invoice_status_enum` | `UNPAID`, `PAID` |
| `invoice_item_type_enum` | `SERVICE`, `MEDICINE` |
| `payment_method_enum` | `CASH`, `BANK_TRANSFER` |
| `payment_status_enum` | `PENDING_CONFIRMATION`, `CONFIRMED` |
| `setting_value_type_enum` | `BOOLEAN`, `INTEGER` |
| `bank_account_status_enum` | `ACTIVE`, `INACTIVE` |
| `audit_actor_type_enum` | `USER`, `SERVICE`, `SYSTEM` |
| `audit_outcome_enum` | `SUCCESS`, `FAILURE`, `DENIED` |
| `idempotency_status_enum` | `PROCESSING`, `COMPLETED` |

Enum chỉ được thêm value bằng forward migration; không đổi tên/xóa value đã có dữ liệu.

### 2.2. `gender_codes`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `code` | `varchar(20)` | PK | `MALE`, `FEMALE`, `OTHER`, `UNSPECIFIED` |
| `display_name_vi` | `varchar(50)` | NOT NULL | Tên hiển thị |
| `sort_order` | `smallint` | NOT NULL | UQ, `>= 0` |
| `is_active` | `boolean` | NOT NULL default `true` | Không hard delete lookup đã dùng |

## 3. Identity và authorization

### 3.1. `roles`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `code` | `varchar(64)` | NOT NULL, UQ | Upper snake case, immutable |
| `name` | `varchar(150)` | NOT NULL | |
| `description` | `varchar(1000)` | NULL | |
| `role_type` | `role_type_enum` | NOT NULL | |
| `account_scope` | `account_scope_enum` | NOT NULL | |
| `is_protected` | `boolean` | NOT NULL default `false` | System role là `true` |
| `status` | `role_status_enum` | NOT NULL default `ACTIVE` | |
| `created_by` | `bigint` | NULL, FK users deferred | Null với seed |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Code không tái sử dụng |

### 3.2. `permissions`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `code` | `varchar(150)` | NOT NULL, UQ | Ví dụ `payment.confirm`, immutable |
| `module` | `varchar(80)` | NOT NULL | Upper snake case |
| `name` | `varchar(150)` | NOT NULL | |
| `description` | `varchar(1000)` | NULL | |
| `risk_level` | `permission_risk_enum` | NOT NULL | |
| `status` | `permission_status_enum` | NOT NULL default `ACTIVE` | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Catalog source-controlled |

### 3.3. `users`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `role_id` | `bigint` | NOT NULL, FK roles | Role hiện hành |
| `account_type` | `account_type_enum` | NOT NULL | |
| `username` | `varchar(100)` | conditional | Chỉ STAFF |
| `normalized_username` | `varchar(100)` | conditional, UQ | Không tái sử dụng sau archive |
| `phone_number` | `varchar(20)` | conditional | Chỉ PATIENT, dạng E.164 |
| `normalized_phone_number` | `varchar(20)` | conditional, UQ | Nguồn login/unique |
| `password_hash` | `varchar(255)` | NOT NULL | Argon2id/bcrypt encoded hash |
| `status` | `user_status_enum` | NOT NULL default `ACTIVE` | |
| `must_change_password` | `boolean` | NOT NULL default `false` | |
| `failed_login_count` | `integer` | NOT NULL default `0` | `>= 0` |
| `locked_until` | `timestamptz` | NULL | |
| `last_login_at` | `timestamptz` | NULL | |
| `password_changed_at` | `timestamptz` | NULL | |
| `token_version` | `bigint` | NOT NULL default `1` | `>= 1` |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Không hard delete |

### 3.4. `auth_sessions`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Giá trị dùng làm JWT `sid` |
| `user_id` | `bigint` | NOT NULL, FK users | |
| `refresh_token_hash` | `varchar(255)` | NOT NULL, UQ | Không lưu raw token |
| `issued_at` | `timestamptz` | NOT NULL | |
| `expires_at` | `timestamptz` | NOT NULL | `expires_at > issued_at` |
| `revoked_at` | `timestamptz` | NULL | |
| `revoke_reason` | `varchar(500)` | NULL | |
| `ip_address` | `inet` | NULL | Không đưa vào audit metadata dạng raw |
| `user_agent` | `varchar(1000)` | NULL | |
| `updated_at` | `timestamptz` | NOT NULL | Refresh rotation/revoke |

### 3.5. `role_permissions`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `role_id` | `bigint` | PK part, FK roles | |
| `permission_id` | `bigint` | PK part, FK permissions | |
| `granted_by` | `bigint` | NULL, FK users | Null với seed |
| `created_at` | `timestamptz` | NOT NULL | |

### 3.6. `user_permission_overrides`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `user_id` | `bigint` | NOT NULL, FK users | Chỉ STAFF |
| `permission_id` | `bigint` | NOT NULL, FK permissions | |
| `effect` | `permission_effect_enum` | NOT NULL | |
| `reason` | `varchar(1000)` | NOT NULL | Không rỗng |
| `granted_by` | `bigint` | NOT NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Partial UQ active pair |

### 3.7. `user_role_histories`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Append-only |
| `user_id` | `bigint` | NOT NULL, FK users | |
| `from_role_id` | `bigint` | NULL, FK roles | Null lần gán đầu |
| `to_role_id` | `bigint` | NOT NULL, FK roles | |
| `changed_by_user_id` | `bigint` | NOT NULL, FK users | |
| `reason` | `varchar(1000)` | NULL | |
| `created_at` | `timestamptz` | NOT NULL | |

## 4. Platform: settings, bank, audit và idempotency

### 4.1. `system_settings`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `setting_key` | `varchar(100)` | NOT NULL, UQ | Catalog đóng |
| `value_type` | `setting_value_type_enum` | NOT NULL | Immutable |
| `boolean_value` | `boolean` | conditional | |
| `integer_value` | `integer` | conditional | |
| `version` | `bigint` | NOT NULL default `1` | |
| `updated_by_user_id` | `bigint` | NULL, FK users | Null với seed |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |

### 4.2. `bank_accounts`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `bank_bin` | `char(6)` | NOT NULL | 6 chữ số |
| `bank_name` | `varchar(200)` | NOT NULL | |
| `account_number` | `varchar(30)` | NOT NULL | 6..30 chữ số |
| `account_name` | `varchar(200)` | NOT NULL | |
| `status` | `bank_account_status_enum` | NOT NULL default `INACTIVE` | |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at` | `timestamptz` | NOT NULL | |
| `activated_by_user_id`, `deactivated_by_user_id` | `bigint` | NULL, FK users | |
| `activated_at`, `deactivated_at` | `timestamptz` | NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |
| `deleted_by_user_id` | `bigint` | NULL, FK users | |
| `deleted_at` | `timestamptz` | NULL | |
| `archive_reason` | `varchar(500)` | NULL | |

### 4.3. `audit_logs`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Append-only |
| `occurred_at` | `timestamptz` | NOT NULL | Business event time |
| `actor_type` | `audit_actor_type_enum` | NOT NULL | |
| `actor_user_id` | `bigint` | conditional, FK users | USER |
| `actor_service` | `varchar(100)` | conditional | SERVICE |
| `module` | `varchar(80)` | NOT NULL | Server allowlist |
| `action_code` | `varchar(150)` | NOT NULL | Upper snake case |
| `outcome` | `audit_outcome_enum` | NOT NULL | |
| `target_type` | `varchar(80)` | NULL | Logical reference |
| `target_id` | `bigint` | NULL | Logical reference |
| `target_code` | `varchar(100)` | NULL | Không chứa PII |
| `correlation_id` | `uuid` | NULL | |
| `request_id` | `uuid` | NULL | |
| `ip_hash` | `varchar(128)` | NULL | HMAC/hash, không raw IP |
| `metadata` | `jsonb` | NOT NULL default `{}` | Tối đa 16 KiB |
| `before_snapshot`, `after_snapshot` | `jsonb` | NULL | Mỗi field tối đa 16 KiB |
| `retention_until` | `timestamptz` | NOT NULL | Tối thiểu `occurred_at + 7 years` |
| `created_at` | `timestamptz` | NOT NULL | Insert time |

### 4.4. `idempotency_records`

Bảng hạ tầng này được bổ sung để hiện thực contract retry đã chốt; không phải aggregate nghiệp vụ.

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `principal_key` | `varchar(200)` | NOT NULL | Ví dụ `USER:42`, `SERVICE:BILLING` |
| `actor_user_id` | `bigint` | NULL, FK users | Hỗ trợ truy vết, không tham gia authorization |
| `operation_scope` | `varchar(200)` | NOT NULL | Command + target scope chuẩn hóa |
| `idempotency_key` | `varchar(128)` | NOT NULL | 16..128 ASCII; UUID command vẫn lưu dạng text |
| `request_hash` | `char(64)` | NOT NULL | SHA-256 hex của payload chuẩn hóa |
| `status` | `idempotency_status_enum` | NOT NULL | |
| `resource_type` | `varchar(80)` | NULL | Logical result reference |
| `resource_id` | `bigint` | NULL | |
| `http_status` | `smallint` | NULL | Chỉ khi COMPLETED |
| `response_body` | `jsonb` | NULL | Projection tối thiểu/redacted |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `completed_at` | `timestamptz` | NULL | |
| `expires_at` | `timestamptz` | NOT NULL | Theo policy command, financial tối thiểu 30 ngày |

UQ `(principal_key, operation_scope, idempotency_key)`; cùng key khác hash bị từ chối.

## 5. Profile

### 5.1. `staff_profiles`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `user_id` | `bigint` | NOT NULL, UQ, FK users | |
| `staff_code` | `varchar(32)` | NOT NULL, UQ | `STF-{id}` pad tối thiểu 8, mở rộng theo bigint |
| `full_name` | `varchar(150)` | NOT NULL | |
| `phone_number`, `normalized_phone_number` | `varchar(20)` | NULL | Normalized value UQ khi khác null |
| `email` | `varchar(254)` | NULL | Display value |
| `normalized_email` | `varchar(254)` | NULL | UQ khi khác null |
| `date_of_birth` | `date` | NULL | |
| `gender_code` | `varchar(20)` | NULL, FK gender_codes | |
| `address` | `varchar(500)` | NULL | |
| `employment_status` | `employment_status_enum` | NOT NULL default `ACTIVE` | |
| `created_by`, `updated_by` | `bigint` | NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |

### 5.2. `doctor_profiles`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `staff_profile_id` | `bigint` | NOT NULL, UQ, FK staff_profiles | |
| `doctor_code` | `varchar(32)` | NOT NULL, UQ | `DOC-{id}` pad tối thiểu 8, mở rộng theo bigint |
| `status` | `doctor_status_enum` | NOT NULL default `ACTIVE` | |
| `created_by` | `bigint` | NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |

### 5.3. `patients`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `user_id` | `bigint` | NOT NULL, UQ, FK users | |
| `patient_code` | `varchar(32)` | NOT NULL, UQ | `PAT-{id}` pad tối thiểu 8, mở rộng theo bigint |
| `full_name` | `varchar(150)` | NOT NULL | |
| `normalized_full_name` | `varchar(150)` | NOT NULL | pg_trgm search |
| `date_of_birth` | `date` | NULL | |
| `gender_code` | `varchar(20)` | NULL, FK gender_codes | |
| `address` | `varchar(500)` | NULL | |
| `cccd` | `varchar(12)` | NULL, UQ | 12 chữ số |
| `health_insurance_number` | `varchar(15)` | NULL, UQ | `[A-Z0-9]{15}` |
| `medical_history` | `varchar(4000)` | NULL | Patient-declared |
| `allergies` | `varchar(2000)` | NULL | Patient-declared |
| `created_source` | `patient_created_source_enum` | NOT NULL | |
| `created_by_user_id`, `updated_by_user_id` | `bigint` | NULL, FK users | |
| `version` | `bigint` | NOT NULL default `1` | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |
| `deleted_at`, `restored_at` | `timestamptz` | NULL | |
| `deleted_by_user_id`, `restored_by_user_id` | `bigint` | NULL, FK users | |
| `deletion_reason` | `varchar(1000)` | NULL | |

## 6. Clinic master data

### 6.1. `specialties`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `code` | `varchar(34)` | NOT NULL, UQ | `SPC_[A-Z0-9_]{2,30}` |
| `name`, `normalized_name` | `varchar(200)` | NOT NULL | normalized name UQ |
| `description` | `varchar(1000)` | NULL | |
| `status` | `catalog_status_enum` | NOT NULL default `ACTIVE` | |
| `created_by`, `updated_by` | `bigint` | NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |

### 6.2. `rooms`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `code` | `varchar(23)` | NOT NULL, UQ | `RM-[A-Z0-9-]{2,20}` |
| `name`, `normalized_name` | `varchar(200)` | NOT NULL | normalized name UQ |
| `room_type` | `room_type_enum` | NOT NULL | |
| `floor` | `varchar(100)` | NULL | |
| `location_note` | `varchar(500)` | NULL | |
| `description` | `varchar(1000)` | NULL | |
| `status` | `catalog_status_enum` | NOT NULL default `ACTIVE` | |
| `created_by`, `updated_by` | `bigint` | NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |

### 6.3. `doctor_specialties`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Một lần gán |
| `doctor_profile_id` | `bigint` | NOT NULL, FK doctor_profiles | |
| `specialty_id` | `bigint` | NOT NULL, FK specialties | |
| `assigned_by` | `bigint` | NOT NULL, FK users | |
| `assigned_at` | `timestamptz` | NOT NULL | |
| `removed_by` | `bigint` | NULL, FK users | |
| `removed_at` | `timestamptz` | NULL | |
| `removal_reason` | `varchar(1000)` | NULL | |
| `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Partial UQ active pair |

## 7. Scheduling và appointment

### 7.1. `doctor_schedules`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `doctor_profile_id` | `bigint` | NOT NULL, FK doctor_profiles | |
| `room_id` | `bigint` | NOT NULL, FK rooms | |
| `start_at`, `end_at` | `timestamptz` | NOT NULL | Half-open range |
| `schedule_range` | `tstzrange` | generated stored | `tstzrange(start_at,end_at,'[)')` |
| `slot_duration_minutes` | `smallint` | NOT NULL | 15/20/30/45/60 |
| `status` | `doctor_schedule_status_enum` | NOT NULL default `DRAFT` | |
| `notes` | `varchar(1000)` | NULL | |
| `cancellation_reason` | `varchar(1000)` | NULL | Conditional |
| `cancelled_by` | `bigint` | NULL, FK users | |
| `cancelled_at` | `timestamptz` | NULL | |
| `created_by`, `updated_by` | `bigint` | NOT NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |
| `deleted_by` | `bigint` | NULL, FK users | |
| `deleted_at` | `timestamptz` | NULL | |

### 7.2. `appointments`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `appointment_code` | `varchar(32)` | NOT NULL, UQ | `APT-YYYYMMDD-{SEQ}` |
| `doctor_schedule_id` | `bigint` | NOT NULL, FK schedules | Composite FK cùng doctor/room |
| `patient_id` | `bigint` | NOT NULL, FK patients | |
| `doctor_profile_id` | `bigint` | NOT NULL, FK doctor_profiles | Snapshot ID |
| `specialty_id` | `bigint` | NOT NULL, FK specialties | |
| `room_id` | `bigint` | NOT NULL, FK rooms | Snapshot ID |
| `doctor_code_at_booking` | `varchar(32)` | NOT NULL | |
| `doctor_name_at_booking` | `varchar(150)` | NOT NULL | |
| `specialty_code_at_booking` | `varchar(34)` | NOT NULL | |
| `specialty_name_at_booking` | `varchar(200)` | NOT NULL | |
| `room_code_at_booking` | `varchar(23)` | NOT NULL | |
| `room_name_at_booking` | `varchar(200)` | NOT NULL | |
| `start_at`, `end_at` | `timestamptz` | NOT NULL | |
| `appointment_range` | `tstzrange` | generated stored | Half-open |
| `status` | `appointment_status_enum` | NOT NULL | |
| `source` | `appointment_source_enum` | NOT NULL | |
| `approval_mode_at_booking` | `appointment_approval_mode_enum` | NOT NULL | |
| `check_in_early_minutes_at_booking` | `smallint` | NOT NULL | 0..240 |
| `check_in_late_minutes_at_booking` | `smallint` | NOT NULL | 0..240 |
| `no_show_grace_minutes_at_booking` | `smallint` | NOT NULL | `<= late` |
| `booked_by_user_id` | `bigint` | NOT NULL, FK users | |
| `follow_up_from_encounter_id` | `bigint` | NULL, FK encounters deferred | |
| `idempotency_key` | `varchar(128)` | NOT NULL | Scoped unique |
| `idempotency_request_hash` | `char(64)` | NOT NULL | SHA-256 hex |
| `version` | `bigint` | NOT NULL default `1` | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Holding row không được delete |

### 7.3. `appointment_status_histories`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Append-only |
| `appointment_id` | `bigint` | NOT NULL, FK appointments | |
| `from_status` | `appointment_status_enum` | NULL | Null event đầu |
| `to_status` | `appointment_status_enum` | NOT NULL | |
| `actor_user_id` | `bigint` | NULL, FK users | Conditional theo actor type |
| `actor_type` | `appointment_actor_type_enum` | NOT NULL | |
| `reason` | `varchar(1000)` | NULL | Conditional theo transition |
| `metadata` | `jsonb` | NOT NULL default `{}` | Không chứa clinical data |
| `created_at` | `timestamptz` | NOT NULL | |

## 8. Service catalog

### 8.1. `services`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `code` | `varchar(34)` | NOT NULL, UQ | `SVC-[A-Z0-9-]{2,30}` |
| `name`, `normalized_name` | `varchar(200)` | NOT NULL | normalized name UQ |
| `service_type` | `service_type_enum` | NOT NULL | |
| `specialty_id` | `bigint` | conditional, FK specialties | Chỉ CONSULTATION |
| `unit_label` | `varchar(50)` | NOT NULL default `Lần` | |
| `description` | `varchar(1000)` | NULL | |
| `status` | `catalog_status_enum` | NOT NULL default `INACTIVE` | |
| `created_by_user_id`, `updated_by_user_id` | `bigint` | NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |
| `deleted_at` | `timestamptz` | NULL | |
| `deleted_by_user_id` | `bigint` | NULL, FK users | |
| `archive_reason` | `varchar(1000)` | NULL | |

### 8.2. `service_price_versions`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable trừ cancel future |
| `service_id` | `bigint` | NOT NULL, FK services | |
| `effective_from` | `timestamptz` | NOT NULL | |
| `unit_price_vnd` | `numeric(14,0)` | NOT NULL | `>= 0` |
| `currency` | `char(3)` | NOT NULL default `VND` | |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at` | `timestamptz` | NOT NULL | |
| `cancellation_reason` | `varchar(500)` | NULL | |
| `cancelled_by_user_id` | `bigint` | NULL, FK users | |
| `cancelled_at` | `timestamptz` | NULL | |

## 9. Medical Encounter

### 9.1. `medical_encounters`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `encounter_code` | `varchar(32)` | NOT NULL, UQ | `ENC-YYYYMMDD-{SEQ}` |
| `appointment_id` | `bigint` | NOT NULL, UQ, FK appointments | Composite source FK |
| `patient_id` | `bigint` | NOT NULL, FK patients | |
| `doctor_profile_id` | `bigint` | NOT NULL, FK doctor_profiles | |
| `specialty_id` | `bigint` | NOT NULL, FK specialties | |
| `patient_code_at_start` | `varchar(32)` | NOT NULL | |
| `patient_name_at_start` | `varchar(150)` | NOT NULL | |
| `patient_date_of_birth_at_start` | `date` | NULL | |
| `patient_gender_at_start` | `varchar(20)` | NULL | Snapshot display code |
| `doctor_code_at_start` | `varchar(32)` | NOT NULL | |
| `doctor_name_at_start` | `varchar(150)` | NOT NULL | |
| `specialty_code_at_start` | `varchar(34)` | NOT NULL | |
| `specialty_name_at_start` | `varchar(200)` | NOT NULL | |
| `status` | `encounter_status_enum` | NOT NULL default `DRAFT` | |
| `chief_complaint` | `text` | NULL | Max 1,000; required finalize |
| `symptoms` | `text` | NULL | Max 4,000 |
| `diagnosis` | `text` | NULL | Max 4,000; required finalize |
| `conclusion` | `text` | NULL | Max 4,000; required finalize |
| `clinical_notes` | `text` | NULL | Max 8,000 |
| `follow_up_recommendation` | `text` | NULL | Max 2,000 |
| `started_at` | `timestamptz` | NOT NULL | |
| `finalized_at` | `timestamptz` | NULL | Conditional |
| `finalized_by_user_id` | `bigint` | NULL, FK users | Conditional |
| `version` | `bigint` | NOT NULL default `1` | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `is_deleted` | `boolean` | NOT NULL default `false` | Luôn false trong MVP |

### 9.2. `encounter_vital_signs`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `encounter_id` | `bigint` | NOT NULL, FK encounters | |
| `temperature_c` | `numeric(4,1)` | NULL | Non-negative DB check |
| `systolic_bp`, `diastolic_bp` | `smallint` | NULL | Systolic >= diastolic nếu đủ cặp |
| `heart_rate_bpm`, `respiratory_rate_bpm` | `smallint` | NULL | Non-negative |
| `spo2_percent` | `numeric(5,2)` | NULL | 0..100 |
| `weight_kg` | `numeric(7,3)` | NULL | Non-negative |
| `height_cm` | `numeric(6,2)` | NULL | Non-negative |
| `pain_score` | `smallint` | NULL | 0..10 |
| `notes` | `varchar(1000)` | NULL | |
| `measured_at` | `timestamptz` | NOT NULL | |
| `recorded_by_user_id` | `bigint` | NOT NULL, FK users | |
| `version` | `bigint` | NOT NULL default `1` | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |

### 9.3. `medical_encounter_status_histories`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Append-only |
| `encounter_id` | `bigint` | NOT NULL, FK encounters | |
| `from_status` | `encounter_status_enum` | NULL | |
| `to_status` | `encounter_status_enum` | NOT NULL | |
| `actor_user_id` | `bigint` | NOT NULL, FK users | |
| `metadata` | `jsonb` | NOT NULL default `{}` | Redacted |
| `created_at` | `timestamptz` | NOT NULL | |

### 9.4. `medical_encounter_amendments`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Append-only |
| `encounter_id` | `bigint` | NOT NULL, FK encounters | |
| `sequence_number` | `integer` | NOT NULL | UQ trong encounter, `>= 1` |
| `amendment_type` | `encounter_amendment_type_enum` | NOT NULL | |
| `reason` | `text` | NOT NULL | 1..1,000 |
| `content` | `text` | NOT NULL | 1..8,000 |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at` | `timestamptz` | NOT NULL | |

### 9.5. `encounter_services`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `encounter_id` | `bigint` | NOT NULL, FK encounters | |
| `service_id` | `bigint` | NOT NULL, FK services | |
| `service_price_version_id_at_order` | `bigint` | NOT NULL | Composite FK tới price+service |
| `service_code_at_order` | `varchar(34)` | NOT NULL | Snapshot |
| `service_name_at_order` | `varchar(200)` | NOT NULL | Snapshot |
| `unit_label_at_order` | `varchar(50)` | NOT NULL | Snapshot |
| `source` | `encounter_service_source_enum` | NOT NULL | |
| `quantity` | `smallint` | NOT NULL | 1..999 |
| `status` | `encounter_service_status_enum` | NOT NULL default `ACTIVE` | |
| `assigned_by_user_id` | `bigint` | NULL, FK users | Conditional |
| `assigned_at` | `timestamptz` | NOT NULL | |
| `removed_by_user_id` | `bigint` | NULL, FK users | |
| `removed_at` | `timestamptz` | NULL | |
| `removal_reason` | `varchar(500)` | NULL | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |

## 10. Medicine và inventory

### 10.1. `medicines`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `code` | `varchar(34)` | NOT NULL, UQ | `MED-[A-Z0-9-]{2,30}` |
| `name`, `normalized_name` | `varchar(200)` | NOT NULL | normalized name UQ |
| `active_ingredient`, `dosage_form`, `strength` | `varchar(200)` | NOT NULL | |
| `dispense_unit` | `varchar(50)` | NOT NULL | |
| `description` | `varchar(1000)` | NULL | |
| `low_stock_threshold` | `numeric(14,3)` | NOT NULL default `0` | |
| `status` | `catalog_status_enum` | NOT NULL default `INACTIVE` | |
| `created_by_user_id`, `updated_by_user_id` | `bigint` | NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |
| `is_deleted` | `boolean` | NOT NULL default `false` | |
| `deleted_at` | `timestamptz` | NULL | |
| `deleted_by_user_id` | `bigint` | NULL, FK users | |
| `archive_reason` | `varchar(1000)` | NULL | |

### 10.2. `medicine_price_versions`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable trừ cancel future |
| `medicine_id` | `bigint` | NOT NULL, FK medicines | |
| `effective_from` | `timestamptz` | NOT NULL | |
| `unit_sale_price_vnd` | `numeric(14,0)` | NOT NULL | `>= 0` |
| `currency` | `char(3)` | NOT NULL default `VND` | |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at` | `timestamptz` | NOT NULL | |
| `cancellation_reason` | `varchar(500)` | NULL | |
| `cancelled_by_user_id` | `bigint` | NULL, FK users | |
| `cancelled_at` | `timestamptz` | NULL | |

### 10.3. `stock_receipts`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable header |
| `receipt_code` | `varchar(32)` | NOT NULL, UQ | `RCV-YYYYMMDD-{SEQ}` |
| `supplier_name` | `varchar(200)` | NULL | |
| `supplier_document_ref` | `varchar(100)` | NULL | |
| `received_at` | `timestamptz` | NOT NULL | |
| `note` | `varchar(1000)` | NULL | |
| `received_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at` | `timestamptz` | NOT NULL | |

### 10.4. `medicine_batches`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `medicine_id` | `bigint` | NOT NULL, FK medicines | |
| `lot_number` | `varchar(100)` | NOT NULL | UQ trong medicine |
| `manufacture_date` | `date` | NULL | |
| `expires_on` | `date` | NOT NULL | |
| `initial_received_quantity` | `numeric(14,3)` | NOT NULL | `> 0` |
| `unit_purchase_cost_vnd` | `numeric(14,0)` | NOT NULL | `>= 0` |
| `quantity_on_hand` | `numeric(14,3)` | NOT NULL | `>= 0` |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |

### 10.5. `stock_receipt_items`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable |
| `stock_receipt_id` | `bigint` | NOT NULL, FK receipts | |
| `medicine_id` | `bigint` | NOT NULL, FK medicines | |
| `medicine_batch_id` | `bigint` | NOT NULL, UQ, FK batches | 1:1 item–batch |
| `lot_number` | `varchar(100)` | NOT NULL | Snapshot |
| `manufacture_date` | `date` | NULL | Snapshot |
| `expires_on` | `date` | NOT NULL | Snapshot |
| `received_quantity` | `numeric(14,3)` | NOT NULL | `> 0` |
| `unit_purchase_cost_vnd` | `numeric(14,0)` | NOT NULL | `>= 0` |
| `created_at` | `timestamptz` | NOT NULL | |

### 10.6. `inventory_adjustments`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable header |
| `adjustment_code` | `varchar(32)` | NOT NULL, UQ | `ADJ-YYYYMMDD-{SEQ}` |
| `reason_code` | `inventory_adjustment_reason_enum` | NOT NULL | |
| `reason_detail` | `varchar(500)` | NOT NULL | |
| `correction_of_adjustment_id` | `bigint` | NULL, self FK | Không self-reference |
| `adjusted_by_user_id` | `bigint` | NOT NULL, FK users | |
| `adjusted_at`, `created_at` | `timestamptz` | NOT NULL | |

### 10.7. `inventory_adjustment_items`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable |
| `inventory_adjustment_id` | `bigint` | NOT NULL, FK adjustments | |
| `medicine_batch_id` | `bigint` | NOT NULL, FK batches | |
| `direction` | `inventory_direction_enum` | NOT NULL | |
| `quantity` | `numeric(14,3)` | NOT NULL | `> 0` |
| `created_at` | `timestamptz` | NOT NULL | |

### 10.8. `stock_movements`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Append-only ledger |
| `medicine_batch_id` | `bigint` | NOT NULL, FK batches | Composite FK cùng medicine |
| `medicine_id` | `bigint` | NOT NULL, FK medicines | |
| `movement_type` | `stock_movement_type_enum` | NOT NULL | |
| `quantity_delta` | `numeric(14,3)` | NOT NULL | Khác 0, sign theo type |
| `quantity_before`, `quantity_after` | `numeric(14,3)` | NOT NULL | `>= 0` |
| `reference_type` | `stock_reference_type_enum` | NOT NULL | Polymorphic source |
| `reference_id` | `bigint` | NOT NULL | |
| `occurred_at` | `timestamptz` | NOT NULL | |
| `created_by_user_id` | `bigint` | NULL, FK users | Null/system cho SALE_OUT |
| `created_at` | `timestamptz` | NOT NULL | |

## 11. Prescription

### 11.1. `prescription_series`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `prescription_code` | `varchar(32)` | NOT NULL, UQ | `RX-YYYYMMDD-{SEQ}` |
| `encounter_id` | `bigint` | NOT NULL, FK encounters | |
| `status` | `prescription_series_status_enum` | NOT NULL default `DRAFT` | |
| `current_issued_prescription_id` | `bigint` | NULL, FK prescriptions deferred | Composite series guard |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |

### 11.2. `prescriptions`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `series_id` | `bigint` | NOT NULL, FK series | |
| `version_number` | `integer` | NOT NULL | Bắt đầu 1 |
| `version_code` | `varchar(48)` | NOT NULL, UQ | `{prescription_code}-V{n}` |
| `source_prescription_id` | `bigint` | NULL, self FK | Replacement source |
| `status` | `prescription_status_enum` | NOT NULL default `DRAFT` | |
| `overall_note` | `varchar(1000)` | NULL | |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `issued_by_user_id`, `discarded_by_user_id` | `bigint` | NULL, FK users | |
| `issued_at`, `superseded_at`, `discarded_at` | `timestamptz` | NULL | Conditional theo state |
| `discard_reason` | `varchar(500)` | NULL | |
| `version` | `bigint` | NOT NULL default `1` | |

### 11.3. `prescription_items`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `prescription_id` | `bigint` | NOT NULL, FK prescriptions | |
| `medicine_id` | `bigint` | NOT NULL, FK medicines | |
| `medicine_code_at_prescription` | `varchar(34)` | NOT NULL | Snapshot |
| `medicine_name_at_prescription` | `varchar(200)` | NOT NULL | Snapshot |
| `dosage_form_at_prescription`, `strength_at_prescription` | `varchar(200)` | NULL | Snapshot |
| `dispense_unit_at_prescription` | `varchar(50)` | NOT NULL | Snapshot |
| `quantity` | `smallint` | NOT NULL | 1..999 |
| `usage_instruction` | `varchar(1000)` | NOT NULL | |
| `notes` | `varchar(500)` | NULL | |
| `sort_order` | `smallint` | NOT NULL | `>= 1` |
| `is_removed` | `boolean` | NOT NULL default `false` | |
| `removed_by_user_id` | `bigint` | NULL, FK users | |
| `removed_at` | `timestamptz` | NULL | |
| `removal_reason` | `varchar(500)` | NULL | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |

## 12. Billing và payment

### 12.1. `invoices`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable financial header trừ state |
| `invoice_code` | `varchar(32)` | NOT NULL, UQ | `INV-YYYYMMDD-{SEQ}` |
| `encounter_id` | `bigint` | NOT NULL, FK encounters | |
| `patient_id` | `bigint` | NOT NULL, FK patients | |
| `patient_code_at_invoice` | `varchar(32)` | NOT NULL | Snapshot |
| `patient_name_at_invoice` | `varchar(150)` | NOT NULL | Snapshot |
| `status` | `invoice_status_enum` | NOT NULL default `UNPAID` | |
| `subtotal_vnd`, `total_vnd` | `numeric(14,0)` | NOT NULL | `>= 0`, bằng nhau MVP |
| `created_by_user_id` | `bigint` | NOT NULL, FK users | |
| `created_at` | `timestamptz` | NOT NULL | |
| `paid_at` | `timestamptz` | NULL | Conditional |
| `version` | `bigint` | NOT NULL default `1` | |

### 12.2. `invoice_items`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | Immutable |
| `invoice_id` | `bigint` | NOT NULL, FK invoices | |
| `item_type` | `invoice_item_type_enum` | NOT NULL | |
| `encounter_service_id` | `bigint` | NULL, FK encounter_services | SERVICE source |
| `prescription_item_id` | `bigint` | NULL, FK prescription_items | MEDICINE source |
| `prescription_id` | `bigint` | NULL, FK prescriptions | MEDICINE version |
| `service_price_version_id` | `bigint` | NULL, FK service prices | |
| `medicine_price_version_id` | `bigint` | NULL, FK medicine prices | |
| `item_code_at_invoice` | `varchar(34)` | NOT NULL | Snapshot |
| `item_name_at_invoice` | `varchar(200)` | NOT NULL | Snapshot |
| `dosage_form_at_invoice`, `strength_at_invoice` | `varchar(200)` | NULL | MEDICINE |
| `unit_label_at_invoice` | `varchar(50)` | NOT NULL | |
| `quantity` | `smallint` | NOT NULL | 1..999 |
| `unit_price_snapshot_vnd`, `line_total_vnd` | `numeric(14,0)` | NOT NULL | Exact arithmetic |
| `created_at` | `timestamptz` | NOT NULL | |

### 12.3. `payments`

| Column | Type | Null/Default | Ghi chú |
| --- | --- | --- | --- |
| `id` | `bigint identity` | PK | |
| `payment_code` | `varchar(32)` | NOT NULL, UQ | `PAY-YYYYMMDD-{SEQ}` |
| `invoice_id` | `bigint` | NOT NULL, UQ, FK invoices | Exact one payment |
| `payment_method` | `payment_method_enum` | NOT NULL | Immutable |
| `status` | `payment_status_enum` | NOT NULL default `PENDING_CONFIRMATION` | |
| `expected_amount_vnd` | `numeric(14,0)` | NOT NULL | Invoice total snapshot |
| `bank_account_id_at_creation` | `bigint` | NULL, FK bank_accounts | BANK only |
| `bank_bin_at_creation` | `char(6)` | NULL | Snapshot |
| `account_number_at_creation` | `varchar(30)` | NULL | Snapshot |
| `account_name_at_creation` | `varchar(200)` | NULL | Snapshot |
| `transfer_content` | `varchar(100)` | NULL | `DNCS-{invoice_code}` |
| `qr_payload` | `text` | NULL | Backend-generated snapshot |
| `bank_transaction_reference` | `varchar(100)` | NULL | BANK confirmation |
| `bank_transaction_reference_normalized` | `varchar(100)` | NULL | Unique confirmed per account |
| `external_reference` | `varchar(100)` | NULL | |
| `confirmed_amount_vnd` | `numeric(14,0)` | NULL | Exact amount |
| `confirmed_by_user_id` | `bigint` | NULL, FK users | |
| `confirmed_at` | `timestamptz` | NULL | |
| `created_at`, `updated_at` | `timestamptz` | NOT NULL | |
| `version` | `bigint` | NOT NULL default `1` | |

## 13. Business code sequence

Tạo sequence riêng, không cycle và không reset:

| Sequence | Code sử dụng |
| --- | --- |
| `appointment_code_seq` | `APT-YYYYMMDD-{SEQ}` |
| `encounter_code_seq` | `ENC-YYYYMMDD-{SEQ}` |
| `prescription_code_seq` | `RX-YYYYMMDD-{SEQ}` |
| `invoice_code_seq` | `INV-YYYYMMDD-{SEQ}` |
| `payment_code_seq` | `PAY-YYYYMMDD-{SEQ}` |
| `stock_receipt_code_seq` | `RCV-YYYYMMDD-{SEQ}` |
| `inventory_adjustment_code_seq` | `ADJ-YYYYMMDD-{SEQ}` |

`PAT`, `STF`, `DOC` lấy trực tiếp identity ID và zero-pad tối thiểu 8 chữ số. Code ngày lấy ngày địa phương `Asia/Ho_Chi_Minh` tại thời điểm command và sequence toàn cục pad tối thiểu 6 chữ số.

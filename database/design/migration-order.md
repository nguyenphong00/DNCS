# Migration Order — DNCS MVP

## 1. Mục tiêu

Thứ tự này phá mọi vòng FK, cho phép triển khai theo P0–P6 và bảo đảm mỗi phase có schema, seed, constraint và test tương ứng. Tên file dùng timestamp hoặc số thứ tự tăng dần; ví dụ dưới đây dùng `0001_...` để thể hiện dependency.

## 2. Quy tắc migration

- Mỗi migration có checksum và được ghi vào `schema_migrations` bởi migration tool của backend Go.
- Migration đã chạy ở môi trường dùng chung là immutable; sửa bằng forward migration mới.
- Mỗi file chạy trong transaction trừ operation PostgreSQL bắt buộc ngoài transaction. Initial build không cần `CREATE INDEX CONCURRENTLY`.
- DDL table tạo cùng local PK/FK/UQ/CHECK an toàn; deferred FK, exclusion, trigger và index lớn tách file để lỗi dễ khoanh vùng.
- Không seed secret/password/token trong SQL.
- Production rollback dùng forward-fix/restore backup. Down migration chỉ dùng cho local/CI trước khi release và không được xóa dữ liệu lịch sử.
- Chạy migration bằng owner role riêng; application role chỉ nhận quyền tối thiểu sau cùng.

## 3. Thứ tự migration chi tiết

> **Ghi chú implementation P0–P3:** bảng số `0001`–`0036` dưới đây là dependency order của clean install toàn MVP, không phải tên bất biến. Implementation hiện đóng gói thành `202609010001`–`202609010009`: P0 auth/seed, prerequisite dùng chung, P1 profile/master, P2 schedule/appointment và P3 service/clinical/FK vòng. Các bước logic 0017/0019 được đặt cùng transaction tạo bảng tương ứng. Migration tiếp theo phải dùng timestamp tăng dần và không được tạo lại object đã có.

### Foundation

| Order | Migration | Nội dung | Phụ thuộc/Gate |
| ---: | --- | --- | --- |
| 0001 | `enable_required_extensions` | `btree_gist`, `pg_trgm` | Extension availability check |
| 0002 | `create_domain_enums` | Toàn bộ enum tại `tables.md` | Không có data |
| 0003 | `create_lookup_tables` | `gender_codes` | Seed ở 0024 |
| 0004 | `create_business_code_sequences` | 7 sequence APT/ENC/RX/INV/PAY/RCV/ADJ | `NO CYCLE` |

Enum và extension không drop trong rollback production vì có thể được object/data khác sử dụng.

### P0 — Identity & operational foundation

| Order | Migration | Table/object | Ghi chú vòng phụ thuộc |
| ---: | --- | --- | --- |
| 0005 | `create_roles_and_permissions` | `roles`, `permissions` | Tạo `roles.created_by` nhưng chưa gắn FK users |
| 0006 | `create_users` | `users` | FK `users.role_id → roles` |
| 0007 | `create_auth_sessions` | `auth_sessions` | FK users, token hash UQ |
| 0008 | `create_authorization_relations` | `role_permissions`, `user_permission_overrides`, `user_role_histories` | Các actor FK đã có users |
| 0009 | `complete_identity_actor_fks` | Add FK `roles.created_by → users` và actor FK còn deferred | Phá cycle roles ↔ users |
| 0010 | `create_system_settings` | `system_settings` | Typed catalog checks |
| 0011 | `create_bank_accounts` | `bank_accounts` | Single-active partial UQ |
| 0012 | `create_audit_logs` | `audit_logs` | Actor FK users; target polymorphic |
| 0013 | `create_idempotency_records` | `idempotency_records` | Technical support table |

Gate P0 schema:

- Tạo được system role/user trong một transaction.
- Login lookup và refresh rotation constraint pass.
- Effective permission query trả đúng role + override.
- Setting catalog seed đầy đủ sau bước 0024.
- Audit row insert được nhưng update/delete bị chặn sau 0022.

### P1 — Master và profile

| Order | Migration | Table/object | Ghi chú |
| ---: | --- | --- | --- |
| 0014 | `create_staff_patient_profiles` | `staff_profiles`, `doctor_profiles`, `patients` | FK users/gender; absolute unique identity/code |
| 0015 | `create_clinic_master_data` | `specialties`, `rooms`, `doctor_specialties` | Relation partial UQ active |

Gate P1:

- User + staff/patient rollback nguyên tử khi profile lỗi.
- Phone/CCCD/BHYT/staff email unique vẫn giữ qua soft delete.
- Doctor-specialty remove rồi reassign tạo row lịch sử mới.

### P2 — Scheduling và appointment

| Order | Migration | Table/object | Ghi chú |
| ---: | --- | --- | --- |
| 0016 | `create_doctor_schedules` | `doctor_schedules`, generated range, local checks | Composite UQ schedule snapshot |
| 0017 | `add_schedule_exclusion_constraints` | Doctor/room active overlap | Cần `btree_gist`; test SQLSTATE `23P01` |
| 0018 | `create_appointments` | `appointments`, `appointment_status_histories`, generated range | Tạo `follow_up_from_encounter_id` nullable nhưng chưa FK |
| 0019 | `add_appointment_exclusion_constraints` | Doctor/patient/room holding overlap | Predicate theo status, không theo is_deleted |

Không tạo bảng `slots`; slot sinh động từ schedule.

Gate P2:

- Hai transaction tạo/activate schedule overlap chỉ một commit.
- Hai booking overlap doctor/patient/room chỉ một commit.
- Composite FK chặn appointment có doctor/room khác schedule.
- Idempotency booking replay đúng, khác payload bị từ chối.

### P3 — Service và clinical

| Order | Migration | Table/object | Ghi chú |
| ---: | --- | --- | --- |
| 0020 | `create_service_catalog` | `services`, `service_price_versions` | Consultation active partial UQ, price composite UQ |
| 0021 | `create_medical_encounters` | `medical_encounters`, `encounter_vital_signs`, `medical_encounter_status_histories`, `medical_encounter_amendments`, `encounter_services` | Composite FK appointment snapshot và service price |
| 0022 | `complete_appointment_encounter_cycle` | FK `appointments.follow_up_from_encounter_id → medical_encounters.id` | Thêm sau khi encounters tồn tại |

Gate P3:

- Appointment tối đa một encounter và snapshot IDs phải khớp.
- Start/finalize transition composite transaction pass.
- Finalize thiếu vital/required clinical field bị rollback.
- Chỉ một consultation service active cho specialty và một active service/encounter.

Implementation gate P3 đã đạt ngày 2026-09-01 trên PostgreSQL 16 cho cả clean install và upgrade P0–P2 → P3; migration thực tế là `202609010007`–`202609010009`.

### P4 — Medicine, inventory và prescription

| Order | Migration | Table/object | Ghi chú |
| ---: | --- | --- | --- |
| 0023 | `create_medicine_catalog` | `medicines`, `medicine_price_versions` | Price composite UQ |
| 0024 | `create_inventory_documents` | `stock_receipts`, `medicine_batches`, `stock_receipt_items`, `inventory_adjustments`, `inventory_adjustment_items`, `stock_movements` | Batch trước receipt item để tạo 1:1 FK; source movement polymorphic |
| 0025 | `create_prescriptions` | `prescription_series`, `prescriptions`, `prescription_items` | Series current FK chưa thêm lúc create series |
| 0026 | `complete_prescription_cycles` | Deferred composite current-issued FK và same-series source FK | Cần unique `(prescriptions.id,series_id)` |

Gate P4:

- Receipt tạo header/items/batch/RECEIPT_IN nguyên tử.
- Adjustment OUT không âm, movement balance khớp.
- Chỉ một DRAFT và một ISSUED mỗi series.

Implementation gate P4 đã đạt ngày 2026-09-01 trên PostgreSQL 16 cho clean install, upgrade P0–P3 → P4 và migration rerun; migration thực tế là `202609010010`–`202609010013`. Integration suite bao phủ receipt/adjustment idempotency, balance/ledger, concurrent OUT, FEFO/full rollback và prescription issue/replacement/current-version scope.
- Replacement issue supersede/current update nguyên tử.
- Issued prescription/item/history immutable.

### P5 — Billing và payment

| Order | Migration | Table/object | Ghi chú |
| ---: | --- | --- | --- |
| 0027 | `create_billing_tables` | `invoices`, `invoice_items`, `payments` | Source partial UQ, one unpaid, payment method/state checks |
| 0028 | `add_billing_composite_fks` | Encounter/patient, service source/price, prescription item/version | Medicine price ownership còn service guard vì invoice item không lặp medicine_id |
| 0029 | `add_financial_reference_indexes` | Bank reference confirmed UQ và financial query indexes | Không dùng raw bank ref search |

Gate P5:

- Invoice source không bị charge hai lần qua nhiều invoice.
- Invoice luôn tạo cùng ít nhất một item và đúng một pending payment.
- Hai confirm đồng thời chỉ một thành công.
- BANK reference confirmed không tái sử dụng.
- Thiếu stock rollback invoice/payment/batch/movement/audit toàn bộ.

### Cross-cutting hardening

| Order | Migration | Nội dung | Ghi chú |
| ---: | --- | --- | --- |
| 0030 | `create_immutability_functions` | `fn_reject_update_delete`, price/state guard helpers tối thiểu | Function nhỏ, deterministic |
| 0031 | `attach_immutability_triggers` | Audit/history/ledger/document/invoice-item trigger | Chạy sau mọi table |
| 0032 | `create_operational_indexes_identity_platform` | Index mục 3–5 của `indexes.md` | Review duplicate trước create |
| 0033 | `create_operational_indexes_scheduling_clinical` | Index mục 6–7 | |
| 0034 | `create_operational_indexes_inventory_billing` | Index mục 8–10 | |
| 0035 | `grant_application_roles` | Owner/migrator/app read-write/audit read permissions | Không cấp generic DELETE |
| 0036 | `record_schema_baseline` | Comment/version metadata `MVP-BL-01`, table/column comments quan trọng | Không chứa secret |

Immutability trigger được tạo sau schema để tránh cản seed/migration DDL, nhưng trước khi application ghi dữ liệu.

Implementation gate P6 đã đạt ngày 2026-09-02 bằng forward migration `202609010018_p6_hardening_baseline.sql` và được tái xác minh ngày 2026-09-06 trên cả clean-install lẫn upgrade P5 → P6. Các function/trigger/index correctness 0030–0034 đã được kéo sớm vào migration P0–P5 sở hữu table tương ứng; migration 0018 hoàn tất bốn group role, DML/sequence grant tối thiểu, không generic `DELETE`, audit trace index và schema comment `MVP-BL-01`. Integration test xác minh API login → `/me` → logout-all chạy qua `SET ROLE dncs_app`, audit-reader isolation và audit/history/ledger privilege denial.

## 4. Seed order

Seed là versioned và idempotent theo natural key; không dùng delete-all/reinsert.

| Order | Seed | Nội dung |
| ---: | --- | --- |
| S001 | `seed_gender_codes` | MALE, FEMALE, OTHER, UNSPECIFIED |
| S002 | `seed_permissions` | Toàn bộ permission code/risk/module từ Authorization baseline |
| S003 | `seed_system_roles` | SUPER_ADMIN, ADMIN, DOCTOR, CASHIER, PATIENT với account scope/protected flag |
| S004 | `seed_role_permissions` | Ma trận mặc định versioned; SUPER_ADMIN được xử lý theo protected policy |
| S005 | `seed_system_settings` | 9 key typed và default từ Audit Settings |
| S006 | `seed_reference_master_data` | Specialty/room/service/medicine mẫu chỉ khi môi trường yêu cầu; production seed phải được chủ dự án duyệt |

Bootstrap super-admin không nằm trong static SQL seed vì cần password secret:

1. Chạy CLI/bootstrap command một lần qua secure input.
2. Tạo user + staff profile + role history + audit trong transaction.
3. `must_change_password=true`, không ghi password tạm vào log/migration output.
4. Command idempotent theo normalized username và từ chối tạo super admin thứ hai nếu policy không cho phép.

Implementation P0–P3: `backend/cmd/bootstrap-admin` nhận secret từ file/stdin; schema, transaction và concurrency suite đến hết Service/Clinical đã qua PostgreSQL 16.

## 5. Mapping migration → implementation phase

| Phase | Migration |
| --- | --- |
| P0 | 0001–0013, seed S001–S005 cần thiết |
| P1 | 0014–0015 |
| P2 | 0016–0019 |
| P3 | 0020–0022 |
| P4 | 0023–0026 |
| P5 | 0027–0029 |
| P6 | 0030–0036, full verification/load/security test |

Các index và trigger cần cho correctness của phase phải được kéo sớm vào cùng release phase nếu application được triển khai từng phần. Số thứ tự trên biểu diễn clean install cuối cùng, không cho phép chạy application P2 trước exclusion constraint P2.

## 6. Rollback strategy

### Local/CI trước release

- Down theo thứ tự ngược, drop child FK/index/trigger trước table.
- Drop FK vòng ở 0026/0022 trước khi drop table hai phía.
- Không drop extension nếu database dùng chung.
- Enum chỉ drop sau khi mọi column dùng enum đã drop.

### Staging/production sau release

- Không down migration phá dữ liệu domain.
- Dùng forward migration sửa constraint/index/function.
- Nếu migration DDL lỗi giữa release, transaction rollback file hiện tại; các file trước vẫn được ghi checksum.
- Nếu lỗi logic đã ghi dữ liệu, dừng writer, backup/snapshot, viết data correction + forward constraint; không sửa trực tiếp history/ledger/audit.
- Thay đổi column lớn dùng expand/contract: add nullable/new column → dual write/backfill → validate → switch read → set constraint → bỏ column cũ ở release sau.

## 7. Verification bắt buộc

### Schema catalog

- Đếm đúng 42 table application và đối chiếu column/type/null/default.
- Snapshot `pg_constraint`, `pg_indexes`, enum values và extensions.
- Mọi FK child quan trọng có supporting index hoặc lý do rõ ràng.
- Không có `ON DELETE CASCADE` trong schema domain.

### Constraint test

- Duplicate username/phone/patient identity/master code.
- Role scope/account type mismatch qua service transaction.
- Schedule và appointment exclusion với hai connection đồng thời.
- One-active bank, active consultation service, one draft/issued prescription, one unpaid invoice.
- Invoice source dedup, bank reference dedup, stock movement allocation dedup.
- Append-only/immutable table từ chối UPDATE/DELETE.

### Transaction/concurrency test

- Register patient rollback khi profile insert lỗi.
- Refresh token rotation double-submit.
- Booking same/different idempotency key.
- Start/finalize encounter concurrent transition.
- Replacement prescription issue concurrent request.
- Receipt/adjustment/payment cùng tranh chấp batch.
- Hai payment confirm cùng invoice; retry sau timeout trả cùng representation.
- Required audit insert failure làm rollback business mutation.

### Migration test

1. Clean install từ database rỗng.
2. Chạy lại migration command không thay đổi schema/data.
3. Seed chạy lại không duplicate.
4. Dump schema rồi so sánh với snapshot mong đợi.
5. Với mỗi release sau này, test upgrade từ version production gần nhất, không chỉ clean install.

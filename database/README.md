# DNCS Database Design

## 1. Trạng thái

| Thuộc tính | Giá trị |
| --- | --- |
| Loại tài liệu | Physical database design plan |
| Trạng thái | Thiết kế `0.1.3`; migration P0–P6 và clean-install/upgrade/concurrency/security gate PostgreSQL 16 đã hoàn thành |
| Ngày | 2026-09-06 |
| Nguồn nghiệp vụ | Baseline [`MVP-BL-01`](../Docs/idea/idea_01.md) |
| Database mục tiêu | PostgreSQL 16; không dùng tính năng mới hơn PostgreSQL 16 |
| Phạm vi | Một cơ sở, MVP |
| Số bảng | 42, gồm 41 bảng nghiệp vụ và 1 bảng hạ tầng idempotency |

Bộ tài liệu này hợp nhất logical model đang phân tán trong 13 feature spec và flow Payment & Stock Deduction. Thiết kế vật lý toàn MVP đã sẵn sàng; 18 migration timestamp `202609010001_*` đến `202609010018_*` hiện thực P0 Identity & Access đến P5 Settings/Audit/Billing/Payment và P6 least-privilege/trace baseline.

## 2. Artifact

| File | Nội dung |
| --- | --- |
| [`design/erd.md`](design/erd.md) | ERD tổng thể và ERD theo domain |
| [`design/tables.md`](design/tables.md) | Table catalog, column, kiểu dữ liệu, nullability và ownership |
| [`design/constraints.md`](design/constraints.md) | PK/FK/unique/check/exclusion, invariant xuyên bảng và lock order |
| [`design/indexes.md`](design/indexes.md) | Index catalog gắn với query/use case |
| [`design/migration-order.md`](design/migration-order.md) | Thứ tự migration, seed, rollback và verification |
| `migrations/` | Migration SQL thực thi được cho P0–P6, gồm seed, exclusion, catalog price, clinical, inventory ledger, prescription, financial guards và least-privilege baseline |
| `seeds/` | Seed tách riêng cho các phase sau; seed P0 đang versioned trong migration `202609010002` |
| `tests/` | Test manifest và hướng dẫn; integration test thực thi nằm tại `backend/internal/database/p1_p2_integration_test.go`, `p3_integration_test.go`, `p4_integration_test.go` và `p5_integration_test.go` |

### Migration đã hiện thực

| Migration | Phase | Nội dung |
| --- | --- | --- |
| `202609010001_p0_auth_schema.sql` | P0 | Identity, session, authorization relation và audit |
| `202609010002_p0_role_permission_seed.sql` | P0 | 5 system role, 75 permission và ma trận mặc định |
| `202609010003_p1_p2_prerequisites.sql` | P0/P1/P2 | Extension, enum, gender, typed setting, idempotency và sequence appointment |
| `202609010004_p1_profiles_master.sql` | P1 | Staff/doctor/patient profile, specialty, room, doctor-specialty |
| `202609010005_p2_doctor_schedules.sql` | P2 | Schedule range và doctor/room exclusion |
| `202609010006_p2_appointments.sql` | P2 | Appointment/history, snapshot FK và doctor/patient/room exclusion |
| `202609010007_p3_service_catalog.sql` | P3 | Service catalog, consultation uniqueness và immutable price versions |
| `202609010008_p3_medical_encounters.sql` | P3 | Encounter, vital signs, histories, amendments và service orders |
| `202609010009_p3_encounter_guards.sql` | P3 | FK vòng follow-up, price/assignment immutability và cross-table guards |
| `202609010010_p4_medicine_catalog.sql` | P4 | Medicine catalog, lifecycle và immutable sale-price version |
| `202609010011_p4_inventory_documents.sql` | P4 | Receipt, batch, adjustment, stock movement ledger và immutability guards |
| `202609010012_p4_prescriptions.sql` | P4 | Prescription series/version/item, partial uniqueness và snapshots |
| `202609010013_p4_prescription_guards.sql` | P4 | Deferred cycle FK và draft/history immutability guards |
| `202609010014_p5_settings_bank_accounts.sql` | P5 | Typed setting guards, bank account lifecycle và single-active invariant |
| `202609010015_p5_billing_tables.sql` | P5 | Invoice/item/payment snapshot, financial state và business-code sequence |
| `202609010016_p5_billing_guards.sql` | P5 | Financial immutability/transition guards và read indexes |
| `202609010017_p5_billing_relations.sql` | P5 | Encounter/patient/source composite guard và deferred aggregate completeness |
| `202609010018_p6_hardening_baseline.sql` | P6 | Group role/grant tối thiểu, không generic DELETE, audit request/correlation index và comment `MVP-BL-01` |

Gate P0–P6 đã được tái xác minh ngày 2026-09-06: clean-install từ database rỗng, upgrade P5 → P6, migration rerun, integration/concurrency/privacy/financial/FEFO/least-privilege/redaction/E2E suite, race detector và vet đều đạt trên PostgreSQL 16. `go test ./...` không có `TEST_DATABASE_URL` vẫn skip có chủ đích các test phụ thuộc PostgreSQL; CI luôn cấp database test riêng.

## 3. Quy ước vật lý

- Primary key dùng `bigint GENERATED ALWAYS AS IDENTITY`.
- Foreign key dùng `bigint`, mặc định `ON DELETE RESTRICT`; không dùng cascade cho dữ liệu domain.
- Thời điểm dùng `timestamptz`; backend/database lưu theo UTC, UI hiển thị `Asia/Ho_Chi_Minh`.
- Ngày nghiệp vụ như hạn dùng và ngày sinh dùng `date`.
- Tiền VND dùng `numeric(14,0)`; không dùng `float`/`double`.
- Số lượng kho dùng `numeric(14,3)`.
- State machine đóng dùng PostgreSQL enum riêng theo domain. `gender` dùng lookup table vì baseline xác định đây là danh mục dùng chung.
- Chuỗi chuẩn hóa (`normalized_*`) do application tạo bằng thuật toán đã chốt; database unique/index trên giá trị đã chuẩn hóa.
- Các bảng mutable có `version bigint NOT NULL DEFAULT 1` cho optimistic concurrency.
- Bảng lịch sử, ledger, giá đã hiệu lực, invoice item và audit là append-only/immutable theo [`constraints.md`](design/constraints.md).
- Business code có thể có gap. Không dùng `MAX(id) + 1`, không reset sequence và không tái sử dụng code sau archive.
- PostgreSQL extensions bắt buộc: `btree_gist` cho exclusion constraint và `pg_trgm` cho tìm kiếm tên patient.
- Mỗi môi trường dùng một database riêng và object MVP nằm trong schema `public`; migration role sở hữu DDL, application role chỉ có DML/sequence usage cần thiết.
- MVP không dùng PostgreSQL Row Level Security. Authorization/resource scope vẫn bắt buộc ở backend; database grant chỉ là lớp least-privilege theo loại thao tác.

## 4. Quyết định physical design bổ sung

Các quyết định dưới đây hiện thực baseline nhưng không đổi semantics nghiệp vụ:

1. Thêm `gender_codes` để Staff và Patient dùng chung lookup giới tính.
2. Thêm `idempotency_records` làm hạ tầng dùng chung cho booking, receipt, adjustment, invoice create và payment confirm.
3. Dùng composite unique key/FK để bảo vệ snapshot ID có thể kiểm tra được trong cùng database, ví dụ appointment–schedule và encounter–appointment.
4. Vòng FK `appointments.follow_up_from_encounter_id` và `prescription_series.current_issued_prescription_id` được thêm bằng migration deferred sau khi hai phía đã tồn tại.
5. Polymorphic reference của `audit_logs` và `stock_movements` không có generic FK. Tính hợp lệ source được bảo vệ bằng command transaction, enum type, unique allocation và test.
6. Invariant phụ thuộc thời gian hiện tại, trạng thái row khác hoặc số lượng child không được nhét vào `CHECK`; chúng dùng service guard/row lock, và dùng trigger chỉ khi cần defense-in-depth.

## 5. Definition of ready cho migration

Thiết kế được xem là sẵn sàng viết migration khi:

- 42 bảng trong table catalog đã được review ownership và cardinality.
- Tất cả constraint được phân loại `DB`, `TRIGGER` hoặc `SERVICE/TRANSACTION`.
- Index có use case cụ thể và không trùng index do PK/unique/exclusion tự tạo.
- Migration order không còn vòng phụ thuộc chưa có bước deferred constraint.
- Seed role, permission, setting và gender có nguồn versioned.
- Có test plan cho booking/schedule overlap, idempotency, audit immutability, prescription version, invoice source deduplication và FEFO stock deduction.

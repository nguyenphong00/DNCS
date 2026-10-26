# DNCS Backend — P0–P6

## Trạng thái

Source hiện có:

- PostgreSQL schema cho `roles`, `permissions`, `users`, `auth_sessions`, `role_permissions`, `user_permission_overrides`, `user_role_histories` và `audit_logs`.
- Seed versioned gồm 5 system role, 75 permission và ma trận role mặc định theo `Docs/features/authorization.md`.
- JWT access token HS256 (mặc định 15 phút), opaque refresh token chỉ lưu SHA-256 hash, rotation có row lock và không kéo dài expiry gốc.
- Login STAFF/PATIENT với Argon2id (đọc được bcrypt legacy), lock tạm sau số lần sai cấu hình được, refresh, logout idempotent, logout-all và đổi mật khẩu.
- Middleware xác thực session hiện hành và middleware quyền theo thứ tự `SUPER_ADMIN → user override → role → default deny`.
- Scope hook bắt buộc để module nghiệp vụ kiểm tra ownership/phạm vi dữ liệu sau functional permission.
- Audit bắt buộc cho đổi mật khẩu và logout-all; audit/history có trigger append-only.
- P1 Staff/Doctor Profile: tạo aggregate với password tạm, lifecycle employment/doctor, archive/restore protected, optimistic update và dependency guard.
- P1 Patient: self-registration/counter creation, scope own/assigned/all, profile update, phone change thu hồi session và archive/restore aggregate.
- P1 Clinic Master: specialty, room, doctor-specialty, status/archive và dependency guard.
- P2 Scheduling: draft/active/cancel/archive, slot duration/window, doctor/room exclusion constraint và slot generation.
- P2 Appointment: slot search, booking idempotent, snapshot, patient limit, manual/auto approval, approve/reject/cancel/check-in/no-show và ba exclusion constraint.
- P3 Service Catalog: service CONSULTATION/PROCEDURE/OTHER, lifecycle activate/deactivate/archive, giá VND bất biến theo thời điểm và projection theo actor.
- P3 Medical Encounter: start atomic từ appointment, clinical draft, vital-sign timeline, service order, finalize/complete atomic, amendment append-only và patient/admin/doctor scope.
- Follow-up appointment là command idempotent riêng sau finalize, tạo trực tiếp ở trạng thái `CONFIRMED` và vẫn chịu toàn bộ slot/exclusion guard.
- P4 Medicine Inventory: catalog/giá VND versioned, receipt idempotent, batch/expiry, adjustment, exact balance, append-only ledger và internal FEFO allocation full rollback.
- P4 Prescription: series/version, draft item medicine snapshot, issue/supersede/discard, replacement clone, post-finalize existing-draft policy và actor projection.
- P5 Audit Settings: typed setting group/version, bank account identity bất biến, active handover nguyên tử và audit explorer cursor/filter.
- P5 Billing Payment: immutable invoice/item/payment snapshot, CASH/BANK VietQR, idempotent create/confirm, patient ownership và payment-time FEFO full rollback.
- P6 HTTP hardening: UUID request/correlation propagation, keyed audit IP hash, structured route log không chứa query/header/body, security headers, request deadline, panic recovery và JSON body `1 MiB`.
- P6 abuse/redaction guard: auth rate limit theo direct peer IP + identifier hash, field-safe validation error và audit writer từ chối secret/PII/clinical/bank payload hoặc JSON quá `16 KiB`.
- P6 PostgreSQL least privilege: group role owner/migrator/app/audit-reader; app không có generic `DELETE`, không update audit/history/ledger/document/invoice item; audit reader chỉ đọc audit.
- P6 acceptance: HTTP/PostgreSQL E2E chạy qua `dncs_app`, concurrency gate, công cụ load-smoke fail-closed và deployment/incident runbook.
- Post-P6 Account Administration: reset password một lần, lock/unlock/deactivate với optimistic version, protected/self guard, session revocation và required audit.
- CLI bootstrap super-admin một lần, idempotent theo normalized username, nhận password qua secret file/stdin và không ghi secret vào log.
- OpenAPI 3.1 `0.7.0` cho toàn bộ operation hiện có và hardening contract; test tự động buộc router và contract luôn khớp nhau.
- GitHub Actions chạy format, unit/integration test PostgreSQL 16, race detector và vet.

Không có tài khoản hoặc mật khẩu mặc định trong seed. Không chèn user production bằng static SQL.

Trạng thái verification: P0–P6 clean-install, upgrade P5 → P6, migration rerun, integration/concurrency/privacy/financial/FEFO/least-privilege/redaction/E2E suite, `go test`, `go test -race` và `go vet` đã được tái xác minh đạt trên PostgreSQL 16 ngày 2026-09-06.

## Chạy local

Yêu cầu Go 1.25+ và PostgreSQL 16.

```bash
cp .env.example .env
set -a
source .env
set +a
go run ./cmd/migrate
go run ./cmd/api
```

Nếu Docker đang chạy, có thể dựng PostgreSQL local:

```bash
docker compose up -d postgres
```

Các biến bắt buộc:

- `DATABASE_URL`: PostgreSQL connection string.
- `JWT_SECRET`: secret ngẫu nhiên tối thiểu 32 byte; production lấy từ secret manager.
- `AUDIT_IP_HASH_KEY`: key HMAC riêng tối thiểu 32 byte; không tái sử dụng JWT/database secret.
- `JWT_ISSUER`: issuer access token, mặc định `dncs-api`.
- `MIGRATIONS_DIR`: từ thư mục `backend` mặc định là `../database/migrations`.

`APP_MIGRATE_ON_START=false` là mặc định an toàn. Production nên chạy `cmd/migrate` bằng migration role riêng trước khi rollout API.

Timeout và auth rate-limit có default fail-safe trong `.env.example`. `REQUEST_TIMEOUT` phải nhỏ hơn `HTTP_WRITE_TIMEOUT`; cấu hình không an toàn làm API từ chối start. Backend không tin `X-Forwarded-For`: gateway/WAF phải rate-limit theo source IP thật, backend tiếp tục giới hạn theo direct peer IP và identifier hash.

### Flutter Web development CORS

Mặc định CORS tắt. Khi chạy Flutter Web development server khác origin API, chỉ allowlist các origin local cần thiết:

```bash
CORS_ALLOWED_ORIGINS=http://127.0.0.1:5173,http://localhost:5173
```

Backend chỉ cho phép origin HTTP(S) khai báo tường minh; không chấp nhận `*`. Preflight chỉ cho các method `GET`, `POST`, `PUT`, `PATCH`, `DELETE` và header `Authorization`, `Content-Type`, `Idempotency-Key`, `X-Request-ID`, `X-Correlation-ID`. Không đặt biến này ở staging/production nếu không có review bảo mật.

### Kế hoạch đề xuất: reverse proxy cùng origin

**Trạng thái: đề xuất, chưa triển khai.** Khi phát hành staging/production, gateway/reverse proxy nên phục vụ Flutter Web và API dưới cùng một origin:

```text
https://clinic.example/
  ├── /          → Flutter Web static assets
  └── /api/*     → DNCS API upstream
```

Mục tiêu là bỏ CORS khỏi browser path, terminate TLS tại gateway, áp HSTS/rate-limit/WAF theo source IP thật và giữ DNCS API ở private network. Trước khi triển khai cần chốt gateway (Nginx/Caddy/ingress), health check/timeout/body-limit, forwarding policy, cache policy cho asset so với `/api`, secret/TLS rotation và staging smoke. Khi mô hình này hoạt động, `CORS_ALLOWED_ORIGINS` vẫn để trống.

## Bootstrap super-admin

Sau migration, chạy command một lần với password lấy từ secret file. Dùng `BOOTSTRAP_ADMIN_PASSWORD_FILE=-` để đọc từ stdin. Command không nhận password qua argument, không in password và từ chối tạo identity super-admin thứ hai; chạy lại cùng normalized username trả kết quả idempotent.

```bash
DATABASE_URL='postgres://dncs:dncs-local-only@localhost:5432/dncs?sslmode=disable' \
BOOTSTRAP_ADMIN_USERNAME='root.admin' \
BOOTSTRAP_ADMIN_FULL_NAME='System Administrator' \
BOOTSTRAP_ADMIN_PASSWORD_FILE='/run/secrets/dncs-bootstrap-password' \
go run ./cmd/bootstrap-admin
```

## API hiện có

| Method | Endpoint | Xác thực | Ghi chú |
| --- | --- | --- | --- |
| `POST` | `/api/v1/auth/staff/login` | Public | `username`, `password` |
| `POST` | `/api/v1/auth/patient/login` | Public | `phone_number`, `password` |
| `POST` | `/api/v1/auth/refresh` | Refresh token | Rotation, giữ expiry gốc |
| `POST` | `/api/v1/auth/logout` | Refresh token | Idempotent, luôn `204` với token sai/cũ |
| `POST` | `/api/v1/auth/logout-all` | Bearer access token | Revoke tất cả session và tăng `token_version` |
| `POST` | `/api/v1/auth/change-password` | Bearer access token | Cho phép khi `must_change_password=true`; trả session thay thế |
| `POST` | `/api/v1/accounts/{userId}/reset-password` | `account.password.reset` | Password tạm một lần; revoke session |
| `POST` | `/api/v1/accounts/{userId}/lock` | `account.status.manage` | Khóa account; revoke session |
| `POST` | `/api/v1/accounts/{userId}/unlock` | `account.status.manage` | Mở khóa từ trạng thái `LOCKED` |
| `POST` | `/api/v1/accounts/{userId}/deactivate` | `account.status.manage` | Vô hiệu hóa account độc lập profile |
| `GET` | `/api/v1/me` | Bearer access token | Principal và effective permissions hiện hành |
| `GET` | `/health/live` | Public | Liveness |
| `GET` | `/health/ready` | Public | Readiness kiểm tra baseline P6 và quyền mới của migration `202610090020` |

Nhóm endpoint nghiệp vụ:

| Nhóm | Endpoint chính | Permission |
| --- | --- | --- |
| Staff/Doctor | `/api/v1/staff`, `/api/v1/staff/accounts`, lifecycle và doctor-profile | `staff.*`, `account.status.manage`, `user.role.assign` |
| Patient | `/api/v1/auth/patient/register`, `/api/v1/patients`, `/api/v1/me/patient-profile` | public hoặc `patient.*` |
| Clinic Master | `/api/v1/specialties`, `/api/v1/rooms`, `/api/v1/doctors/{id}/specialties` | `specialty.*`, `room.*`, `doctor_specialty.manage` |
| Scheduling | `/api/v1/doctor-schedules`, activate/cancel/archive | `doctor_schedule.*` |
| Appointment | `/api/v1/appointment-slots`, `/api/v1/appointments`, lifecycle commands | `appointment.*` |
| Service Catalog | `/api/v1/services`, lifecycle và price versions | `service.*` |
| Medical Encounter | appointment `start`/`complete`, `/api/v1/encounters`, vital signs, services, amendments | `encounter.*` |
| Follow-up | `/api/v1/encounters/{id}/follow-up-appointments` | `appointment.follow_up.create` |
| Medicine Inventory | `/api/v1/medicines`, `/api/v1/inventory/{summary,batches,receipts,adjustments,movements}` | `medicine.*`, `inventory.*` |
| Prescription | `/api/v1/prescription-series`, encounter series, draft item/issue/discard/replacement | `prescription.*` |
| Settings/Bank | `/api/v1/settings/{appointment,scheduling,inventory-alert,bank-accounts}` và bank lifecycle | `settings.*` |
| Audit | `/api/v1/audit-logs` và detail cursor-scoped | `audit_log.view` |
| Billing/Payment | `/api/v1/invoices`, `/api/v1/me/invoices`, create invoice, QR và confirm | `invoice.*`, `payment.*` |

Ví dụ login STAFF:

```json
{
  "username": "admin.user",
  "password": "replace-me"
}
```

Ví dụ bảo vệ handler của module kế tiếp:

```go
protected := authenticator.Middleware(
    authorizationMiddleware.Require(
        "patient.view.assigned_or_history",
        patientHandler,
        assignedOrHistoryScope,
    ),
)
```

JWT không chứa permission. `Require` đọc permission/override từ PostgreSQL ở từng protected request; thay đổi quyền có hiệu lực sau commit ở request kế tiếp.

Contract machine-readable nằm tại [`openapi.json`](openapi.json). Test `internal/httpapi/openapi_contract_test.go` từ chối route thiếu contract, route thừa hoặc `operationId` trùng.

## Kiểm thử

```bash
go test ./...
go vet ./...
```

Các test thông thường skip integration suite khi chưa có `TEST_DATABASE_URL`. Chạy gate PostgreSQL P0–P6:

```bash
TEST_DATABASE_URL='postgres://dncs:dncs-local-only@localhost:5432/dncs?sslmode=disable' \
  go test -count=1 -v ./internal/database
```

Suite bao phủ toàn bộ gate cũ cùng Service Catalog/Clinical, Medicine/Prescription, setting optimistic group, bank handover race, audit cursor/immutability, invoice/payment idempotency, source deduplication, QR snapshot, reference normalization, concurrent create/confirm, FEFO full rollback và P6 trace/rate-limit/timeout/redaction/least-privilege/E2E.

Chạy staging load-smoke trên endpoint GET an toàn:

```bash
go run ./cmd/load-smoke -url=https://staging.example/health/live \
  -requests=1000 -concurrency=32 -max-p95=250ms -max-error-rate=0
```

Quy trình role provisioning, deploy, rollback, backup/restore, alert và incident nằm tại [`../Docs/operations/deployment-runbook.md`](../Docs/operations/deployment-runbook.md). Workflow [`../.github/workflows/ci.yml`](../.github/workflows/ci.yml) luôn cấp PostgreSQL test, hardening/E2E gate, migration replay, race detector và vet.

## P0 UI: pending billing và prescription identity (09/10/2026)

Migration append-only `202610090020_p0_pending_billing_permission.sql` cấp `billing.pending_encounter.view` cho ADMIN/CASHIER. Chạy migrator trước khi rollout API; readiness yêu cầu baseline P6 và permission này tồn tại, hoạt động.

`GET /api/v1/billing/pending-encounters` trả các ca FINALIZED chưa có invoice PAID, mọi ngày, cũ nhất trước. Hỗ trợ `q`, `from`/`to`, `doctor_id`, `invoice_state=all|none|unpaid`, `page`/`page_size` (≤100). Projection có định danh, liên hệ và tiền; không có nội dung lâm sàng. Preview dùng transaction read-only với snapshot nhất quán và cùng bộ giải giá/nguồn tính tiền như create invoice; không allocate kho, không tạo invoice/audit. Giá thuốc có thể thay đổi giữa preview và thời điểm lập hóa đơn. Invoice UNPAID hiện có lấy tiền từ snapshot đã chốt.

Prescription series trả thêm `encounter_code`, `patient_code`, `patient_name`. Validation text quá dài trả 422 thay vì bỏ ghi chú. VietQR của hóa đơn mới dùng tên ASCII tối đa 25 ký tự và nội dung chữ-số `DNCS` + invoice ID; snapshot hóa đơn cũ giữ nguyên.

Chạy gate trên **database test riêng** (suite thay đổi dữ liệu test):

```bash
TEST_DATABASE_URL="$DNCS_TEST_DATABASE_URL" go test -p 1 -count=1 ./...
TEST_DATABASE_URL="$DNCS_TEST_DATABASE_URL" go test -race -p 1 -count=1 ./...
go vet ./...
go build ./cmd/...
```

Để tái xuất fixture tổng hợp cho Flutter:

```bash
TEST_DATABASE_URL="$DNCS_TEST_DATABASE_URL" \
DNCS_FIXTURE_OUTPUT="$PWD/../frontend/test/fixtures" \
  go test -p 1 -count=1 -run TestP0 ./internal/database
```

Chi tiết test, migration replay và nghiệm thu người dùng/ngân hàng: [`../plans/2026-10-09-p0-fixes/status.md`](../plans/2026-10-09-p0-fixes/status.md).

P2: list medicines/invoices/prescription-series/inventory-batches trả `has_more` từ lookahead; phạm vi đơn thuốc áp dụng trước khi chia trang. Audit trả tên người/nhãn nghiệp vụ và có hai endpoint picker cùng quyền `audit_log.view`. Các test `TestP2*` trong `internal/database/p2_ui_integration_test.go` kiểm tra ranh giới trang, quyền, lịch sử bất biến, pool một kết nối và `dncs_app`; chạy trên DB test độc lập. Chi tiết: [`../plans/2026-10-09-p2/status.md`](../plans/2026-10-09-p2/status.md).

P3 bổ sung metadata `page/page_size/has_more` cho patients, staff, appointments/me appointments, doctor-schedules, services, specialties và rooms; lọc và quyền áp dụng trước LIMIT/OFFSET, thứ tự hiện hành giữ nguyên. Pending billing chỉ trả envelope này khi `with_page=true`; client cũ vẫn nhận array. Lookahead pending không định giá dòng dư và không ghi dữ liệu. `TestP3*` kiểm tra dữ liệu vượt 50/100 dòng, thứ tự ổn định, scope, preview/invoice và tương thích array. Harness trình duyệt thật và cách chạy: [`../frontend/README.md`](../frontend/README.md). Báo cáo: [`../plans/2026-10-09-p3/status.md`](../plans/2026-10-09-p3/status.md).

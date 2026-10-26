# PostgreSQL verification — P0–P6

Integration suite nằm tại `backend/internal/database/p1_p2_integration_test.go`, `p3_integration_test.go`, `p4_integration_test.go`, `p5_integration_test.go`, `p6_integration_test.go` và HTTP E2E `backend/internal/httpapi/e2e_integration_test.go` để dùng trực tiếp migration runner, router và service production.

Suite bao phủ:

- Hai bootstrap super-admin đồng thời: chỉ một aggregate được tạo, lần còn lại trả cùng identity; identity thứ hai bị từ chối.
- Hai refresh đồng thời cùng token: đúng một rotation thành công, request còn lại và token cũ bị từ chối.
- Permission override vừa commit có hiệu lực ở lần kiểm tra kế tiếp và bỏ override quay về role grant ngay lập tức.
- Doctor không thể no-show appointment của doctor khác nhưng request trên appointment của mình qua được scope gate.
- Hai transaction tạo schedule overlap: chỉ một commit; kiểm tra exclusion doctor và room (`23P01`).
- Hai transaction tạo appointment overlap: tách riêng và kiểm tra exclusion doctor, patient và room (`23P01`).
- Hai booking đồng thời cùng `Idempotency-Key` và payload: một resource, một replay; dùng lại key với payload khác trả `IDEMPOTENCY_KEY_REUSED`.
- Booking chạy đua với cancel schedule nhiều vòng: không deadlock (`40P01`), chỉ một kết quả nghiệp vụ hợp lệ được commit.
- Hai Admin tạo phí khám active cùng specialty: đúng một commit; price version scheduled/cancelled và immutability guard hoạt động.
- Start thiếu phí khám rollback toàn bộ; start thành công tạo encounter + phí khám + appointment transition cùng transaction và retry không nhân đôi.
- Clinical draft, vital signs và doctor-order service obey scope/version; patient không list draft hoặc nhận `clinical_notes`/catalog metadata nội bộ.
- Finalize thiếu field/vital bị từ chối; hai finalize concurrent chỉ tạo một history và appointment/encounter luôn chuyển terminal cùng nhau.
- Amendment append-only đúng actor/type/sequence và follow-up là command idempotent riêng sau finalize.
- Receipt retry không nhân batch/movement; adjustment OUT khóa batch, không âm và ledger luôn khớp balance.
- FEFO chọn batch sớm hết hạn trước, thiếu một line rollback toàn bộ và không để SALE_OUT orphan.
- Prescription issue/replacement atomically giữ đúng một current ISSUED; patient chỉ đọc version đã phát hành.
- Appointment setting group update khóa/version nguyên tử; stale version và cross-rule no-show bị chặn.
- Bank active handover concurrent luôn giữ tối đa một ACTIVE; identity và archived state bất biến.
- Audit cursor/filter ổn định, invalid cursor/date bị chặn và audit row append-only.
- Invoice create/confirm idempotent; race khác key chỉ một winner, không nhân invoice/item/payment/audit.
- Invoice không reserve tồn; confirm mới trừ FEFO, thiếu thuốc rollback stock/movement/payment/invoice toàn bộ.
- BANK QR giữ snapshot qua handover; transaction reference NFKC/trim/case unique theo snapshot account.
- P6 role/grant không cho app generic `DELETE` hoặc sửa audit/history/ledger; audit reader chỉ đọc audit.
- Request/correlation/HMAC IP đi xuyên HTTP tới audit; sensitive/non-allowlisted/oversized audit payload làm rollback mutation.
- E2E login → `/me` → logout-all chạy bằng connection `SET ROLE dncs_app`; concurrency/load/security middleware gate đạt.

Chạy trên database PostgreSQL 16 dành riêng cho test:

```bash
cd DNCS/backend
TEST_DATABASE_URL='postgres://dncs:dncs-local-only@localhost:5432/dncs?sslmode=disable' \
  go test -count=1 -v ./internal/database
```

Không trỏ suite vào database dùng chung hoặc production vì test tạo fixture và không tự xóa dữ liệu để giữ bằng chứng khi điều tra lỗi. P0–P6 clean-install, upgrade P5 → P6, migration rerun và suite này đã được tái xác minh đạt trên PostgreSQL 16 ngày 2026-09-06.

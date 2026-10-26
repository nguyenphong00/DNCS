# DNCS Flutter frontend

Flutter web/mobile client cho DNCS backend OpenAPI `0.7.0`.

## Chức năng hiện có

- UI-0/UI-1 design system Material 3, responsive và accessibility baseline.
- Login STAFF/PATIENT, đăng ký bệnh nhân, secure session/refresh và đổi mật khẩu tạm.
- Navigation theo effective permission từ `GET /api/v1/me`.
- Bệnh nhân, nhân sự/bác sĩ và danh mục phòng khám.
- Lịch bác sĩ, slot search, booking và lifecycle lịch hẹn.
- Doctor workspace, bệnh án, sinh hiệu, dịch vụ, amendment và follow-up.
- Đơn thuốc và lịch sử phiên bản.
- Hóa đơn, CASH/BANK_TRANSFER, VietQR payload và xác nhận thanh toán.
- Catalog dịch vụ/thuốc, giá, tồn kho, lô, nhập kho, điều chỉnh và sổ kho.
- Settings, tài khoản nhận tiền, audit explorer và quyền hiệu lực.
- Quản trị trạng thái/reset mật khẩu tài khoản.

Chi tiết trạng thái từng màn hình: [`../Docs/flutter/screens-inventory.md`](../Docs/flutter/screens-inventory.md).  
Rule UI: [`../Docs/flutter/ui-guidelines.md`](../Docs/flutter/ui-guidelines.md).

## Chạy local

Backend mặc định tại `http://localhost:8080`:

```bash
flutter run
```

Web development server:

```bash
flutter run -d web-server --web-hostname=127.0.0.1 --web-port=5173 \
  --dart-define=API_BASE_URL=http://localhost:8080
```

Hoặc Chrome:

```bash
flutter run -d chrome --web-port=5173 \
  --dart-define=API_BASE_URL=http://localhost:8080
```

Backend không mở CORS mặc định. Browser cần frontend/API cùng origin hoặc reverse proxy/gateway cấu hình CORS.

Android emulator dùng host qua `10.0.2.2`:

```bash
flutter run --dart-define=API_BASE_URL=http://10.0.2.2:8080
```

Thiết bị thật và production phải dùng HTTPS.

## Kiểm tra

```bash
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test
flutter build web
```

Backend là nguồn quyết định authorization. Frontend chỉ sử dụng permission để tổ chức menu và action. Trước production cần UAT theo role và seed specialty, service, schedule, medicine price/batch cùng bank settings phù hợp.

## P0 T01–T23, P1 và regression gate

SDK đã kiểm chứng: Flutter **3.44.2**, Dart **3.12.2**; CI pin cùng phiên bản. Nếu máy có nhiều SDK, dùng đúng `flutter` và `dart` của SDK này rồi chạy `flutter pub get` để cập nhật package config.

```bash
flutter pub get
dart format --output=none --set-exit-if-changed lib test
flutter analyze
flutter test --coverage --reporter expanded
python3 tool/check_coverage.py
flutter build web --dart-define=API_BASE_URL=http://localhost:8080
```

Coverage gate bao gồm mọi file Dart trong `lib/`, không có danh sách loại trừ: toàn bộ ≥60% (P1), `design_system/` ≥80%; thiếu source trong LCOV cũng làm CI fail.

`test/support/fake_api.dart` dùng MockClient theo method/path, ghi nhận request để kiểm tra body, filter và idempotency. Fixture trong `test/fixtures/` được xuất từ test backend trên PostgreSQL tạm với dữ liệu hoàn toàn tổng hợp. Regression bao gồm đặt lịch/check-in, lịch bác sĩ, nhập kho, workspace bác sĩ → đơn thuốc → hoàn tất, thu tiền CASH/BANK, quyền, lỗi và refresh session. Các test laptop kiểm tra 1366×768 và 1536×864.

Có thể xuất ảnh UI khi chạy test với `DNCS_CAPTURE_DIR` và `DNCS_CAPTURE_FONT_DIR` (thư mục `bin/cache/artifacts/material_fonts` của SDK); CI thông thường không xuất ảnh.

Bằng chứng và trạng thái từng task: [`../plans/2026-10-09-p0-fixes/status.md`](../plans/2026-10-09-p0-fixes/status.md). CI được cấu hình; cần xác minh run trên PR sau khi repository có remote và PR.

P1 đã bổ sung picker ngày/giá hiệu lực, dropdown vai trò, tài khoản trong hồ sơ, phiên bản quan hệ chuyên khoa, menu nhóm, nhãn tiếng Việt, lỗi API theo trường, cảnh báo giới hạn danh sách và chọn lô theo thuốc. Test P1: `test/p1_flows_test.dart`; trạng thái và bằng chứng: [`../plans/2026-10-09-p1/status.md`](../plans/2026-10-09-p1/status.md). Người dùng xác nhận P0 đã nghiệm thu ngày 10/10/2026; [ma trận T01–T23](../plans/2026-10-09-p0-fixes/status.md) ghi trạng thái và giới hạn evidence.

P2 bổ sung phân trang thật cho thuốc/lô/hóa đơn/đơn thuốc và cursor bệnh án/nhật ký; bộ chọn người/đối tượng audit, tiêu đề theo màn, ẩn phiên bản hồ sơ, gỡ hai màn phát triển, thẻ dưới 1366px và khi cỡ chữ lớn. `test/p2_flows_test.dart` kiểm tra bàn phím, semantics, target 48px, dữ liệu lỗi/stale, và 12 màn tại 360×800, 768×1024, 1280×800, 1440×900 với TextScaler 1×/2×, gồm cuộn nội dung. Bằng chứng: [`../plans/2026-10-09-p2/status.md`](../plans/2026-10-09-p2/status.md).

## P3: danh sách và luồng trình duyệt thật

P3 phân trang bệnh nhân, nhân sự, lịch hẹn, lịch bác sĩ, dịch vụ, chuyên khoa, phòng và ca chờ thu tiền bằng `has_more` từ server. Bộ lọc mới về trang 1; lỗi giữ trang/dữ liệu cũ; phản hồi cũ không ghi đè. Bộ lọc mặc định lịch bác sĩ gửi RFC3339 có múi giờ phòng khám. Giá trị chi tiết có nội dung text trong cây accessibility, kể cả dấu `—` khi rỗng, đồng thời vẫn cho chọn/copy chữ. Test: `test/p3_flows_test.dart` và [`../backend/internal/database/p3_ui_integration_test.go`](../backend/internal/database/p3_ui_integration_test.go).

Harness `e2e/run.py` dựng Flutter Web → Go API → PostgreSQL 16 dùng một lần, tạo fixture tổng hợp qua API và dùng bốn tài khoản ADMIN/PATIENT/DOCTOR/CASHIER trên Chrome. API kết nối bằng role kế thừa `dncs_app`, không dùng superuser. Sau luồng tạo lịch → đặt lịch → check-in → khám/sinh hiệu/kê đơn/hoàn tất → CASH → lịch sử/hóa đơn bệnh nhân, harness kiểm tra audit, hóa đơn và tồn kho rồi dọn server/container. Không cần `.env` hoặc DB vận hành.

Yêu cầu: Go theo `backend/go.mod`, Flutter 3.44.2, Node 22, Python ≥3.9, Docker đang chạy, Chrome. Chạy từ gốc DNCS:

```bash
npm ci --prefix frontend/e2e --ignore-scripts --no-audit --no-fund
(cd frontend && flutter pub get)
python3 frontend/e2e/run.py --evidence /tmp/dncs-p3-ui-evidence
```

Có thể chọn executable bằng `DNCS_GO`, `DNCS_FLUTTER`, `DNCS_NODE`, `DNCS_CHROME_PATH`; `DNCS_PLAYWRIGHT_MODULE` chỉ cần khi dùng thư viện Playwright cài sẵn ở đường dẫn khác. Cổng loopback 18083/15173 phải trống; harness sẽ từ chối gắn vào server đang có. `--skip-build` chỉ dùng khi build web hiện tại trỏ đúng API test; `--serve` giữ stack debug và Ctrl+C dọn stack.

CI job `ui-e2e` cài dependency từ lockfile, chạy harness và lưu ảnh/log/JSON kể cả khi lỗi. Kết quả local và phạm vi nghiệm thu: [`../plans/2026-10-09-p3/status.md`](../plans/2026-10-09-p3/status.md).

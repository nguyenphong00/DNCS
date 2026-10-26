import 'package:flutter/material.dart';
import '../../design_system/components/entity_picker.dart';
import '../../design_system/formatters/app_formatters.dart';
import '../../session_controller.dart';

List<Map<String, dynamic>> apiItems(Object? response) {
  final raw = response is Map ? response['items'] : response;
  if (raw is! List) return const [];
  return raw
      .whereType<Map>()
      .map((item) => Map<String, dynamic>.from(item))
      .toList();
}

Map<String, dynamic> apiMap(Object? value) {
  if (value is Map<String, dynamic>) return value;
  if (value is Map) return Map<String, dynamic>.from(value);
  return <String, dynamic>{};
}

int apiInt(Object? value, {int fallback = 0}) {
  if (value is int) return value;
  if (value is num) return value.toInt();
  return int.tryParse('$value') ?? fallback;
}

String apiString(Object? value) => value?.toString() ?? '';

bool hasAnyPermission(SessionController session, Iterable<String> values) =>
    values.any(session.permissions.contains);

String friendlyError(Object error) {
  if (error is ApiException) {
    const messages = {
      'CONCURRENT_UPDATE':
          'Dữ liệu vừa được thay đổi. Tải lại trước khi thử lại.',
      'AUTH_ACCOUNT_CONCURRENT_UPDATE':
          'Tài khoản vừa được thay đổi. Tải lại trước khi thử lại.',
      'STAFF_CONCURRENT_UPDATE':
          'Hồ sơ nhân sự vừa thay đổi. Tải lại trước khi lưu.',
      'SERVICE_CONCURRENT_UPDATE':
          'Dịch vụ vừa thay đổi. Tải lại trước khi lưu.',
      'MEDICINE_CONCURRENT_UPDATE':
          'Thuốc vừa thay đổi. Tải lại trước khi lưu.',
      'AUTH_USERNAME_ALREADY_EXISTS': 'Tên đăng nhập đã được sử dụng.',
      'PATIENT_PHONE_EXISTS': 'Số điện thoại đã được sử dụng.',
      'FORBIDDEN': 'Bạn chưa có quyền thực hiện thao tác này.',
      'INVENTORY_ADJUSTMENT_OUT_EXCEEDS_STOCK':
          'Số lượng giảm vượt tồn hiện tại của lô. Kiểm tra lại số lượng.',
      'APPOINTMENT_CHECK_IN_WINDOW_INVALID':
          'Chưa nằm trong thời gian check-in. Kiểm tra giờ hẹn và cấu hình tiếp nhận.',
      'APPOINTMENT_CHECK_IN_WINDOW':
          'Chưa nằm trong thời gian check-in. Kiểm tra giờ hẹn và cấu hình tiếp nhận.',
      'ENCOUNTER_CONCURRENT_UPDATE':
          'Bệnh án vừa được cập nhật. Tải lại trước khi lưu.',
      'INVOICE_UNPAID_ALREADY_EXISTS':
          'Đã có hóa đơn chờ thanh toán. Mở hóa đơn hiện có.',
      'INVOICE_MEDICINE_NOT_ELIGIBLE':
          'Có thuốc chưa hoạt động hoặc chưa có giá. Kiểm tra danh mục thuốc.',
    };
    final message = messages[error.code] ?? error.message;
    return error.requestId == null
        ? message
        : '$message (Mã hỗ trợ: ${error.requestId})';
  }

  return error.toString();
}

String idempotencyKey(String scope) =>
    'dncs-$scope-${DateTime.now().microsecondsSinceEpoch}';

void showActionError(BuildContext context, Object cause) {
  ScaffoldMessenger.of(
    context,
  ).showSnackBar(SnackBar(content: Text(friendlyError(cause))));
}

Future<List<EntityOption>> searchEntities(
  SessionController session,
  String path,
  String query, {
  String nameKey = 'name',
  String codeKey = 'code',
  bool doctors = false,
  bool activeOnly = true,
}) async {
  final items = apiItems(
    await session.requestJson(
      'GET',
      path,
      query: {
        if (path == '/api/v1/staff') 'keyword': query else 'q': query,
        if (activeOnly &&
            (path == '/api/v1/medicines' || path == '/api/v1/services'))
          'status': 'ACTIVE',
        'page': '1',
        'page_size': '30',
      },
    ),
  );
  return items
      .where((item) => !doctors || apiMap(item['doctor_profile']).isNotEmpty)
      .map((item) {
        final doctor = doctors ? apiMap(item['doctor_profile']) : item;
        return EntityOption(
          id: apiInt(doctor['id']),
          label: valueText(item[nameKey]),
          subtitle: [
            apiString(item[codeKey]),
            apiString(item['phone_number']),
          ].where((v) => v.isNotEmpty).join(' · '),
        );
      })
      .toList();
}

String formatVnd(Object? raw) {
  final value = raw is num ? raw.toInt() : int.tryParse('$raw');
  if (value == null) return '—';
  final negative = value < 0;
  final digits = value.abs().toString();
  final output = StringBuffer();
  for (var index = 0; index < digits.length; index++) {
    if (index > 0 && (digits.length - index) % 3 == 0) output.write('.');
    output.write(digits[index]);
  }
  return '${negative ? '-' : ''}${output.toString()} ₫';
}

String formatDate(Object? raw) {
  final value = DateTime.tryParse('$raw');
  if (value == null) return raw == null || '$raw'.isEmpty ? '—' : '$raw';
  final local = RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch('$raw')
      ? value
      : value.toUtc().add(const Duration(hours: 7));
  return '${_two(local.day)}/${_two(local.month)}/${local.year}';
}

String formatDateTime(Object? raw) {
  final value = DateTime.tryParse('$raw');
  if (value == null) return raw == null || '$raw'.isEmpty ? '—' : '$raw';
  final local = value.toUtc().add(const Duration(hours: 7));
  return '${_two(local.day)}/${_two(local.month)}/${local.year}, '
      '${_two(local.hour)}:${_two(local.minute)}';
}

String valueText(Object? value) {
  if (value == null || '$value'.trim().isEmpty) return '—';
  return '$value';
}

String _two(int value) => value.toString().padLeft(2, '0');

String enumLabel(Object? value) =>
    const {
      'USER': 'Tài khoản',
      'STAFF_PROFILE': 'Hồ sơ nhân viên',
      'DOCTOR_PROFILE': 'Hồ sơ bác sĩ',
      'SPECIALTY': 'Chuyên khoa',
      'ROOM': 'Phòng',
      'DOCTOR_SPECIALTY': 'Phân công chuyên khoa',
      'DOCTOR_SCHEDULE': 'Lịch làm việc',
      'APPOINTMENT': 'Lịch hẹn',
      'ENCOUNTER': 'Bệnh án',
      'ENCOUNTER_SERVICE': 'Chỉ định dịch vụ',
      'ENCOUNTER_VITAL_SIGN': 'Sinh hiệu',
      'ENCOUNTER_AMENDMENT': 'Bổ sung bệnh án',
      'PRESCRIPTION_SERIES': 'Đơn thuốc',
      'PRESCRIPTION': 'Lần kê đơn',
      'PRESCRIPTION_ITEM': 'Thuốc kê đơn',
      'SERVICE_PRICE_VERSION': 'Giá dịch vụ',
      'MEDICINE_PRICE_VERSION': 'Giá thuốc',
      'STOCK_RECEIPT': 'Phiếu nhập kho',
      'INVENTORY_ADJUSTMENT': 'Điều chỉnh kho',
      'INVOICE': 'Hóa đơn',
      'PAYMENT': 'Thanh toán',
      'SYSTEM_SETTING': 'Cấu hình',
      'BANK_ACCOUNT': 'Ngân hàng',
      'CASH': 'Tiền mặt',
      'BANK_TRANSFER': 'Chuyển khoản',
      'PENDING_CONFIRMATION': 'Chờ xác nhận',
      'SERVICE': 'Dịch vụ',
      'MEDICINE': 'Thuốc',
      'MALE': 'Nam',
      'FEMALE': 'Nữ',
      'OTHER': 'Khác',
      'UNSPECIFIED': 'Chưa cung cấp',
      'CONSULTATION': 'Khám bệnh',
      'LAB': 'Xét nghiệm',
      'IMAGING': 'Chẩn đoán hình ảnh',
      'PROCEDURE': 'Thủ thuật',
      'FUNCTIONAL': 'Phòng chức năng',
      'RECEIPT_IN': 'Nhập kho',
      'SALE_OUT': 'Xuất bán',
      'ADJUSTMENT_IN': 'Điều chỉnh tăng',
      'ADJUSTMENT_OUT': 'Điều chỉnh giảm',
      'RECEIPT_ITEM': 'Phiếu nhập',
      'INVOICE_ITEM': 'Hóa đơn',
      'ADJUSTMENT_ITEM': 'Phiếu điều chỉnh',
      'ADMIN': 'Quản lý',
      'SUPER_ADMIN': 'Quản trị hệ thống',
      'DOCTOR': 'Bác sĩ',
      'CASHIER': 'Thu ngân',
      'PATIENT': 'Bệnh nhân',
      'STAFF': 'Nhân viên',
      'IN': 'Tăng tồn',
      'OUT': 'Giảm tồn',
      'COUNT_CORRECTION': 'Hiệu chỉnh kiểm kê',
      'DAMAGE': 'Hư hỏng',
      'EXPIRED_DISPOSAL': 'Hủy thuốc hết hạn',
    }[value?.toString()] ??
    valueText(value);

String dateInputValue(Object? raw) {
  final date = DateTime.tryParse('$raw');
  return date == null
      ? ''
      : '${date.year.toString().padLeft(4, '0')}-${_two(date.month)}-${_two(date.day)}';
}

import 'package:flutter/material.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../design_system/components/feedback.dart';
import '../../design_system/theme/app_theme.dart';

/// Renders the immutable VietQR payload returned by
/// `GET /api/v1/invoices/{id}/payment-qr` as a scannable QR image.
///
/// The payload is drawn exactly as the backend produced it; the client never
/// rebuilds or edits it. The raw text stays available below the image.
class PaymentQrPanel extends StatelessWidget {
  const PaymentQrPanel({super.key, required this.payload, this.size = 280});

  /// Raw EMVCo/VietQR string from the backend (`qr_payload`).
  final String payload;

  /// Edge length in logical pixels. 280 is readable by a phone camera held in
  /// front of a 15.6" laptop screen at 100–125% scaling.
  final double size;

  static const qrKey = ValueKey('payment-qr-image');

  @override
  Widget build(BuildContext context) {
    final data = payload.trim();
    if (data.isEmpty) {
      return const InlineMessage(
        message: 'Hóa đơn này chưa có dữ liệu QR chuyển khoản.',
        tone: AppStatusTone.warning,
      );
    }
    return Center(
      child: Container(
        padding: const EdgeInsets.all(AppSpacing.md),
        decoration: BoxDecoration(
          color: Colors.white,
          border: Border.all(color: AppColors.border),
          borderRadius: BorderRadius.circular(AppRadius.medium),
        ),
        child: QrImageView(
          key: qrKey,
          data: data,
          size: size,
          // White quiet zone and dark modules on white: highest scan contrast.
          backgroundColor: Colors.white,
          errorCorrectionLevel: QrErrorCorrectLevel.M,
          semanticsLabel: 'Mã QR chuyển khoản',
          errorStateBuilder: (context, error) => const SizedBox.square(
            dimension: 200,
            child: Center(
              child: Text(
                'Không tạo được ảnh QR. Dùng thông tin chuyển khoản bên dưới.',
                textAlign: TextAlign.center,
              ),
            ),
          ),
        ),
      ),
    );
  }
}

import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

enum AppStatusTone { info, success, warning, danger, neutral, clinical }

class StatusBadge extends StatelessWidget {
  const StatusBadge({super.key, required this.status, this.label});

  final String status;
  final String? label;

  @override
  Widget build(BuildContext context) {
    final normalized = status.trim().toUpperCase();
    final tone = _tone(normalized);
    final colors = _colors(tone);
    return Semantics(
      label: 'Trạng thái ${label ?? _label(normalized)}',
      excludeSemantics: true,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: colors.$2,
          borderRadius: BorderRadius.circular(AppRadius.pill),
          border: Border.all(color: colors.$1.withValues(alpha: 0.35)),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(
            horizontal: AppSpacing.sm,
            vertical: AppSpacing.xxs,
          ),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(_icon(tone), size: 14, color: colors.$1),
              const SizedBox(width: AppSpacing.xxs),
              Flexible(
                child: Text(
                  label ?? _label(normalized),
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelMedium?.copyWith(
                    color: colors.$1,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  AppStatusTone _tone(String value) => switch (value) {
    'ACTIVE' ||
    'PAID' ||
    'COMPLETED' ||
    'SUCCESS' ||
    'AVAILABLE' ||
    'IN_STOCK' ||
    'EFFECTIVE' => AppStatusTone.success,
    'CONFIRMED' || 'CHECKED_IN' || 'IN_PROGRESS' => AppStatusTone.info,
    'PENDING' ||
    'PENDING_APPROVAL' ||
    'UNPAID' ||
    'SCHEDULED' ||
    'LOW_STOCK' => AppStatusTone.warning,
    'FINALIZED' || 'ISSUED' || 'SUPERSEDED' => AppStatusTone.clinical,
    'LOCKED' ||
    'FAILURE' ||
    'FAILED' ||
    'CANCELLED' ||
    'REJECTED' ||
    'NO_SHOW' ||
    'OUT_OF_STOCK' ||
    'EXPIRED_ONLY' ||
    'EXPIRED' => AppStatusTone.danger,
    _ => AppStatusTone.neutral,
  };

  (Color, Color) _colors(AppStatusTone tone) => switch (tone) {
    AppStatusTone.info => (AppColors.info, AppColors.infoContainer),
    AppStatusTone.success => (AppColors.success, AppColors.successContainer),
    AppStatusTone.warning => (AppColors.warning, AppColors.warningContainer),
    AppStatusTone.danger => (AppColors.danger, AppColors.dangerContainer),
    AppStatusTone.clinical => (AppColors.clinical, AppColors.clinicalContainer),
    AppStatusTone.neutral => (AppColors.textSecondary, AppColors.surfaceSubtle),
  };

  IconData _icon(AppStatusTone tone) => switch (tone) {
    AppStatusTone.info => Icons.info_outline,
    AppStatusTone.success => Icons.check_circle_outline,
    AppStatusTone.warning => Icons.schedule_outlined,
    AppStatusTone.danger => Icons.error_outline,
    AppStatusTone.clinical => Icons.medical_information_outlined,
    AppStatusTone.neutral => Icons.circle_outlined,
  };

  String _label(String value) => switch (value) {
    'ACTIVE' => 'Hoạt động',
    'INACTIVE' => 'Không hoạt động',
    'ARCHIVED' => 'Đã lưu trữ',
    'DRAFT' => 'Bản nháp',
    'PENDING' => 'Đang chờ',
    'PENDING_CONFIRMATION' => 'Chờ xác nhận',
    'PENDING_APPROVAL' => 'Chờ duyệt',
    'CONFIRMED' => 'Đã xác nhận',
    'CHECKED_IN' => 'Đã check-in',
    'IN_PROGRESS' => 'Đang khám',
    'FINALIZED' => 'Đã hoàn tất',
    'COMPLETED' => 'Hoàn thành',
    'CANCELLED' => 'Đã hủy',
    'REJECTED' => 'Đã từ chối',
    'NO_SHOW' => 'Vắng mặt',
    'LOCKED' => 'Đã khóa',
    'PAID' => 'Đã thanh toán',
    'UNPAID' => 'Chưa thanh toán',
    'ISSUED' => 'Đã phát hành',
    'SUPERSEDED' => 'Đã thay thế',
    'DISCARDED' => 'Đã bỏ',
    'FAILURE' || 'FAILED' => 'Thất bại',
    'SCHEDULED' => 'Đã lên lịch',
    'EXPIRED' => 'Đã hết hạn',
    'SUCCESS' => 'Thành công',
    'AVAILABLE' => 'Khả dụng',
    'IN_STOCK' => 'Còn hàng',
    'OUT_OF_STOCK' => 'Hết hàng',
    'EXPIRED_ONLY' => 'Chỉ còn lô hết hạn',
    'LOW_STOCK' => 'Sắp hết hàng',
    'EFFECTIVE' => 'Đang hiệu lực',
    _ => value.isEmpty ? 'Không xác định' : value,
  };
}

class InlineMessage extends StatelessWidget {
  const InlineMessage({
    super.key,
    required this.message,
    this.tone = AppStatusTone.info,
    this.action,
  });

  final String message;
  final AppStatusTone tone;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final colors = switch (tone) {
      AppStatusTone.info => (AppColors.info, AppColors.infoContainer),
      AppStatusTone.success => (AppColors.success, AppColors.successContainer),
      AppStatusTone.warning => (AppColors.warning, AppColors.warningContainer),
      AppStatusTone.danger => (AppColors.danger, AppColors.dangerContainer),
      AppStatusTone.clinical => (
        AppColors.clinical,
        AppColors.clinicalContainer,
      ),
      AppStatusTone.neutral => (
        AppColors.textSecondary,
        AppColors.surfaceSubtle,
      ),
    };
    return Semantics(
      liveRegion: true,
      child: Container(
        padding: const EdgeInsets.all(AppSpacing.md),
        decoration: BoxDecoration(
          color: colors.$2,
          borderRadius: BorderRadius.circular(AppRadius.medium),
          border: Border.all(color: colors.$1.withValues(alpha: 0.35)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(Icons.info_outline, color: colors.$1),
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              child: Text(message, style: TextStyle(color: colors.$1)),
            ),
            if (action != null) ...[
              const SizedBox(width: AppSpacing.sm),
              action!,
            ],
          ],
        ),
      ),
    );
  }
}

class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.title,
    required this.message,
    this.icon = Icons.inbox_outlined,
    this.action,
  });

  final String title;
  final String message;
  final IconData icon;
  final Widget? action;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: AppSpacing.section),
    child: Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 480),
        child: Column(
          children: [
            Icon(icon, size: 48, color: AppColors.textMuted),
            const SizedBox(height: AppSpacing.md),
            Text(
              title,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.titleLarge,
            ),
            const SizedBox(height: AppSpacing.xs),
            Text(
              message,
              textAlign: TextAlign.center,
              style: Theme.of(
                context,
              ).textTheme.bodyMedium?.copyWith(color: AppColors.textSecondary),
            ),
            if (action != null) ...[
              const SizedBox(height: AppSpacing.lg),
              action!,
            ],
          ],
        ),
      ),
    ),
  );
}

class AsyncContent extends StatelessWidget {
  const AsyncContent({
    super.key,
    required this.loading,
    required this.child,
    this.error,
    this.keepChildOnError = false,
    this.empty = false,
    this.onRetry,
    this.emptyTitle = 'Chưa có dữ liệu',
    this.emptyMessage = 'Dữ liệu sẽ xuất hiện tại đây khi được tạo.',
  });

  final bool loading;
  final bool keepChildOnError;
  final String? error;
  final bool empty;
  final VoidCallback? onRetry;
  final Widget child;
  final String emptyTitle;
  final String emptyMessage;

  @override
  Widget build(BuildContext context) {
    if (loading) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: AppSpacing.section),
        child: Center(child: CircularProgressIndicator()),
      );
    }
    if (error != null) {
      final message = InlineMessage(
        message: error!,
        tone: AppStatusTone.danger,
        action: onRetry == null
            ? null
            : TextButton.icon(
                onPressed: onRetry,
                icon: const Icon(Icons.refresh),
                label: const Text('Thử lại'),
              ),
      );
      if (keepChildOnError && !empty) {
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            message,
            const SizedBox(height: AppSpacing.md),
            child,
          ],
        );
      }
      return message;
    }
    if (empty) {
      return EmptyState(title: emptyTitle, message: emptyMessage);
    }
    return child;
  }
}

class ConflictPanel extends StatelessWidget {
  const ConflictPanel({
    super.key,
    required this.message,
    required this.onReload,
  });

  final String message;
  final VoidCallback onReload;

  @override
  Widget build(BuildContext context) => InlineMessage(
    message: message,
    tone: AppStatusTone.warning,
    action: TextButton.icon(
      onPressed: onReload,
      icon: const Icon(Icons.refresh),
      label: const Text('Tải bản mới'),
    ),
  );
}

import 'package:flutter/material.dart';
import '../../design_system/components/dialogs.dart';
import '../../design_system/components/feedback.dart';
import '../../design_system/components/layout.dart';
import '../../session_controller.dart';
import 'api_helpers.dart';

class AccountActions extends StatefulWidget {
  const AccountActions({
    super.key,
    required this.session,
    required this.profile,
    required this.profilePath,
    required this.onChanged,
  });
  final SessionController session;
  final Map<String, dynamic> profile;
  final String profilePath;
  final ValueChanged<Map<String, dynamic>> onChanged;
  @override
  State<AccountActions> createState() => _AccountActionsState();
}

class _AccountActionsState extends State<AccountActions> {
  late Map<String, dynamic> profile = Map.of(widget.profile);
  @override
  void didUpdateWidget(AccountActions oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.profile != widget.profile) profile = Map.of(widget.profile);
  }

  bool busy = false;
  String? error;
  Future<void> refresh() async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      final updated = await widget.session.requestJson(
        'GET',
        widget.profilePath,
      );
      if (mounted) {
        setState(() => profile = updated);
        widget.onChanged(Map.of(profile));
      }
    } catch (cause) {
      if (mounted) setState(() => error = friendlyError(cause));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> execute(String command, String label) async {
    final reason = await showReasonDialog(
      context,
      title: '$label tài khoản?',
      message:
          'Tài khoản của ${valueName(profile)}. Vui lòng ghi lý do thao tác.',
      confirmLabel: label,
      destructive: command == 'deactivate',
    );
    if (reason == null || !mounted) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      final result = await widget.session.requestJson(
        'POST',
        '/api/v1/accounts/${profile['user_id']}/$command',
        body: {
          'expected_version': profile['account_version'],
          'reason': reason,
        },
      );
      if (!mounted) return;
      setState(() {
        profile = {
          ...profile,
          'account_version': result['version'],
          'account_status': result['status'],
          'must_change_password': result['must_change_password'],
        };
      });
      setState(() => busy = false);
      widget.onChanged(Map.of(profile));
      final password = apiString(result['temporary_password']);
      if (password.isNotEmpty) {
        await showDialog<void>(
          context: context,
          barrierDismissible: false,
          builder: (context) => AlertDialog(
            title: const Text('Mật khẩu tạm thời'),
            content: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Text(
                  'Bàn giao trực tiếp cho chủ tài khoản. Mật khẩu chỉ hiển thị lần này.',
                ),
                const SizedBox(height: 16),
                SelectableText(password),
              ],
            ),
            actions: [
              FilledButton(
                onPressed: () => Navigator.pop(context),
                child: const Text('Đã bàn giao'),
              ),
            ],
          ),
        );
      }
    } catch (cause) {
      if (mounted) setState(() => error = friendlyError(cause));
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  String valueName(Map<String, dynamic> value) => apiString(value['full_name']);
  @override
  Widget build(BuildContext context) {
    final canStatus = widget.session.permissions.contains(
      'account.status.manage',
    );
    final canReset = widget.session.permissions.contains(
      'account.password.reset',
    );
    final own =
        apiInt(profile['user_id']) ==
        apiInt(widget.session.principal?['user_id']);
    if ((!canStatus && !canReset) ||
        own ||
        profile['role_code'] == 'SUPER_ADMIN' ||
        profile['is_deleted'] == true) {
      return const SizedBox.shrink();
    }
    final ready =
        !busy &&
        apiInt(profile['user_id']) > 0 &&
        apiInt(profile['account_version']) > 0;
    final status = apiString(profile['account_status']);
    return SectionCard(
      title: 'Tài khoản',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          StatusBadge(status: status),
          const SizedBox(height: 12),
          if (busy) const LinearProgressIndicator(),
          if (error != null)
            InlineMessage(message: error!, tone: AppStatusTone.danger),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              if (canReset && status != 'INACTIVE')
                OutlinedButton(
                  onPressed: ready
                      ? () => execute('reset-password', 'Đặt lại mật khẩu')
                      : null,
                  child: const Text('Đặt lại mật khẩu'),
                ),
              if (canStatus && status == 'ACTIVE')
                OutlinedButton(
                  onPressed: ready ? () => execute('lock', 'Khóa') : null,
                  child: const Text('Khóa tài khoản'),
                ),
              if (canStatus && status == 'LOCKED')
                OutlinedButton(
                  onPressed: ready ? () => execute('unlock', 'Mở khóa') : null,
                  child: const Text('Mở khóa tài khoản'),
                ),
              if (canStatus && status != 'INACTIVE')
                OutlinedButton(
                  onPressed: ready
                      ? () => execute('deactivate', 'Ngưng sử dụng')
                      : null,
                  child: const Text('Ngưng sử dụng tài khoản'),
                ),
              TextButton(
                onPressed: busy ? null : refresh,
                child: const Text('Tải lại tài khoản'),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

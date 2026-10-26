import 'package:flutter/material.dart';

import '../theme/app_theme.dart';

Future<bool> showConfirmActionDialog(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  bool destructive = false,
}) async {
  final result = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text(title),
      content: Text(message),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context, false),
          child: const Text('Hủy'),
        ),
        FilledButton(
          style: destructive
              ? FilledButton.styleFrom(backgroundColor: AppColors.danger)
              : null,
          onPressed: () => Navigator.pop(context, true),
          child: Text(confirmLabel),
        ),
      ],
    ),
  );
  return result ?? false;
}

Future<String?> showReasonDialog(
  BuildContext context, {
  required String title,
  required String message,
  required String confirmLabel,
  bool destructive = false,
}) => showDialog<String>(
  context: context,
  builder: (_) => _ReasonDialog(
    title: title,
    message: message,
    confirmLabel: confirmLabel,
    destructive: destructive,
  ),
);

class _ReasonDialog extends StatefulWidget {
  const _ReasonDialog({
    required this.title,
    required this.message,
    required this.confirmLabel,
    required this.destructive,
  });
  final String title, message, confirmLabel;
  final bool destructive;
  @override
  State<_ReasonDialog> createState() => _ReasonDialogState();
}

class _ReasonDialogState extends State<_ReasonDialog> {
  final controller = TextEditingController();
  String? error;
  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.title),
    content: SizedBox(
      width: 480,
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(widget.message),
            const SizedBox(height: AppSpacing.md),
            TextField(
              controller: controller,
              autofocus: true,
              maxLength: 500,
              maxLines: 3,
              decoration: InputDecoration(
                labelText: 'Lý do *',
                errorText: error,
              ),
            ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        style: widget.destructive
            ? FilledButton.styleFrom(backgroundColor: AppColors.danger)
            : null,
        onPressed: () {
          final value = controller.text.trim();
          if (value.isEmpty) {
            setState(() => error = 'Vui lòng nhập lý do.');
            return;
          }
          Navigator.pop(context, value);
        },
        child: Text(widget.confirmLabel),
      ),
    ],
  );
}

void showSuccessMessage(BuildContext context, String message) {
  ScaffoldMessenger.of(context)
    ..hideCurrentSnackBar()
    ..showSnackBar(
      SnackBar(
        content: Row(
          children: [
            const Icon(Icons.check_circle_outline, color: Colors.white),
            const SizedBox(width: AppSpacing.sm),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
}

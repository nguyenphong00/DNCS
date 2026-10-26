import 'package:flutter/material.dart';
import '../../session_controller.dart';
import '../../design_system/components/feedback.dart';
import 'api_helpers.dart';

typedef ApiSubmit =
    Future<Map<String, dynamic>> Function(Map<String, dynamic> values);

Future<Map<String, dynamic>?> showApiFormDialog({
  required BuildContext context,
  required WidgetBuilder builder,
  required ApiSubmit submit,
}) => showDialog<Map<String, dynamic>>(
  context: context,
  barrierDismissible: false,
  builder: (_) => _ApiFormScope(submit: submit, builder: builder),
);

class _ApiFormScope extends StatefulWidget {
  const _ApiFormScope({required this.submit, required this.builder});
  final ApiSubmit submit;
  final WidgetBuilder builder;
  @override
  State<_ApiFormScope> createState() => _ApiFormScopeState();
}

class _ApiFormScopeState extends State<_ApiFormScope> {
  bool busy = false;
  Object? error;
  Future<void> send(Map<String, dynamic> values) async {
    if (busy) return;
    setState(() {
      busy = true;
      error = null;
    });
    try {
      final result = await widget.submit(values);
      if (mounted) Navigator.pop(context, result);
    } catch (cause) {
      if (mounted) setState(() => error = cause);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => _FormData(
    state: this,
    busy: busy,
    error: error,
    child: Builder(builder: widget.builder),
  );
}

class _FormData extends InheritedWidget {
  const _FormData({
    required this.state,
    required this.busy,
    required this.error,
    required super.child,
  });
  final _ApiFormScopeState state;
  final bool busy;
  final Object? error;
  @override
  bool updateShouldNotify(_FormData old) =>
      busy != old.busy || error != old.error;
}

bool apiFormBusy(BuildContext context) =>
    context.dependOnInheritedWidgetOfExactType<_FormData>()?.busy ?? false;
String? apiFieldError(BuildContext context, String field) {
  final error = context.dependOnInheritedWidgetOfExactType<_FormData>()?.error;
  if (error is! ApiException) return null;
  if (error.fields.containsKey(field)) return error.fields[field];
  if (error.code == 'AUTH_USERNAME_ALREADY_EXISTS' && field == 'username') {
    return 'Tên đăng nhập đã được sử dụng.';
  }
  if (error.code == 'PATIENT_PHONE_EXISTS' && field == 'phone_number') {
    return 'Số điện thoại đã được sử dụng.';
  }
  return null;
}

void submitApiForm(BuildContext context, Map<String, dynamic> values) {
  final scope = context.dependOnInheritedWidgetOfExactType<_FormData>();
  if (scope == null) {
    Navigator.pop(context, values);
  } else {
    scope.state.send(values);
  }
}

class ApiFormNotice extends StatelessWidget {
  const ApiFormNotice({super.key});
  @override
  Widget build(BuildContext context) {
    final data = context.dependOnInheritedWidgetOfExactType<_FormData>();
    if (data?.busy == true) return const LinearProgressIndicator();
    return data?.error == null
        ? const SizedBox.shrink()
        : InlineMessage(
            message: friendlyError(data!.error!),
            tone: AppStatusTone.danger,
          );
  }
}

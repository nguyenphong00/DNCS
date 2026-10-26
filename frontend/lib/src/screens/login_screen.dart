import 'package:flutter/material.dart';

import '../session_controller.dart';
import '../ui/responsive.dart';

class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key, required this.session});
  final SessionController session;
  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final identifier = TextEditingController();
  final password = TextEditingController();
  bool staff = true;
  bool busy = false;
  String? error;

  Future<void> login() async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await widget.session.login(
        staff: staff,
        identifier: identifier.text,
        password: password.text,
      );
    } catch (value) {
      if (mounted) setState(() => error = value.toString());
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    body: LayoutBuilder(
      builder: (context, constraints) {
        final desktop = constraints.maxWidth >= desktopBreakpoint;
        final form = ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 440),
          child: Card(
            child: Padding(
              padding: EdgeInsets.all(desktop ? 32 : 24),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Icon(Icons.local_hospital_outlined, size: 56),
                  const SizedBox(height: 12),
                  Text(
                    'DNCS Clinic',
                    style: Theme.of(context).textTheme.headlineMedium,
                    textAlign: TextAlign.center,
                  ),
                  const SizedBox(height: 24),
                  SegmentedButton<bool>(
                    segments: const [
                      ButtonSegment(
                        value: true,
                        label: Text('Nhân sự'),
                        icon: Icon(Icons.badge_outlined),
                      ),
                      ButtonSegment(
                        value: false,
                        label: Text('Bệnh nhân'),
                        icon: Icon(Icons.person_outline),
                      ),
                    ],
                    selected: {staff},
                    onSelectionChanged: (value) =>
                        setState(() => staff = value.first),
                  ),
                  const SizedBox(height: 20),
                  TextField(
                    controller: identifier,
                    decoration: InputDecoration(
                      labelText: staff ? 'Tên đăng nhập' : 'Số điện thoại',
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: password,
                    obscureText: true,
                    onSubmitted: (_) => busy ? null : login(),
                    decoration: const InputDecoration(labelText: 'Mật khẩu'),
                  ),
                  if (error != null)
                    Padding(
                      padding: const EdgeInsets.only(top: 12),
                      child: Text(
                        error!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                  const SizedBox(height: 20),
                  FilledButton.icon(
                    onPressed: busy ? null : login,
                    icon: busy
                        ? const SizedBox.square(
                            dimension: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.login),
                    label: const Text('Đăng nhập'),
                  ),
                  if (!staff)
                    TextButton(
                      onPressed: busy
                          ? null
                          : () => showDialog<void>(
                              context: context,
                              builder: (_) =>
                                  _RegisterDialog(session: widget.session),
                            ),
                      child: const Text('Đăng ký tài khoản bệnh nhân'),
                    ),
                  const SizedBox(height: 8),
                  Text(
                    'API: ${widget.session.baseUri}',
                    textAlign: TextAlign.center,
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ),
        );
        return Row(
          children: [
            if (desktop)
              Expanded(
                child: Container(
                  color: Theme.of(context).colorScheme.primary,
                  padding: const EdgeInsets.all(72),
                  child: const _LoginBrandPanel(),
                ),
              ),
            Expanded(
              child: Center(
                child: SingleChildScrollView(
                  padding: EdgeInsets.all(desktop ? 48 : 24),
                  child: form,
                ),
              ),
            ),
          ],
        );
      },
    ),
  );
}

class _LoginBrandPanel extends StatelessWidget {
  const _LoginBrandPanel();
  @override
  Widget build(BuildContext context) => Column(
    mainAxisAlignment: MainAxisAlignment.center,
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Icon(Icons.local_hospital_outlined, size: 72, color: Colors.white),
      const SizedBox(height: 28),
      const Text(
        'DNCS Clinic',
        style: TextStyle(
          color: Colors.white,
          fontSize: 38,
          fontWeight: FontWeight.w700,
        ),
      ),
      const SizedBox(height: 12),
      Text(
        'Cổng vận hành thống nhất cho tiếp nhận, khám bệnh và thanh toán.',
        style: TextStyle(
          color: Colors.white.withValues(alpha: 0.82),
          fontSize: 18,
          height: 1.5,
        ),
      ),
    ],
  );
}

class _RegisterDialog extends StatefulWidget {
  const _RegisterDialog({required this.session});
  final SessionController session;
  @override
  State<_RegisterDialog> createState() => _RegisterDialogState();
}

class _RegisterDialogState extends State<_RegisterDialog> {
  final phone = TextEditingController();
  final password = TextEditingController();
  final name = TextEditingController();
  final dob = TextEditingController();
  bool busy = false;
  String? error;

  Future<void> submit() async {
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await widget.session.registerPatient({
        'phone_number': phone.text,
        'password': password.text,
        'full_name': name.text,
        'date_of_birth': dob.text,
        'gender_code': 'UNSPECIFIED',
        'address': '',
        'cccd': '',
        'health_insurance_number': '',
        'medical_history': '',
        'allergies': '',
      });
      if (mounted) Navigator.pop(context);
    } catch (value) {
      if (mounted) setState(() => error = value.toString());
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Đăng ký bệnh nhân'),
    content: SizedBox(
      width: 440,
      child: SingleChildScrollView(
        child: Column(
          children: [
            TextField(
              controller: phone,
              decoration: const InputDecoration(labelText: 'Số điện thoại'),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: password,
              obscureText: true,
              decoration: const InputDecoration(
                labelText: 'Mật khẩu (8–128 ký tự)',
              ),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: name,
              decoration: const InputDecoration(labelText: 'Họ tên'),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: dob,
              decoration: const InputDecoration(
                labelText: 'Ngày sinh YYYY-MM-DD (có thể trống)',
              ),
            ),
            if (error != null)
              Padding(
                padding: const EdgeInsets.only(top: 12),
                child: Text(
                  error!,
                  style: TextStyle(color: Theme.of(context).colorScheme.error),
                ),
              ),
          ],
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: busy ? null : () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        onPressed: busy ? null : submit,
        child: const Text('Đăng ký'),
      ),
    ],
  );
}

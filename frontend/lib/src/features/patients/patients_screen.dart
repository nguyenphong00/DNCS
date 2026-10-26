import '../shared/list_pagination.dart';
import 'package:flutter/material.dart';
import '../../design_system/components/date_fields.dart';
import '../shared/account_actions.dart';
import '../shared/api_form.dart';

import '../../design_system/components/data_views.dart';
import '../../design_system/components/dialogs.dart';
import '../../design_system/components/feedback.dart';
import '../../design_system/components/layout.dart';
import '../../design_system/formatters/app_formatters.dart';
import '../../design_system/theme/app_theme.dart';
import '../../session_controller.dart';
import '../shared/api_helpers.dart';

class PatientsScreen extends StatefulWidget {
  const PatientsScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<PatientsScreen> createState() => _PatientsScreenState();
}

class _PatientsScreenState extends State<PatientsScreen> {
  final search = TextEditingController();
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination(pageSize: 50);
  Map<String, dynamic>? ownProfile;
  bool loading = true;
  bool mutating = false;
  String? error;

  bool get isPatient => widget.session.accountType == 'PATIENT';
  bool can(String permission) =>
      widget.session.permissions.contains(permission);

  @override
  void initState() {
    super.initState();
    load();
  }

  @override
  void dispose() {
    paging.dispose();
    search.dispose();
    super.dispose();
  }

  Future<void> load([String? query, int? targetPage]) async {
    final request = paging.start({
      if ((query ?? search.text).trim().isNotEmpty)
        'q': (query ?? search.text).trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      if (isPatient) {
        final response = await widget.session.requestJson(
          'GET',
          '/api/v1/me/patient-profile',
        );
        if (!mounted || !paging.accept(request, response)) return;
        ownProfile = response;
      } else {
        final response = await widget.session.requestJson(
          'GET',
          '/api/v1/patients',
          query: request.query,
        );
        if (!mounted || !paging.accept(request, response)) return;
        items = apiItems(response);
      }
    } catch (cause) {
      if (mounted && paging.isCurrent(request)) error = friendlyError(cause);
    } finally {
      if (mounted && paging.isCurrent(request)) setState(() => loading = false);
    }
  }

  Future<void> createPatient() async {
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => _PatientFormDialog(create: true),
      submit: (values) => widget.session.requestJson(
        'POST',
        '/api/v1/patients/accounts',
        body: values,
      ),
    );
    if (response == null || !mounted) return;
    final password = apiString(response['temporary_password']);
    if (password.isNotEmpty) await _showTemporaryPassword(password);
    if (mounted) {
      showSuccessMessage(context, 'Đã tạo hồ sơ bệnh nhân.');
      await load();
    }
  }

  Future<void> editPatient(Map<String, dynamic> patient) async {
    final result = await showApiFormDialog(
      context: context,
      builder: (_) => _PatientFormDialog(initial: patient),
      submit: (values) => widget.session.requestJson(
        'PATCH',
        isPatient
            ? '/api/v1/me/patient-profile'
            : '/api/v1/patients/${patient['id']}',
        body: {'expected_version': patient['version'], ...values},
      ),
    );
    if (result != null && mounted) {
      showSuccessMessage(context, 'Đã cập nhật hồ sơ bệnh nhân.');
      await load();
    }
  }

  Future<void> changePhone(Map<String, dynamic> patient) async {
    final values = await showDialog<Map<String, String>>(
      context: context,
      builder: (_) =>
          _PhoneChangeDialog(currentPhone: apiString(patient['phone_number'])),
    );
    if (values == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/patients/${patient['id']}/phone-change',
        body: {
          'expected_version': patient['version'],
          'phone_number': values['phone_number'],
          'reason': values['reason'],
        },
      );
      if (mounted) {
        showSuccessMessage(context, 'Đã đổi số điện thoại bệnh nhân.');
      }
    });
  }

  Future<void> archivePatient(Map<String, dynamic> patient) async {
    final reason = await showReasonDialog(
      context,
      title: 'Lưu trữ hồ sơ bệnh nhân?',
      message:
          'Hồ sơ ${apiString(patient['patient_code'])} sẽ không còn xuất hiện trong danh sách hoạt động.',
      confirmLabel: 'Lưu trữ hồ sơ',
      destructive: true,
    );
    if (reason == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/patients/${patient['id']}/archive',
        body: {'expected_version': patient['version'], 'reason': reason},
      );
      if (mounted) showSuccessMessage(context, 'Đã lưu trữ hồ sơ bệnh nhân.');
    });
  }

  Future<void> restorePatient(Map<String, dynamic> patient) async {
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Khôi phục hồ sơ bệnh nhân?',
      message:
          'Hệ thống sẽ kiểm tra lại identity và dependency trước khi khôi phục.',
      confirmLabel: 'Khôi phục',
    );
    if (!confirmed || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/patients/${patient['id']}/restore',
        body: {'expected_version': patient['version']},
      );
      if (mounted) showSuccessMessage(context, 'Đã khôi phục hồ sơ bệnh nhân.');
    });
  }

  Future<void> _mutate(Future<void> Function() operation) async {
    setState(() {
      mutating = true;
      error = null;
    });
    try {
      await operation();
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => mutating = false);
    }
  }

  Future<void> _showTemporaryPassword(String password) => showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (context) => AlertDialog(
      title: const Text('Mật khẩu tạm — chỉ hiển thị một lần'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const InlineMessage(
            message:
                'Bàn giao trực tiếp cho bệnh nhân. Không lưu, chụp màn hình hoặc gửi qua kênh không an toàn.',
            tone: AppStatusTone.warning,
          ),
          const SizedBox(height: AppSpacing.md),
          SelectableText(
            password,
            textAlign: TextAlign.center,
            style: Theme.of(context).textTheme.headlineMedium,
          ),
        ],
      ),
      actions: [
        FilledButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Tôi đã bàn giao'),
        ),
      ],
    ),
  );

  @override
  Widget build(BuildContext context) => isPatient
      ? _buildOwnProfile()
      : AppPageScaffold(
          title: 'Bệnh nhân',
          description:
              'Tra cứu, tạo tại quầy và quản lý vòng đời hồ sơ bệnh nhân.',
          actions: [
            if (can('patient.account.create_at_counter'))
              FilledButton.icon(
                onPressed: mutating ? null : createPatient,
                icon: const Icon(Icons.person_add_alt),
                label: const Text('Tạo bệnh nhân'),
              ),
          ],
          children: [
            if (error != null)
              InlineMessage(message: error!, tone: AppStatusTone.danger),
            SectionCard(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  FilterBar(
                    searchController: search,
                    hintText: 'Tìm theo mã, tên hoặc số điện thoại',
                    onSearch: load,
                    onClear: () {
                      search.clear();
                      load('');
                    },
                  ),
                  const SizedBox(height: AppSpacing.lg),
                  DataTableShell(
                    page: paging.page,
                    hasMore: paging.hasMore,
                    onPrevious: () => load(null, paging.page - 1),
                    onNext: () => load(null, paging.page + 1),
                    loading: loading,
                    error: error,
                    onRetry: load,
                    columns: const [
                      DataColumn(label: Text('Mã bệnh nhân')),
                      DataColumn(label: Text('Họ tên')),
                      DataColumn(label: Text('Số điện thoại')),
                      DataColumn(label: Text('Tài khoản')),
                      DataColumn(label: Text('Thao tác')),
                    ],
                    rows: items.map(_patientRow).toList(),
                    mobileCards: items.map(_patientCard).toList(),
                    emptyTitle: 'Chưa có bệnh nhân',
                    emptyMessage: search.text.isEmpty
                        ? 'Tạo bệnh nhân tại quầy để bắt đầu tiếp nhận.'
                        : 'Không có bệnh nhân phù hợp với từ khóa.',
                  ),
                ],
              ),
            ),
          ],
        );

  Widget _buildOwnProfile() {
    final patient = ownProfile;
    return AppPageScaffold(
      title: 'Hồ sơ của tôi',
      description:
          'Thông tin cá nhân và thông tin liên hệ đang lưu tại phòng khám.',
      maxWidth: 960,
      actions: [
        if (patient != null && can('patient.update.own'))
          FilledButton.icon(
            onPressed: mutating ? null : () => editPatient(patient),
            icon: const Icon(Icons.edit_outlined),
            label: const Text('Chỉnh sửa hồ sơ'),
          ),
      ],
      children: [
        AsyncContent(
          loading: loading,
          error: error,
          onRetry: load,
          empty: patient == null,
          child: patient == null
              ? const SizedBox.shrink()
              : _patientDetailCard(patient),
        ),
      ],
    );
  }

  DataRow _patientRow(Map<String, dynamic> patient) => DataRow(
    onSelectChanged: (_) => _showDetail(patient),
    cells: [
      DataCell(Text(valueText(patient['patient_code']))),
      DataCell(Text(valueText(patient['full_name']))),
      DataCell(Text(valueText(patient['phone_number']))),
      DataCell(StatusBadge(status: apiString(patient['account_status']))),
      DataCell(_actions(patient)),
    ],
  );

  Widget _patientCard(Map<String, dynamic> patient) => Card(
    child: InkWell(
      borderRadius: BorderRadius.circular(AppRadius.card),
      onTap: () => _showDetail(patient),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: Row(
          children: [
            const CircleAvatar(child: Icon(Icons.person_outline)),
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    valueText(patient['full_name']),
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  Text(
                    '${valueText(patient['patient_code'])} · ${valueText(patient['phone_number'])}',
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  StatusBadge(status: apiString(patient['account_status'])),
                ],
              ),
            ),
            _actions(patient),
          ],
        ),
      ),
    ),
  );

  Widget _actions(Map<String, dynamic> patient) => PopupMenuButton<String>(
    tooltip: 'Thao tác bệnh nhân',
    enabled: !mutating,
    onSelected: (value) {
      switch (value) {
        case 'view':
          _showDetail(patient);
        case 'edit':
          editPatient(patient);
        case 'phone':
          changePhone(patient);
        case 'archive':
          archivePatient(patient);
        case 'restore':
          restorePatient(patient);
      }
    },
    itemBuilder: (_) => [
      const PopupMenuItem(value: 'view', child: Text('Xem chi tiết')),
      if (can('patient.manage.all'))
        const PopupMenuItem(value: 'edit', child: Text('Chỉnh sửa')),
      if (can('patient.phone.change.at_counter'))
        const PopupMenuItem(value: 'phone', child: Text('Đổi số điện thoại')),
      if (can('patient.manage.all') && can('account.status.manage'))
        PopupMenuItem(
          value: patient['is_deleted'] == true ? 'restore' : 'archive',
          child: Text(patient['is_deleted'] == true ? 'Khôi phục' : 'Lưu trữ'),
        ),
    ],
  );

  Future<void> _showDetail(Map<String, dynamic> initial) async {
    var patient = Map<String, dynamic>.of(initial);
    await showDialog<void>(
      context: context,
      builder: (_) => StatefulBuilder(
        builder: (context, setDetail) => Dialog(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 760, maxHeight: 720),
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.xl),
              child: SingleChildScrollView(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            valueText(patient['full_name']),
                            style: Theme.of(context).textTheme.headlineSmall,
                          ),
                        ),
                        IconButton(
                          tooltip: 'Đóng',
                          onPressed: () => Navigator.pop(context),
                          icon: const Icon(Icons.close),
                        ),
                      ],
                    ),
                    const SizedBox(height: AppSpacing.lg),
                    _patientDetailCard(patient),
                    const SizedBox(height: 16),
                    AccountActions(
                      session: widget.session,
                      profile: patient,
                      profilePath: '/api/v1/patients/${patient['id']}',
                      onChanged: (updated) {
                        setDetail(() => patient = updated);
                        if (mounted) load();
                      },
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _patientDetailCard(Map<String, dynamic> patient) => SectionCard(
    title: 'Thông tin bệnh nhân',
    child: Wrap(
      spacing: AppSpacing.xl,
      children: [
        DetailField(
          label: 'Mã bệnh nhân',
          value: apiString(patient['patient_code']),
        ),
        DetailField(label: 'Họ tên', value: apiString(patient['full_name'])),
        DetailField(
          label: 'Số điện thoại',
          value: apiString(patient['phone_number']),
        ),
        DetailField(
          label: 'Ngày sinh',
          value: formatDate(patient['date_of_birth']),
        ),
        DetailField(
          label: 'Giới tính',
          value: enumLabel(patient['gender_code']),
        ),
        DetailField(label: 'CCCD', value: apiString(patient['cccd'])),
        DetailField(
          label: 'Bảo hiểm y tế',
          value: apiString(patient['health_insurance_number']),
        ),
        DetailField(label: 'Địa chỉ', value: apiString(patient['address'])),
        DetailField(
          label: 'Trạng thái tài khoản',
          value: apiString(patient['account_status']),
          status: true,
        ),
        DetailField(
          label: 'Tiền sử bệnh',
          value: apiString(patient['medical_history']),
        ),
        DetailField(label: 'Dị ứng', value: apiString(patient['allergies'])),
      ],
    ),
  );
}

class _PatientFormDialog extends StatefulWidget {
  const _PatientFormDialog({this.create = false, this.initial});

  final bool create;
  final Map<String, dynamic>? initial;

  @override
  State<_PatientFormDialog> createState() => _PatientFormDialogState();
}

class _PatientFormDialogState extends State<_PatientFormDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController phone;
  late final TextEditingController name;
  late final TextEditingController dob;
  late final TextEditingController address;
  late final TextEditingController cccd;
  late final TextEditingController insurance;
  late final TextEditingController history;
  late final TextEditingController allergies;
  late String gender;

  @override
  void initState() {
    super.initState();
    final value = widget.initial ?? const <String, dynamic>{};
    phone = TextEditingController(text: apiString(value['phone_number']));
    name = TextEditingController(text: apiString(value['full_name']));
    dob = TextEditingController(text: dateInputValue(value['date_of_birth']));
    address = TextEditingController(text: apiString(value['address']));
    cccd = TextEditingController(text: apiString(value['cccd']));
    insurance = TextEditingController(
      text: apiString(value['health_insurance_number']),
    );
    history = TextEditingController(text: apiString(value['medical_history']));
    allergies = TextEditingController(text: apiString(value['allergies']));
    gender = apiString(value['gender_code']);
    if (!{'MALE', 'FEMALE', 'OTHER', 'UNSPECIFIED'}.contains(gender)) {
      gender = 'UNSPECIFIED';
    }
  }

  @override
  void dispose() {
    for (final controller in [
      phone,
      name,
      dob,
      address,
      cccd,
      insurance,
      history,
      allergies,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(
      widget.create ? 'Tạo bệnh nhân tại quầy' : 'Chỉnh sửa bệnh nhân',
    ),
    content: SizedBox(
      width: 720,
      child: Form(
        key: formKey,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const ApiFormNotice(),
              if (widget.create) ...[
                TextFormField(
                  controller: phone,
                  autofocus: true,
                  keyboardType: TextInputType.phone,
                  decoration: InputDecoration(
                    errorText: apiFieldError(context, 'phone_number'),
                    labelText: 'Số điện thoại *',
                  ),
                  validator: _required,
                ),
                const SizedBox(height: AppSpacing.sm),
              ],
              TextFormField(
                controller: name,
                autofocus: !widget.create,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'full_name'),
                  labelText: 'Họ tên *',
                ),
                validator: _required,
              ),
              const SizedBox(height: AppSpacing.sm),
              Row(
                children: [
                  Expanded(
                    child: DateField(
                      controller: dob,
                      label: 'Ngày sinh',
                      lastDate: clinicNow(),
                      errorText: apiFieldError(context, 'date_of_birth'),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(
                    child: DropdownButtonFormField<String>(
                      isExpanded: true,
                      initialValue: gender,
                      decoration: const InputDecoration(labelText: 'Giới tính'),
                      items: const [
                        DropdownMenuItem(
                          value: 'UNSPECIFIED',
                          child: Text('Chưa cung cấp'),
                        ),
                        DropdownMenuItem(value: 'MALE', child: Text('Nam')),
                        DropdownMenuItem(value: 'FEMALE', child: Text('Nữ')),
                        DropdownMenuItem(value: 'OTHER', child: Text('Khác')),
                      ],
                      onChanged: (value) => gender = value ?? 'UNSPECIFIED',
                    ),
                  ),
                ],
              ),
              const SizedBox(height: AppSpacing.sm),
              TextFormField(
                controller: address,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'address'),
                  labelText: 'Địa chỉ',
                ),
              ),
              const SizedBox(height: AppSpacing.sm),
              Row(
                children: [
                  Expanded(
                    child: TextFormField(
                      controller: cccd,
                      decoration: InputDecoration(
                        errorText: apiFieldError(context, 'cccd'),
                        labelText: 'CCCD',
                      ),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(
                    child: TextFormField(
                      controller: insurance,
                      decoration: InputDecoration(
                        errorText: apiFieldError(
                          context,
                          'health_insurance_number',
                        ),
                        labelText: 'Mã BHYT',
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: AppSpacing.sm),
              TextFormField(
                controller: history,
                maxLines: 3,
                decoration: const InputDecoration(labelText: 'Tiền sử bệnh'),
              ),
              const SizedBox(height: AppSpacing.sm),
              TextFormField(
                controller: allergies,
                maxLines: 3,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'allergies'),
                  labelText: 'Dị ứng',
                ),
              ),
            ],
          ),
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: apiFormBusy(context) ? null : () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        onPressed: apiFormBusy(context)
            ? null
            : () {
                if (!(formKey.currentState?.validate() ?? false)) return;
                submitApiForm(context, <String, dynamic>{
                  if (widget.create) 'phone_number': phone.text.trim(),
                  if (widget.create) 'password': '',
                  'full_name': name.text.trim(),
                  'date_of_birth': dob.text.trim(),
                  'gender_code': gender,
                  'address': address.text.trim(),
                  'cccd': cccd.text.trim(),
                  'health_insurance_number': insurance.text.trim(),
                  'medical_history': history.text.trim(),
                  'allergies': allergies.text.trim(),
                });
              },
        child: Text(widget.create ? 'Tạo bệnh nhân' : 'Lưu thay đổi'),
      ),
    ],
  );

  String? _required(String? value) =>
      value == null || value.trim().isEmpty ? 'Trường này là bắt buộc.' : null;
}

class _PhoneChangeDialog extends StatefulWidget {
  const _PhoneChangeDialog({required this.currentPhone});

  final String currentPhone;

  @override
  State<_PhoneChangeDialog> createState() => _PhoneChangeDialogState();
}

class _PhoneChangeDialogState extends State<_PhoneChangeDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController phone;
  final reason = TextEditingController();

  @override
  void initState() {
    super.initState();
    phone = TextEditingController(text: widget.currentPhone);
  }

  @override
  void dispose() {
    phone.dispose();
    reason.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Đổi số điện thoại đăng nhập'),
    content: SizedBox(
      width: 480,
      child: Form(
        key: formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const InlineMessage(
              message: 'Thao tác này sẽ thu hồi các phiên đăng nhập hiện tại.',
              tone: AppStatusTone.warning,
            ),
            const SizedBox(height: AppSpacing.md),
            TextFormField(
              controller: phone,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Số điện thoại mới *',
              ),
              validator: _required,
            ),
            const SizedBox(height: AppSpacing.sm),
            TextFormField(
              controller: reason,
              maxLines: 3,
              decoration: const InputDecoration(labelText: 'Lý do *'),
              validator: _required,
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
        onPressed: () {
          if (!(formKey.currentState?.validate() ?? false)) return;
          Navigator.pop(context, {
            'phone_number': phone.text.trim(),
            'reason': reason.text.trim(),
          });
        },
        child: const Text('Đổi số điện thoại'),
      ),
    ],
  );

  String? _required(String? value) =>
      value == null || value.trim().isEmpty ? 'Trường này là bắt buộc.' : null;
}

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

class StaffScreen extends StatefulWidget {
  const StaffScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<StaffScreen> createState() => _StaffScreenState();
}

class _StaffScreenState extends State<StaffScreen> {
  final search = TextEditingController();
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination(pageSize: 50);
  Map<String, dynamic>? ownProfile;
  bool loading = true;
  bool mutating = false;
  String? error;
  String employmentStatus = '';

  bool can(String permission) =>
      widget.session.permissions.contains(permission);
  bool get listMode => can('staff.view.all');

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

  Future<void> load([String? keyword, int? targetPage]) async {
    final request = paging.start({
      if ((keyword ?? search.text).trim().isNotEmpty)
        'keyword': (keyword ?? search.text).trim(),
      if (employmentStatus.isNotEmpty) 'employment_status': employmentStatus,
      'include_deleted': 'true',
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      if (!listMode) {
        final response = await widget.session.requestJson(
          'GET',
          '/api/v1/me/staff-profile',
        );
        if (!mounted || !paging.accept(request, response)) return;
        ownProfile = response;
      } else {
        final response = await widget.session.requestJson(
          'GET',
          '/api/v1/staff',
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

  Future<void> createStaff() async {
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => _StaffFormDialog(create: true, session: widget.session),
      submit: (values) => widget.session.requestJson(
        'POST',
        '/api/v1/staff/accounts',
        body: values,
      ),
    );
    if (response == null || !mounted) return;
    final password = apiString(response['temporary_password']);
    if (password.isNotEmpty) await _showTemporaryPassword(password);
    if (mounted) {
      showSuccessMessage(context, 'Đã tạo nhân sự.');
      await load();
    }
  }

  Future<void> editStaff(Map<String, dynamic> staff) async {
    final result = await showApiFormDialog(
      context: context,
      builder: (_) => _StaffFormDialog(initial: staff, session: widget.session),
      submit: (values) => widget.session.requestJson(
        'PATCH',
        '/api/v1/staff/${staff['id']}',
        body: {'expected_version': staff['version'], ...values},
      ),
    );
    if (result != null && mounted) {
      showSuccessMessage(context, 'Đã cập nhật nhân sự.');
      await load();
    }
  }

  Future<void> changeStatus(
    Map<String, dynamic> staff,
    String command,
    String label,
  ) async {
    final reason = await showReasonDialog(
      context,
      title: '$label nhân sự?',
      message:
          'Thao tác áp dụng cho ${apiString(staff['staff_code'])} — ${apiString(staff['full_name'])}.',
      confirmLabel: label,
      destructive: command != 'activate' && command != 'restore',
    );
    if (reason == null || !mounted) return;
    await _mutate(() async {
      final method = command == 'archive' ? 'DELETE' : 'POST';
      await widget.session.requestJson(
        method,
        '/api/v1/staff/${staff['id']}${command == 'archive' ? '' : '/$command'}',
        body: {
          'expected_version': staff['version'],
          'reason': reason,
          if ({'activate', 'deactivate', 'terminate'}.contains(command))
            'confirm_existing_overrides': false,
        },
      );
      if (mounted) showSuccessMessage(context, 'Đã $label nhân sự.');
    });
  }

  Future<void> doctorProfileAction(
    Map<String, dynamic> staff,
    String command,
    String label,
  ) async {
    String? reason;
    if (command != 'create') {
      reason = await showReasonDialog(
        context,
        title: '$label hồ sơ bác sĩ?',
        message: 'Hành động không tự thay đổi lịch làm việc đã tồn tại.',
        confirmLabel: label,
        destructive: command == 'deactivate',
      );
      if (reason == null || !mounted) return;
    } else {
      final confirmed = await showConfirmActionDialog(
        context,
        title: 'Tạo hồ sơ bác sĩ?',
        message: 'Hồ sơ bác sĩ sẽ được tạo cho nhân sự này.',
        confirmLabel: 'Tạo hồ sơ',
      );
      if (!confirmed || !mounted) return;
    }
    await _mutate(() async {
      final suffix = command == 'create'
          ? '/doctor-profile'
          : '/doctor-profile/$command';
      await widget.session.requestJson(
        'POST',
        '/api/v1/staff/${staff['id']}$suffix',
        body: {'expected_version': staff['version'], 'reason': ?reason},
      );
      if (mounted) showSuccessMessage(context, 'Đã $label hồ sơ bác sĩ.');
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
                'Bàn giao trực tiếp cho nhân sự. Người dùng phải đổi mật khẩu trước khi vào nghiệp vụ.',
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
  Widget build(BuildContext context) {
    if (!listMode) return _ownProfilePage();
    return AppPageScaffold(
      title: 'Nhân sự',
      description:
          'Quản lý tài khoản nội bộ, trạng thái làm việc và hồ sơ bác sĩ.',
      actions: [
        if (can('staff.account.create') && can('user.role.assign'))
          FilledButton.icon(
            onPressed: mutating ? null : createStaff,
            icon: const Icon(Icons.badge_outlined),
            label: const Text('Tạo nhân sự'),
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
                hintText: 'Tìm theo mã, tên hoặc username',
                onSearch: load,
                filters: [
                  SizedBox(
                    width: 210,
                    child: DropdownButtonFormField<String>(
                      isExpanded: true,
                      initialValue: employmentStatus,
                      decoration: const InputDecoration(
                        labelText: 'Trạng thái làm việc',
                      ),
                      items: const [
                        DropdownMenuItem(value: '', child: Text('Tất cả')),
                        DropdownMenuItem(
                          value: 'ACTIVE',
                          child: Text('Đang làm việc'),
                        ),
                        DropdownMenuItem(
                          value: 'INACTIVE',
                          child: Text('Tạm ngừng'),
                        ),
                        DropdownMenuItem(
                          value: 'TERMINATED',
                          child: Text('Đã nghỉ việc'),
                        ),
                      ],
                      onChanged: (value) {
                        employmentStatus = value ?? '';
                        load();
                      },
                    ),
                  ),
                ],
                onClear: () {
                  search.clear();
                  employmentStatus = '';
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
                  DataColumn(label: Text('Mã')),
                  DataColumn(label: Text('Nhân sự')),
                  DataColumn(label: Text('Vai trò')),
                  DataColumn(label: Text('Làm việc')),
                  DataColumn(label: Text('Tài khoản')),
                  DataColumn(label: Text('Thao tác')),
                ],
                rows: items.map(_staffRow).toList(),
                mobileCards: items.map(_staffCard).toList(),
                emptyTitle: 'Chưa có nhân sự',
                emptyMessage: 'Không có nhân sự phù hợp với bộ lọc hiện tại.',
              ),
            ],
          ),
        ),
      ],
    );
  }

  Widget _ownProfilePage() => AppPageScaffold(
    title: 'Hồ sơ nhân sự của tôi',
    description: 'Thông tin nhân sự nội bộ ở chế độ chỉ đọc.',
    maxWidth: 960,
    children: [
      AsyncContent(
        loading: loading,
        error: error,
        onRetry: load,
        empty: ownProfile == null,
        child: ownProfile == null
            ? const SizedBox.shrink()
            : _staffDetailCard(ownProfile!),
      ),
    ],
  );

  DataRow _staffRow(Map<String, dynamic> staff) => DataRow(
    onSelectChanged: (_) => _showDetail(staff),
    cells: [
      DataCell(Text(valueText(staff['staff_code']))),
      DataCell(
        Column(
          mainAxisAlignment: MainAxisAlignment.center,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(valueText(staff['full_name'])),
            Text(
              valueText(staff['username']),
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
      DataCell(Text(enumLabel(staff['role_code']))),
      DataCell(StatusBadge(status: apiString(staff['employment_status']))),
      DataCell(StatusBadge(status: apiString(staff['account_status']))),
      DataCell(_actions(staff)),
    ],
  );

  Widget _staffCard(Map<String, dynamic> staff) => Card(
    child: InkWell(
      borderRadius: BorderRadius.circular(AppRadius.card),
      onTap: () => _showDetail(staff),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: Row(
          children: [
            const CircleAvatar(child: Icon(Icons.badge_outlined)),
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    valueText(staff['full_name']),
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  Text(
                    '${valueText(staff['staff_code'])} · ${enumLabel(staff['role_code'])}',
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  Wrap(
                    spacing: AppSpacing.xs,
                    children: [
                      StatusBadge(
                        status: apiString(staff['employment_status']),
                      ),
                      StatusBadge(status: apiString(staff['account_status'])),
                    ],
                  ),
                ],
              ),
            ),
            _actions(staff),
          ],
        ),
      ),
    ),
  );

  Widget _actions(Map<String, dynamic> staff) => PopupMenuButton<String>(
    tooltip: 'Thao tác nhân sự',
    enabled: !mutating,
    onSelected: (value) {
      switch (value) {
        case 'view':
          _showDetail(staff);
        case 'edit':
          editStaff(staff);
        case 'activate':
          changeStatus(staff, 'activate', 'kích hoạt');
        case 'deactivate':
          changeStatus(staff, 'deactivate', 'tạm ngừng');
        case 'terminate':
          changeStatus(staff, 'terminate', 'cho nghỉ việc');
        case 'archive':
          changeStatus(staff, 'archive', 'lưu trữ');
        case 'restore':
          changeStatus(staff, 'restore', 'khôi phục');
        case 'doctor-create':
          doctorProfileAction(staff, 'create', 'tạo');
        case 'doctor-activate':
          doctorProfileAction(staff, 'activate', 'kích hoạt');
        case 'doctor-deactivate':
          doctorProfileAction(staff, 'deactivate', 'vô hiệu hóa');
      }
    },
    itemBuilder: (_) {
      final doctor = apiMap(staff['doctor_profile']);
      return [
        const PopupMenuItem(value: 'view', child: Text('Xem chi tiết')),
        if (can('staff.manage'))
          const PopupMenuItem(value: 'edit', child: Text('Chỉnh sửa')),
        if (can('staff.manage') && can('account.status.manage')) ...[
          if (staff['is_deleted'] == true)
            const PopupMenuItem(value: 'restore', child: Text('Khôi phục'))
          else ...[
            const PopupMenuItem(value: 'activate', child: Text('Kích hoạt')),
            const PopupMenuItem(value: 'deactivate', child: Text('Tạm ngừng')),
            const PopupMenuItem(
              value: 'terminate',
              child: Text('Cho nghỉ việc'),
            ),
            const PopupMenuItem(value: 'archive', child: Text('Lưu trữ')),
          ],
        ],
        if (can('staff.manage')) ...[
          if (doctor.isEmpty)
            const PopupMenuItem(
              value: 'doctor-create',
              child: Text('Tạo hồ sơ bác sĩ'),
            )
          else if (doctor['status'] == 'ACTIVE')
            const PopupMenuItem(
              value: 'doctor-deactivate',
              child: Text('Vô hiệu hóa hồ sơ bác sĩ'),
            )
          else
            const PopupMenuItem(
              value: 'doctor-activate',
              child: Text('Kích hoạt hồ sơ bác sĩ'),
            ),
        ],
      ];
    },
  );

  Future<void> _showDetail(Map<String, dynamic> initial) async {
    var staff = Map<String, dynamic>.of(initial);
    await showDialog<void>(
      context: context,
      builder: (_) => StatefulBuilder(
        builder: (context, setDetail) => Dialog(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 800, maxHeight: 720),
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
                            valueText(staff['full_name']),
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
                    _staffDetailCard(staff),
                    const SizedBox(height: 16),
                    AccountActions(
                      session: widget.session,
                      profile: staff,
                      profilePath: '/api/v1/staff/${staff['id']}',
                      onChanged: (updated) {
                        setDetail(() => staff = updated);
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

  Widget _staffDetailCard(Map<String, dynamic> staff) {
    final doctor = apiMap(staff['doctor_profile']);
    return Column(
      children: [
        SectionCard(
          title: 'Hồ sơ nhân sự',
          child: Wrap(
            spacing: AppSpacing.xl,
            children: [
              DetailField(
                label: 'Mã nhân sự',
                value: apiString(staff['staff_code']),
              ),
              DetailField(
                label: 'Họ tên',
                value: apiString(staff['full_name']),
              ),
              DetailField(
                label: 'Username',
                value: apiString(staff['username']),
              ),
              DetailField(
                label: 'Vai trò',
                value: enumLabel(staff['role_code']),
              ),
              DetailField(
                label: 'Điện thoại',
                value: apiString(staff['phone_number']),
              ),
              DetailField(label: 'Email', value: apiString(staff['email'])),
              DetailField(
                label: 'Ngày sinh',
                value: formatDate(staff['date_of_birth']),
              ),
              DetailField(label: 'Địa chỉ', value: apiString(staff['address'])),
              DetailField(
                label: 'Trạng thái làm việc',
                value: apiString(staff['employment_status']),
                status: true,
              ),
              DetailField(
                label: 'Trạng thái tài khoản',
                value: apiString(staff['account_status']),
                status: true,
              ),
            ],
          ),
        ),
        if (doctor.isNotEmpty) ...[
          const SizedBox(height: AppSpacing.md),
          SectionCard(
            title: 'Hồ sơ bác sĩ',
            child: Wrap(
              spacing: AppSpacing.xl,
              children: [
                DetailField(
                  label: 'Mã bác sĩ',
                  value: apiString(doctor['doctor_code']),
                ),
                DetailField(
                  label: 'Trạng thái',
                  value: apiString(doctor['status']),
                  status: true,
                ),
                DetailField(label: 'Doctor ID', value: apiString(doctor['id'])),
              ],
            ),
          ),
        ],
      ],
    );
  }
}

class _StaffFormDialog extends StatefulWidget {
  const _StaffFormDialog({
    required this.session,
    this.create = false,
    this.initial,
  });
  final SessionController session;

  final bool create;
  final Map<String, dynamic>? initial;

  @override
  State<_StaffFormDialog> createState() => _StaffFormDialogState();
}

class _StaffFormDialogState extends State<_StaffFormDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController username;
  late final TextEditingController roleId;
  late final TextEditingController name;
  late final TextEditingController phone;
  late final TextEditingController email;
  late final TextEditingController dob;
  late final TextEditingController address;
  late String gender;
  bool createDoctor = false;
  List<Map<String, dynamic>> roles = const [];
  String? roleError;
  bool rolesLoading = true;
  Future<void> loadRoles() async {
    setState(() {
      rolesLoading = true;
      roleError = null;
    });
    try {
      final values = apiItems(
        await widget.session.requestJson(
          'GET',
          '/api/v1/roles',
          query: {'account_scope': 'STAFF'},
        ),
      );
      if (mounted) setState(() => roles = values);
    } catch (cause) {
      if (mounted) setState(() => roleError = friendlyError(cause));
    } finally {
      if (mounted) setState(() => rolesLoading = false);
    }
  }

  @override
  void initState() {
    super.initState();
    final value = widget.initial ?? const <String, dynamic>{};
    username = TextEditingController(text: apiString(value['username']));
    roleId = TextEditingController(text: apiString(value['role_id']));
    name = TextEditingController(text: apiString(value['full_name']));
    phone = TextEditingController(text: apiString(value['phone_number']));
    email = TextEditingController(text: apiString(value['email']));
    dob = TextEditingController(text: dateInputValue(value['date_of_birth']));
    address = TextEditingController(text: apiString(value['address']));
    if (widget.create) loadRoles();
    gender = apiString(value['gender_code']);
    if (!{'MALE', 'FEMALE', 'OTHER', 'UNSPECIFIED'}.contains(gender)) {
      gender = 'UNSPECIFIED';
    }
  }

  @override
  void dispose() {
    for (final controller in [
      username,
      roleId,
      name,
      phone,
      email,
      dob,
      address,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.create ? 'Tạo nhân sự' : 'Chỉnh sửa nhân sự'),
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
                if (roleError != null)
                  InlineMessage(
                    message: roleError!,
                    tone: AppStatusTone.danger,
                    action: TextButton(
                      onPressed: loadRoles,
                      child: const Text('Tải lại vai trò'),
                    ),
                  ),
                Row(
                  children: [
                    Expanded(
                      child: TextFormField(
                        controller: username,
                        autofocus: true,
                        decoration: InputDecoration(
                          errorText: apiFieldError(context, 'username'),
                          labelText: 'Username *',
                        ),
                        validator: _required,
                      ),
                    ),
                    const SizedBox(width: AppSpacing.sm),
                    Expanded(
                      child: DropdownButtonFormField<int>(
                        isExpanded: true,
                        initialValue: int.tryParse(roleId.text),
                        decoration: InputDecoration(
                          labelText: 'Vai trò *',
                          errorText: apiFieldError(context, 'role_id'),
                          helperText: rolesLoading ? 'Đang tải vai trò…' : null,
                        ),
                        items: roles
                            .map(
                              (role) => DropdownMenuItem(
                                value: apiInt(role['id']),
                                child: Text(apiString(role['name'])),
                              ),
                            )
                            .toList(),
                        onChanged: rolesLoading
                            ? null
                            : (value) => roleId.text = value?.toString() ?? '',
                        validator: (value) =>
                            value == null ? 'Chọn vai trò.' : null,
                      ),
                    ),
                  ],
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
                    child: TextFormField(
                      controller: phone,
                      keyboardType: TextInputType.phone,
                      decoration: InputDecoration(
                        errorText: apiFieldError(context, 'phone_number'),
                        labelText: 'Điện thoại',
                      ),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(
                    child: TextFormField(
                      controller: email,
                      keyboardType: TextInputType.emailAddress,
                      decoration: InputDecoration(
                        errorText: apiFieldError(context, 'email'),
                        labelText: 'Email',
                      ),
                    ),
                  ),
                ],
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
              if (widget.create)
                CheckboxListTile(
                  contentPadding: EdgeInsets.zero,
                  value: createDoctor,
                  onChanged: (value) =>
                      setState(() => createDoctor = value ?? false),
                  title: const Text('Tạo hồ sơ bác sĩ'),
                  subtitle: const Text(
                    'Chuyên khoa và lịch làm việc được cấu hình ở module tương ứng.',
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
                  if (widget.create) 'username': username.text.trim(),
                  if (widget.create) 'role_id': int.parse(roleId.text),
                  'full_name': name.text.trim(),
                  'phone_number': phone.text.trim(),
                  'email': email.text.trim(),
                  'date_of_birth': dob.text.trim(),
                  'gender_code': gender,
                  'address': address.text.trim(),
                  if (widget.create) 'create_doctor_profile': createDoctor,
                });
              },
        child: Text(widget.create ? 'Tạo nhân sự' : 'Lưu thay đổi'),
      ),
    ],
  );

  String? _required(String? value) =>
      value == null || value.trim().isEmpty ? 'Trường này là bắt buộc.' : null;
}

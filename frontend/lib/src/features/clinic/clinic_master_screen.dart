import '../shared/list_pagination.dart';
import 'package:flutter/material.dart';
import '../../design_system/components/entity_picker.dart';

import '../../design_system/components/data_views.dart';
import '../../design_system/components/dialogs.dart';
import '../../design_system/components/feedback.dart';
import '../../design_system/components/layout.dart';
import '../../design_system/formatters/app_formatters.dart';
import '../../design_system/theme/app_theme.dart';
import '../../session_controller.dart';
import '../shared/api_helpers.dart';
import '../shared/api_form.dart';

class ClinicMasterScreen extends StatelessWidget {
  const ClinicMasterScreen({super.key, required this.session});

  final SessionController session;

  @override
  Widget build(BuildContext context) => DefaultTabController(
    length: session.permissions.contains('doctor_specialty.manage') ? 3 : 2,
    child: Column(
      children: [
        Material(
          color: AppColors.surface,
          child: TabBar(
            isScrollable: true,
            tabs: [
              const Tab(
                icon: Icon(Icons.medical_services_outlined),
                text: 'Chuyên khoa',
              ),
              const Tab(icon: Icon(Icons.meeting_room_outlined), text: 'Phòng'),
              if (session.permissions.contains('doctor_specialty.manage'))
                const Tab(icon: Icon(Icons.link), text: 'Bác sĩ–chuyên khoa'),
            ],
          ),
        ),
        Expanded(
          child: TabBarView(
            children: [
              _SpecialtiesPanel(session: session),
              _RoomsPanel(session: session),
              if (session.permissions.contains('doctor_specialty.manage'))
                _DoctorSpecialtiesPanel(session: session),
            ],
          ),
        ),
      ],
    ),
  );
}

class _SpecialtiesPanel extends StatefulWidget {
  const _SpecialtiesPanel({required this.session});

  final SessionController session;

  @override
  State<_SpecialtiesPanel> createState() => _SpecialtiesPanelState();
}

class _SpecialtiesPanelState extends State<_SpecialtiesPanel> {
  final search = TextEditingController();
  final paging = ListPagination();
  List<Map<String, dynamic>> items = const [];
  bool loading = true;
  bool mutating = false;
  bool activeOnly = false;
  String? error;

  bool get canManage => widget.session.permissions.contains('specialty.manage');

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
      'active_only': activeOnly.toString(),
      if ((query ?? search.text).trim().isNotEmpty)
        'q': (query ?? search.text).trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/specialties',
        query: request.query,
      );
      if (!mounted || !paging.accept(request, response)) return;
      items = apiItems(response);
    } catch (cause) {
      if (mounted && paging.isCurrent(request)) error = friendlyError(cause);
    } finally {
      if (mounted && paging.isCurrent(request)) setState(() => loading = false);
    }
  }

  Future<void> createOrEdit([Map<String, dynamic>? item]) async {
    final result = await showApiFormDialog(
      context: context,
      builder: (_) => _SpecialtyDialog(initial: item),
      submit: (values) => widget.session.requestJson(
        item == null ? 'POST' : 'PATCH',
        item == null
            ? '/api/v1/specialties'
            : '/api/v1/specialties/${item['id']}',
        body: item == null
            ? values
            : {'expected_version': item['version'], ...values},
      ),
    );
    if (result != null && mounted) {
      showSuccessMessage(context, 'Đã lưu danh mục phòng khám.');
      await load();
    }
  }

  Future<void> changeState(Map<String, dynamic> item, String action) async {
    String reason = '';
    if (action != 'activate') {
      final value = await showReasonDialog(
        context,
        title: action == 'archive'
            ? 'Lưu trữ chuyên khoa?'
            : 'Vô hiệu hóa chuyên khoa?',
        message:
            'Hệ thống sẽ kiểm tra lịch hẹn và quan hệ bác sĩ đang phụ thuộc.',
        confirmLabel: action == 'archive' ? 'Lưu trữ' : 'Vô hiệu hóa',
        destructive: true,
      );
      if (value == null || !mounted) return;
      reason = value;
    } else {
      final confirmed = await showConfirmActionDialog(
        context,
        title: 'Kích hoạt chuyên khoa?',
        message: 'Chuyên khoa sẽ có thể được dùng trong các luồng mới.',
        confirmLabel: 'Kích hoạt',
      );
      if (!confirmed || !mounted) return;
    }
    await _mutate(() async {
      await widget.session.requestJson(
        action == 'archive' ? 'DELETE' : 'POST',
        '/api/v1/specialties/${item['id']}${action == 'archive' ? '' : '/$action'}',
        body: {'expected_version': item['version'], 'reason': reason},
      );
      if (mounted) {
        showSuccessMessage(context, 'Đã cập nhật trạng thái chuyên khoa.');
      }
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

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Chuyên khoa',
    description:
        'Danh mục chuyên khoa dùng cho bác sĩ, lịch làm việc và đặt lịch.',
    actions: [
      if (canManage)
        FilledButton.icon(
          onPressed: mutating ? null : () => createOrEdit(),
          icon: const Icon(Icons.add),
          label: const Text('Tạo chuyên khoa'),
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
              hintText: 'Tìm theo mã hoặc tên chuyên khoa',
              onSearch: load,
              filters: [
                FilterChip(
                  label: const Text('Chỉ đang hoạt động'),
                  selected: activeOnly,
                  onSelected: (value) {
                    activeOnly = value;
                    load();
                  },
                ),
              ],
              onClear: () {
                search.clear();
                activeOnly = false;
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
                DataColumn(label: Text('Tên chuyên khoa')),
                DataColumn(label: Text('Trạng thái')),
                DataColumn(label: Text('Thao tác')),
              ],
              rows: items.map(_row).toList(),
              mobileCards: items.map(_card).toList(),
            ),
          ],
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    cells: [
      DataCell(Text(valueText(item['code']))),
      DataCell(Text(valueText(item['name']))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(_actions(item)),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: Padding(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: Row(
        children: [
          const Icon(Icons.medical_services_outlined),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  valueText(item['name']),
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                Text(valueText(item['code'])),
                const SizedBox(height: AppSpacing.xs),
                StatusBadge(status: apiString(item['status'])),
              ],
            ),
          ),
          _actions(item),
        ],
      ),
    ),
  );

  Widget _actions(Map<String, dynamic> item) => PopupMenuButton<String>(
    tooltip: 'Thao tác chuyên khoa',
    enabled: canManage && !mutating,
    onSelected: (value) {
      if (value == 'edit') {
        createOrEdit(item);
      } else {
        changeState(item, value);
      }
    },
    itemBuilder: (_) => const [
      PopupMenuItem(value: 'edit', child: Text('Chỉnh sửa')),
      PopupMenuItem(value: 'activate', child: Text('Kích hoạt')),
      PopupMenuItem(value: 'deactivate', child: Text('Vô hiệu hóa')),
      PopupMenuItem(value: 'archive', child: Text('Lưu trữ')),
    ],
  );
}

class _RoomsPanel extends StatefulWidget {
  const _RoomsPanel({required this.session});

  final SessionController session;

  @override
  State<_RoomsPanel> createState() => _RoomsPanelState();
}

class _RoomsPanelState extends State<_RoomsPanel> {
  final search = TextEditingController();
  final paging = ListPagination();
  List<Map<String, dynamic>> items = const [];
  bool loading = true;
  bool mutating = false;
  bool activeOnly = false;
  String roomType = '';
  String? error;

  bool get canManage => widget.session.permissions.contains('room.manage');

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
      'active_only': activeOnly.toString(),
      if (roomType.isNotEmpty) 'room_type': roomType,
      if ((query ?? search.text).trim().isNotEmpty)
        'q': (query ?? search.text).trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/rooms',
        query: request.query,
      );
      if (!mounted || !paging.accept(request, response)) return;
      items = apiItems(response);
    } catch (cause) {
      if (mounted && paging.isCurrent(request)) error = friendlyError(cause);
    } finally {
      if (mounted && paging.isCurrent(request)) setState(() => loading = false);
    }
  }

  Future<void> createOrEdit([Map<String, dynamic>? item]) async {
    final result = await showApiFormDialog(
      context: context,
      builder: (_) => _RoomDialog(initial: item),
      submit: (values) => widget.session.requestJson(
        item == null ? 'POST' : 'PATCH',
        item == null ? '/api/v1/rooms' : '/api/v1/rooms/${item['id']}',
        body: item == null
            ? values
            : {'expected_version': item['version'], ...values},
      ),
    );
    if (result != null && mounted) {
      showSuccessMessage(context, 'Đã lưu danh mục phòng khám.');
      await load();
    }
  }

  Future<void> changeState(Map<String, dynamic> item, String action) async {
    String reason = '';
    if (action != 'activate') {
      final value = await showReasonDialog(
        context,
        title: action == 'archive' ? 'Lưu trữ phòng?' : 'Vô hiệu hóa phòng?',
        message:
            'Hệ thống sẽ kiểm tra lịch làm việc và lịch hẹn đang phụ thuộc.',
        confirmLabel: action == 'archive' ? 'Lưu trữ' : 'Vô hiệu hóa',
        destructive: true,
      );
      if (value == null || !mounted) return;
      reason = value;
    } else {
      final confirmed = await showConfirmActionDialog(
        context,
        title: 'Kích hoạt phòng?',
        message: 'Phòng sẽ có thể được chọn cho lịch làm việc mới.',
        confirmLabel: 'Kích hoạt',
      );
      if (!confirmed || !mounted) return;
    }
    await _mutate(() async {
      await widget.session.requestJson(
        action == 'archive' ? 'DELETE' : 'POST',
        '/api/v1/rooms/${item['id']}${action == 'archive' ? '' : '/$action'}',
        body: {'expected_version': item['version'], 'reason': reason},
      );
      if (mounted) showSuccessMessage(context, 'Đã cập nhật trạng thái phòng.');
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

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Phòng khám',
    description: 'Danh mục phòng khám và phòng chức năng của cơ sở.',
    actions: [
      if (canManage)
        FilledButton.icon(
          onPressed: mutating ? null : () => createOrEdit(),
          icon: const Icon(Icons.add),
          label: const Text('Tạo phòng'),
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
              hintText: 'Tìm theo mã hoặc tên phòng',
              onSearch: load,
              filters: [
                SizedBox(
                  width: 180,
                  child: DropdownButtonFormField<String>(
                    isExpanded: true,
                    initialValue: roomType,
                    decoration: const InputDecoration(labelText: 'Loại phòng'),
                    items: const [
                      DropdownMenuItem(value: '', child: Text('Tất cả')),
                      DropdownMenuItem(
                        value: 'CONSULTATION',
                        child: Text('Phòng khám'),
                      ),
                      DropdownMenuItem(
                        value: 'FUNCTIONAL',
                        child: Text('Phòng chức năng'),
                      ),
                      DropdownMenuItem(value: 'OTHER', child: Text('Khác')),
                    ],
                    onChanged: (value) {
                      roomType = value ?? '';
                      load();
                    },
                  ),
                ),
                FilterChip(
                  label: const Text('Chỉ đang hoạt động'),
                  selected: activeOnly,
                  onSelected: (value) {
                    activeOnly = value;
                    load();
                  },
                ),
              ],
              onClear: () {
                search.clear();
                roomType = '';
                activeOnly = false;
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
                DataColumn(label: Text('Tên phòng')),
                DataColumn(label: Text('Loại')),
                DataColumn(label: Text('Tầng')),
                DataColumn(label: Text('Trạng thái')),
                DataColumn(label: Text('Thao tác')),
              ],
              rows: items.map(_row).toList(),
              mobileCards: items.map(_card).toList(),
            ),
          ],
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    cells: [
      DataCell(Text(valueText(item['code']))),
      DataCell(Text(valueText(item['name']))),
      DataCell(Text(enumLabel(item['room_type']))),
      DataCell(Text(valueText(item['floor']))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(_actions(item)),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: Padding(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: Row(
        children: [
          const Icon(Icons.meeting_room_outlined),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  valueText(item['name']),
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                Text(
                  '${valueText(item['code'])} · ${enumLabel(item['room_type'])}',
                ),
                const SizedBox(height: AppSpacing.xs),
                StatusBadge(status: apiString(item['status'])),
              ],
            ),
          ),
          _actions(item),
        ],
      ),
    ),
  );

  Widget _actions(Map<String, dynamic> item) => PopupMenuButton<String>(
    tooltip: 'Thao tác phòng',
    enabled: canManage && !mutating,
    onSelected: (value) {
      if (value == 'edit') {
        createOrEdit(item);
      } else {
        changeState(item, value);
      }
    },
    itemBuilder: (_) => const [
      PopupMenuItem(value: 'edit', child: Text('Chỉnh sửa')),
      PopupMenuItem(value: 'activate', child: Text('Kích hoạt')),
      PopupMenuItem(value: 'deactivate', child: Text('Vô hiệu hóa')),
      PopupMenuItem(value: 'archive', child: Text('Lưu trữ')),
    ],
  );
}

class _DoctorSpecialtiesPanel extends StatefulWidget {
  const _DoctorSpecialtiesPanel({required this.session});

  final SessionController session;

  @override
  State<_DoctorSpecialtiesPanel> createState() =>
      _DoctorSpecialtiesPanelState();
}

class _DoctorSpecialtiesPanelState extends State<_DoctorSpecialtiesPanel> {
  final doctorId = TextEditingController();
  final specialtyId = TextEditingController();

  List<Map<String, dynamic>> items = const [];
  bool loading = false;
  String? error;

  @override
  void dispose() {
    doctorId.dispose();
    specialtyId.dispose();

    super.dispose();
  }

  int? _id(TextEditingController controller) => int.tryParse(controller.text);

  Future<void> load() async {
    final doctor = _id(doctorId);
    if (doctor == null) {
      setState(() => error = 'Chọn bác sĩ để xem chuyên khoa.');
      return;
    }
    setState(() {
      loading = true;
      error = null;
    });
    try {
      items = apiItems(
        await widget.session.requestJson(
          'GET',
          '/api/v1/doctors/$doctor/specialties',
        ),
      );
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> assign() async {
    final doctor = _id(doctorId);
    final specialty = _id(specialtyId);
    if (doctor == null || specialty == null) {
      setState(() => error = 'Chọn bác sĩ và chuyên khoa.');
      return;
    }
    await _run(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/doctors/$doctor/specialties/$specialty',
      );
      if (mounted) {
        showSuccessMessage(context, 'Đã gán chuyên khoa cho bác sĩ.');
      }
    });
  }

  Future<void> remove(Map<String, dynamic> specialty) async {
    final doctor = _id(doctorId);
    final version = apiInt(specialty['relation_version']);
    if (doctor == null || version <= 0) {
      setState(() => error = 'Tải lại danh sách chuyên khoa trước khi bỏ gán.');
      return;
    }
    final reason = await showReasonDialog(
      context,
      title: 'Bỏ gán chuyên khoa?',
      message: 'Quan hệ chuyên khoa sẽ được bỏ khỏi bác sĩ đang chọn.',
      confirmLabel: 'Bỏ gán',
      destructive: true,
    );
    if (reason == null || !mounted) return;
    await _run(() async {
      await widget.session.requestJson(
        'DELETE',
        '/api/v1/doctors/$doctor/specialties/${specialty['id']}',
        body: {'expected_version': version, 'reason': reason},
      );
      if (mounted) showSuccessMessage(context, 'Đã bỏ gán chuyên khoa.');
    });
  }

  Future<void> _run(Future<void> Function() operation) async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      await operation();
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Bác sĩ–chuyên khoa',
    description: 'Xem và quản lý chuyên khoa phụ trách của một bác sĩ.',
    maxWidth: 1100,
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Chọn bác sĩ và chuyên khoa',
        child: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            SizedBox(
              width: 230,
              child: EntityPicker(
                label: 'Bác sĩ',
                search: (query) => searchEntities(
                  widget.session,
                  '/api/v1/staff',
                  query,
                  nameKey: 'full_name',
                  codeKey: 'staff_code',
                  doctors: true,
                ),
                onSelected: (option) {
                  doctorId.text = option?.id.toString() ?? '';
                  setState(() {
                    items = const [];
                    error = null;
                  });
                },
              ),
            ),
            OutlinedButton.icon(
              onPressed: loading ? null : load,
              icon: const Icon(Icons.search),
              label: const Text('Tải chuyên khoa'),
            ),
            SizedBox(
              width: 210,
              child: EntityPicker(
                label: 'Chuyên khoa',
                search: (query) => searchEntities(
                  widget.session,
                  '/api/v1/specialties',
                  query,
                ),
                onSelected: (option) =>
                    specialtyId.text = option?.id.toString() ?? '',
              ),
            ),
            FilledButton.icon(
              onPressed: loading ? null : assign,
              icon: const Icon(Icons.add_link),
              label: const Text('Gán chuyên khoa'),
            ),
          ],
        ),
      ),
      SectionCard(
        title: 'Chuyên khoa đang gán',
        child: AsyncContent(
          loading: loading,
          error: error,
          onRetry: load,
          empty: items.isEmpty,
          keepChildOnError: items.isNotEmpty,
          emptyTitle: 'Chưa có chuyên khoa',
          emptyMessage: 'Chọn bác sĩ rồi tải danh sách.',
          child: Wrap(
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            children: items
                .map(
                  (item) => InputChip(
                    avatar: const Icon(Icons.medical_services_outlined),
                    label: Text(
                      '${valueText(item['code'])} · ${valueText(item['name'])}',
                    ),
                    onDeleted: loading ? null : () => remove(item),
                    deleteButtonTooltipMessage: 'Bỏ gán chuyên khoa',
                  ),
                )
                .toList(),
          ),
        ),
      ),
    ],
  );
}

class _SpecialtyDialog extends StatefulWidget {
  const _SpecialtyDialog({this.initial});

  final Map<String, dynamic>? initial;

  @override
  State<_SpecialtyDialog> createState() => _SpecialtyDialogState();
}

class _SpecialtyDialogState extends State<_SpecialtyDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController code;
  late final TextEditingController name;
  late final TextEditingController description;

  @override
  void initState() {
    super.initState();
    code = TextEditingController(text: apiString(widget.initial?['code']));
    name = TextEditingController(text: apiString(widget.initial?['name']));
    description = TextEditingController(
      text: apiString(widget.initial?['description']),
    );
  }

  @override
  void dispose() {
    code.dispose();
    name.dispose();
    description.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(
      widget.initial == null ? 'Tạo chuyên khoa' : 'Chỉnh sửa chuyên khoa',
    ),
    content: SizedBox(
      width: 560,
      child: Form(
        key: formKey,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const ApiFormNotice(),
            TextFormField(
              controller: code,
              autofocus: true,
              readOnly: widget.initial != null,
              decoration: InputDecoration(
                errorText: apiFieldError(context, 'code'),
                labelText: 'Mã chuyên khoa *',
                helperText: 'Ví dụ: SPC_INTERNAL',
              ),
              validator: _required,
            ),
            const SizedBox(height: AppSpacing.sm),
            TextFormField(
              controller: name,
              decoration: InputDecoration(
                errorText: apiFieldError(context, 'name'),
                labelText: 'Tên chuyên khoa *',
              ),
              validator: _required,
            ),
            const SizedBox(height: AppSpacing.sm),
            TextFormField(
              controller: description,
              maxLines: 3,
              decoration: InputDecoration(
                errorText: apiFieldError(context, 'description'),
                labelText: 'Mô tả',
              ),
            ),
          ],
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
                submitApiForm(context, {
                  if (widget.initial == null) 'code': code.text.trim(),
                  'name': name.text.trim(),
                  'description': description.text.trim(),
                });
              },
        child: const Text('Lưu'),
      ),
    ],
  );

  String? _required(String? value) =>
      value == null || value.trim().isEmpty ? 'Trường này là bắt buộc.' : null;
}

class _RoomDialog extends StatefulWidget {
  const _RoomDialog({this.initial});

  final Map<String, dynamic>? initial;

  @override
  State<_RoomDialog> createState() => _RoomDialogState();
}

class _RoomDialogState extends State<_RoomDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController code;
  late final TextEditingController name;
  late final TextEditingController floor;
  late final TextEditingController location;
  late final TextEditingController description;
  late String roomType;

  @override
  void initState() {
    super.initState();
    code = TextEditingController(text: apiString(widget.initial?['code']));
    name = TextEditingController(text: apiString(widget.initial?['name']));
    floor = TextEditingController(text: apiString(widget.initial?['floor']));
    location = TextEditingController(
      text: apiString(widget.initial?['location_note']),
    );
    description = TextEditingController(
      text: apiString(widget.initial?['description']),
    );
    roomType = apiString(widget.initial?['room_type']);
    if (!{'CONSULTATION', 'FUNCTIONAL', 'OTHER'}.contains(roomType)) {
      roomType = 'CONSULTATION';
    }
  }

  @override
  void dispose() {
    for (final controller in [code, name, floor, location, description]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(widget.initial == null ? 'Tạo phòng' : 'Chỉnh sửa phòng'),
    content: SizedBox(
      width: 600,
      child: Form(
        key: formKey,
        child: SingleChildScrollView(
          child: Column(
            children: [
              const ApiFormNotice(),
              TextFormField(
                controller: code,
                autofocus: true,
                readOnly: widget.initial != null,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'code'),
                  labelText: 'Mã phòng *',
                  helperText: 'Ví dụ: RM-101',
                ),
                validator: _required,
              ),
              const SizedBox(height: AppSpacing.sm),
              TextFormField(
                controller: name,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'name'),
                  labelText: 'Tên phòng *',
                ),
                validator: _required,
              ),
              const SizedBox(height: AppSpacing.sm),
              DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: roomType,
                decoration: const InputDecoration(labelText: 'Loại phòng *'),
                items: const [
                  DropdownMenuItem(
                    value: 'CONSULTATION',
                    child: Text('Phòng khám'),
                  ),
                  DropdownMenuItem(
                    value: 'FUNCTIONAL',
                    child: Text('Phòng chức năng'),
                  ),
                  DropdownMenuItem(value: 'OTHER', child: Text('Khác')),
                ],
                onChanged: (value) => roomType = value ?? 'CONSULTATION',
              ),
              const SizedBox(height: AppSpacing.sm),
              Row(
                children: [
                  Expanded(
                    child: TextFormField(
                      controller: floor,
                      decoration: InputDecoration(
                        errorText: apiFieldError(context, 'floor'),
                        labelText: 'Tầng',
                      ),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(
                    child: TextFormField(
                      controller: location,
                      decoration: InputDecoration(
                        errorText: apiFieldError(context, 'location_note'),
                        labelText: 'Vị trí',
                      ),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: AppSpacing.sm),
              TextFormField(
                controller: description,
                maxLines: 3,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'description'),
                  labelText: 'Mô tả',
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
                submitApiForm(context, {
                  if (widget.initial == null) 'code': code.text.trim(),
                  'name': name.text.trim(),
                  'room_type': roomType,
                  'floor': floor.text.trim(),
                  'location_note': location.text.trim(),
                  'description': description.text.trim(),
                });
              },
        child: const Text('Lưu'),
      ),
    ],
  );

  String? _required(String? value) =>
      value == null || value.trim().isEmpty ? 'Trường này là bắt buộc.' : null;
}

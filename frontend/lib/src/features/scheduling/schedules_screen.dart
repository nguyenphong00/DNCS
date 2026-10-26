import '../shared/list_pagination.dart';
import 'package:flutter/material.dart';
import '../../design_system/components/entity_picker.dart';
import '../../design_system/components/date_fields.dart';

import '../../design_system/components/data_views.dart';
import '../../design_system/components/dialogs.dart';
import '../../design_system/components/feedback.dart';
import '../../design_system/components/layout.dart';
import '../../design_system/formatters/app_formatters.dart';
import '../../design_system/theme/app_theme.dart';
import '../../session_controller.dart';
import '../shared/api_helpers.dart';
import '../shared/api_form.dart';

class SchedulesScreen extends StatefulWidget {
  const SchedulesScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<SchedulesScreen> createState() => _SchedulesScreenState();
}

class _SchedulesScreenState extends State<SchedulesScreen> {
  final doctorId = TextEditingController();
  final roomId = TextEditingController();
  final from = TextEditingController();
  final to = TextEditingController();
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination(pageSize: 100);
  bool loading = true;
  bool mutating = false;
  String status = '';
  String? error;

  bool get canManage =>
      widget.session.permissions.contains('doctor_schedule.manage');

  @override
  void initState() {
    super.initState();
    final now = clinicNow();
    from.text = clinicTimestamp(now, const TimeOfDay(hour: 0, minute: 0));
    to.text = clinicDayEnd(now.add(const Duration(days: 7)));
    load();
  }

  @override
  void dispose() {
    paging.dispose();
    for (final controller in [doctorId, roomId, from, to]) {
      controller.dispose();
    }
    super.dispose();
  }

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (from.text.trim().isNotEmpty) 'from': from.text.trim(),
      if (to.text.trim().isNotEmpty) 'to': to.text.trim(),
      if (status.isNotEmpty) 'status': status,
      if (doctorId.text.trim().isNotEmpty) 'doctor_id': doctorId.text.trim(),
      if (roomId.text.trim().isNotEmpty) 'room_id': roomId.text.trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/doctor-schedules',
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

  Future<void> createOrEdit([Map<String, dynamic>? schedule]) async {
    final result = await showApiFormDialog(
      context: context,
      builder: (_) =>
          _ScheduleDialog(session: widget.session, initial: schedule),
      submit: (values) => widget.session.requestJson(
        schedule == null ? 'POST' : 'PATCH',
        schedule == null
            ? '/api/v1/doctor-schedules'
            : '/api/v1/doctor-schedules/${schedule['id']}',
        body: schedule == null
            ? values
            : {'expected_version': schedule['version'], ...values},
      ),
    );
    if (result != null && mounted) {
      showSuccessMessage(context, 'Đã lưu lịch làm việc.');
      await load();
    }
  }

  Future<void> changeState(Map<String, dynamic> schedule, String action) async {
    String? reason;
    if (action == 'cancel') {
      reason = await showReasonDialog(
        context,
        title: 'Hủy lịch làm việc?',
        message:
            'Hệ thống sẽ từ chối nếu lịch còn appointment đang giữ chỗ. Không có thao tác hủy lịch bệnh nhân hàng loạt.',
        confirmLabel: 'Hủy lịch làm việc',
        destructive: true,
      );
      if (reason == null || !mounted) return;
    } else {
      final confirmed = await showConfirmActionDialog(
        context,
        title: action == 'activate'
            ? 'Kích hoạt lịch làm việc?'
            : 'Lưu trữ lịch làm việc?',
        message: action == 'activate'
            ? 'Các slot khám sẽ có thể xuất hiện trong tìm kiếm.'
            : 'Lịch chỉ được lưu trữ khi không còn dependency chặn.',
        confirmLabel: action == 'activate' ? 'Kích hoạt' : 'Lưu trữ',
        destructive: action == 'archive',
      );
      if (!confirmed || !mounted) return;
    }
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/doctor-schedules/${schedule['id']}/$action',
        body: {'expected_version': schedule['version'], 'reason': ?reason},
      );
      if (mounted) showSuccessMessage(context, 'Đã cập nhật lịch làm việc.');
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
    title: canManage ? 'Lịch làm việc bác sĩ' : 'Lịch của tôi',
    description:
        'Theo dõi ca làm việc theo ngày, bác sĩ và phòng; trạng thái slot được quyết định bởi backend.',
    actions: [
      if (canManage)
        FilledButton.icon(
          onPressed: mutating ? null : () => createOrEdit(),
          icon: const Icon(Icons.add),
          label: const Text('Tạo lịch làm việc'),
        ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Bộ lọc',
        child: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            SizedBox(
              width: 245,
              child: DateTimeField(controller: from, label: 'Từ thời điểm'),
            ),
            SizedBox(
              width: 245,
              child: DateTimeField(controller: to, label: 'Đến thời điểm'),
            ),
            if (canManage)
              SizedBox(
                width: 170,
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
                  onSelected: (option) =>
                      doctorId.text = option?.id.toString() ?? '',
                ),
              ),
            if (canManage)
              SizedBox(
                width: 150,
                child: EntityPicker(
                  label: 'Phòng *',
                  search: (q) => searchEntities(
                    widget.session,
                    '/api/v1/rooms',
                    q,
                    nameKey: 'name',
                    codeKey: 'code',
                    doctors: false,
                  ),
                  onSelected: (option) {
                    roomId.text = option?.id.toString() ?? '';
                  },
                ),
              ),
            SizedBox(
              width: 180,
              child: DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: status,
                decoration: const InputDecoration(labelText: 'Trạng thái'),
                items: const [
                  DropdownMenuItem(value: '', child: Text('Tất cả')),
                  DropdownMenuItem(value: 'DRAFT', child: Text('Bản nháp')),
                  DropdownMenuItem(value: 'ACTIVE', child: Text('Hoạt động')),
                  DropdownMenuItem(value: 'CANCELLED', child: Text('Đã hủy')),
                ],
                onChanged: (value) => status = value ?? '',
              ),
            ),
            FilledButton.tonalIcon(
              onPressed: loading ? null : load,
              icon: const Icon(Icons.filter_alt_outlined),
              label: const Text('Áp dụng'),
            ),
          ],
        ),
      ),
      SectionCard(
        title: 'Danh sách ca làm việc',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Bác sĩ')),
            DataColumn(label: Text('Phòng')),
            DataColumn(label: Text('Bắt đầu')),
            DataColumn(label: Text('Kết thúc')),
            DataColumn(label: Text('Slot')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('Thao tác')),
          ],
          rows: items.map(_row).toList(),
          mobileCards: items.map(_card).toList(),
          emptyTitle: 'Không có lịch làm việc',
          emptyMessage: 'Thử thay đổi khoảng ngày hoặc bộ lọc.',
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    cells: [
      DataCell(Text(valueText(item['doctor_name']))),
      DataCell(
        Text(
          '${valueText(item['room_code'])} · ${valueText(item['room_name'])}',
        ),
      ),
      DataCell(Text(formatDateTime(item['start_at']))),
      DataCell(Text(formatDateTime(item['end_at']))),
      DataCell(Text('${valueText(item['slot_duration_minutes'])} phút')),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(_actions(item)),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: Padding(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: Row(
        children: [
          const Icon(Icons.calendar_month_outlined),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  valueText(item['doctor_name']),
                  style: Theme.of(context).textTheme.titleMedium,
                ),
                Text(
                  '${formatDateTime(item['start_at'])} – ${formatDateTime(item['end_at'])}',
                ),
                Text(
                  '${valueText(item['room_code'])} · ${valueText(item['room_name'])}',
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
    tooltip: 'Thao tác lịch làm việc',
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
      PopupMenuItem(value: 'cancel', child: Text('Hủy lịch')),
      PopupMenuItem(value: 'archive', child: Text('Lưu trữ')),
    ],
  );
}

class _ScheduleDialog extends StatefulWidget {
  const _ScheduleDialog({required this.session, this.initial});
  final SessionController session;

  final Map<String, dynamic>? initial;

  @override
  State<_ScheduleDialog> createState() => _ScheduleDialogState();
}

class _ScheduleDialogState extends State<_ScheduleDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController doctorId;
  late final TextEditingController roomId;
  late final TextEditingController startAt;
  late final TextEditingController endAt;
  late final TextEditingController notes;
  late int duration;
  String initialStatus = 'DRAFT';

  @override
  void initState() {
    super.initState();
    final value = widget.initial ?? const <String, dynamic>{};
    doctorId = TextEditingController(
      text: apiString(value['doctor_profile_id']),
    );
    roomId = TextEditingController(text: apiString(value['room_id']));
    startAt = TextEditingController(text: apiString(value['start_at']));
    endAt = TextEditingController(text: apiString(value['end_at']));
    notes = TextEditingController(text: apiString(value['notes']));
    duration = apiInt(value['slot_duration_minutes'], fallback: 30);
    if (![15, 20, 30, 45, 60].contains(duration)) duration = 30;
  }

  @override
  void dispose() {
    for (final controller in [doctorId, roomId, startAt, endAt, notes]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(
      widget.initial == null ? 'Tạo lịch làm việc' : 'Chỉnh sửa lịch làm việc',
    ),
    content: SizedBox(
      width: 680,
      child: Form(
        key: formKey,
        child: SingleChildScrollView(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              const ApiFormNotice(),
              Row(
                children: [
                  Expanded(
                    child: EntityPicker(
                      label: 'Bác sĩ *',
                      initial: widget.initial == null
                          ? null
                          : EntityOption(
                              id: apiInt(widget.initial!['doctor_profile_id']),
                              label: valueText(widget.initial!['doctor_name']),
                            ),
                      enabled: widget.initial == null,
                      search: (q) => searchEntities(
                        widget.session,
                        '/api/v1/staff',
                        q,
                        nameKey: 'full_name',
                        codeKey: 'staff_code',
                        doctors: true,
                      ),
                      onSelected: (option) {
                        doctorId.text = option?.id.toString() ?? '';
                      },
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(
                    child: EntityPicker(
                      label: 'Phòng *',
                      initial: widget.initial == null
                          ? null
                          : EntityOption(
                              id: apiInt(widget.initial!['room_id']),
                              label: valueText(widget.initial!['room_name']),
                            ),
                      search: (q) => searchEntities(
                        widget.session,
                        '/api/v1/rooms',
                        q,
                        nameKey: 'name',
                        codeKey: 'code',
                        doctors: false,
                      ),
                      onSelected: (option) {
                        roomId.text = option?.id.toString() ?? '';
                      },
                    ),
                  ),
                ],
              ),
              const SizedBox(height: AppSpacing.sm),
              DateTimeField(
                controller: startAt,
                label: 'Bắt đầu *',
                errorText: apiFieldError(context, 'start_at'),
              ),
              Wrap(
                spacing: 8,
                children: [
                  for (final shift in ['Sáng', 'Chiều'])
                    TextButton(
                      onPressed: () {
                        final date = DateTime.tryParse(startAt.text);
                        final local = date == null
                            ? clinicNow().add(const Duration(days: 1))
                            : clinicNow(date);
                        setState(() {
                          startAt.text = clinicTimestamp(
                            local,
                            TimeOfDay(
                              hour: shift == 'Sáng' ? 7 : 13,
                              minute: 30,
                            ),
                          );
                          endAt.text = clinicTimestamp(
                            local,
                            TimeOfDay(
                              hour: shift == 'Sáng' ? 11 : 17,
                              minute: 30,
                            ),
                          );
                        });
                      },
                      child: Text('Ca $shift'),
                    ),
                ],
              ),
              const SizedBox(height: AppSpacing.sm),
              DateTimeField(
                controller: endAt,
                label: 'Kết thúc *',
                errorText: apiFieldError(context, 'end_at'),
              ),
              const SizedBox(height: AppSpacing.sm),
              DropdownButtonFormField<int>(
                isExpanded: true,
                initialValue: duration,
                decoration: const InputDecoration(
                  labelText: 'Thời lượng slot *',
                ),
                items: [15, 20, 30, 45, 60]
                    .map(
                      (value) => DropdownMenuItem(
                        value: value,
                        child: Text('$value phút'),
                      ),
                    )
                    .toList(),
                onChanged: (value) => duration = value ?? 30,
              ),
              if (widget.initial == null) ...[
                const SizedBox(height: AppSpacing.sm),
                DropdownButtonFormField<String>(
                  isExpanded: true,
                  initialValue: initialStatus,
                  decoration: const InputDecoration(
                    labelText: 'Trạng thái ban đầu',
                  ),
                  items: const [
                    DropdownMenuItem(value: 'DRAFT', child: Text('Bản nháp')),
                    DropdownMenuItem(
                      value: 'ACTIVE',
                      child: Text('Kích hoạt ngay'),
                    ),
                  ],
                  onChanged: (value) => initialStatus = value ?? 'DRAFT',
                ),
              ],
              const SizedBox(height: AppSpacing.sm),
              TextFormField(
                controller: notes,
                maxLines: 3,
                decoration: const InputDecoration(labelText: 'Ghi chú'),
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
                if (int.tryParse(doctorId.text) == null ||
                    int.tryParse(roomId.text) == null ||
                    DateTime.tryParse(startAt.text) == null ||
                    DateTime.tryParse(endAt.text) == null) {
                  showActionError(
                    context,
                    'Chọn bác sĩ, phòng và ngày giờ đầy đủ.',
                  );
                  return;
                }
                if (!DateTime.parse(
                  endAt.text,
                ).isAfter(DateTime.parse(startAt.text))) {
                  showActionError(
                    context,
                    'Giờ kết thúc phải sau giờ bắt đầu.',
                  );
                  return;
                }
                submitApiForm(context, {
                  if (widget.initial == null)
                    'doctor_profile_id': int.parse(doctorId.text),
                  'room_id': int.parse(roomId.text),
                  'start_at': startAt.text.trim(),
                  'end_at': endAt.text.trim(),
                  'slot_duration_minutes': duration,
                  if (widget.initial == null) 'initial_status': initialStatus,
                  'notes': notes.text.trim(),
                });
              },
        child: const Text('Lưu'),
      ),
    ],
  );
}

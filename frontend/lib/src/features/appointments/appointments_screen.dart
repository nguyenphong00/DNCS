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

class AppointmentsScreen extends StatefulWidget {
  const AppointmentsScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<AppointmentsScreen> createState() => _AppointmentsScreenState();
}

class _AppointmentsScreenState extends State<AppointmentsScreen> {
  final from = TextEditingController();
  final to = TextEditingController();
  final patientId = TextEditingController();
  final doctorId = TextEditingController();
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination(pageSize: 100);
  bool loading = true;
  bool mutating = false;
  String status = '';
  String? error;
  EntityOption? selectedPatient;

  bool get own => widget.session.accountType == 'PATIENT';
  bool can(String permission) =>
      widget.session.permissions.contains(permission);

  @override
  void initState() {
    super.initState();
    final today = clinicNow();
    from.text = clinicTimestamp(today, const TimeOfDay(hour: 0, minute: 0));
    to.text = clinicDayEnd(today);
    load();
  }

  @override
  void dispose() {
    paging.dispose();
    for (final controller in [from, to, patientId, doctorId]) {
      controller.dispose();
    }
    super.dispose();
  }

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (from.text.trim().isNotEmpty) 'from': from.text.trim(),
      if (to.text.trim().isNotEmpty) 'to': to.text.trim(),
      if (status.isNotEmpty) 'status': status,
      if (!own && patientId.text.trim().isNotEmpty)
        'patient_id': patientId.text.trim(),
      if (!own && doctorId.text.trim().isNotEmpty)
        'doctor_id': doctorId.text.trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        own ? '/api/v1/me/appointments' : '/api/v1/appointments',
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

  Future<void> openSlotSearch() async {
    final booked = await showDialog<bool>(
      context: context,
      builder: (_) => SlotSearchDialog(session: widget.session),
    );
    if (booked == true) {
      if (mounted) showSuccessMessage(context, 'Đã đặt lịch hẹn.');
      await load();
    }
  }

  Future<void> transition(Map<String, dynamic> item, String action) async {
    String reason = '';
    if ({'reject', 'cancel'}.contains(action)) {
      final value = await showReasonDialog(
        context,
        title: action == 'reject' ? 'Từ chối lịch hẹn?' : 'Hủy lịch hẹn?',
        message:
            '${valueText(item['appointment_code'])} · ${valueText(item['patient_name'])}',
        confirmLabel: action == 'reject' ? 'Từ chối' : 'Hủy lịch',
        destructive: true,
      );
      if (value == null || !mounted) return;
      reason = value;
    } else {
      final labels = {
        'approve': ('Duyệt lịch hẹn?', 'Duyệt lịch'),
        'check-in': ('Check-in bệnh nhân?', 'Check-in'),
        'no-show': ('Đánh dấu bệnh nhân vắng mặt?', 'Đánh dấu vắng'),
      };
      final label = labels[action]!;
      final confirmed = await showConfirmActionDialog(
        context,
        title: label.$1,
        message:
            '${valueText(item['appointment_code'])} · ${formatDateTime(item['start_at'])}',
        confirmLabel: label.$2,
        destructive: action == 'no-show',
      );
      if (!confirmed || !mounted) return;
    }
    setState(() {
      mutating = true;
      error = null;
    });
    try {
      await widget.session.requestJson(
        'POST',
        '/api/v1/appointments/${item['id']}/$action',
        body: {'expected_version': item['version'], 'reason': reason},
      );
      if (mounted) showSuccessMessage(context, 'Đã cập nhật lịch hẹn.');
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => mutating = false);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: own ? 'Lịch hẹn của tôi' : 'Lịch hẹn',
    description: own
        ? 'Đặt lịch, theo dõi trạng thái và xem lịch sử khám.'
        : 'Theo dõi, duyệt và tiếp nhận lịch hẹn theo phạm vi được cấp.',
    actions: [
      if (can('appointment.slot.search') &&
          (can('appointment.book.patient_own') ||
              can('appointment.book.at_counter')))
        FilledButton.icon(
          onPressed: mutating ? null : openSlotSearch,
          icon: const Icon(Icons.add),
          label: Text(own ? 'Đặt lịch khám' : 'Đặt lịch tại quầy'),
        ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Bộ lọc lịch hẹn',
        child: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            SizedBox(
              width: 220,
              child: DateTimeField(controller: from, label: 'Từ ngày giờ'),
            ),
            SizedBox(
              width: 220,
              child: DateTimeField(controller: to, label: 'Đến ngày giờ'),
            ),
            SizedBox(
              width: 190,
              child: DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: status,
                decoration: const InputDecoration(labelText: 'Trạng thái'),
                items: const [
                  DropdownMenuItem(value: '', child: Text('Tất cả')),
                  DropdownMenuItem(
                    value: 'PENDING_APPROVAL',
                    child: Text('Chờ duyệt'),
                  ),
                  DropdownMenuItem(
                    value: 'CONFIRMED',
                    child: Text('Đã xác nhận'),
                  ),
                  DropdownMenuItem(
                    value: 'CHECKED_IN',
                    child: Text('Đã check-in'),
                  ),
                  DropdownMenuItem(
                    value: 'IN_PROGRESS',
                    child: Text('Đang khám'),
                  ),
                  DropdownMenuItem(
                    value: 'COMPLETED',
                    child: Text('Hoàn thành'),
                  ),
                  DropdownMenuItem(value: 'CANCELLED', child: Text('Đã hủy')),
                  DropdownMenuItem(value: 'NO_SHOW', child: Text('Vắng mặt')),
                ],
                onChanged: (value) => status = value ?? '',
              ),
            ),
            if (!own)
              SizedBox(
                width: 150,
                child: EntityPicker(
                  label: 'Bệnh nhân',
                  search: (q) => searchEntities(
                    widget.session,
                    '/api/v1/patients',
                    q,
                    nameKey: 'full_name',
                    codeKey: 'patient_code',
                    doctors: false,
                  ),
                  onSelected: (option) {
                    patientId.text = option?.id.toString() ?? '';
                  },
                ),
              ),
            if (!own && can('staff.view.all'))
              SizedBox(
                width: 150,
                child: EntityPicker(
                  label: 'Bác sĩ',
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
            FilledButton.tonalIcon(
              onPressed: loading ? null : load,
              icon: const Icon(Icons.filter_alt_outlined),
              label: const Text('Áp dụng'),
            ),
          ],
        ),
      ),
      SectionCard(
        title: own ? 'Lịch của tôi' : 'Danh sách lịch hẹn',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: [
            const DataColumn(label: Text('Mã lịch')),
            if (!own) const DataColumn(label: Text('Bệnh nhân')),
            const DataColumn(label: Text('Bác sĩ')),
            const DataColumn(label: Text('Thời gian')),
            const DataColumn(label: Text('Trạng thái')),
            const DataColumn(label: Text('Thao tác')),
          ],
          rows: items.map(_row).toList(),
          mobileCards: items.map(_card).toList(),
          emptyTitle: 'Chưa có lịch hẹn',
          emptyMessage: own
              ? 'Chọn “Đặt lịch khám” để tìm slot phù hợp.'
              : 'Không có lịch hẹn phù hợp với bộ lọc.',
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    onSelectChanged: (_) => _showDetail(item),
    cells: [
      DataCell(Text(valueText(item['appointment_code']))),
      if (!own) DataCell(Text(valueText(item['patient_name']))),
      DataCell(
        Text(
          '${valueText(item['doctor_name'])}\n${valueText(item['specialty_name'])}',
        ),
      ),
      DataCell(Text(formatDateTime(item['start_at']))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(_actions(item)),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: InkWell(
      borderRadius: BorderRadius.circular(AppRadius.card),
      onTap: () => _showDetail(item),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: Row(
          children: [
            const Icon(Icons.event_note_outlined),
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    valueText(item['appointment_code']),
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  if (!own) Text(valueText(item['patient_name'])),
                  Text(
                    '${valueText(item['doctor_name'])} · ${formatDateTime(item['start_at'])}',
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
    ),
  );

  Widget _actions(Map<String, dynamic> item) {
    final actions = <Widget>[
      if (item['status'] == 'CONFIRMED' && can('appointment.check_in'))
        FilledButton.tonal(
          onPressed: mutating ? null : () => transition(item, 'check-in'),
          child: const Text('Check-in'),
        ),
      if (item['status'] == 'PENDING_APPROVAL' &&
          (can('appointment.approve.all') ||
              can('appointment.approve.doctor_own')))
        FilledButton.tonal(
          onPressed: mutating ? null : () => transition(item, 'approve'),
          child: const Text('Duyệt'),
        ),
      PopupMenuButton<String>(
        tooltip: 'Thao tác lịch hẹn',
        enabled: !mutating,
        onSelected: (value) {
          if (value == 'view') {
            _showDetail(item);
          } else {
            transition(item, value);
          }
        },
        itemBuilder: (_) => [
          const PopupMenuItem(value: 'view', child: Text('Xem chi tiết')),
          if (can('appointment.approve.all') ||
              can('appointment.approve.doctor_own')) ...[
            const PopupMenuItem(value: 'approve', child: Text('Duyệt lịch')),
            const PopupMenuItem(value: 'reject', child: Text('Từ chối')),
          ],
          if (can('appointment.check_in'))
            const PopupMenuItem(value: 'check-in', child: Text('Check-in')),
          if (can('appointment.no_show.mark'))
            const PopupMenuItem(value: 'no-show', child: Text('Đánh dấu vắng')),
          if (can('appointment.cancel.all') ||
              can('appointment.cancel.patient_own'))
            const PopupMenuItem(value: 'cancel', child: Text('Hủy lịch')),
        ],
      ),
    ];
    final compact =
        MediaQuery.sizeOf(context).width < 1366 ||
        MediaQuery.textScalerOf(context).scale(14) > 21;
    if (compact) {
      return Wrap(
        spacing: AppSpacing.xs,
        runSpacing: AppSpacing.xs,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: actions,
      );
    }
    // A desktop table needs the full intrinsic width for its fixed-height row.
    return Row(mainAxisSize: MainAxisSize.min, children: actions);
  }

  Future<void> _showDetail(Map<String, dynamic> item) => showDialog<void>(
    context: context,
    builder: (context) => AlertDialog(
      title: Text(valueText(item['appointment_code'])),
      content: SizedBox(
        width: 720,
        child: SingleChildScrollView(
          child: Wrap(
            spacing: AppSpacing.xl,
            children: [
              DetailField(
                label: 'Bệnh nhân',
                value: apiString(item['patient_name']),
              ),
              DetailField(
                label: 'Bác sĩ',
                value: apiString(item['doctor_name']),
              ),
              DetailField(
                label: 'Chuyên khoa',
                value: apiString(item['specialty_name']),
              ),
              DetailField(label: 'Phòng', value: apiString(item['room_name'])),
              DetailField(
                label: 'Bắt đầu',
                value: formatDateTime(item['start_at']),
              ),
              DetailField(
                label: 'Kết thúc',
                value: formatDateTime(item['end_at']),
              ),
              DetailField(
                label: 'Trạng thái',
                value: apiString(item['status']),
                status: true,
              ),
              DetailField(label: 'Nguồn đặt', value: apiString(item['source'])),
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Đóng'),
        ),
      ],
    ),
  );
}

class SlotSearchDialog extends StatefulWidget {
  const SlotSearchDialog({
    super.key,
    required this.session,
    this.followUp = false,
    this.initialPatient,
    this.initialSpecialtyId,
    this.initialDoctorId,
  });
  final bool followUp;
  final EntityOption? initialPatient;
  final int? initialSpecialtyId;
  final int? initialDoctorId;

  final SessionController session;

  @override
  State<SlotSearchDialog> createState() => SlotSearchDialogState();
}

class SlotSearchDialogState extends State<SlotSearchDialog> {
  final specialtyId = TextEditingController();
  final doctorId = TextEditingController();
  final patientId = TextEditingController();
  final date = TextEditingController();
  List<Map<String, dynamic>> slots = const [];
  bool loading = false;
  String? error;
  EntityOption? selectedPatient;
  int searchGeneration = 0;

  bool get own => widget.session.accountType == 'PATIENT';

  @override
  void initState() {
    super.initState();
    if (widget.initialPatient != null) {
      patientId.text = '${widget.initialPatient!.id}';
    }
    if (widget.initialSpecialtyId != null) {
      specialtyId.text = '${widget.initialSpecialtyId}';
    }
    if (widget.initialDoctorId != null) {
      doctorId.text = '${widget.initialDoctorId}';
    }
    final now = clinicNow().add(const Duration(days: 1));
    date.text =
        '${now.year}-${now.month.toString().padLeft(2, '0')}-${now.day.toString().padLeft(2, '0')}';
    date.addListener(invalidateSlots);
  }

  void invalidateSlots() {
    searchGeneration++;
    if (!mounted) return;
    setState(() {
      slots = [];
      loading = false;
      error = null;
    });
  }

  @override
  void dispose() {
    searchGeneration++;
    date.removeListener(invalidateSlots);
    for (final controller in [specialtyId, doctorId, patientId, date]) {
      controller.dispose();
    }
    super.dispose();
  }

  Future<void> search() async {
    final specialty = int.tryParse(specialtyId.text);
    if (specialty == null || date.text.trim().isEmpty) {
      setState(() => error = 'Chọn chuyên khoa và ngày khám.');
      return;
    }
    final generation = ++searchGeneration;
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final result = apiItems(
        await widget.session.requestJson(
          'GET',
          '/api/v1/appointment-slots',
          query: {
            'specialty_id': '$specialty',
            'date': date.text.trim(),
            if (doctorId.text.trim().isNotEmpty)
              'doctor_id': doctorId.text.trim(),
            if (!own && patientId.text.trim().isNotEmpty)
              'patient_id': patientId.text.trim(),
          },
        ),
      );
      if (!mounted || generation != searchGeneration) return;
      setState(() => slots = result);
    } catch (cause) {
      if (mounted && generation == searchGeneration) {
        setState(() => error = friendlyError(cause));
      }
    } finally {
      if (mounted && generation == searchGeneration) {
        setState(() => loading = false);
      }
    }
  }

  Future<void> book(Map<String, dynamic> slot) async {
    if (!own && int.tryParse(patientId.text) == null) {
      setState(() => error = 'Chọn bệnh nhân để đặt lịch tại quầy.');
      return;
    }
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Xác nhận đặt lịch?',
      message:
          '${(selectedPatient ?? widget.initialPatient)?.label ?? ''} ${(selectedPatient ?? widget.initialPatient)?.subtitle ?? ''}\n${valueText(slot['doctor_name'])} · ${formatDateTime(slot['start_at'])}',
      confirmLabel: 'Đặt lịch',
    );
    if (!confirmed || !mounted) return;
    if (widget.followUp) {
      Navigator.pop(context, {
        'doctor_schedule_id': slot['doctor_schedule_id'],
        'start_at': slot['start_at'],
      });
      return;
    }
    setState(() {
      loading = true;
      error = null;
    });
    try {
      await widget.session.requestJson(
        'POST',
        '/api/v1/appointments',
        headers: {'Idempotency-Key': idempotencyKey('booking')},
        body: {
          'doctor_schedule_id': slot['doctor_schedule_id'],
          if (!own) 'patient_id': int.parse(patientId.text),
          'specialty_id': slot['specialty_id'],
          'start_at': slot['start_at'],
        },
      );
      if (mounted) Navigator.pop(context, true);
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  @override
  Widget build(BuildContext context) => Dialog(
    child: ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 900, maxHeight: 760),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.xl),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    widget.followUp
                        ? 'Chọn lịch tái khám'
                        : own
                        ? 'Đặt lịch khám'
                        : 'Đặt lịch tại quầy',
                    style: Theme.of(context).textTheme.headlineSmall,
                  ),
                ),
                IconButton(
                  tooltip: 'Đóng',
                  onPressed: loading ? null : () => Navigator.pop(context),
                  icon: const Icon(Icons.close),
                ),
              ],
            ),
            const SizedBox(height: AppSpacing.md),
            Wrap(
              spacing: AppSpacing.sm,
              runSpacing: AppSpacing.sm,
              children: [
                SizedBox(
                  width: 180,
                  child: EntityPicker(
                    label: 'Chuyên khoa *',
                    search: (q) => searchEntities(
                      widget.session,
                      '/api/v1/specialties',
                      q,
                    ),
                    onSelected: (option) {
                      setState(() {
                        specialtyId.text = option?.id.toString() ?? '';
                        doctorId.clear();
                      });
                      invalidateSlots();
                    },
                  ),
                ),
                SizedBox(
                  width: 180,
                  child: EntityPicker(
                    key: ValueKey(specialtyId.text),
                    label: 'Bác sĩ',
                    search: (q) async {
                      if (specialtyId.text.isEmpty) return [];
                      return searchEntities(
                        widget.session,
                        '/api/v1/specialties/${specialtyId.text}/doctors',
                        q,
                        nameKey: 'full_name',
                        codeKey: 'doctor_code',
                      );
                    },
                    onSelected: (option) {
                      doctorId.text = option?.id.toString() ?? '';
                      invalidateSlots();
                    },
                  ),
                ),
                if (!own)
                  SizedBox(
                    width: 180,
                    child: EntityPicker(
                      label: 'Bệnh nhân *',
                      initial: widget.initialPatient,
                      enabled: !widget.followUp,
                      search: (q) => searchEntities(
                        widget.session,
                        '/api/v1/patients',
                        q,
                        nameKey: 'full_name',
                        codeKey: 'patient_code',
                      ),
                      onSelected: (option) {
                        selectedPatient = option;
                        patientId.text = option?.id.toString() ?? '';
                        invalidateSlots();
                      },
                    ),
                  ),
                SizedBox(
                  width: 180,
                  child: DateField(controller: date, label: 'Ngày khám *'),
                ),
                FilledButton.tonalIcon(
                  onPressed: loading ? null : search,
                  icon: const Icon(Icons.search),
                  label: const Text('Tìm slot'),
                ),
              ],
            ),
            if (error != null) ...[
              const SizedBox(height: AppSpacing.md),
              InlineMessage(message: error!, tone: AppStatusTone.danger),
            ],
            const SizedBox(height: AppSpacing.md),
            Expanded(
              child: AsyncContent(
                loading: loading,
                error: null,
                empty: slots.isEmpty,
                emptyTitle: 'Chưa có slot',
                emptyMessage: 'Nhập tiêu chí và chọn “Tìm slot”.',
                child: ListView.separated(
                  itemCount: slots.length,
                  separatorBuilder: (_, _) =>
                      const SizedBox(height: AppSpacing.sm),
                  itemBuilder: (context, index) {
                    final slot = slots[index];
                    return Card(
                      child: ListTile(
                        leading: const Icon(Icons.schedule_outlined),
                        title: Text(valueText(slot['doctor_name'])),
                        subtitle: Text(
                          '${formatDateTime(slot['start_at'])} · ${valueText(slot['room_name'])}',
                        ),
                        trailing: FilledButton(
                          onPressed: loading ? null : () => book(slot),
                          child: const Text('Chọn'),
                        ),
                      ),
                    );
                  },
                ),
              ),
            ),
          ],
        ),
      ),
    ),
  );
}

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
import '../shared/list_pagination.dart';
import '../appointments/appointments_screen.dart';
import '../prescriptions/prescriptions_screen.dart';

class EncountersScreen extends StatefulWidget {
  const EncountersScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<EncountersScreen> createState() => _EncountersScreenState();
}

class _EncountersScreenState extends State<EncountersScreen> {
  final patientId = TextEditingController();
  final doctorId = TextEditingController();
  final code = TextEditingController();
  final from = TextEditingController(
    text: clinicTimestamp(clinicNow(), const TimeOfDay(hour: 0, minute: 0)),
  );
  final to = TextEditingController(text: clinicDayEnd(clinicNow()));
  List<Map<String, dynamic>> encounters = const [];
  List<Map<String, dynamic>> queue = const [];
  final paging = ListPagination(cursor: true);
  bool loading = true;
  bool mutating = false;
  String status = '';
  String? error;

  bool get own => widget.session.accountType == 'PATIENT';
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
    patientId.dispose();
    doctorId.dispose();
    code.dispose();
    from.dispose();
    to.dispose();
    super.dispose();
  }

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (from.text.isNotEmpty) 'from': from.text,
      if (to.text.isNotEmpty) 'to': to.text,
      if (status.isNotEmpty) 'status': status,
      if (!own && patientId.text.trim().isNotEmpty)
        'patient_id': patientId.text.trim(),
      if (!own && doctorId.text.trim().isNotEmpty)
        'doctor_profile_id': doctorId.text.trim(),
      if (code.text.trim().isNotEmpty) 'encounter_code': code.text.trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final encounterFuture = widget.session.requestJson(
        'GET',
        own ? '/api/v1/me/encounters' : '/api/v1/encounters',
        query: request.query,
      );
      final queueFuture = can('encounter.create.assigned')
          ? widget.session.requestJson(
              'GET',
              '/api/v1/appointments',
              query: {
                'from': request.filters['from'] ?? '',
                'to': request.filters['to'] ?? '',
                'page': '1',
                'page_size': '50',
                'status': 'CHECKED_IN',
              },
            )
          : Future.value(<String, dynamic>{'items': <dynamic>[]});
      final result = await Future.wait([encounterFuture, queueFuture]);
      if (!mounted || !paging.accept(request, result[0])) return;
      encounters = apiItems(result[0]);
      queue = apiItems(result[1]);
    } catch (cause) {
      if (mounted && paging.isCurrent(request)) error = friendlyError(cause);
    } finally {
      if (mounted && paging.isCurrent(request)) setState(() => loading = false);
    }
  }

  Future<void> startEncounter(Map<String, dynamic> appointment) async {
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Bắt đầu khám?',
      message:
          '${valueText(appointment['patient_name'])} · ${valueText(appointment['appointment_code'])}',
      confirmLabel: 'Bắt đầu khám',
    );
    if (!confirmed || !mounted) return;
    await _mutate(() async {
      final started = await widget.session.requestJson(
        'POST',
        '/api/v1/appointments/${appointment['id']}/start',
        body: {'expected_version': appointment['version']},
      );
      if (mounted) {
        showSuccessMessage(context, 'Đã bắt đầu lần khám.');
        await openWorkspace(
          apiMap(started['encounter']).isNotEmpty
              ? apiMap(started['encounter'])
              : started,
        );
      }
    });
  }

  Future<void> editEncounter(Map<String, dynamic> summary) async {
    final detail = await _getDetail(summary);
    if (detail == null || !mounted) return;
    final values = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => _EncounterFormDialog(initial: detail),
    );
    if (values == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'PATCH',
        '/api/v1/encounters/${detail['id']}',
        body: {'expected_version': detail['version'], ...values},
      );
      if (mounted) showSuccessMessage(context, 'Đã lưu bệnh án.');
    });
  }

  Future<void> addVitalSigns(Map<String, dynamic> encounter) async {
    final values = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => const _VitalSignsDialog(),
    );
    if (values == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${encounter['id']}/vital-signs',
        body: values,
      );
      if (mounted) showSuccessMessage(context, 'Đã ghi chỉ số sinh tồn.');
    });
  }

  Future<void> addService(Map<String, dynamic> encounter) async {
    final values = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => _ServiceOrderDialog(session: widget.session),
    );
    if (values == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${encounter['id']}/services',
        body: values,
      );
      if (mounted) showSuccessMessage(context, 'Đã chỉ định dịch vụ.');
    });
  }

  Future<void> addAmendment(Map<String, dynamic> encounter) async {
    final values = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => const _AmendmentDialog(),
    );
    if (values == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${encounter['id']}/amendments',
        body: values,
      );
      if (mounted) showSuccessMessage(context, 'Đã thêm nội dung bổ sung.');
    });
  }

  Future<void> createFollowUp(Map<String, dynamic> encounter) async {
    final values = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => SlotSearchDialog(
        session: widget.session,
        followUp: true,
        initialPatient: EntityOption(
          id: apiInt(encounter['patient_id']),
          label: valueText(encounter['patient_name_at_start']),
        ),
        initialSpecialtyId: apiInt(encounter['specialty_id']),
        initialDoctorId: apiInt(encounter['doctor_profile_id']),
      ),
    );
    if (values == null || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${encounter['id']}/follow-up-appointments',
        headers: {'Idempotency-Key': idempotencyKey('follow-up')},
        body: values,
      );
      if (mounted) showSuccessMessage(context, 'Đã tạo lịch tái khám.');
    });
  }

  Future<void> completeEncounter(Map<String, dynamic> encounter) async {
    final appointment = await _getAppointment(encounter['appointment_id']);
    if (appointment == null || !mounted) return;
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Hoàn tất lần khám?',
      message:
          'Bệnh án gốc sẽ chuyển sang chỉ đọc. Hãy kiểm tra chẩn đoán, kết luận, dịch vụ và đơn thuốc trước khi tiếp tục.',
      confirmLabel: 'Hoàn tất khám',
    );
    if (!confirmed || !mounted) return;
    await _mutate(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/appointments/${encounter['appointment_id']}/complete',
        body: {
          'appointment_expected_version': appointment['version'],
          'encounter_expected_version': encounter['version'],
        },
      );
      if (mounted) showSuccessMessage(context, 'Đã hoàn tất lần khám.');
    });
  }

  Future<Map<String, dynamic>?> _getDetail(Map<String, dynamic> summary) async {
    setState(() => mutating = true);
    try {
      return await widget.session.requestJson(
        'GET',
        '/api/v1/encounters/${summary['id']}',
      );
    } catch (cause) {
      if (mounted) showActionError(context, cause);
      return null;
    } finally {
      if (mounted) setState(() => mutating = false);
    }
  }

  Future<Map<String, dynamic>?> _getAppointment(Object? id) async {
    try {
      return await widget.session.requestJson(
        'GET',
        '/api/v1/appointments/$id',
      );
    } catch (cause) {
      if (mounted) showActionError(context, cause);
      return null;
    }
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
    title: own ? 'Lịch sử khám' : 'Không gian khám bệnh',
    description: own
        ? 'Các lần khám đã được hoàn tất và công bố cho bệnh nhân.'
        : 'Bắt đầu khám, cập nhật bệnh án và hoàn tất quy trình chuyên môn.',
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      if (can('encounter.create.assigned'))
        SectionCard(
          title: 'Hàng chờ khám',
          description:
              'Chỉ lịch được phân công cho bác sĩ hiện tại mới có thể bắt đầu.',
          child: AsyncContent(
            loading: loading,
            error: error,
            onRetry: load,
            empty: queue.isEmpty,
            emptyTitle: 'Không có bệnh nhân đang chờ',
            emptyMessage: 'Các lịch CHECKED_IN sẽ xuất hiện tại đây.',
            child: Wrap(
              spacing: AppSpacing.sm,
              runSpacing: AppSpacing.sm,
              children: queue
                  .map(
                    (appointment) => Card(
                      child: Padding(
                        padding: const EdgeInsets.all(AppSpacing.md),
                        child: SizedBox(
                          width: 280,
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                valueText(appointment['patient_name']),
                                style: Theme.of(context).textTheme.titleMedium,
                              ),
                              Text(valueText(appointment['appointment_code'])),
                              Text(formatDateTime(appointment['start_at'])),
                              const SizedBox(height: AppSpacing.sm),
                              FilledButton.icon(
                                onPressed: mutating
                                    ? null
                                    : () => startEncounter(appointment),
                                icon: const Icon(Icons.play_arrow),
                                label: const Text('Bắt đầu khám'),
                              ),
                            ],
                          ),
                        ),
                      ),
                    ),
                  )
                  .toList(),
            ),
          ),
        ),
      if (!own)
        SectionCard(
          title: 'Bộ lọc bệnh án',
          child: Wrap(
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            children: [
              SizedBox(
                width: 200,
                child: DateTimeField(controller: from, label: 'Từ ngày giờ'),
              ),
              SizedBox(
                width: 200,
                child: DateTimeField(controller: to, label: 'Đến ngày giờ'),
              ),
              SizedBox(
                width: 190,
                child: TextField(
                  controller: code,
                  decoration: const InputDecoration(labelText: 'Mã lần khám'),
                ),
              ),
              SizedBox(
                width: 160,
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
              if (can('staff.view.all'))
                SizedBox(
                  width: 160,
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
              SizedBox(
                width: 170,
                child: DropdownButtonFormField<String>(
                  isExpanded: true,
                  initialValue: status,
                  decoration: const InputDecoration(labelText: 'Trạng thái'),
                  items: const [
                    DropdownMenuItem(value: '', child: Text('Tất cả')),
                    DropdownMenuItem(value: 'DRAFT', child: Text('Đang khám')),
                    DropdownMenuItem(
                      value: 'FINALIZED',
                      child: Text('Đã hoàn tất'),
                    ),
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
        title: own ? 'Các lần khám đã hoàn tất' : 'Danh sách bệnh án',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Mã lần khám')),
            DataColumn(label: Text('Bệnh nhân')),
            DataColumn(label: Text('Bắt đầu')),
            DataColumn(label: Text('Chẩn đoán')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('Thao tác')),
          ],
          rows: encounters.map(_row).toList(),
          mobileCards: encounters.map(_card).toList(),
          emptyTitle: 'Chưa có lần khám',
          emptyMessage: 'Không có bệnh án phù hợp với phạm vi và bộ lọc.',
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    onSelectChanged: (_) => showDetail(item),
    cells: [
      DataCell(Text(valueText(item['encounter_code']))),
      DataCell(
        Text(
          '${valueText(item['patient_name_at_start'])}\n${valueText(item['patient_code_at_start'])}',
        ),
      ),
      DataCell(Text(formatDateTime(item['started_at']))),
      DataCell(
        ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 260),
          child: Text(
            valueText(item['diagnosis']),
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(_actions(item)),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: InkWell(
      borderRadius: BorderRadius.circular(AppRadius.card),
      onTap: () => showDetail(item),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: Row(
          children: [
            const Icon(Icons.medical_information_outlined),
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    valueText(item['encounter_code']),
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  Text(formatDateTime(item['started_at'])),
                  Text(
                    valueText(item['diagnosis']),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
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

  Widget _actions(Map<String, dynamic> item) => PopupMenuButton<String>(
    tooltip: 'Thao tác bệnh án',
    enabled: !mutating,
    onSelected: (value) {
      switch (value) {
        case 'view':
          showDetail(item);
        case 'edit':
          editEncounter(item);
        case 'vitals':
          addVitalSigns(item);
        case 'service':
          addService(item);
        case 'complete':
          completeEncounter(item);
        case 'amend':
          addAmendment(item);
        case 'follow-up':
          createFollowUp(item);
      }
    },
    itemBuilder: (_) => [
      const PopupMenuItem(value: 'view', child: Text('Xem chi tiết')),
      if (item['status'] == 'DRAFT' && can('encounter.update.assigned')) ...[
        const PopupMenuItem(value: 'edit', child: Text('Cập nhật bệnh án')),
        const PopupMenuItem(value: 'vitals', child: Text('Ghi sinh hiệu')),
      ],
      if (item['status'] == 'DRAFT' && can('encounter.service.assign'))
        const PopupMenuItem(value: 'service', child: Text('Chỉ định dịch vụ')),
      if (item['status'] == 'DRAFT' && can('encounter.finalize.assigned'))
        const PopupMenuItem(value: 'complete', child: Text('Hoàn tất khám')),
      if (item['status'] == 'FINALIZED' &&
          (can('encounter.amend.assigned') || can('encounter.amend.all')))
        const PopupMenuItem(value: 'amend', child: Text('Thêm amendment')),
      if (item['status'] == 'FINALIZED' && can('appointment.follow_up.create'))
        const PopupMenuItem(
          value: 'follow-up',
          child: Text('Tạo lịch tái khám'),
        ),
    ],
  );

  Future<void> openWorkspace(Map<String, dynamic> summary) async {
    if (apiInt(summary['id']) == 0) return;
    await showDialog<void>(
      context: context,
      builder: (_) => EncounterWorkspace(
        session: widget.session,
        encounterId: apiInt(summary['id']),
      ),
    );
  }

  Future<void> showDetail(Map<String, dynamic> summary) async {
    if (can('encounter.update.assigned') || can('encounter.create.assigned')) {
      await openWorkspace(summary);
      await load();
      return;
    }
    final detail = await _getDetail(summary);
    if (detail == null || !mounted) return;
    final services = apiItems(
      await widget.session.requestJson(
        'GET',
        '/api/v1/encounters/${detail['id']}/services',
      ),
    );
    final amendments = apiItems(
      await widget.session.requestJson(
        'GET',
        '/api/v1/encounters/${detail['id']}/amendments',
      ),
    );
    if (!mounted) return;
    await showDialog<void>(
      context: context,
      builder: (context) => Dialog(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 960, maxHeight: 780),
          child: Padding(
            padding: const EdgeInsets.all(AppSpacing.xl),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        valueText(detail['encounter_code']),
                        style: Theme.of(context).textTheme.headlineSmall,
                      ),
                    ),
                    StatusBadge(status: apiString(detail['status'])),
                    IconButton(
                      tooltip: 'Đóng',
                      onPressed: () => Navigator.pop(context),
                      icon: const Icon(Icons.close),
                    ),
                  ],
                ),
                const SizedBox(height: AppSpacing.md),
                Expanded(
                  child: SingleChildScrollView(
                    child: Column(
                      children: [
                        SectionCard(
                          title: 'Nội dung bệnh án',
                          child: Wrap(
                            spacing: AppSpacing.xl,
                            children: [
                              DetailField(
                                label: 'Lý do khám',
                                value: apiString(detail['chief_complaint']),
                              ),
                              DetailField(
                                label: 'Triệu chứng',
                                value: apiString(detail['symptoms']),
                              ),
                              DetailField(
                                label: 'Chẩn đoán',
                                value: apiString(detail['diagnosis']),
                              ),
                              DetailField(
                                label: 'Kết luận',
                                value: apiString(detail['conclusion']),
                              ),
                              DetailField(
                                label: 'Ghi chú lâm sàng',
                                value: apiString(detail['clinical_notes']),
                              ),
                              DetailField(
                                label: 'Khuyến nghị tái khám',
                                value: apiString(
                                  detail['follow_up_recommendation'],
                                ),
                              ),
                            ],
                          ),
                        ),
                        const SizedBox(height: AppSpacing.md),
                        SectionCard(
                          title: 'Chỉ số sinh tồn',
                          child: _VitalTimeline(
                            items: apiItems({'items': detail['vital_signs']}),
                          ),
                        ),
                        const SizedBox(height: AppSpacing.md),
                        SectionCard(
                          title: 'Dịch vụ đã chỉ định',
                          child: services.isEmpty
                              ? const Text('Chưa có dịch vụ.')
                              : Column(
                                  children: services
                                      .map(
                                        (service) => ListTile(
                                          contentPadding: EdgeInsets.zero,
                                          title: Text(
                                            valueText(
                                              service['service_name_at_order'],
                                            ),
                                          ),
                                          subtitle: Text(
                                            'Số lượng ${valueText(service['quantity'])} · ${formatVnd(service['unit_price_vnd'])}',
                                          ),
                                          trailing: StatusBadge(
                                            status: apiString(
                                              service['status'],
                                            ),
                                          ),
                                        ),
                                      )
                                      .toList(),
                                ),
                        ),
                        if (amendments.isNotEmpty) ...[
                          const SizedBox(height: AppSpacing.md),
                          SectionCard(
                            title: 'Nội dung bổ sung',
                            child: Column(
                              children: amendments
                                  .map(
                                    (item) => ListTile(
                                      contentPadding: EdgeInsets.zero,
                                      leading: const Icon(
                                        Icons.history_edu_outlined,
                                      ),
                                      title: Text(
                                        '${valueText(item['amendment_type'])} · ${formatDateTime(item['created_at'])}',
                                      ),
                                      subtitle: Text(
                                        '${valueText(item['reason'])}\n${valueText(item['content'])}',
                                      ),
                                    ),
                                  )
                                  .toList(),
                            ),
                          ),
                        ],
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _EncounterFormDialog extends StatefulWidget {
  const _EncounterFormDialog({required this.initial});

  final Map<String, dynamic> initial;

  @override
  State<_EncounterFormDialog> createState() => _EncounterFormDialogState();
}

class _EncounterFormDialogState extends State<_EncounterFormDialog> {
  late final List<TextEditingController> controllers;

  @override
  void initState() {
    super.initState();
    controllers = [
      for (final key in [
        'chief_complaint',
        'symptoms',
        'diagnosis',
        'conclusion',
        'clinical_notes',
        'follow_up_recommendation',
      ])
        TextEditingController(text: apiString(widget.initial[key])),
    ];
  }

  @override
  void dispose() {
    for (final controller in controllers) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    const labels = [
      'Lý do khám',
      'Triệu chứng',
      'Chẩn đoán',
      'Kết luận',
      'Ghi chú lâm sàng',
      'Khuyến nghị tái khám',
    ];
    const keys = [
      'chief_complaint',
      'symptoms',
      'diagnosis',
      'conclusion',
      'clinical_notes',
      'follow_up_recommendation',
    ];
    return AlertDialog(
      title: const Text('Cập nhật bệnh án'),
      content: SizedBox(
        width: 760,
        child: SingleChildScrollView(
          padding: const EdgeInsets.only(top: AppSpacing.md),
          child: Column(
            children: [
              for (var index = 0; index < controllers.length; index++) ...[
                TextField(
                  controller: controllers[index],
                  maxLines: index == 4 ? 5 : 3,
                  decoration: InputDecoration(labelText: labels[index]),
                ),
                if (index < controllers.length - 1)
                  const SizedBox(height: AppSpacing.md),
              ],
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
          onPressed: () => Navigator.pop(context, {
            for (var index = 0; index < keys.length; index++)
              keys[index]: controllers[index].text.trim(),
          }),
          child: const Text('Lưu bệnh án'),
        ),
      ],
    );
  }
}

class _VitalSignsDialog extends StatefulWidget {
  const _VitalSignsDialog();

  @override
  State<_VitalSignsDialog> createState() => _VitalSignsDialogState();
}

class _VitalSignsDialogState extends State<_VitalSignsDialog> {
  final temperature = TextEditingController();
  final systolic = TextEditingController();
  final diastolic = TextEditingController();
  final heartRate = TextEditingController();
  final respiratory = TextEditingController();
  final spo2 = TextEditingController();
  final weight = TextEditingController();
  final height = TextEditingController();
  final pain = TextEditingController();
  final notes = TextEditingController();

  @override
  void dispose() {
    for (final controller in [
      temperature,
      systolic,
      diastolic,
      heartRate,
      respiratory,
      spo2,
      weight,
      height,
      pain,
      notes,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Ghi chỉ số sinh tồn'),
    content: SizedBox(
      width: 680,
      child: SingleChildScrollView(
        child: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: [
            _numberField(temperature, 'Nhiệt độ °C'),
            _numberField(systolic, 'Huyết áp tâm thu'),
            _numberField(diastolic, 'Huyết áp tâm trương'),
            _numberField(heartRate, 'Nhịp tim'),
            _numberField(respiratory, 'Nhịp thở'),
            _numberField(spo2, 'SpO₂ %'),
            _numberField(weight, 'Cân nặng kg'),
            _numberField(height, 'Chiều cao cm'),
            _numberField(pain, 'Mức đau 0–10'),
            SizedBox(
              width: 410,
              child: TextField(
                controller: notes,
                decoration: const InputDecoration(labelText: 'Ghi chú'),
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
        onPressed: () => Navigator.pop(context, {
          'temperature_c': double.tryParse(temperature.text),
          'systolic_bp': int.tryParse(systolic.text),
          'diastolic_bp': int.tryParse(diastolic.text),
          'heart_rate_bpm': int.tryParse(heartRate.text),
          'respiratory_rate_bpm': int.tryParse(respiratory.text),
          'spo2_percent': double.tryParse(spo2.text),
          'weight_kg': double.tryParse(weight.text),
          'height_cm': double.tryParse(height.text),
          'pain_score': int.tryParse(pain.text),
          'notes': notes.text.trim(),
          'measured_at': DateTime.now().toUtc().toIso8601String(),
        }),
        child: const Text('Lưu chỉ số'),
      ),
    ],
  );

  Widget _numberField(TextEditingController controller, String label) =>
      SizedBox(
        width: 195,
        child: TextField(
          controller: controller,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
          decoration: InputDecoration(labelText: label),
        ),
      );
}

class _ServiceOrderDialog extends StatefulWidget {
  const _ServiceOrderDialog({required this.session});
  final SessionController session;

  @override
  State<_ServiceOrderDialog> createState() => _ServiceOrderDialogState();
}

class _ServiceOrderDialogState extends State<_ServiceOrderDialog> {
  final serviceId = TextEditingController();
  final quantity = TextEditingController(text: '1');

  @override
  void dispose() {
    serviceId.dispose();
    quantity.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Chỉ định dịch vụ'),
    content: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        SizedBox(
          width: 200,
          child: EntityPicker(
            label: 'Dịch vụ *',
            search: (q) => searchEntities(
              widget.session,
              '/api/v1/services',
              q,
              nameKey: 'name',
              codeKey: 'code',
              doctors: false,
            ),
            onSelected: (option) {
              serviceId.text = option?.id.toString() ?? '';
            },
          ),
        ),
        const SizedBox(width: AppSpacing.sm),
        SizedBox(
          width: 150,
          child: TextField(
            controller: quantity,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(labelText: 'Số lượng *'),
          ),
        ),
      ],
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        onPressed: () {
          final service = int.tryParse(serviceId.text);
          final count = int.tryParse(quantity.text);
          if (service == null || count == null || count < 1) return;
          Navigator.pop(context, {'service_id': service, 'quantity': count});
        },
        child: const Text('Thêm dịch vụ'),
      ),
    ],
  );
}

class _AmendmentDialog extends StatefulWidget {
  const _AmendmentDialog();

  @override
  State<_AmendmentDialog> createState() => _AmendmentDialogState();
}

class _AmendmentDialogState extends State<_AmendmentDialog> {
  final reason = TextEditingController();
  final content = TextEditingController();
  String type = 'CLINICAL_ADDENDUM';

  @override
  void dispose() {
    reason.dispose();
    content.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Thêm nội dung bổ sung'),
    content: SizedBox(
      width: 620,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          DropdownButtonFormField<String>(
            isExpanded: true,
            initialValue: type,
            decoration: const InputDecoration(labelText: 'Loại amendment'),
            items: const [
              DropdownMenuItem(
                value: 'CLINICAL_ADDENDUM',
                child: Text('Bổ sung lâm sàng'),
              ),
              DropdownMenuItem(
                value: 'CLINICAL_CORRECTION',
                child: Text('Đính chính lâm sàng'),
              ),
              DropdownMenuItem(
                value: 'ADMINISTRATIVE_NOTE',
                child: Text('Ghi chú hành chính'),
              ),
            ],
            onChanged: (value) => type = value ?? type,
          ),
          const SizedBox(height: AppSpacing.sm),
          TextField(
            controller: reason,
            decoration: const InputDecoration(labelText: 'Lý do *'),
          ),
          const SizedBox(height: AppSpacing.sm),
          TextField(
            controller: content,
            maxLines: 5,
            decoration: const InputDecoration(labelText: 'Nội dung *'),
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        onPressed: () {
          if (reason.text.trim().isEmpty || content.text.trim().isEmpty) return;
          Navigator.pop(context, {
            'amendment_type': type,
            'reason': reason.text.trim(),
            'content': content.text.trim(),
          });
        },
        child: const Text('Thêm nội dung'),
      ),
    ],
  );
}

class _VitalTimeline extends StatelessWidget {
  const _VitalTimeline({required this.items});

  final List<Map<String, dynamic>> items;

  @override
  Widget build(BuildContext context) {
    if (items.isEmpty) return const Text('Chưa có chỉ số sinh tồn.');
    return Column(
      children: items
          .map(
            (item) => ListTile(
              contentPadding: EdgeInsets.zero,
              leading: const Icon(Icons.monitor_heart_outlined),
              title: Text(formatDateTime(item['measured_at'])),
              subtitle: Text(
                'Nhiệt độ ${valueText(item['temperature_c'])}°C · '
                'HA ${valueText(item['systolic_bp'])}/${valueText(item['diastolic_bp'])} · '
                'Mạch ${valueText(item['heart_rate_bpm'])} · SpO₂ ${valueText(item['spo2_percent'])}%',
              ),
            ),
          )
          .toList(),
    );
  }
}

class EncounterWorkspace extends StatefulWidget {
  const EncounterWorkspace({
    super.key,
    required this.session,
    required this.encounterId,
  });
  final SessionController session;
  final int encounterId;
  @override
  State<EncounterWorkspace> createState() => _EncounterWorkspaceState();
}

class _EncounterWorkspaceState extends State<EncounterWorkspace> {
  Map<String, dynamic> detail = {};
  List<Map<String, dynamic>> services = [], series = [];
  bool loading = true, busy = false;
  String? error;
  bool can(String permission) =>
      widget.session.permissions.contains(permission);
  bool get draft => detail['status'] == 'DRAFT';
  @override
  void initState() {
    super.initState();
    load();
  }

  Future<void> load() async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final data = await Future.wait([
        widget.session.requestJson(
          'GET',
          '/api/v1/encounters/${widget.encounterId}',
        ),
        widget.session.requestJson(
          'GET',
          '/api/v1/encounters/${widget.encounterId}/services',
        ),
        if (can('prescription.view.assigned_or_history') ||
            can('prescription.draft.manage.assigned'))
          widget.session.requestJson(
            'GET',
            '/api/v1/encounters/${widget.encounterId}/prescription-series',
          )
        else
          Future.value(<String, dynamic>{'items': []}),
      ]);
      if (mounted) {
        setState(() {
          detail = data[0];
          services = apiItems(data[1]);
          series = apiItems(data[2]);
        });
      }
    } catch (cause) {
      if (mounted) setState(() => error = friendlyError(cause));
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> run(Future<void> Function() action) async {
    setState(() => busy = true);
    try {
      await action();
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  Future<void> edit() async {
    final result = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => _EncounterFormDialog(initial: detail),
    );
    if (result == null || !mounted) return;
    await run(() async {
      await widget.session.requestJson(
        'PATCH',
        '/api/v1/encounters/${widget.encounterId}',
        body: {...result, 'expected_version': detail['version']},
      );
    });
  }

  Future<void> vital() async {
    final result = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => const _VitalSignsDialog(),
    );
    if (result == null || !mounted) return;
    await run(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${widget.encounterId}/vital-signs',
        body: result,
      );
    });
  }

  Future<void> service() async {
    final result = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => _ServiceOrderDialog(session: widget.session),
    );
    if (result == null || !mounted) return;
    await run(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${widget.encounterId}/services',
        body: result,
      );
    });
  }

  Future<void> prescription() async {
    await run(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/${widget.encounterId}/prescription-series',
        body: {'overall_note': ''},
      );
    });
  }

  List<String> get missing => [
    if (apiString(detail['chief_complaint']).trim().isEmpty) 'Lý do khám',
    if (apiString(detail['diagnosis']).trim().isEmpty) 'Chẩn đoán',
    if (apiString(detail['conclusion']).trim().isEmpty) 'Kết luận',
    if (apiItems(detail['vital_signs']).isEmpty) 'Ít nhất một sinh hiệu',
  ];
  Future<void> complete() async {
    if (missing.isNotEmpty) {
      showActionError(context, 'Cần bổ sung: ${missing.join(', ')}.');
      return;
    }
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Hoàn tất lần khám?',
      message: series.any((s) => s['status'] == 'DRAFT')
          ? 'Còn đơn thuốc nháp chưa phát hành. Xác nhận hoàn tất bệnh án và xử lý đơn nháp sau?'
          : 'Bệnh án sẽ chuyển sang chỉ đọc. Đã kiểm tra dịch vụ và đơn thuốc?',
      confirmLabel: 'Hoàn tất khám',
    );
    if (!confirmed || !mounted) return;
    await run(() async {
      final appointment = await widget.session.requestJson(
        'GET',
        '/api/v1/appointments/${detail['appointment_id']}',
      );
      await widget.session.requestJson(
        'POST',
        '/api/v1/appointments/${detail['appointment_id']}/complete',
        body: {
          'appointment_expected_version': appointment['version'],
          'encounter_expected_version': detail['version'],
        },
      );
    });
  }

  @override
  Widget build(BuildContext context) => Dialog(
    child: SizedBox(
      width: 1000,
      height: 720,
      child: DefaultTabController(
        length: 5,
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.md),
          child: Column(
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      '${valueText(detail['patient_name_at_start'])} · ${valueText(detail['patient_code_at_start'])}\n${valueText(detail['encounter_code'])}',
                      style: Theme.of(context).textTheme.titleLarge,
                    ),
                  ),
                  IconButton(
                    tooltip: 'Tải lại',
                    onPressed: busy ? null : load,
                    icon: const Icon(Icons.refresh),
                  ),
                  IconButton(
                    tooltip: 'Đóng',
                    onPressed: busy ? null : () => Navigator.pop(context),
                    icon: const Icon(Icons.close),
                  ),
                ],
              ),
              const TabBar(
                isScrollable: true,
                tabs: [
                  Tab(text: 'Bệnh án'),
                  Tab(text: 'Sinh hiệu'),
                  Tab(text: 'Dịch vụ'),
                  Tab(text: 'Đơn thuốc'),
                  Tab(text: 'Hoàn tất'),
                ],
              ),
              Expanded(
                child: AsyncContent(
                  loading: loading,
                  error: error,
                  keepChildOnError: detail.isNotEmpty,
                  onRetry: load,
                  child: TabBarView(
                    children: [
                      ListView(
                        children: [
                          for (final entry in {
                            'chief_complaint': 'Lý do khám',
                            'symptoms': 'Triệu chứng',
                            'diagnosis': 'Chẩn đoán',
                            'conclusion': 'Kết luận',
                            'clinical_notes': 'Ghi chú lâm sàng',
                          }.entries)
                            ListTile(
                              title: Text(entry.value),
                              subtitle: Text(valueText(detail[entry.key])),
                            ),
                          if (draft && can('encounter.update.assigned'))
                            FilledButton(
                              onPressed: busy ? null : edit,
                              child: const Text('Cập nhật bệnh án'),
                            ),
                        ],
                      ),
                      ListView(
                        children: [
                          _VitalTimeline(
                            items: apiItems(detail['vital_signs']),
                          ),
                          if (draft && can('encounter.update.assigned'))
                            FilledButton(
                              onPressed: busy ? null : vital,
                              child: const Text('Ghi sinh hiệu'),
                            ),
                        ],
                      ),
                      ListView(
                        children: [
                          for (final item in services)
                            ListTile(
                              title: Text(
                                valueText(item['service_name_at_order']),
                              ),
                              subtitle: Text(
                                'Số lượng: ${valueText(item['quantity'])}',
                              ),
                            ),
                          if (draft && can('encounter.service.assign'))
                            FilledButton(
                              onPressed: busy ? null : service,
                              child: const Text('Chỉ định dịch vụ'),
                            ),
                        ],
                      ),
                      series.isEmpty
                          ? Center(
                              child:
                                  can('prescription.draft.manage.assigned') &&
                                      draft
                                  ? FilledButton(
                                      onPressed: busy ? null : prescription,
                                      child: const Text('Tạo đơn thuốc'),
                                    )
                                  : const Text('Chưa có đơn thuốc'),
                            )
                          : ListView(
                              children: [
                                for (final item in series)
                                  SizedBox(
                                    height: 530,
                                    child: PrescriptionEditor(
                                      session: widget.session,
                                      seriesId: apiInt(item['id']),
                                      embedded: true,
                                      onChanged: load,
                                    ),
                                  ),
                              ],
                            ),
                      ListView(
                        children: [
                          for (final entry in {
                            'chief_complaint': 'Lý do khám',
                            'diagnosis': 'Chẩn đoán',
                            'conclusion': 'Kết luận',
                          }.entries)
                            ListTile(
                              leading: Icon(
                                apiString(detail[entry.key]).trim().isEmpty
                                    ? Icons.warning_amber
                                    : Icons.check_circle,
                              ),
                              title: Text(entry.value),
                            ),
                          ListTile(
                            leading: Icon(
                              apiItems(detail['vital_signs']).isEmpty
                                  ? Icons.warning_amber
                                  : Icons.check_circle,
                            ),
                            title: const Text('Có sinh hiệu'),
                          ),
                          ListTile(
                            title: Text(
                              series.isEmpty
                                  ? 'Không kê thuốc'
                                  : series.any((s) => s['status'] == 'DRAFT')
                                  ? 'Có đơn nháp chưa phát hành'
                                  : 'Đơn thuốc đã phát hành',
                            ),
                          ),
                          if (draft && can('encounter.finalize.assigned'))
                            FilledButton(
                              onPressed: busy ? null : complete,
                              child: const Text('Hoàn tất khám'),
                            )
                          else
                            const Text(
                              'Bệnh án đã hoàn tất hoặc bạn không có quyền hoàn tất.',
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

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
import '../shared/list_pagination.dart';

class PrescriptionsScreen extends StatefulWidget {
  const PrescriptionsScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<PrescriptionsScreen> createState() => _PrescriptionsScreenState();
}

class _PrescriptionsScreenState extends State<PrescriptionsScreen> {
  final encounterId = TextEditingController();
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination();
  bool loading = true;
  bool mutating = false;
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
    encounterId.dispose();
    super.dispose();
  }

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (!own && encounterId.text.trim().isNotEmpty)
        'encounter_id': encounterId.text.trim(),
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        own ? '/api/v1/me/prescriptions' : '/api/v1/prescription-series',
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

  Future<void> openDetail(Map<String, dynamic> item) async {
    await showDialog<void>(
      context: context,
      builder: (_) => PrescriptionEditor(
        session: widget.session,
        seriesId: apiInt(item['id']),
      ),
    );
    await load();
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: own ? 'Đơn thuốc của tôi' : 'Đơn thuốc',
    description: own
        ? 'Các đơn đã phát hành và lịch sử phiên bản của bạn.'
        : 'Theo dõi đơn thuốc và các lần kê đơn đã phát hành.',
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      if (!own)
        SectionCard(
          title: 'Bộ lọc',
          child: Wrap(
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            children: [
              SizedBox(
                width: 200,
                child: EntityPicker(
                  label: 'Bệnh án',
                  search: (q) async {
                    final rows = apiItems(
                      await widget.session.requestJson(
                        'GET',
                        '/api/v1/encounters',
                        query: {
                          'limit': '30',
                          if (q.isNotEmpty) 'encounter_code': q,
                        },
                      ),
                    );
                    return rows
                        .map(
                          (item) => EntityOption(
                            id: apiInt(item['id']),
                            label: valueText(item['encounter_code']),
                            subtitle: valueText(item['patient_name_at_start']),
                          ),
                        )
                        .toList();
                  },
                  onSelected: (item) {
                    encounterId.text = item?.id.toString() ?? '';
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
        title: 'Danh sách đơn thuốc',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Mã đơn')),
            DataColumn(label: Text('Bệnh nhân')),
            DataColumn(label: Text('Cập nhật')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('Thao tác')),
          ],
          rows: items.map(_row).toList(),
          mobileCards: items.map(_card).toList(),
          emptyTitle: 'Chưa có đơn thuốc',
          emptyMessage: 'Không có đơn thuốc phù hợp với phạm vi hiện tại.',
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    onSelectChanged: (_) => openDetail(item),
    cells: [
      DataCell(Text(valueText(item['prescription_code']))),
      DataCell(
        Text(
          '${valueText(item['patient_name'])}\n${valueText(item['patient_code'])}',
        ),
      ),
      DataCell(Text(formatDateTime(item['updated_at']))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(
        IconButton(
          tooltip: 'Xem đơn thuốc',
          onPressed: () => openDetail(item),
          icon: const Icon(Icons.open_in_new),
        ),
      ),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: ListTile(
      onTap: () => openDetail(item),
      leading: const Icon(Icons.medication_outlined),
      title: Text(valueText(item['prescription_code'])),
      subtitle: Text(
        '${valueText(item['patient_name'])} · ${valueText(item['patient_code'])} · ${formatDateTime(item['updated_at'])}',
      ),
      trailing: StatusBadge(status: apiString(item['status'])),
    ),
  );
}

class PrescriptionEditor extends StatefulWidget {
  const PrescriptionEditor({
    super.key,
    required this.session,
    required this.seriesId,
    this.embedded = false,
    this.onChanged,
  });

  final SessionController session;
  final int seriesId;
  final bool embedded;
  final Future<void> Function()? onChanged;

  @override
  State<PrescriptionEditor> createState() => PrescriptionEditorState();
}

class PrescriptionEditorState extends State<PrescriptionEditor> {
  Map<String, dynamic>? series;
  bool loading = true;
  String? error;

  bool can(String permission) =>
      widget.session.permissions.contains(permission);

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
      series = await widget.session.requestJson(
        'GET',
        '/api/v1/prescription-series/${widget.seriesId}',
      );
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  List<Map<String, dynamic>> get versions =>
      apiItems({'items': series?['versions']});

  Map<String, dynamic>? get draft {
    for (final item in versions) {
      if (item['status'] == 'DRAFT') return item;
    }
    return null;
  }

  Future<void> editNote(Map<String, dynamic> prescription) async {
    final controller = TextEditingController(
      text: apiString(prescription['overall_note']),
    );
    final note = await showDialog<String>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Ghi chú chung của đơn thuốc'),
        content: SizedBox(
          width: 520,
          child: TextField(
            controller: controller,
            autofocus: true,
            maxLines: 4,
            decoration: const InputDecoration(labelText: 'Ghi chú'),
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Hủy'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, controller.text.trim()),
            child: const Text('Lưu'),
          ),
        ],
      ),
    );
    controller.dispose();
    if (note == null || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'PATCH',
        '/api/v1/prescriptions/${prescription['id']}',
        body: {
          'expected_version': prescription['version'],
          'overall_note': note,
        },
      ),
    );
  }

  Future<void> addItem(Map<String, dynamic> prescription) async {
    final values = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => _PrescriptionItemDialog(session: widget.session),
    );
    if (values == null || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/prescriptions/${prescription['id']}/items',
        body: values,
      ),
    );
  }

  Future<void> removeItem(
    Map<String, dynamic> prescription,
    Map<String, dynamic> item,
  ) async {
    final reason = await showReasonDialog(
      context,
      title: 'Gỡ thuốc khỏi đơn?',
      message: valueText(item['medicine_name_at_prescription']),
      confirmLabel: 'Gỡ thuốc',
      destructive: true,
    );
    if (reason == null || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'DELETE',
        '/api/v1/prescriptions/${prescription['id']}/items/${item['id']}',
        body: {'expected_version': item['version'], 'reason': reason},
      ),
    );
  }

  Future<void> issue(Map<String, dynamic> prescription) async {
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Phát hành đơn thuốc?',
      message:
          'Sau khi phát hành, phiên bản này chuyển sang chỉ đọc. Kiểm tra thuốc, số lượng và hướng dẫn dùng trước khi tiếp tục.',
      confirmLabel: 'Phát hành',
    );
    if (!confirmed || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/prescriptions/${prescription['id']}/issue',
        body: {'expected_version': prescription['version']},
      ),
    );
  }

  Future<void> discard(Map<String, dynamic> prescription) async {
    final reason = await showReasonDialog(
      context,
      title: 'Bỏ bản nháp đơn thuốc?',
      message: 'Bản nháp sẽ được giữ trong lịch sử với trạng thái đã bỏ.',
      confirmLabel: 'Bỏ bản nháp',
      destructive: true,
    );
    if (reason == null || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/prescriptions/${prescription['id']}/discard',
        body: {'expected_version': prescription['version'], 'reason': reason},
      ),
    );
  }

  Future<void> replacement() async {
    final confirmed = await showConfirmActionDialog(
      context,
      title: 'Tạo phiên bản thay thế?',
      message:
          'Hệ thống sẽ clone phiên bản hiện hành thành bản nháp mới nếu encounter còn cho phép.',
      confirmLabel: 'Tạo phiên bản',
    );
    if (!confirmed || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/prescription-series/${widget.seriesId}/versions',
      ),
    );
  }

  Future<void> _run(Future<Map<String, dynamic>> Function() operation) async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      await operation();
      await load();
      if (widget.onChanged != null) await widget.onChanged!();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  @override
  Widget build(BuildContext context) =>
      widget.embedded ? _content(context) : Dialog(child: _content(context));

  Widget _content(BuildContext context) => ConstrainedBox(
    constraints: const BoxConstraints(maxWidth: 980, maxHeight: 800),
    child: Padding(
      padding: const EdgeInsets.all(AppSpacing.xl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  series == null
                      ? 'Chi tiết đơn thuốc'
                      : valueText(series!['prescription_code']),
                  style: Theme.of(context).textTheme.headlineSmall,
                ),
              ),
              if (series != null)
                StatusBadge(status: apiString(series!['status'])),
              if (series != null &&
                  draft == null &&
                  can('prescription.version.create.own'))
                TextButton.icon(
                  onPressed: loading ? null : replacement,
                  icon: const Icon(Icons.copy_all_outlined),
                  label: const Text('Tạo phiên bản thay thế'),
                ),
              if (!widget.embedded)
                IconButton(
                  tooltip: 'Đóng',
                  onPressed: () => Navigator.pop(context),
                  icon: const Icon(Icons.close),
                ),
            ],
          ),
          const SizedBox(height: AppSpacing.md),
          Expanded(
            child: AsyncContent(
              loading: loading,
              error: error,
              keepChildOnError: series != null,
              onRetry: load,
              empty: series == null,
              child: ListView.separated(
                itemCount: versions.length,
                separatorBuilder: (_, _) =>
                    const SizedBox(height: AppSpacing.md),
                itemBuilder: (context, index) => _versionCard(versions[index]),
              ),
            ),
          ),
        ],
      ),
    ),
  );

  Widget _versionCard(Map<String, dynamic> prescription) {
    final items = apiItems({'items': prescription['items']});
    final editable =
        prescription['status'] == 'DRAFT' &&
        can('prescription.draft.manage.assigned');
    return SectionCard(
      title:
          '${valueText(prescription['version_code'])} · phiên bản ${valueText(prescription['version_number'])}',
      description: valueText(prescription['overall_note']),
      trailing: StatusBadge(status: apiString(prescription['status'])),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (items.isEmpty)
            const Text('Chưa có thuốc trong phiên bản này.')
          else
            ...items.map(
              (item) => ListTile(
                contentPadding: EdgeInsets.zero,
                leading: const Icon(Icons.medication_outlined),
                title: Text(valueText(item['medicine_name_at_prescription'])),
                subtitle: Text(
                  '${valueText(item['strength_at_prescription'])} · '
                  'SL ${valueText(item['quantity'])} ${valueText(item['dispense_unit_at_prescription'])}\n'
                  '${valueText(item['usage_instruction'])}',
                ),
                isThreeLine: true,
                trailing: editable
                    ? IconButton(
                        tooltip: 'Gỡ thuốc',
                        onPressed: () => removeItem(prescription, item),
                        icon: const Icon(Icons.delete_outline),
                      )
                    : null,
              ),
            ),
          if (editable) ...[
            const Divider(),
            Wrap(
              spacing: AppSpacing.xs,
              runSpacing: AppSpacing.xs,
              children: [
                OutlinedButton.icon(
                  onPressed: () => editNote(prescription),
                  icon: const Icon(Icons.edit_note_outlined),
                  label: const Text('Sửa ghi chú'),
                ),
                FilledButton.tonalIcon(
                  onPressed: () => addItem(prescription),
                  icon: const Icon(Icons.add),
                  label: const Text('Thêm thuốc'),
                ),
                if (can('prescription.issue.assigned'))
                  FilledButton.icon(
                    onPressed: items.isEmpty ? null : () => issue(prescription),
                    icon: const Icon(Icons.verified_outlined),
                    label: const Text('Phát hành'),
                  ),
                TextButton.icon(
                  onPressed: () => discard(prescription),
                  icon: const Icon(Icons.delete_outline),
                  label: const Text('Bỏ bản nháp'),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _PrescriptionItemDialog extends StatefulWidget {
  const _PrescriptionItemDialog({required this.session});
  final SessionController session;

  @override
  State<_PrescriptionItemDialog> createState() =>
      _PrescriptionItemDialogState();
}

class _PrescriptionItemDialogState extends State<_PrescriptionItemDialog> {
  final medicineId = TextEditingController();
  final quantity = TextEditingController(text: '1');
  final instruction = TextEditingController();
  final notes = TextEditingController();
  final sortOrder = TextEditingController(text: '1');

  @override
  void dispose() {
    for (final controller in [
      medicineId,
      quantity,
      instruction,
      notes,
      sortOrder,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Thêm thuốc'),
    content: SizedBox(
      width: 620,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Row(
            children: [
              Expanded(
                child: EntityPicker(
                  label: 'Thuốc *',
                  search: (q) => searchEntities(
                    widget.session,
                    '/api/v1/medicines',
                    q,
                    nameKey: 'name',
                    codeKey: 'code',
                    doctors: false,
                  ),
                  onSelected: (option) {
                    medicineId.text = option?.id.toString() ?? '';
                  },
                ),
              ),
              const SizedBox(width: AppSpacing.sm),
              Expanded(
                child: TextField(
                  controller: quantity,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(labelText: 'Số lượng *'),
                ),
              ),
              const SizedBox(width: AppSpacing.sm),
              Expanded(
                child: TextField(
                  controller: sortOrder,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(labelText: 'Thứ tự'),
                ),
              ),
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          TextField(
            controller: instruction,
            maxLines: 2,
            decoration: const InputDecoration(labelText: 'Hướng dẫn sử dụng *'),
          ),
          const SizedBox(height: AppSpacing.sm),
          TextField(
            controller: notes,
            maxLines: 2,
            decoration: const InputDecoration(labelText: 'Ghi chú'),
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
          final medicine = int.tryParse(medicineId.text);
          final count = int.tryParse(quantity.text);
          final order = int.tryParse(sortOrder.text) ?? 1;
          if (medicine == null ||
              count == null ||
              instruction.text.trim().isEmpty) {
            return;
          }
          Navigator.pop(context, {
            'medicine_id': medicine,
            'quantity': count,
            'usage_instruction': instruction.text.trim(),
            'notes': notes.text.trim(),
            'sort_order': order,
          });
        },
        child: const Text('Thêm thuốc'),
      ),
    ],
  );
}

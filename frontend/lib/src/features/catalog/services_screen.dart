import '../shared/list_pagination.dart';
import 'package:flutter/material.dart';
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

class ServicesScreen extends StatefulWidget {
  const ServicesScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<ServicesScreen> createState() => _ServicesScreenState();
}

class _ServicesScreenState extends State<ServicesScreen> {
  final search = TextEditingController();
  String status = '';
  String type = '';
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination(pageSize: 100);
  bool loading = true;
  bool mutating = false;
  String? error;

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

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (search.text.trim().isNotEmpty) 'q': search.text.trim(),
      if (status.isNotEmpty) 'status': status,
      if (type.isNotEmpty) 'type': type,
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/services',
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

  Future<void> create() async {
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => const _ServiceEditor(),
      submit: (values) =>
          widget.session.requestJson('POST', '/api/v1/services', body: values),
    );
    if (response != null && mounted) {
      showSuccessMessage(context, 'Đã tạo danh mục.');
      await load();
    }
  }

  Future<void> edit(Map<String, dynamic> item) async {
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => _ServiceEditor(service: item),
      submit: (values) => widget.session.requestJson(
        'PATCH',
        '/api/v1/services/${item['id']}',
        body: values,
      ),
    );
    if (response != null && mounted) {
      showSuccessMessage(context, 'Đã cập nhật danh mục.');
      await load();
    }
  }

  Future<void> changeState(Map<String, dynamic> item, String action) async {
    String? reason;
    if (action == 'archive') {
      reason = await showReasonDialog(
        context,
        title: 'Lưu trữ dịch vụ',
        message: 'Dịch vụ phải ở trạng thái INACTIVE.',
        confirmLabel: 'Lưu trữ',
        destructive: true,
      );
      if (reason == null || !mounted) return;
    } else {
      final confirmed = await showConfirmActionDialog(
        context,
        title: action == 'activate' ? 'Kích hoạt dịch vụ?' : 'Ngừng dịch vụ?',
        message: valueText(item['name']),
        confirmLabel: action == 'activate' ? 'Kích hoạt' : 'Ngừng',
      );
      if (!confirmed || !mounted) return;
    }
    await _run(() async {
      await widget.session.requestJson(
        'POST',
        '/api/v1/services/${item['id']}/$action',
        body: {
          'expected_version': apiInt(item['version']),
          'reason': reason ?? '',
        },
      );
      if (mounted) showSuccessMessage(context, 'Đã cập nhật trạng thái.');
    });
  }

  Future<void> prices(Map<String, dynamic> item) async {
    await showDialog<void>(
      context: context,
      builder: (_) =>
          _ServicePricesDialog(session: widget.session, service: item),
    );
    await load();
  }

  Future<void> _run(Future<void> Function() operation) async {
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
    title: 'Danh mục dịch vụ',
    description: 'Quản lý dịch vụ khám, thủ thuật và lịch sử giá có hiệu lực.',
    actions: [
      if (can('service.manage'))
        FilledButton.icon(
          onPressed: mutating ? null : create,
          icon: const Icon(Icons.add),
          label: const Text('Thêm dịch vụ'),
        ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Bộ lọc',
        child: FilterBar(
          searchController: search,
          onSearch: (_) => load(),
          hintText: 'Mã hoặc tên dịch vụ',
          onClear: () {
            search.clear();
            setState(() {
              status = '';
              type = '';
            });
            load();
          },
          filters: [
            _DropdownFilter(
              label: 'Trạng thái',
              value: status,
              values: const ['', 'ACTIVE', 'INACTIVE'],
              onChanged: (value) {
                setState(() => status = value ?? '');
                load();
              },
            ),
            _DropdownFilter(
              label: 'Loại dịch vụ',
              value: type,
              values: const ['', 'CONSULTATION', 'PROCEDURE', 'OTHER'],
              onChanged: (value) {
                setState(() => type = value ?? '');
                load();
              },
            ),
          ],
        ),
      ),
      SectionCard(
        title: 'Dịch vụ',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Mã')),
            DataColumn(label: Text('Tên')),
            DataColumn(label: Text('Loại')),
            DataColumn(label: Text('Giá hiện tại')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('Thao tác')),
          ],
          rows: items.map(_row).toList(),
          mobileCards: items.map(_card).toList(),
          emptyTitle: 'Chưa có dịch vụ',
          emptyMessage: 'Hãy tạo dịch vụ đầu tiên hoặc đổi bộ lọc.',
        ),
      ),
    ],
  );

  String _price(Map<String, dynamic> item) =>
      formatVnd(apiMap(item['current_price'])['unit_price_vnd']);

  DataRow _row(Map<String, dynamic> item) => DataRow(
    cells: [
      DataCell(Text(valueText(item['code']))),
      DataCell(Text(valueText(item['name']))),
      DataCell(Text(enumLabel(item['service_type']))),
      DataCell(Text(_price(item))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(
        PopupMenuButton<String>(
          tooltip: 'Thao tác',
          onSelected: (value) {
            switch (value) {
              case 'edit':
                edit(item);
              case 'prices':
                prices(item);
              default:
                changeState(item, value);
            }
          },
          itemBuilder: (_) => [
            if (can('service.manage')) ...[
              const PopupMenuItem(value: 'edit', child: Text('Chỉnh sửa')),
              const PopupMenuItem(value: 'prices', child: Text('Quản lý giá')),
              if (item['status'] == 'INACTIVE')
                const PopupMenuItem(
                  value: 'activate',
                  child: Text('Kích hoạt'),
                ),
              if (item['status'] == 'ACTIVE')
                const PopupMenuItem(value: 'deactivate', child: Text('Ngừng')),
              if (item['status'] == 'INACTIVE')
                const PopupMenuItem(value: 'archive', child: Text('Lưu trữ')),
            ],
          ],
        ),
      ),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: ListTile(
      leading: const Icon(Icons.medical_services_outlined),
      title: Text('${valueText(item['code'])} · ${valueText(item['name'])}'),
      subtitle: Text('${enumLabel(item['service_type'])} · ${_price(item)}'),
      trailing: StatusBadge(status: apiString(item['status'])),
      onTap: can('service.manage') ? () => prices(item) : null,
    ),
  );
}

class _ServicePricesDialog extends StatefulWidget {
  const _ServicePricesDialog({required this.session, required this.service});

  final SessionController session;
  final Map<String, dynamic> service;

  @override
  State<_ServicePricesDialog> createState() => _ServicePricesDialogState();
}

class _ServicePricesDialogState extends State<_ServicePricesDialog> {
  List<Map<String, dynamic>> items = const [];
  bool loading = true;
  String? error;

  @override
  void initState() {
    super.initState();
    load();
  }

  Future<void> load() async {
    setState(() => loading = true);
    try {
      items = apiItems(
        await widget.session.requestJson(
          'GET',
          '/api/v1/services/${widget.service['id']}/price-versions',
        ),
      );
      error = null;
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> add() async {
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => const _PriceEditor(servicePrice: true),
      submit: (values) => widget.session.requestJson(
        'POST',
        '/api/v1/services/${widget.service['id']}/price-versions',
        body: values,
      ),
    );
    if (response != null && mounted) await load();
  }

  Future<void> cancel(Map<String, dynamic> item) async {
    final reason = await showReasonDialog(
      context,
      title: 'Hủy phiên bản giá',
      message: 'Chỉ phiên bản SCHEDULED mới có thể hủy.',
      confirmLabel: 'Hủy giá',
      destructive: true,
    );
    if (reason == null || !mounted) return;
    try {
      await widget.session.requestJson(
        'POST',
        '/api/v1/services/${widget.service['id']}/price-versions/${item['id']}/cancel',
        body: {'reason': reason},
      );
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('Giá · ${valueText(widget.service['name'])}'),
    content: SizedBox(
      width: 720,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (error != null) ...[
            InlineMessage(message: error!, tone: AppStatusTone.danger),
            const SizedBox(height: AppSpacing.md),
          ],
          Flexible(
            child: DataTableShell(
              loading: loading,
              error: error,
              onRetry: load,
              columns: const [
                DataColumn(label: Text('Hiệu lực từ')),
                DataColumn(label: Text('Đơn giá')),
                DataColumn(label: Text('Giai đoạn')),
                DataColumn(label: Text('')),
              ],
              rows: items
                  .map(
                    (item) => DataRow(
                      cells: [
                        DataCell(Text(formatDateTime(item['effective_from']))),
                        DataCell(Text(formatVnd(item['unit_price_vnd']))),
                        DataCell(StatusBadge(status: apiString(item['phase']))),
                        DataCell(
                          item['phase'] == 'SCHEDULED'
                              ? IconButton(
                                  tooltip: 'Hủy giá',
                                  onPressed: () => cancel(item),
                                  icon: const Icon(Icons.cancel_outlined),
                                )
                              : const SizedBox.shrink(),
                        ),
                      ],
                    ),
                  )
                  .toList(),
            ),
          ),
        ],
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Đóng'),
      ),
      FilledButton.icon(
        onPressed: add,
        icon: const Icon(Icons.add),
        label: const Text('Thêm giá'),
      ),
    ],
  );
}

class _ServiceEditor extends StatefulWidget {
  const _ServiceEditor({this.service});

  final Map<String, dynamic>? service;

  @override
  State<_ServiceEditor> createState() => _ServiceEditorState();
}

class _ServiceEditorState extends State<_ServiceEditor> {
  final key = GlobalKey<FormState>();
  late final TextEditingController code;
  late final TextEditingController name;
  late final TextEditingController unit;
  late final TextEditingController description;
  late final TextEditingController specialtyId;
  final price = TextEditingController();
  String type = 'OTHER';
  String status = 'INACTIVE';

  bool get editing => widget.service != null;

  @override
  void initState() {
    super.initState();
    final data = widget.service ?? const <String, dynamic>{};
    code = TextEditingController(text: apiString(data['code']));
    name = TextEditingController(text: apiString(data['name']));
    unit = TextEditingController(
      text: apiString(data['unit_label']).isEmpty
          ? 'Lần'
          : apiString(data['unit_label']),
    );
    description = TextEditingController(text: apiString(data['description']));
    specialtyId = TextEditingController(
      text: data['specialty_id']?.toString() ?? '',
    );
    type = apiString(data['service_type']).isEmpty
        ? 'OTHER'
        : apiString(data['service_type']);
    status = apiString(data['status']).isEmpty
        ? 'INACTIVE'
        : apiString(data['status']);
  }

  @override
  void dispose() {
    for (final controller in [
      code,
      name,
      unit,
      description,
      specialtyId,
      price,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(editing ? 'Cập nhật dịch vụ' : 'Thêm dịch vụ'),
    content: SizedBox(
      width: 560,
      child: Form(
        key: key,
        child: SingleChildScrollView(
          child: Column(
            children: [
              const ApiFormNotice(),
              TextFormField(
                controller: code,
                enabled: !editing,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'code'),
                  labelText: 'Mã (SVC-...) *',
                ),
                validator: _required,
              ),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: name,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'name'),
                  labelText: 'Tên dịch vụ *',
                ),
                validator: _required,
              ),
              const SizedBox(height: AppSpacing.md),
              if (!editing)
                DropdownButtonFormField<String>(
                  isExpanded: true,
                  initialValue: type,
                  decoration: const InputDecoration(labelText: 'Loại *'),
                  items: const [
                    DropdownMenuItem(
                      value: 'CONSULTATION',
                      child: Text('Khám'),
                    ),
                    DropdownMenuItem(
                      value: 'PROCEDURE',
                      child: Text('Thủ thuật'),
                    ),
                    DropdownMenuItem(value: 'OTHER', child: Text('Khác')),
                  ],
                  onChanged: (value) => setState(() => type = value ?? 'OTHER'),
                ),
              if (!editing) const SizedBox(height: AppSpacing.md),
              if (!editing && type == 'CONSULTATION')
                TextFormField(
                  controller: specialtyId,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: 'Specialty ID *',
                  ),
                  validator: (value) =>
                      int.tryParse(value ?? '') == null ? 'Bắt buộc.' : null,
                ),
              if (!editing && type == 'CONSULTATION')
                const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: unit,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'unit_label'),
                  labelText: 'Đơn vị *',
                ),
                validator: _required,
              ),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: description,
                maxLines: 3,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'description'),
                  labelText: 'Mô tả',
                ),
              ),
              if (!editing) ...[
                const SizedBox(height: AppSpacing.md),
                DropdownButtonFormField<String>(
                  isExpanded: true,
                  initialValue: status,
                  decoration: const InputDecoration(labelText: 'Trạng thái'),
                  items: const [
                    DropdownMenuItem(
                      value: 'INACTIVE',
                      child: Text('Chưa hoạt động'),
                    ),
                    DropdownMenuItem(value: 'ACTIVE', child: Text('Hoạt động')),
                  ],
                  onChanged: (value) =>
                      setState(() => status = value ?? 'INACTIVE'),
                ),
                const SizedBox(height: AppSpacing.md),
                TextFormField(
                  controller: price,
                  keyboardType: TextInputType.number,
                  decoration: InputDecoration(
                    errorText: apiFieldError(context, 'initial_unit_price_vnd'),
                    labelText: 'Giá ban đầu (VND)',
                  ),
                  validator: (value) {
                    if (status == 'ACTIVE' &&
                        int.tryParse(value ?? '') == null) {
                      return 'Dịch vụ ACTIVE cần giá.';
                    }
                    return null;
                  },
                ),
              ],
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
        onPressed: apiFormBusy(context) ? null : submit,
        child: Text(editing ? 'Lưu' : 'Tạo'),
      ),
    ],
  );

  void submit() {
    if (!(key.currentState?.validate() ?? false)) return;
    if (editing) {
      submitApiForm(context, {
        'expected_version': apiInt(widget.service?['version']),
        'name': name.text.trim(),
        'unit_label': unit.text.trim(),
        'description': description.text.trim(),
      });
      return;
    }
    final initialPrice = int.tryParse(price.text.trim());
    submitApiForm(context, {
      'code': code.text.trim().toUpperCase(),
      'name': name.text.trim(),
      'service_type': type,
      if (type == 'CONSULTATION')
        'specialty_id': int.parse(specialtyId.text.trim()),
      'unit_label': unit.text.trim(),
      'description': description.text.trim(),
      'status': status,
      'initial_unit_price_vnd': ?initialPrice,
    });
  }
}

class _PriceEditor extends StatefulWidget {
  const _PriceEditor({required this.servicePrice});
  final bool servicePrice;

  @override
  State<_PriceEditor> createState() => _PriceEditorState();
}

class _PriceEditorState extends State<_PriceEditor> {
  final key = GlobalKey<FormState>();
  final amount = TextEditingController();
  final effective = TextEditingController();
  bool effectiveNow = true;

  @override
  void dispose() {
    amount.dispose();
    effective.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Thêm phiên bản giá'),
    content: Form(
      key: key,
      child: SizedBox(
        width: 460,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const ApiFormNotice(),
            TextFormField(
              controller: amount,
              keyboardType: TextInputType.number,
              decoration: InputDecoration(
                errorText: apiFieldError(context, 'unit_price_vnd'),
                labelText: 'Đơn giá (VND) *',
              ),
              validator: (value) =>
                  int.tryParse(value ?? '') == null ? 'Nhập số tiền.' : null,
            ),
            const SizedBox(height: AppSpacing.md),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('Ngay bây giờ'),
              value: effectiveNow,
              onChanged: (value) => setState(() {
                effectiveNow = value;
                if (!value && effective.text.isEmpty) {
                  effective.text = clinicTimestamp(
                    clinicNow().add(const Duration(minutes: 1)),
                    TimeOfDay.fromDateTime(
                      clinicNow().add(const Duration(minutes: 1)),
                    ),
                  );
                }
              }),
            ),
            if (!effectiveNow)
              DateTimeField(controller: effective, label: 'Hiệu lực từ'),
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
                if (!(key.currentState?.validate() ?? false)) return;
                submitApiForm(context, {
                  'unit_price_vnd': int.parse(amount.text.trim()),
                  if (!effectiveNow)
                    'effective_from': DateTime.parse(
                      effective.text.trim(),
                    ).toUtc().toIso8601String(),
                });
              },
        child: const Text('Tạo'),
      ),
    ],
  );
}

String? _required(String? value) =>
    value == null || value.trim().isEmpty ? 'Bắt buộc.' : null;

class _DropdownFilter extends StatelessWidget {
  const _DropdownFilter({
    required this.label,
    required this.value,
    required this.values,
    required this.onChanged,
  });
  final String label;
  final String value;
  final List<String> values;
  final ValueChanged<String?> onChanged;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: 170,
    child: DropdownButtonFormField<String>(
      isExpanded: true,
      initialValue: value,
      decoration: InputDecoration(labelText: label),
      items: values
          .map(
            (item) => DropdownMenuItem(
              value: item,
              child: Text(item.isEmpty ? 'Tất cả' : item),
            ),
          )
          .toList(),
      onChanged: onChanged,
    ),
  );
}

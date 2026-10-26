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
import '../shared/api_form.dart';

class InventoryScreen extends StatefulWidget {
  const InventoryScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<InventoryScreen> createState() => _InventoryScreenState();
}

class _InventoryScreenState extends State<InventoryScreen>
    with SingleTickerProviderStateMixin {
  late final TabController tabs;

  bool can(String permission) =>
      widget.session.permissions.contains(permission);

  @override
  void initState() {
    super.initState();
    tabs = TabController(length: 4, vsync: this);
  }

  @override
  void dispose() {
    tabs.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Column(
    children: [
      Material(
        color: Theme.of(context).colorScheme.surface,
        child: TabBar(
          controller: tabs,
          isScrollable: true,
          tabs: const [
            Tab(icon: Icon(Icons.medication_outlined), text: 'Thuốc'),
            Tab(icon: Icon(Icons.inventory_2_outlined), text: 'Tồn & lô'),
            Tab(icon: Icon(Icons.move_to_inbox_outlined), text: 'Nhập kho'),
            Tab(
              icon: Icon(Icons.sync_alt_outlined),
              text: 'Điều chỉnh & sổ kho',
            ),
          ],
        ),
      ),
      Expanded(
        child: TabBarView(
          controller: tabs,
          children: [
            can('medicine.view')
                ? _MedicinesTab(session: widget.session)
                : const _NoAccess(),
            can('inventory.view')
                ? _StockTab(session: widget.session)
                : const _NoAccess(),
            can('inventory.receipt.manage')
                ? _ReceiptsTab(session: widget.session)
                : const _NoAccess(),
            hasAnyPermission(widget.session, const [
                  'inventory.adjust',
                  'inventory.movement.view',
                ])
                ? _LedgerTab(session: widget.session)
                : const _NoAccess(),
          ],
        ),
      ),
    ],
  );
}

class _MedicinesTab extends StatefulWidget {
  const _MedicinesTab({required this.session});
  final SessionController session;

  @override
  State<_MedicinesTab> createState() => _MedicinesTabState();
}

class _MedicinesTabState extends State<_MedicinesTab> {
  final search = TextEditingController();
  String status = '';
  List<Map<String, dynamic>> items = const [];
  final paging = ListPagination();
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
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/medicines',
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
      builder: (_) => const _MedicineEditor(),
      submit: (values) =>
          widget.session.requestJson('POST', '/api/v1/medicines', body: values),
    );
    if (response != null && mounted) {
      showSuccessMessage(context, 'Đã tạo danh mục.');
      await load();
    }
  }

  Future<void> edit(Map<String, dynamic> medicine) async {
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => _MedicineEditor(medicine: medicine),
      submit: (values) => widget.session.requestJson(
        'PATCH',
        '/api/v1/medicines/${medicine['id']}',
        body: values,
      ),
    );
    if (response != null && mounted) {
      showSuccessMessage(context, 'Đã cập nhật danh mục.');
      await load();
    }
  }

  Future<void> state(Map<String, dynamic> medicine, String action) async {
    String reason = '';
    if (action == 'archive') {
      final value = await showReasonDialog(
        context,
        title: 'Lưu trữ thuốc',
        message: 'Thuốc phải ở trạng thái INACTIVE.',
        confirmLabel: 'Lưu trữ',
        destructive: true,
      );
      if (value == null || !mounted) return;
      reason = value;
    } else {
      final confirmed = await showConfirmActionDialog(
        context,
        title: action == 'activate' ? 'Kích hoạt thuốc?' : 'Ngừng thuốc?',
        message: valueText(medicine['name']),
        confirmLabel: action == 'activate' ? 'Kích hoạt' : 'Ngừng',
      );
      if (!confirmed || !mounted) return;
    }
    await run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/medicines/${medicine['id']}/$action',
        body: {
          'expected_version': apiInt(medicine['version']),
          'reason': reason,
        },
      ),
    );
  }

  Future<void> prices(Map<String, dynamic> medicine) async {
    await showDialog<void>(
      context: context,
      builder: (_) =>
          _MedicinePricesDialog(session: widget.session, medicine: medicine),
    );
    await load();
  }

  Future<void> run(Future<Object?> Function() command) async {
    setState(() {
      mutating = true;
      error = null;
    });
    try {
      await command();
      await load();
      if (mounted) showSuccessMessage(context, 'Đã lưu thay đổi.');
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => mutating = false);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Danh mục thuốc',
    description: 'Tra cứu thuốc, giá bán và cảnh báo tồn kho.',
    actions: [
      if (can('medicine.manage'))
        FilledButton.icon(
          onPressed: mutating ? null : create,
          icon: const Icon(Icons.add),
          label: const Text('Thêm thuốc'),
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
          hintText: 'Mã, tên hoặc hoạt chất',
          onClear: () {
            search.clear();
            setState(() => status = '');
            load();
          },
          filters: [
            _Select(
              label: 'Trạng thái',
              value: status,
              values: const ['', 'ACTIVE', 'INACTIVE'],
              onChanged: (value) {
                setState(() => status = value ?? '');
                load();
              },
            ),
          ],
        ),
      ),
      SectionCard(
        title: 'Thuốc',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Mã / Tên')),
            DataColumn(label: Text('Hàm lượng / Dạng')),
            DataColumn(label: Text('Giá bán')),
            DataColumn(label: Text('Tồn bán được')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('')),
          ],
          rows: items.map(_row).toList(),
          mobileCards: items.map(_card).toList(),
          emptyTitle: 'Chưa có thuốc',
          emptyMessage: 'Không có thuốc phù hợp.',
        ),
      ),
    ],
  );

  String price(Map<String, dynamic> item) =>
      formatVnd(apiMap(item['current_price'])['unit_sale_price_vnd']);

  DataRow _row(Map<String, dynamic> item) => DataRow(
    cells: [
      DataCell(Text('${valueText(item['code'])}\n${valueText(item['name'])}')),
      DataCell(
        Text(
          '${valueText(item['strength'])} · ${valueText(item['dosage_form'])}',
        ),
      ),
      DataCell(Text(price(item))),
      DataCell(Text(valueText(item['sellable_quantity']))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(
        PopupMenuButton<String>(
          onSelected: (value) {
            if (value == 'edit') {
              edit(item);
            } else if (value == 'prices') {
              prices(item);
            } else {
              state(item, value);
            }
          },
          itemBuilder: (_) => can('medicine.manage')
              ? [
                  const PopupMenuItem(value: 'edit', child: Text('Chỉnh sửa')),
                  const PopupMenuItem(
                    value: 'prices',
                    child: Text('Quản lý giá'),
                  ),
                  if (item['status'] == 'INACTIVE')
                    const PopupMenuItem(
                      value: 'activate',
                      child: Text('Kích hoạt'),
                    ),
                  if (item['status'] == 'ACTIVE')
                    const PopupMenuItem(
                      value: 'deactivate',
                      child: Text('Ngừng'),
                    ),
                  if (item['status'] == 'INACTIVE')
                    const PopupMenuItem(
                      value: 'archive',
                      child: Text('Lưu trữ'),
                    ),
                ]
              : const [],
        ),
      ),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: ListTile(
      onTap: can('medicine.manage') ? () => prices(item) : null,
      leading: const Icon(Icons.medication_outlined),
      title: Text('${valueText(item['code'])} · ${valueText(item['name'])}'),
      subtitle: Text(
        '${price(item)} · Tồn: ${valueText(item['sellable_quantity'])}',
      ),
      trailing: StatusBadge(status: apiString(item['status'])),
    ),
  );
}

class _StockTab extends StatefulWidget {
  const _StockTab({required this.session});
  final SessionController session;

  @override
  State<_StockTab> createState() => _StockTabState();
}

class _StockTabState extends State<_StockTab> {
  Map<String, dynamic> summary = const {};
  List<Map<String, dynamic>> batches = const [];
  final paging = ListPagination();
  final search = TextEditingController();
  bool loading = true;
  String? error;

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
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final results = await Future.wait([
        widget.session.requestJson('GET', '/api/v1/inventory/summary'),
        widget.session.requestJson(
          'GET',
          '/api/v1/inventory/batches',
          query: request.query,
        ),
      ]);
      if (!mounted || !paging.accept(request, results[1])) return;
      summary = results[0];
      batches = apiItems(results[1]);
    } catch (cause) {
      if (mounted && paging.isCurrent(request)) error = friendlyError(cause);
    } finally {
      if (mounted && paging.isCurrent(request)) setState(() => loading = false);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Tồn kho & lô thuốc',
    description: 'Số dư chính xác theo lô, hạn dùng và cảnh báo vận hành.',
    actions: [
      IconButton(
        onPressed: loading ? null : load,
        tooltip: 'Làm mới',
        icon: const Icon(Icons.refresh),
      ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      Wrap(
        spacing: AppSpacing.md,
        runSpacing: AppSpacing.md,
        children: [
          _Metric(
            label: 'Tồn vật lý',
            value: valueText(summary['physical_quantity']),
          ),
          _Metric(
            label: 'Có thể bán',
            value: valueText(summary['sellable_quantity']),
          ),
          _Metric(
            label: 'Đã hết hạn',
            value: valueText(summary['expired_quantity']),
            danger: true,
          ),
          _Metric(
            label: 'Thuốc sắp hết',
            value: valueText(summary['low_stock_count']),
            danger: true,
          ),
          _Metric(
            label: 'Lô sắp hết hạn',
            value: valueText(summary['expiry_soon_count']),
            danger: true,
          ),
        ],
      ),
      SectionCard(
        title: 'Tìm lô thuốc',
        child: FilterBar(
          searchController: search,
          onSearch: (_) => load(),
          hintText: 'Tìm lô, mã hoặc tên thuốc',
        ),
      ),
      SectionCard(
        title: 'Các lô thuốc',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Lô')),
            DataColumn(label: Text('Thuốc')),
            DataColumn(label: Text('Hạn dùng')),
            DataColumn(label: Text('Tồn')),
            DataColumn(label: Text('Giá nhập')),
            DataColumn(label: Text('Trạng thái')),
          ],
          rows: batches
              .map(
                (item) => DataRow(
                  cells: [
                    DataCell(Text(valueText(item['lot_number']))),
                    DataCell(
                      Text(
                        '${valueText(item['medicine_code'])}\n${valueText(item['medicine_name'])}',
                      ),
                    ),
                    DataCell(Text(formatDate(item['expires_on']))),
                    DataCell(Text(valueText(item['quantity_on_hand']))),
                    DataCell(Text(formatVnd(item['unit_purchase_cost_vnd']))),
                    DataCell(StatusBadge(status: apiString(item['state']))),
                  ],
                ),
              )
              .toList(),
          mobileCards: batches
              .map(
                (item) => Card(
                  child: ListTile(
                    title: Text(
                      '${valueText(item['medicine_code'])} · Lô ${valueText(item['lot_number'])}',
                    ),
                    subtitle: Text(
                      'HSD ${formatDate(item['expires_on'])} · Tồn ${valueText(item['quantity_on_hand'])}',
                    ),
                    trailing: StatusBadge(status: apiString(item['state'])),
                  ),
                ),
              )
              .toList(),
        ),
      ),
    ],
  );
}

class _ReceiptsTab extends StatefulWidget {
  const _ReceiptsTab({required this.session});
  final SessionController session;

  @override
  State<_ReceiptsTab> createState() => _ReceiptsTabState();
}

class _ReceiptsTabState extends State<_ReceiptsTab> {
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
        await widget.session.requestJson('GET', '/api/v1/inventory/receipts'),
      );
      error = null;
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> create() async {
    final requestKey = idempotencyKey('stock-receipt');
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => _ReceiptDialog(session: widget.session),
      submit: (values) => widget.session.requestJson(
        'POST',
        '/api/v1/inventory/receipts',
        headers: {'Idempotency-Key': requestKey},
        body: values,
      ),
    );
    if (response != null && mounted) {
      showSuccessMessage(context, 'Đã ghi nhận kho.');
      await load();
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Phiếu nhập kho',
    description: 'Phiếu nhập bất biến; mỗi dòng tạo lô và bút toán kho.',
    actions: [
      FilledButton.icon(
        onPressed: create,
        icon: const Icon(Icons.add),
        label: const Text('Nhập kho'),
      ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Lịch sử phiếu nhập',
        child: DataTableShell(
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Mã phiếu')),
            DataColumn(label: Text('Người nhập')),
            DataColumn(label: Text('Ngày nhận')),
            DataColumn(label: Text('Ngày tạo')),
          ],
          rows: items
              .map(
                (item) => DataRow(
                  cells: [
                    DataCell(Text(valueText(item['code']))),
                    DataCell(Text(valueText(item['actor_user_id']))),
                    DataCell(Text(formatDateTime(item['occurred_at']))),
                    DataCell(Text(formatDateTime(item['created_at']))),
                  ],
                ),
              )
              .toList(),
          mobileCards: items
              .map(
                (item) => Card(
                  child: ListTile(
                    leading: const Icon(Icons.move_to_inbox_outlined),
                    title: Text(valueText(item['code'])),
                    subtitle: Text(formatDateTime(item['occurred_at'])),
                  ),
                ),
              )
              .toList(),
        ),
      ),
    ],
  );
}

class _LedgerTab extends StatefulWidget {
  const _LedgerTab({required this.session});
  final SessionController session;

  @override
  State<_LedgerTab> createState() => _LedgerTabState();
}

class _LedgerTabState extends State<_LedgerTab> {
  final medicineId = TextEditingController();
  final batchId = TextEditingController();
  List<Map<String, dynamic>> movements = const [];
  List<Map<String, dynamic>> adjustments = const [];
  bool loading = true;
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
    medicineId.dispose();
    batchId.dispose();
    super.dispose();
  }

  Future<void> load() async {
    setState(() => loading = true);
    try {
      final requests = <Future<Map<String, dynamic>>>[
        if (can('inventory.movement.view'))
          widget.session.requestJson(
            'GET',
            '/api/v1/inventory/movements',
            query: {
              if (medicineId.text.trim().isNotEmpty)
                'medicine_id': medicineId.text.trim(),
              if (batchId.text.trim().isNotEmpty)
                'batch_id': batchId.text.trim(),
            },
          ),
        if (can('inventory.adjust'))
          widget.session.requestJson('GET', '/api/v1/inventory/adjustments'),
      ];
      final results = await Future.wait(requests);
      var index = 0;
      if (can('inventory.movement.view')) {
        movements = apiItems(results[index++]);
      }
      if (can('inventory.adjust')) adjustments = apiItems(results[index]);
      error = null;
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> createAdjustment() async {
    final requestKey = idempotencyKey('stock-adjustment');
    final response = await showApiFormDialog(
      context: context,
      builder: (_) => _AdjustmentDialog(session: widget.session),
      submit: (values) => widget.session.requestJson(
        'POST',
        '/api/v1/inventory/adjustments',
        headers: {'Idempotency-Key': requestKey},
        body: values,
      ),
    );
    if (response != null && mounted) {
      showSuccessMessage(context, 'Đã ghi nhận kho.');
      await load();
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Điều chỉnh & sổ kho',
    description: 'Theo dõi lịch sử nhập, xuất và điều chỉnh theo từng lô.',
    actions: [
      if (can('inventory.adjust'))
        FilledButton.icon(
          onPressed: createAdjustment,
          icon: const Icon(Icons.tune),
          label: const Text('Điều chỉnh'),
        ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      if (can('inventory.movement.view'))
        SectionCard(
          title: 'Bộ lọc sổ kho',
          child: Wrap(
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            children: [
              SizedBox(
                width: 180,
                child: TextField(
                  controller: medicineId,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(labelText: 'Medicine ID'),
                ),
              ),
              SizedBox(
                width: 180,
                child: TextField(
                  controller: batchId,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(labelText: 'Batch ID'),
                ),
              ),
              FilledButton.tonalIcon(
                onPressed: load,
                icon: const Icon(Icons.filter_alt_outlined),
                label: const Text('Áp dụng'),
              ),
            ],
          ),
        ),
      if (can('inventory.adjust'))
        SectionCard(
          title: 'Phiếu điều chỉnh',
          child: DataTableShell(
            loading: loading,
            error: error,
            onRetry: load,
            columns: const [
              DataColumn(label: Text('Mã')),
              DataColumn(label: Text('Người thực hiện')),
              DataColumn(label: Text('Thời gian')),
            ],
            rows: adjustments
                .map(
                  (item) => DataRow(
                    cells: [
                      DataCell(Text(valueText(item['code']))),
                      DataCell(Text(valueText(item['actor_user_id']))),
                      DataCell(Text(formatDateTime(item['occurred_at']))),
                    ],
                  ),
                )
                .toList(),
          ),
        ),
      if (can('inventory.movement.view'))
        SectionCard(
          title: 'Bút toán kho (100 gần nhất)',
          child: DataTableShell(
            resultLimit: 100,
            loading: loading,
            error: error,
            onRetry: load,
            columns: const [
              DataColumn(label: Text('Thời gian')),
              DataColumn(label: Text('Thuốc / Lô')),
              DataColumn(label: Text('Loại')),
              DataColumn(label: Text('Thay đổi')),
              DataColumn(label: Text('Số dư')),
              DataColumn(label: Text('Tham chiếu')),
            ],
            rows: movements
                .map(
                  (item) => DataRow(
                    cells: [
                      DataCell(Text(formatDateTime(item['occurred_at']))),
                      DataCell(
                        Text(
                          '#${item['medicine_id']} / #${item['medicine_batch_id']}',
                        ),
                      ),
                      DataCell(Text(enumLabel(item['movement_type']))),
                      DataCell(Text(valueText(item['quantity_delta']))),
                      DataCell(
                        Text(
                          '${valueText(item['quantity_before'])} → ${valueText(item['quantity_after'])}',
                        ),
                      ),
                      DataCell(
                        Text(
                          '${enumLabel(item['reference_type'])} #${valueText(item['reference_id'])}',
                        ),
                      ),
                    ],
                  ),
                )
                .toList(),
          ),
        ),
    ],
  );
}

class _MedicineEditor extends StatefulWidget {
  const _MedicineEditor({this.medicine});
  final Map<String, dynamic>? medicine;

  @override
  State<_MedicineEditor> createState() => _MedicineEditorState();
}

class _MedicineEditorState extends State<_MedicineEditor> {
  final key = GlobalKey<FormState>();
  late final Map<String, TextEditingController> fields;
  String status = 'INACTIVE';
  bool get editing => widget.medicine != null;

  @override
  void initState() {
    super.initState();
    final m = widget.medicine ?? const <String, dynamic>{};
    fields = {
      'code': TextEditingController(text: apiString(m['code'])),
      'name': TextEditingController(text: apiString(m['name'])),
      'active_ingredient': TextEditingController(
        text: apiString(m['active_ingredient']),
      ),
      'dosage_form': TextEditingController(text: apiString(m['dosage_form'])),
      'strength': TextEditingController(text: apiString(m['strength'])),
      'dispense_unit': TextEditingController(
        text: apiString(m['dispense_unit']),
      ),
      'description': TextEditingController(text: apiString(m['description'])),
      'low_stock_threshold': TextEditingController(
        text: valueText(m['low_stock_threshold']) == '—'
            ? '0'
            : valueText(m['low_stock_threshold']),
      ),
      'price': TextEditingController(),
    };
    status = apiString(m['status']).isEmpty
        ? 'INACTIVE'
        : apiString(m['status']);
  }

  @override
  void dispose() {
    for (final controller in fields.values) {
      controller.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text(editing ? 'Cập nhật thuốc' : 'Thêm thuốc'),
    content: SizedBox(
      width: 650,
      child: Form(
        key: key,
        child: SingleChildScrollView(
          child: Column(
            children: [
              const ApiFormNotice(),
              for (final spec in const [
                ('code', 'Mã (MED-...) *'),
                ('name', 'Tên thuốc *'),
                ('active_ingredient', 'Hoạt chất *'),
                ('dosage_form', 'Dạng bào chế *'),
                ('strength', 'Hàm lượng *'),
                ('dispense_unit', 'Đơn vị cấp phát *'),
              ]) ...[
                TextFormField(
                  controller: fields[spec.$1],
                  enabled: spec.$1 != 'code' || !editing,
                  decoration: InputDecoration(
                    labelText: spec.$2,
                    errorText: apiFieldError(context, spec.$1),
                  ),
                  validator: (value) => value == null || value.trim().isEmpty
                      ? 'Bắt buộc.'
                      : null,
                ),
                const SizedBox(height: AppSpacing.md),
              ],
              TextField(
                controller: fields['description'],
                maxLines: 2,
                decoration: const InputDecoration(labelText: 'Mô tả'),
              ),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: fields['low_stock_threshold'],
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: const InputDecoration(labelText: 'Ngưỡng tồn thấp'),
                validator: (value) => double.tryParse(value ?? '') == null
                    ? 'Nhập số hợp lệ.'
                    : null,
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
                  controller: fields['price'],
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: 'Giá bán ban đầu (VND)',
                  ),
                  validator: (value) =>
                      status == 'ACTIVE' && int.tryParse(value ?? '') == null
                      ? 'Thuốc ACTIVE cần giá.'
                      : null,
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
    final body = <String, dynamic>{
      if (editing) 'expected_version': apiInt(widget.medicine?['version']),
      if (!editing) 'code': fields['code']!.text.trim().toUpperCase(),
      'name': fields['name']!.text.trim(),
      'active_ingredient': fields['active_ingredient']!.text.trim(),
      'dosage_form': fields['dosage_form']!.text.trim(),
      'strength': fields['strength']!.text.trim(),
      'dispense_unit': fields['dispense_unit']!.text.trim(),
      'description': fields['description']!.text.trim(),
      'low_stock_threshold': double.parse(
        fields['low_stock_threshold']!.text.trim(),
      ),
    };
    if (!editing) {
      final initial = int.tryParse(fields['price']!.text.trim());
      body['status'] = status;
      if (initial != null) {
        body['initial_unit_sale_price_vnd'] = initial;
      }
    }
    submitApiForm(context, body);
  }
}

class _MedicinePricesDialog extends StatefulWidget {
  const _MedicinePricesDialog({required this.session, required this.medicine});
  final SessionController session;
  final Map<String, dynamic> medicine;

  @override
  State<_MedicinePricesDialog> createState() => _MedicinePricesDialogState();
}

class _MedicinePricesDialogState extends State<_MedicinePricesDialog> {
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
          '/api/v1/medicines/${widget.medicine['id']}/price-versions',
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
      builder: (_) => const _MedicinePriceDialog(),
      submit: (values) => widget.session.requestJson(
        'POST',
        '/api/v1/medicines/${widget.medicine['id']}/price-versions',
        body: values,
      ),
    );
    if (response != null && mounted) await load();
  }

  Future<void> cancel(Map<String, dynamic> item) async {
    final reason = await showReasonDialog(
      context,
      title: 'Hủy giá thuốc',
      message: 'Chỉ giá SCHEDULED có thể hủy.',
      confirmLabel: 'Hủy giá',
      destructive: true,
    );
    if (reason == null || !mounted) return;
    try {
      await widget.session.requestJson(
        'POST',
        '/api/v1/medicines/${widget.medicine['id']}/price-versions/${item['id']}/cancel',
        body: {'reason': reason},
      );
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('Giá · ${valueText(widget.medicine['name'])}'),
    content: SizedBox(
      width: 700,
      child: DataTableShell(
        loading: loading,
        error: error,
        onRetry: load,
        columns: const [
          DataColumn(label: Text('Hiệu lực')),
          DataColumn(label: Text('Giá bán')),
          DataColumn(label: Text('Giai đoạn')),
          DataColumn(label: Text('')),
        ],
        rows: items
            .map(
              (item) => DataRow(
                cells: [
                  DataCell(Text(formatDateTime(item['effective_from']))),
                  DataCell(Text(formatVnd(item['unit_sale_price_vnd']))),
                  DataCell(StatusBadge(status: apiString(item['phase']))),
                  DataCell(
                    item['phase'] == 'SCHEDULED'
                        ? IconButton(
                            onPressed: () => cancel(item),
                            tooltip: 'Hủy giá',
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
    actions: [
      TextButton(
        onPressed: apiFormBusy(context) ? null : () => Navigator.pop(context),
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

class _MedicinePriceDialog extends StatefulWidget {
  const _MedicinePriceDialog();
  @override
  State<_MedicinePriceDialog> createState() => _MedicinePriceDialogState();
}

class _MedicinePriceDialogState extends State<_MedicinePriceDialog> {
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
    title: const Text('Thêm giá thuốc'),
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
                errorText: apiFieldError(context, 'unit_sale_price_vnd'),
                labelText: 'Giá bán (VND) *',
              ),
              validator: (value) =>
                  int.tryParse(value ?? '') == null ? 'Không hợp lệ.' : null,
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
                  'unit_sale_price_vnd': int.parse(amount.text.trim()),
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

class _ReceiptDialog extends StatefulWidget {
  const _ReceiptDialog({required this.session});
  final SessionController session;
  @override
  State<_ReceiptDialog> createState() => _ReceiptDialogState();
}

class _ReceiptDialogState extends State<_ReceiptDialog> {
  final key = GlobalKey<FormState>();
  final supplier = TextEditingController();
  final reference = TextEditingController();
  final note = TextEditingController();
  final medicineId = TextEditingController();
  final lot = TextEditingController();
  final manufactureDate = TextEditingController();
  final expiresOn = TextEditingController();
  final quantity = TextEditingController();
  final cost = TextEditingController();

  @override
  void dispose() {
    for (final c in [
      supplier,
      reference,
      note,
      medicineId,
      lot,
      manufactureDate,
      expiresOn,
      quantity,
      cost,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Nhập một lô thuốc'),
    content: SizedBox(
      width: 620,
      child: Form(
        key: key,
        child: SingleChildScrollView(
          child: Column(
            children: [
              const ApiFormNotice(),
              const InlineMessage(
                message:
                    'Mỗi lần có thể nhập một lô trong giao diện. API hỗ trợ tối đa 100 dòng/phiếu.',
                tone: AppStatusTone.info,
              ),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: supplier,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'supplier_name'),
                  labelText: 'Nhà cung cấp',
                ),
              ),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: reference,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'supplier_document_ref'),
                  labelText: 'Số chứng từ nhà cung cấp',
                ),
              ),
              const SizedBox(height: AppSpacing.md),
              EntityPicker(
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
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: lot,
                decoration: const InputDecoration(labelText: 'Số lô *'),
                validator: _requiredText,
              ),
              const SizedBox(height: AppSpacing.md),
              DateField(controller: manufactureDate, label: 'Ngày sản xuất'),
              const SizedBox(height: AppSpacing.md),
              DateField(controller: expiresOn, label: 'Hạn dùng *'),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: quantity,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'quantity'),
                  labelText: 'Số lượng *',
                ),
                validator: _positiveNumber,
              ),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: cost,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(
                  labelText: 'Đơn giá nhập (VND) *',
                ),
                validator: (value) =>
                    int.tryParse(value ?? '') == null ? 'Không hợp lệ.' : null,
              ),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: note,
                maxLines: 2,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'note'),
                  labelText: 'Ghi chú',
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
                if (!(key.currentState?.validate() ?? false)) return;
                if (int.tryParse(medicineId.text) == null ||
                    DateTime.tryParse(expiresOn.text) == null) {
                  showActionError(context, 'Chọn thuốc và hạn dùng.');
                  return;
                }
                submitApiForm(context, {
                  'supplier_name': supplier.text.trim(),
                  'supplier_document_ref': reference.text.trim(),
                  'received_at': DateTime.now()
                      .toUtc()
                      .subtract(const Duration(seconds: 1))
                      .toIso8601String(),
                  'note': note.text.trim(),
                  'items': [
                    {
                      'medicine_id': int.parse(medicineId.text.trim()),
                      'lot_number': lot.text.trim(),
                      if (manufactureDate.text.trim().isNotEmpty)
                        'manufacture_date': manufactureDate.text.trim(),
                      'expires_on': expiresOn.text.trim(),
                      'received_quantity': double.parse(quantity.text.trim()),
                      'unit_purchase_cost_vnd': int.parse(cost.text.trim()),
                    },
                  ],
                });
              },
        child: const Text('Nhập kho'),
      ),
    ],
  );
}

class _AdjustmentDialog extends StatefulWidget {
  const _AdjustmentDialog({required this.session});
  final SessionController session;
  @override
  State<_AdjustmentDialog> createState() => _AdjustmentDialogState();
}

class _AdjustmentDialogState extends State<_AdjustmentDialog> {
  final key = GlobalKey<FormState>();
  final detail = TextEditingController();
  final batchId = TextEditingController();
  final quantity = TextEditingController();
  int? medicineId;
  String reason = 'COUNT_CORRECTION';
  String direction = 'OUT';

  @override
  void dispose() {
    detail.dispose();
    batchId.dispose();
    quantity.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Điều chỉnh một lô'),
    content: SizedBox(
      width: 520,
      child: Form(
        key: key,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const ApiFormNotice(),
              DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: reason,
                decoration: const InputDecoration(labelText: 'Lý do *'),
                items: const [
                  DropdownMenuItem(
                    value: 'COUNT_CORRECTION',
                    child: Text('Hiệu chỉnh kiểm kê'),
                  ),
                  DropdownMenuItem(value: 'DAMAGE', child: Text('Hư hỏng')),
                  DropdownMenuItem(
                    value: 'EXPIRED_DISPOSAL',
                    child: Text('Hủy thuốc hết hạn'),
                  ),
                  DropdownMenuItem(value: 'OTHER', child: Text('Khác')),
                ],
                onChanged: (value) => setState(() => reason = value ?? 'OTHER'),
              ),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: detail,
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'reason_detail'),
                  labelText: 'Mô tả lý do *',
                ),
                validator: _requiredText,
              ),
              const SizedBox(height: AppSpacing.md),
              EntityPicker(
                label: 'Thuốc *',
                search: (query) => searchEntities(
                  widget.session,
                  '/api/v1/medicines',
                  query,
                  activeOnly: false,
                ),
                onSelected: (option) => setState(() {
                  medicineId = option?.id;
                  batchId.clear();
                }),
              ),
              const SizedBox(height: AppSpacing.md),
              EntityPicker(
                key: ValueKey(medicineId),
                label: 'Lô thuốc *',
                search: (query) async {
                  if (medicineId == null) return const <EntityOption>[];
                  final items = apiItems(
                    await widget.session.requestJson(
                      'GET',
                      '/api/v1/inventory/batches',
                      query: {
                        'medicine_id': '$medicineId',
                        'q': query,
                        'page_size': '30',
                      },
                    ),
                  );
                  return items
                      .where(
                        (item) => apiString(
                          item['lot_number'],
                        ).toLowerCase().contains(query.toLowerCase()),
                      )
                      .map(
                        (item) => EntityOption(
                          id: apiInt(item['id']),
                          label: apiString(item['lot_number']),
                          subtitle:
                              'Hạn ${formatDate(item['expires_on'])} · Tồn ${valueText(item['quantity_on_hand'])}',
                        ),
                      )
                      .toList();
                },
                onSelected: (option) =>
                    batchId.text = option?.id.toString() ?? '',
                validator: (_) =>
                    medicineId == null || int.tryParse(batchId.text) == null
                    ? 'Chọn thuốc và lô thuốc.'
                    : null,
              ),
              const SizedBox(height: AppSpacing.md),
              DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: direction,
                decoration: const InputDecoration(
                  labelText: 'Chiều điều chỉnh *',
                ),
                items: const [
                  DropdownMenuItem(value: 'IN', child: Text('Tăng tồn')),
                  DropdownMenuItem(value: 'OUT', child: Text('Giảm tồn')),
                ],
                onChanged: (value) =>
                    setState(() => direction = value ?? 'OUT'),
              ),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: quantity,
                keyboardType: const TextInputType.numberWithOptions(
                  decimal: true,
                ),
                decoration: InputDecoration(
                  errorText: apiFieldError(context, 'quantity'),
                  labelText: 'Số lượng *',
                ),
                validator: _positiveNumber,
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
                if (!(key.currentState?.validate() ?? false)) return;
                submitApiForm(context, {
                  'reason_code': reason,
                  'reason_detail': detail.text.trim(),
                  'items': [
                    {
                      'medicine_batch_id': int.parse(batchId.text.trim()),
                      'direction': direction,
                      'quantity': double.parse(quantity.text.trim()),
                    },
                  ],
                });
              },
        child: const Text('Xác nhận'),
      ),
    ],
  );
}

class _Metric extends StatelessWidget {
  const _Metric({
    required this.label,
    required this.value,
    this.danger = false,
  });
  final String label;
  final String value;
  final bool danger;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: 210,
    child: Card(
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(label, style: Theme.of(context).textTheme.labelLarge),
            const SizedBox(height: AppSpacing.xs),
            Text(
              value,
              style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                color: danger ? AppColors.danger : AppColors.brand,
              ),
            ),
          ],
        ),
      ),
    ),
  );
}

class _Select extends StatelessWidget {
  const _Select({
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

class _NoAccess extends StatelessWidget {
  const _NoAccess();
  @override
  Widget build(BuildContext context) => const EmptyState(
    title: 'Không có quyền truy cập',
    message: 'Tài khoản hiện tại không được cấp quyền cho phân hệ này.',
    icon: Icons.lock_outline,
  );
}

String? _requiredText(String? value) =>
    value == null || value.trim().isEmpty ? 'Bắt buộc.' : null;

String? _positiveNumber(String? value) {
  final parsed = double.tryParse(value ?? '');
  return parsed == null || parsed <= 0 ? 'Nhập số lớn hơn 0.' : null;
}

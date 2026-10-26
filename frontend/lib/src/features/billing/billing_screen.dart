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
import 'payment_qr_panel.dart';
import 'pending_billing_section.dart';
import '../../design_system/components/date_fields.dart';

class BillingScreen extends StatefulWidget {
  const BillingScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<BillingScreen> createState() => _BillingScreenState();
}

class _BillingScreenState extends State<BillingScreen> {
  final search = TextEditingController();
  final patientId = TextEditingController();
  final encounterId = TextEditingController();
  final from = TextEditingController(text: clinicDate(clinicNow()));
  final to = TextEditingController(text: clinicDate(clinicNow()));
  String status = '';
  String paymentMethod = '';
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
    search.dispose();
    patientId.dispose();
    encounterId.dispose();
    from.dispose();
    to.dispose();
    super.dispose();
  }

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (from.text.isNotEmpty) 'date_from': from.text,
      if (to.text.isNotEmpty) 'date_to': to.text,
      if (search.text.trim().isNotEmpty) 'q': search.text.trim(),
      if (status.isNotEmpty) 'status': status,
      if (paymentMethod.isNotEmpty) 'payment_method': paymentMethod,
      if (!own && patientId.text.trim().isNotEmpty)
        'patient_id': patientId.text.trim(),
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
        own ? '/api/v1/me/invoices' : '/api/v1/invoices',
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

  Future<void> createInvoice(Map<String, dynamic> pending) async {
    final result = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => _CreateInvoiceDialog(pending: pending),
    );
    if (result == null || !mounted) return;
    await _mutate(() async {
      final encounter = result['encounter_id'];
      final created = await widget.session.requestJson(
        'POST',
        '/api/v1/encounters/$encounter/invoices',
        headers: {'Idempotency-Key': idempotencyKey('invoice')},
        body: {'payment_method': result['payment_method']},
      );
      if (mounted) {
        showSuccessMessage(context, 'Đã lập hóa đơn.');
        await _openDetail(created);
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

  Future<void> _openDetail(Map<String, dynamic> invoice) async {
    final id = apiInt(invoice['id']);
    if (id == 0) return;
    await showDialog<void>(
      context: context,
      builder: (_) => _InvoiceDetailDialog(
        session: widget.session,
        invoiceId: id,
        own: own,
      ),
    );
    await load();
  }

  void clearFilters() {
    search.clear();
    patientId.clear();
    encounterId.clear();
    setState(() {
      status = '';
      paymentMethod = '';
    });
    load();
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: own ? 'Hóa đơn của tôi' : 'Hóa đơn & thanh toán',
    description: own
        ? 'Xem chi tiết chi phí và thông tin thanh toán.'
        : 'Lập hóa đơn cho ca đã khám, theo dõi và xác nhận thanh toán.',
    children: [
      if (!own && can('billing.pending_encounter.view'))
        PendingBillingSection(
          session: widget.session,
          onCreate: createInvoice,
          onOpen: _openDetail,
        ),
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Bộ lọc',
        child: FilterBar(
          searchController: search,
          onSearch: (_) => load(),
          hintText: 'Mã hóa đơn hoặc tên bệnh nhân',
          onClear: clearFilters,
          filters: [
            SizedBox(
              width: 160,
              child: DateField(controller: from, label: 'Từ ngày'),
            ),
            SizedBox(
              width: 160,
              child: DateField(controller: to, label: 'Đến ngày'),
            ),
            _FilterDropdown(
              label: 'Trạng thái',
              value: status,
              values: const ['', 'UNPAID', 'PAID'],
              onChanged: (value) {
                setState(() => status = value ?? '');
                load();
              },
            ),
            _FilterDropdown(
              label: 'Phương thức',
              value: paymentMethod,
              values: const ['', 'CASH', 'BANK_TRANSFER'],
              onChanged: (value) {
                setState(() => paymentMethod = value ?? '');
                load();
              },
            ),
            if (!own)
              SizedBox(
                width: 145,
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
            if (!own &&
                hasAnyPermission(widget.session, [
                  'encounter.view.all',
                  'encounter.view.assigned_or_history',
                ]))
              SizedBox(
                width: 155,
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
          ],
        ),
      ),
      SectionCard(
        title: 'Danh sách hóa đơn',
        child: DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Mã hóa đơn')),
            DataColumn(label: Text('Bệnh nhân')),
            DataColumn(label: Text('Ngày lập')),
            DataColumn(label: Text('Tổng tiền')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('')),
          ],
          rows: items.map(_row).toList(),
          mobileCards: items.map(_card).toList(),
          emptyTitle: 'Chưa có hóa đơn',
          emptyMessage: 'Không có hóa đơn phù hợp với bộ lọc hiện tại.',
        ),
      ),
    ],
  );

  DataRow _row(Map<String, dynamic> item) => DataRow(
    onSelectChanged: (_) => _openDetail(item),
    cells: [
      DataCell(Text(valueText(item['invoice_code']))),
      DataCell(Text(valueText(item['patient_name_at_invoice']))),
      DataCell(Text(formatDateTime(item['created_at']))),
      DataCell(Text(formatVnd(item['total_vnd']))),
      DataCell(StatusBadge(status: apiString(item['status']))),
      DataCell(
        IconButton(
          tooltip: 'Xem hóa đơn',
          onPressed: () => _openDetail(item),
          icon: const Icon(Icons.open_in_new),
        ),
      ),
    ],
  );

  Widget _card(Map<String, dynamic> item) => Card(
    child: ListTile(
      onTap: () => _openDetail(item),
      leading: const Icon(Icons.receipt_long_outlined),
      title: Text(valueText(item['invoice_code'])),
      subtitle: Text(
        '${valueText(item['patient_name_at_invoice'])}\n${formatVnd(item['total_vnd'])}',
      ),
      isThreeLine: true,
      trailing: StatusBadge(status: apiString(item['status'])),
    ),
  );
}

class _InvoiceDetailDialog extends StatefulWidget {
  const _InvoiceDetailDialog({
    required this.session,
    required this.invoiceId,
    required this.own,
  });

  final SessionController session;
  final int invoiceId;
  final bool own;

  @override
  State<_InvoiceDetailDialog> createState() => _InvoiceDetailDialogState();
}

class _InvoiceDetailDialogState extends State<_InvoiceDetailDialog> {
  Map<String, dynamic>? invoice;
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

  Future<void> load() async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      invoice = await widget.session.requestJson(
        'GET',
        widget.own
            ? '/api/v1/me/invoices/${widget.invoiceId}'
            : '/api/v1/invoices/${widget.invoiceId}',
      );
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> showQr() async {
    try {
      final data = await widget.session.requestJson(
        'GET',
        widget.own
            ? '/api/v1/me/invoices/${widget.invoiceId}/payment-qr'
            : '/api/v1/invoices/${widget.invoiceId}/payment-qr',
      );
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (_) => AlertDialog(
          title: const Text('Thông tin chuyển khoản'),
          content: SizedBox(
            width: 560,
            height: MediaQuery.sizeOf(context).height * 0.65,
            child: SingleChildScrollView(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  PaymentQrPanel(payload: valueText(data['qr_payload'])),
                  const SizedBox(height: AppSpacing.md),
                  Wrap(
                    spacing: AppSpacing.xl,
                    children: [
                      DetailField(
                        label: 'Ngân hàng (BIN)',
                        value: valueText(data['bank_bin']),
                      ),
                      DetailField(
                        label: 'Số tài khoản',
                        value: valueText(data['account_number']),
                      ),
                      DetailField(
                        label: 'Tên tài khoản',
                        value: valueText(data['account_name']),
                      ),
                      DetailField(
                        label: 'Số tiền',
                        value: formatVnd(data['expected_amount_vnd']),
                      ),
                    ],
                  ),
                  const Text('Nội dung chuyển khoản'),
                  SelectableText(valueText(data['transfer_content'])),
                  const SizedBox(height: AppSpacing.md),
                  const Text('Payload QR'),
                  const SizedBox(height: AppSpacing.xxs),
                  Container(
                    width: double.infinity,
                    padding: const EdgeInsets.all(AppSpacing.md),
                    color: AppColors.surfaceSubtle,
                    child: SelectableText(valueText(data['qr_payload'])),
                  ),
                  const SizedBox(height: AppSpacing.md),
                  const InlineMessage(
                    message:
                        'Mã QR được tạo từ payload cố định tại thời điểm lập hóa đơn. Sau khi khách chuyển khoản, đối chiếu sao kê rồi mới bấm "Xác nhận thanh toán".',
                    tone: AppStatusTone.info,
                  ),
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
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    }
  }

  static bool _isBankTransfer(Map<String, dynamic>? invoice) {
    final payment = invoice?['payment'];
    return payment is Map && payment['payment_method'] == 'BANK_TRANSFER';
  }

  Future<void> confirmPayment() async {
    final data = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => ConfirmPaymentDialog(
        expectedAmount: apiInt(invoice?['total_vnd']),
        bankTransfer: _isBankTransfer(invoice),
      ),
    );
    if (data == null || !mounted) return;
    setState(() {
      mutating = true;
      error = null;
    });
    try {
      await widget.session.requestJson(
        'POST',
        '/api/v1/invoices/${widget.invoiceId}/payments/confirm',
        headers: {'Idempotency-Key': idempotencyKey('payment-confirm')},
        body: data,
      );
      await load();
      if (mounted) showSuccessMessage(context, 'Đã xác nhận thanh toán.');
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => mutating = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final data = invoice ?? const <String, dynamic>{};
    final payment = apiMap(data['payment']);
    final lineItems = data['items'] is List
        ? (data['items'] as List)
              .whereType<Map>()
              .map((item) => Map<String, dynamic>.from(item))
              .toList()
        : const <Map<String, dynamic>>[];
    final bankTransfer = payment['payment_method'] == 'BANK_TRANSFER';
    final unpaid = data['status'] == 'UNPAID';
    return AlertDialog(
      title: Text('Hóa đơn ${valueText(data['invoice_code'])}'),
      content: SizedBox(
        width: 900,
        child: loading
            ? const Center(child: CircularProgressIndicator())
            : SingleChildScrollView(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    if (error != null) ...[
                      InlineMessage(
                        message: error!,
                        tone: AppStatusTone.danger,
                      ),
                      const SizedBox(height: AppSpacing.md),
                    ],
                    Wrap(
                      spacing: AppSpacing.xl,
                      children: [
                        DetailField(
                          label: 'Bệnh nhân',
                          value: valueText(data['patient_name_at_invoice']),
                        ),
                        DetailField(
                          label: 'Mã bệnh nhân',
                          value: valueText(data['patient_code_at_invoice']),
                        ),
                        DetailField(
                          label: 'Ngày lập',
                          value: formatDateTime(data['created_at']),
                        ),
                        DetailField(
                          label: 'Trạng thái',
                          value: apiString(data['status']),
                          status: true,
                        ),
                      ],
                    ),
                    const Divider(),
                    Text(
                      'Chi tiết',
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: AppSpacing.sm),
                    if (lineItems.isEmpty)
                      const Text('Không có dòng chi phí.')
                    else
                      SingleChildScrollView(
                        scrollDirection: Axis.horizontal,
                        child: DataTable(
                          columns: const [
                            DataColumn(label: Text('Loại')),
                            DataColumn(label: Text('Mã')),
                            DataColumn(label: Text('Tên')),
                            DataColumn(label: Text('SL')),
                            DataColumn(label: Text('Đơn giá')),
                            DataColumn(label: Text('Thành tiền')),
                          ],
                          rows: lineItems
                              .map(
                                (item) => DataRow(
                                  cells: [
                                    DataCell(
                                      Text(enumLabel(item['item_type'])),
                                    ),
                                    DataCell(
                                      Text(
                                        valueText(item['item_code_at_invoice']),
                                      ),
                                    ),
                                    DataCell(
                                      Text(
                                        valueText(item['item_name_at_invoice']),
                                      ),
                                    ),
                                    DataCell(Text(valueText(item['quantity']))),
                                    DataCell(
                                      Text(
                                        formatVnd(
                                          item['unit_price_snapshot_vnd'],
                                        ),
                                      ),
                                    ),
                                    DataCell(
                                      Text(formatVnd(item['line_total_vnd'])),
                                    ),
                                  ],
                                ),
                              )
                              .toList(),
                        ),
                      ),
                    const Divider(),
                    Align(
                      alignment: Alignment.centerRight,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.end,
                        children: [
                          Text('Tạm tính: ${formatVnd(data['subtotal_vnd'])}'),
                          const SizedBox(height: AppSpacing.xs),
                          Text(
                            'Tổng cộng: ${formatVnd(data['total_vnd'])}',
                            style: Theme.of(context).textTheme.titleLarge,
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: AppSpacing.md),
                    Wrap(
                      spacing: AppSpacing.xl,
                      children: [
                        DetailField(
                          label: 'Phương thức',
                          value: enumLabel(payment['payment_method']),
                        ),
                        DetailField(
                          label: 'Mã thanh toán',
                          value: valueText(payment['payment_code']),
                        ),
                        DetailField(
                          label: 'Trạng thái thanh toán',
                          value: valueText(payment['status']),
                          status: true,
                        ),
                        if (payment['confirmed_at'] != null)
                          DetailField(
                            label: 'Xác nhận lúc',
                            value: formatDateTime(payment['confirmed_at']),
                          ),
                      ],
                    ),
                  ],
                ),
              ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.pop(context),
          child: const Text('Đóng'),
        ),
        if (bankTransfer &&
            (widget.own
                ? can('payment.qr.view.patient_own')
                : can('payment.qr.generate')))
          OutlinedButton.icon(
            onPressed: showQr,
            icon: const Icon(Icons.qr_code_2),
            label: const Text('Thông tin QR'),
          ),
        if (!widget.own && unpaid && can('payment.confirm'))
          FilledButton.icon(
            onPressed: mutating ? null : confirmPayment,
            icon: const Icon(Icons.verified_outlined),
            label: const Text('Xác nhận thanh toán'),
          ),
      ],
    );
  }
}

class _CreateInvoiceDialog extends StatefulWidget {
  const _CreateInvoiceDialog({required this.pending});
  final Map<String, dynamic> pending;

  @override
  State<_CreateInvoiceDialog> createState() => _CreateInvoiceDialogState();
}

class _CreateInvoiceDialogState extends State<_CreateInvoiceDialog> {
  final formKey = GlobalKey<FormState>();
  String method = 'CASH';

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Lập hóa đơn'),
    content: Form(
      key: formKey,
      child: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              '${valueText(widget.pending['patient_name'])} · ${valueText(widget.pending['encounter_code'])}\nTổng dự kiến: ${formatVnd(widget.pending['expected_total_vnd'])}',
            ),
            const SizedBox(height: AppSpacing.md),
            DropdownButtonFormField<String>(
              isExpanded: true,
              initialValue: method,
              decoration: const InputDecoration(labelText: 'Phương thức *'),
              items: const [
                DropdownMenuItem(value: 'CASH', child: Text('Tiền mặt')),
                DropdownMenuItem(
                  value: 'BANK_TRANSFER',
                  child: Text('Chuyển khoản'),
                ),
              ],
              onChanged: (value) => setState(() => method = value ?? 'CASH'),
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
            'encounter_id': widget.pending['encounter_id'],
            'payment_method': method,
          });
        },
        child: const Text('Lập hóa đơn'),
      ),
    ],
  );
}

/// Confirms an invoice payment.
///
/// Per `Docs/features/billing-payment.md` (§ confirm body, lines 243/634):
/// BANK_TRANSFER requires `bank_transaction_reference` (1..100 characters after
/// trim); CASH does not accept it, so the field is hidden and never sent.
class ConfirmPaymentDialog extends StatefulWidget {
  const ConfirmPaymentDialog({
    super.key,
    required this.expectedAmount,
    required this.bankTransfer,
  });

  final int expectedAmount;
  final bool bankTransfer;

  static const bankReferenceKey = ValueKey('bank-transaction-reference');
  static const submitKey = ValueKey('confirm-payment-submit');

  /// Mirrors the backend rule (NFKC + collapsed whitespace, 1..100 runes).
  static String? validateBankReference(String? value) {
    final normalized = (value ?? '').trim().split(RegExp(r'\s+')).join(' ');
    if (normalized.isEmpty) {
      return 'Nhập mã giao dịch ngân hàng (xem trong sao kê hoặc app ngân hàng).';
    }
    if (normalized.runes.length > 100) {
      return 'Mã giao dịch ngân hàng tối đa 100 ký tự.';
    }
    return null;
  }

  @override
  State<ConfirmPaymentDialog> createState() => _ConfirmPaymentDialogState();
}

class _ConfirmPaymentDialogState extends State<ConfirmPaymentDialog> {
  final formKey = GlobalKey<FormState>();
  late final TextEditingController amount;
  final externalReference = TextEditingController();
  final bankReference = TextEditingController();

  @override
  void initState() {
    super.initState();
    amount = TextEditingController(text: widget.expectedAmount.toString());
  }

  @override
  void dispose() {
    amount.dispose();
    externalReference.dispose();
    bankReference.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Xác nhận thanh toán'),
    content: Form(
      key: formKey,
      child: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const InlineMessage(
                message:
                    'Thao tác này chốt thanh toán và có thể đồng thời trừ tồn kho thuốc. Hãy đối soát số tiền trước khi xác nhận.',
                tone: AppStatusTone.warning,
              ),
              const SizedBox(height: AppSpacing.md),
              TextFormField(
                controller: amount,
                keyboardType: TextInputType.number,
                decoration: const InputDecoration(
                  labelText: 'Số tiền xác nhận (VND) *',
                ),
                validator: (value) {
                  final parsed = int.tryParse(value?.trim() ?? '');
                  if (parsed == null || parsed <= 0) {
                    return 'Nhập số tiền hợp lệ.';
                  }
                  if (parsed != widget.expectedAmount) {
                    return 'Số tiền phải bằng ${formatVnd(widget.expectedAmount)}.';
                  }
                  return null;
                },
              ),
              const SizedBox(height: AppSpacing.md),
              TextField(
                controller: externalReference,
                decoration: const InputDecoration(
                  labelText: 'Mã đối soát ngoài (nếu có)',
                ),
              ),
              const SizedBox(height: AppSpacing.md),
              if (widget.bankTransfer) ...[
                const SizedBox(height: AppSpacing.md),
                TextFormField(
                  key: ConfirmPaymentDialog.bankReferenceKey,
                  controller: bankReference,
                  maxLength: 100,
                  decoration: const InputDecoration(
                    labelText: 'Mã giao dịch ngân hàng *',
                    helperText:
                        'Bắt buộc với chuyển khoản. Lấy từ sao kê hoặc app ngân hàng.',
                  ),
                  validator: ConfirmPaymentDialog.validateBankReference,
                ),
              ],
            ],
          ),
        ),
      ),
    ),
    actions: [
      TextButton(
        onPressed: () => Navigator.pop(context),
        child: const Text('Hủy'),
      ),
      FilledButton(
        key: ConfirmPaymentDialog.submitKey,
        onPressed: () {
          if (!(formKey.currentState?.validate() ?? false)) return;
          Navigator.pop(context, {
            'confirmed_amount_vnd': int.parse(amount.text.trim()),
            'external_reference': externalReference.text.trim(),
            if (widget.bankTransfer)
              'bank_transaction_reference': bankReference.text.trim(),
          });
        },
        child: const Text('Xác nhận'),
      ),
    ],
  );
}

class _FilterDropdown extends StatelessWidget {
  const _FilterDropdown({
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

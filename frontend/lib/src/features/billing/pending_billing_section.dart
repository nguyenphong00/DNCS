import '../shared/list_pagination.dart';
import 'package:flutter/material.dart';
import '../../session_controller.dart';
import '../../design_system/components/data_views.dart';
import '../../design_system/components/layout.dart';
import '../../design_system/formatters/app_formatters.dart';
import '../shared/api_helpers.dart';

class PendingBillingSection extends StatefulWidget {
  const PendingBillingSection({
    super.key,
    required this.session,
    required this.onCreate,
    required this.onOpen,
  });
  final SessionController session;
  final Future<void> Function(Map<String, dynamic>) onCreate;
  final Future<void> Function(Map<String, dynamic>) onOpen;
  @override
  State<PendingBillingSection> createState() => _PendingBillingSectionState();
}

class _PendingBillingSectionState extends State<PendingBillingSection> {
  final search = TextEditingController();
  List<Map<String, dynamic>> items = [];
  final paging = ListPagination(pageSize: 30);
  bool loading = true, busy = false;
  String? error;
  String state = 'all';
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
      'q': search.text.trim(),
      'invoice_state': state,
      'with_page': 'true',
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/billing/pending-encounters',
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

  Future<void> act(Map<String, dynamic> item) async {
    setState(() => busy = true);
    try {
      final invoice = apiMap(item['existing_invoice']);
      if (invoice.isEmpty) {
        await widget.onCreate(item);
      } else {
        await widget.onOpen(invoice);
      }
      await load();
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => SectionCard(
    title: 'Chờ thu tiền',
    description:
        'Tất cả ngày · ca chờ lâu nhất trước · số tiền dự kiến được chốt khi lập hóa đơn',
    child: Column(
      children: [
        FilterBar(
          searchController: search,
          onSearch: (_) {
            load();
          },
          hintText: 'Tên, mã, số điện thoại bệnh nhân hoặc mã bệnh án',
          filters: [
            SizedBox(
              width: 180,
              child: DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: state,
                items: const [
                  DropdownMenuItem(value: 'all', child: Text('Tất cả')),
                  DropdownMenuItem(
                    value: 'none',
                    child: Text('Chưa lập hóa đơn'),
                  ),
                  DropdownMenuItem(
                    value: 'unpaid',
                    child: Text('Đã lập hóa đơn'),
                  ),
                ],
                onChanged: (v) {
                  state = v ?? 'all';
                  load();
                },
              ),
            ),
          ],
        ),
        const SizedBox(height: 16),
        DataTableShell(
          page: paging.page,
          hasMore: paging.hasMore,
          onPrevious: () => load(targetPage: paging.page - 1),
          onNext: () => load(targetPage: paging.page + 1),
          dataRowMaxHeight: 80,
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Bệnh nhân')),
            DataColumn(label: Text('Bệnh án')),
            DataColumn(label: Text('Dịch vụ / Thuốc')),
            DataColumn(label: Text('Tổng dự kiến')),
            DataColumn(label: Text('Thao tác')),
          ],
          rows: items
              .map(
                (item) => DataRow(
                  cells: [
                    DataCell(
                      SizedBox(
                        width: 200,
                        child: Column(
                          mainAxisSize: MainAxisSize.min,
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              valueText(item['patient_name']),
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                            ),
                            Text(valueText(item['patient_code'])),
                            Text(valueText(item['patient_phone'])),
                          ],
                        ),
                      ),
                    ),
                    DataCell(
                      SizedBox(
                        width: 240,
                        child: Text(
                          '${valueText(item['encounter_code'])}\n${valueText(item['doctor_name'])}',
                          maxLines: 3,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ),
                    DataCell(
                      Text(
                        '${formatVnd(item['expected_service_total_vnd'])} / ${formatVnd(item['expected_medicine_total_vnd'])}',
                      ),
                    ),
                    DataCell(
                      Text(
                        apiString(item['billing_error']).isEmpty
                            ? formatVnd(item['expected_total_vnd'])
                            : 'Cần kiểm tra thuốc/giá',
                      ),
                    ),
                    DataCell(
                      FilledButton(
                        onPressed:
                            busy ||
                                apiString(item['billing_error']).isNotEmpty ||
                                (apiMap(item['existing_invoice']).isEmpty &&
                                    !widget.session.permissions.contains(
                                      'invoice.create',
                                    )) ||
                                (apiMap(item['existing_invoice']).isNotEmpty &&
                                    !widget.session.permissions.contains(
                                      'invoice.view.all',
                                    ))
                            ? null
                            : () => act(item),
                        child: Text(
                          apiMap(item['existing_invoice']).isEmpty
                              ? 'Lập hóa đơn'
                              : 'Mở hóa đơn',
                        ),
                      ),
                    ),
                  ],
                ),
              )
              .toList(),
          emptyTitle: 'Không có ca chờ thu tiền',
          emptyMessage: 'Ca đã thanh toán không xuất hiện tại đây.',
        ),
      ],
    ),
  );
}

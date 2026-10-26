import 'dart:convert';

import 'package:flutter/material.dart';

import '../../design_system/components/data_views.dart';
import '../../design_system/components/dialogs.dart';
import '../../design_system/components/feedback.dart';
import '../../design_system/components/layout.dart';
import '../../design_system/formatters/app_formatters.dart';
import '../../design_system/theme/app_theme.dart';
import '../../session_controller.dart';
import '../shared/api_helpers.dart';
import '../shared/list_pagination.dart';
import '../../design_system/components/entity_picker.dart';

class SettingsAuditScreen extends StatefulWidget {
  const SettingsAuditScreen({super.key, required this.session});

  final SessionController session;

  @override
  State<SettingsAuditScreen> createState() => _SettingsAuditScreenState();
}

class _SettingsAuditScreenState extends State<SettingsAuditScreen>
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
            Tab(icon: Icon(Icons.tune_outlined), text: 'Cấu hình'),
            Tab(
              icon: Icon(Icons.account_balance_outlined),
              text: 'Tài khoản nhận tiền',
            ),
            Tab(icon: Icon(Icons.manage_search_outlined), text: 'Nhật ký'),
            Tab(
              icon: Icon(Icons.admin_panel_settings_outlined),
              text: 'Phân quyền',
            ),
          ],
        ),
      ),
      Expanded(
        child: TabBarView(
          controller: tabs,
          children: [
            hasAnyPermission(widget.session, const [
                  'settings.appointment.manage',
                  'settings.inventory_alert.manage',
                ])
                ? _SystemSettingsTab(session: widget.session)
                : const _NoAccess(),
            can('settings.bank_account.manage')
                ? _BanksTab(session: widget.session)
                : const _NoAccess(),
            can('audit_log.view')
                ? _AuditTab(session: widget.session)
                : const _NoAccess(),
            _AuthorizationTab(session: widget.session),
          ],
        ),
      ),
    ],
  );
}

class _SystemSettingsTab extends StatefulWidget {
  const _SystemSettingsTab({required this.session});
  final SessionController session;

  @override
  State<_SystemSettingsTab> createState() => _SystemSettingsTabState();
}

class _SystemSettingsTabState extends State<_SystemSettingsTab> {
  static const appointmentKeys = [
    'appointment_auto_approval_enabled',
    'appointment_booking_min_lead_minutes',
    'appointment_booking_max_advance_days',
    'appointment_check_in_early_minutes',
    'appointment_check_in_late_minutes',
    'appointment_no_show_grace_minutes',
    'appointment_max_holding_future_per_patient',
  ];

  Map<String, Map<String, dynamic>> appointment = const {};
  Map<String, dynamic>? scheduling;
  Map<String, dynamic>? inventoryAlert;
  final controllers = <String, TextEditingController>{};
  bool autoApproval = false;
  bool loading = true;
  bool saving = false;
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
    for (final controller in controllers.values) {
      controller.dispose();
    }
    super.dispose();
  }

  Future<void> load() async {
    setState(() {
      loading = true;
      error = null;
    });
    try {
      if (can('settings.appointment.manage')) {
        final results = await Future.wait([
          widget.session.requestJson('GET', '/api/v1/settings/appointment'),
          widget.session.requestJson('GET', '/api/v1/settings/scheduling'),
        ]);
        appointment = {
          for (final item in apiItems(results[0])) apiString(item['key']): item,
        };
        scheduling = results[1];
        autoApproval =
            appointment['appointment_auto_approval_enabled']?['boolean_value'] ==
            true;
        for (final key in appointmentKeys.where(
          (key) => key != 'appointment_auto_approval_enabled',
        )) {
          _controller(key).text = valueText(
            appointment[key]?['integer_value'],
          ).replaceAll('—', '');
        }
        _controller('default_slot_duration_minutes').text = valueText(
          scheduling?['integer_value'],
        ).replaceAll('—', '');
      }
      if (can('settings.inventory_alert.manage')) {
        inventoryAlert = await widget.session.requestJson(
          'GET',
          '/api/v1/settings/inventory-alert',
        );
        _controller('inventory_expiry_alert_days').text = valueText(
          inventoryAlert?['integer_value'],
        ).replaceAll('—', '');
      }
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  TextEditingController _controller(String key) =>
      controllers.putIfAbsent(key, TextEditingController.new);

  Future<void> saveAppointments() async {
    await _run(() async {
      int value(String key) => int.parse(_controller(key).text.trim());
      await widget.session.requestJson(
        'PUT',
        '/api/v1/settings/appointment',
        body: {
          'appointment_auto_approval_enabled': autoApproval,
          'appointment_booking_min_lead_minutes': value(
            'appointment_booking_min_lead_minutes',
          ),
          'appointment_booking_max_advance_days': value(
            'appointment_booking_max_advance_days',
          ),
          'appointment_check_in_early_minutes': value(
            'appointment_check_in_early_minutes',
          ),
          'appointment_check_in_late_minutes': value(
            'appointment_check_in_late_minutes',
          ),
          'appointment_no_show_grace_minutes': value(
            'appointment_no_show_grace_minutes',
          ),
          'appointment_max_holding_future_per_patient': value(
            'appointment_max_holding_future_per_patient',
          ),
          'expected_versions': {
            for (final key in appointmentKeys)
              key: apiInt(appointment[key]?['version']),
          },
        },
      );
      return;
    });
  }

  Future<void> saveSingle(
    String path,
    String key,
    Map<String, dynamic>? current,
  ) async {
    await _run(
      () => widget.session.requestJson(
        'PATCH',
        path,
        body: {
          'value': int.parse(_controller(key).text.trim()),
          'expected_version': apiInt(current?['version']),
        },
      ),
    );
  }

  Future<void> _run(Future<Object?> Function() command) async {
    setState(() {
      saving = true;
      error = null;
    });
    try {
      await command();
      await load();
      if (mounted) showSuccessMessage(context, 'Đã lưu cấu hình.');
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    } finally {
      if (mounted) setState(() => saving = false);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Cấu hình hệ thống',
    description:
        'Điều chỉnh quy định đặt lịch, thời lượng khám và cảnh báo kho.',
    actions: [
      IconButton(
        onPressed: loading ? null : load,
        tooltip: 'Tải lại',
        icon: const Icon(Icons.refresh),
      ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      if (loading) const LinearProgressIndicator(),
      if (can('settings.appointment.manage'))
        SectionCard(
          title: 'Lịch hẹn',
          description: 'Phạm vi và cửa sổ đặt/check-in lịch hẹn.',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('Tự động duyệt lịch hẹn'),
                controlAffinity: ListTileControlAffinity.leading,
                value: autoApproval,
                onChanged: saving
                    ? null
                    : (value) => setState(() => autoApproval = value),
              ),
              const SizedBox(height: AppSpacing.md),
              Wrap(
                spacing: AppSpacing.md,
                runSpacing: AppSpacing.md,
                children: [
                  _NumberSetting(
                    controller: _controller(
                      'appointment_booking_min_lead_minutes',
                    ),
                    label: 'Đặt trước tối thiểu (phút)',
                  ),
                  _NumberSetting(
                    controller: _controller(
                      'appointment_booking_max_advance_days',
                    ),
                    label: 'Đặt trước tối đa (ngày)',
                  ),
                  _NumberSetting(
                    controller: _controller(
                      'appointment_check_in_early_minutes',
                    ),
                    label: 'Check-in sớm (phút)',
                  ),
                  _NumberSetting(
                    controller: _controller(
                      'appointment_check_in_late_minutes',
                    ),
                    label: 'Check-in trễ (phút)',
                  ),
                  _NumberSetting(
                    controller: _controller(
                      'appointment_no_show_grace_minutes',
                    ),
                    label: 'Ân hạn no-show (phút)',
                  ),
                  _NumberSetting(
                    controller: _controller(
                      'appointment_max_holding_future_per_patient',
                    ),
                    label: 'Lịch tương lai tối đa/BN',
                  ),
                ],
              ),
              const SizedBox(height: AppSpacing.md),
              Align(
                alignment: Alignment.centerRight,
                child: FilledButton(
                  onPressed: saving ? null : saveAppointments,
                  child: const Text('Lưu lịch hẹn'),
                ),
              ),
            ],
          ),
        ),
      if (can('settings.appointment.manage'))
        SectionCard(
          title: 'Lịch bác sĩ',
          child: Wrap(
            spacing: AppSpacing.md,
            runSpacing: AppSpacing.md,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              _NumberSetting(
                controller: _controller('default_slot_duration_minutes'),
                label: 'Thời lượng slot mặc định',
              ),
              FilledButton.tonal(
                onPressed: saving
                    ? null
                    : () => saveSingle(
                        '/api/v1/settings/scheduling',
                        'default_slot_duration_minutes',
                        scheduling,
                      ),
                child: const Text('Lưu'),
              ),
            ],
          ),
        ),
      if (can('settings.inventory_alert.manage'))
        SectionCard(
          title: 'Cảnh báo kho',
          child: Wrap(
            spacing: AppSpacing.md,
            runSpacing: AppSpacing.md,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              _NumberSetting(
                controller: _controller('inventory_expiry_alert_days'),
                label: 'Cảnh báo trước HSD (ngày)',
              ),
              FilledButton.tonal(
                onPressed: saving
                    ? null
                    : () => saveSingle(
                        '/api/v1/settings/inventory-alert',
                        'inventory_expiry_alert_days',
                        inventoryAlert,
                      ),
                child: const Text('Lưu'),
              ),
            ],
          ),
        ),
    ],
  );
}

class _BanksTab extends StatefulWidget {
  const _BanksTab({required this.session});
  final SessionController session;

  @override
  State<_BanksTab> createState() => _BanksTabState();
}

class _BanksTabState extends State<_BanksTab> {
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
          '/api/v1/settings/bank-accounts',
          query: {'include_archived': 'true', 'page': '1', 'page_size': '100'},
        ),
      );
      error = null;
    } catch (cause) {
      error = friendlyError(cause);
    } finally {
      if (mounted) setState(() => loading = false);
    }
  }

  Future<void> create() async {
    final body = await showDialog<Map<String, dynamic>>(
      context: context,
      builder: (_) => const _BankDialog(),
    );
    if (body == null || !mounted) return;
    await _run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/settings/bank-accounts',
        body: body,
      ),
    );
  }

  Future<void> command(Map<String, dynamic> item, String action) async {
    String reason = '';
    if (action != 'activate') {
      final value = await showReasonDialog(
        context,
        title: action == 'archive' ? 'Lưu trữ tài khoản' : 'Ngừng tài khoản',
        message: action == 'archive'
            ? 'Tài khoản ACTIVE phải được ngừng trước.'
            : 'Thông tin ngân hàng trên hóa đơn đã lập được giữ nguyên.',
        confirmLabel: action == 'archive' ? 'Lưu trữ' : 'Ngừng',
        destructive: true,
      );
      if (value == null || !mounted) return;
      reason = value;
    } else {
      final yes = await showConfirmActionDialog(
        context,
        title: 'Kích hoạt tài khoản nhận tiền?',
        message: 'Tài khoản ACTIVE hiện tại sẽ tự động chuyển sang INACTIVE.',
        confirmLabel: 'Kích hoạt',
      );
      if (!yes || !mounted) return;
    }
    await _run(
      () => widget.session.requestJson(
        'POST',
        '/api/v1/settings/bank-accounts/${item['id']}/$action',
        body: {'expected_version': apiInt(item['version']), 'reason': reason},
      ),
    );
  }

  Future<void> _run(Future<Object?> Function() command) async {
    try {
      await command();
      await load();
      if (mounted) {
        showSuccessMessage(context, 'Đã cập nhật tài khoản ngân hàng.');
      }
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Tài khoản nhận tiền',
    description: 'Chọn một tài khoản nhận tiền đang hoạt động cho hóa đơn mới.',
    actions: [
      FilledButton.icon(
        onPressed: create,
        icon: const Icon(Icons.add),
        label: const Text('Thêm tài khoản'),
      ),
    ],
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Danh sách tài khoản',
        child: DataTableShell(
          resultLimit: 100,
          loading: loading,
          error: error,
          onRetry: load,
          columns: const [
            DataColumn(label: Text('Ngân hàng')),
            DataColumn(label: Text('Số tài khoản')),
            DataColumn(label: Text('Chủ tài khoản')),
            DataColumn(label: Text('Trạng thái')),
            DataColumn(label: Text('')),
          ],
          rows: items
              .map(
                (item) => DataRow(
                  cells: [
                    DataCell(
                      Text(
                        '${valueText(item['bank_name'])} (${valueText(item['bank_bin'])})',
                      ),
                    ),
                    DataCell(Text(valueText(item['account_number']))),
                    DataCell(Text(valueText(item['account_name']))),
                    DataCell(
                      StatusBadge(
                        status: item['is_deleted'] == true
                            ? 'ARCHIVED'
                            : apiString(item['status']),
                      ),
                    ),
                    DataCell(
                      PopupMenuButton<String>(
                        onSelected: (value) => command(item, value),
                        itemBuilder: (_) => [
                          if (item['is_deleted'] != true &&
                              item['status'] == 'INACTIVE')
                            const PopupMenuItem(
                              value: 'activate',
                              child: Text('Kích hoạt'),
                            ),
                          if (item['is_deleted'] != true &&
                              item['status'] == 'ACTIVE')
                            const PopupMenuItem(
                              value: 'deactivate',
                              child: Text('Ngừng'),
                            ),
                          if (item['is_deleted'] != true &&
                              item['status'] == 'INACTIVE')
                            const PopupMenuItem(
                              value: 'archive',
                              child: Text('Lưu trữ'),
                            ),
                        ],
                      ),
                    ),
                  ],
                ),
              )
              .toList(),
          mobileCards: items
              .map(
                (item) => Card(
                  child: ListTile(
                    leading: const Icon(Icons.account_balance_outlined),
                    title: Text(valueText(item['bank_name'])),
                    subtitle: Text(
                      '${valueText(item['account_number'])} · ${valueText(item['account_name'])}',
                    ),
                    trailing: StatusBadge(
                      status: item['is_deleted'] == true
                          ? 'ARCHIVED'
                          : apiString(item['status']),
                    ),
                  ),
                ),
              )
              .toList(),
        ),
      ),
    ],
  );
}

class _AuditTab extends StatefulWidget {
  const _AuditTab({required this.session});
  final SessionController session;

  @override
  State<_AuditTab> createState() => _AuditTabState();
}

class _AuditTabState extends State<_AuditTab> {
  final module = TextEditingController();
  final action = TextEditingController();
  int? actorId;
  int? targetId;
  String targetType = '';
  List<String> targetTypes = [];
  final paging = ListPagination(
    cursor: true,
    pageSize: 50,
    sizeParameter: 'page_size',
  );
  String outcome = '';
  List<Map<String, dynamic>> items = const [];
  bool loading = true;
  String? error;

  @override
  void initState() {
    super.initState();
    load();
    loadTargetTypes();
  }

  @override
  void dispose() {
    paging.dispose();
    for (final controller in [module, action]) {
      controller.dispose();
    }
    super.dispose();
  }

  Future<void> loadTargetTypes() async {
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/audit-log-targets',
      );
      if (!mounted) return;
      setState(
        () => targetTypes = (response['target_types'] as List? ?? [])
            .map((x) => x.toString())
            .toList(),
      );
    } catch (_) {
      /* Existing rows still offer their target types on retry. */
    }
  }

  Future<List<EntityOption>> lookup(String path, String query) async {
    final response = await widget.session.requestJson(
      'GET',
      path,
      query: {
        'q': query,
        if (path.endsWith('targets')) 'target_type': targetType,
      },
    );
    return apiItems(response)
        .map(
          (x) => EntityOption(
            id: apiInt(x['id']),
            label: valueText(x['label']),
            subtitle: apiString(x['code']),
          ),
        )
        .toList();
  }

  Future<void> load({int? targetPage}) async {
    final request = paging.start({
      if (module.text.trim().isNotEmpty) 'module': module.text.trim(),
      if (action.text.trim().isNotEmpty) 'action_code': action.text.trim(),
      if (outcome.isNotEmpty) 'outcome': outcome,
      if (actorId != null) 'actor_user_id': '$actorId',
      if (targetType.isNotEmpty) 'target_type': targetType,
      if (targetId != null) 'target_id': '$targetId',
    }, targetPage: targetPage);
    setState(() {
      loading = true;
      error = null;
    });
    try {
      final response = await widget.session.requestJson(
        'GET',
        '/api/v1/audit-logs',
        query: request.query,
      );
      if (!mounted || !paging.accept(request, response)) return;
      items = apiItems(response);
      targetTypes = {
        ...targetTypes,
        for (final row in items)
          if (apiString(row['target_type']).isNotEmpty)
            apiString(row['target_type']),
      }.toList()..sort();
    } catch (cause) {
      if (mounted && paging.isCurrent(request)) error = friendlyError(cause);
    } finally {
      if (mounted && paging.isCurrent(request)) setState(() => loading = false);
    }
  }

  Future<void> detail(Map<String, dynamic> row) async {
    try {
      final item = await widget.session.requestJson(
        'GET',
        '/api/v1/audit-logs/${row['id']}',
      );
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (_) => _AuditDetail(item: item),
      );
    } catch (cause) {
      if (mounted) showActionError(context, cause);
    }
  }

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Nhật ký hoạt động',
    description:
        'Tra cứu người thực hiện, thao tác và đối tượng trong nhật ký.',
    children: [
      if (error != null)
        InlineMessage(message: error!, tone: AppStatusTone.danger),
      SectionCard(
        title: 'Bộ lọc',
        child: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: [
            _TextFilter(controller: module, label: 'Phân hệ'),
            _TextFilter(controller: action, label: 'Mã thao tác'),
            SizedBox(
              width: 240,
              child: EntityPicker(
                label: 'Người thực hiện',
                search: (q) => lookup('/api/v1/audit-log-actors', q),
                onSelected: (option) => actorId = option?.id,
              ),
            ),
            SizedBox(
              width: 230,
              child: DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: targetType,
                decoration: const InputDecoration(labelText: 'Loại đối tượng'),
                items: [
                  const DropdownMenuItem(value: '', child: Text('Tất cả')),
                  for (final type in targetTypes)
                    DropdownMenuItem(value: type, child: Text(enumLabel(type))),
                ],
                onChanged: (value) => setState(() {
                  targetType = value ?? '';
                  targetId = null;
                }),
              ),
            ),
            SizedBox(
              width: 240,
              child: EntityPicker(
                key: ValueKey(targetType),
                label: 'Đối tượng',
                enabled: targetType.isNotEmpty,
                search: (q) => lookup('/api/v1/audit-log-targets', q),
                onSelected: (option) => targetId = option?.id,
              ),
            ),
            SizedBox(
              width: 170,
              child: DropdownButtonFormField<String>(
                isExpanded: true,
                initialValue: outcome,
                decoration: const InputDecoration(labelText: 'Kết quả'),
                items: const [
                  DropdownMenuItem(value: '', child: Text('Tất cả')),
                  DropdownMenuItem(value: 'SUCCESS', child: Text('Thành công')),
                  DropdownMenuItem(value: 'FAILURE', child: Text('Thất bại')),
                ],
                onChanged: (value) => setState(() => outcome = value ?? ''),
              ),
            ),
            FilledButton.tonalIcon(
              onPressed: loading ? null : load,
              icon: const Icon(Icons.search),
              label: const Text('Tra cứu'),
            ),
          ],
        ),
      ),
      SectionCard(
        title: 'Sự kiện',
        child: Column(
          children: [
            DataTableShell(
              page: paging.page,
              hasMore: paging.hasMore,
              onPrevious: () => load(targetPage: paging.page - 1),
              onNext: () => load(targetPage: paging.page + 1),
              loading: loading,
              error: error,
              onRetry: load,
              columns: const [
                DataColumn(label: Text('Thời gian')),
                DataColumn(label: Text('Người thực hiện')),
                DataColumn(label: Text('Phân hệ / Thao tác')),
                DataColumn(label: Text('Đối tượng')),
                DataColumn(label: Text('Kết quả')),
                DataColumn(label: Text('')),
              ],
              rows: items
                  .map(
                    (item) => DataRow(
                      cells: [
                        DataCell(Text(formatDateTime(item['occurred_at']))),
                        DataCell(Text(valueText(item['actor_name']))),
                        DataCell(
                          Text(
                            '${valueText(item['module'])}\n${valueText(item['action_code'])}',
                          ),
                        ),
                        DataCell(
                          Text(
                            valueText(
                              item['target_label'] ?? item['target_code'],
                            ),
                          ),
                        ),
                        DataCell(
                          StatusBadge(status: apiString(item['outcome'])),
                        ),
                        DataCell(
                          IconButton(
                            onPressed: () => detail(item),
                            tooltip: 'Chi tiết',
                            icon: const Icon(Icons.open_in_new),
                          ),
                        ),
                      ],
                    ),
                  )
                  .toList(),
            ),
          ],
        ),
      ),
    ],
  );
}

class _AuthorizationTab extends StatelessWidget {
  const _AuthorizationTab({required this.session});
  final SessionController session;

  @override
  Widget build(BuildContext context) {
    final permissions = session.permissions.toList()..sort();
    return AppPageScaffold(
      title: 'Phân quyền hiệu lực',
      description: 'Quyền đã được backend tính cho phiên đăng nhập hiện tại.',
      children: [
        const InlineMessage(
          message:
              'Backend hiện chưa cung cấp API quản trị role, role-permission hoặc user override. Vì vậy màn hình chỉ hiển thị quyền hiệu lực và không giả lập thao tác ghi.',
          tone: AppStatusTone.warning,
        ),
        SectionCard(
          title: 'Principal',
          child: Wrap(
            spacing: AppSpacing.xl,
            children: [
              DetailField(
                label: 'User ID',
                value: valueText(session.principal?['user_id']),
              ),
              DetailField(
                label: 'Account type',
                value: enumLabel(session.accountType),
              ),
              DetailField(label: 'Role', value: enumLabel(session.roleCode)),
            ],
          ),
        ),
        SectionCard(
          title: 'Quyền hiệu lực (${permissions.length})',
          child: permissions.isEmpty
              ? const Text('Không có quyền nghiệp vụ.')
              : Wrap(
                  spacing: AppSpacing.xs,
                  runSpacing: AppSpacing.xs,
                  children: permissions
                      .map((permission) => Chip(label: Text(permission)))
                      .toList(),
                ),
        ),
      ],
    );
  }
}

class _BankDialog extends StatefulWidget {
  const _BankDialog();
  @override
  State<_BankDialog> createState() => _BankDialogState();
}

class _BankDialogState extends State<_BankDialog> {
  final key = GlobalKey<FormState>();
  final bin = TextEditingController();
  final bank = TextEditingController();
  final number = TextEditingController();
  final name = TextEditingController();

  @override
  void dispose() {
    bin.dispose();
    bank.dispose();
    number.dispose();
    name.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: const Text('Thêm tài khoản ngân hàng'),
    content: SizedBox(
      width: 480,
      child: Form(
        key: key,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextFormField(
              controller: bin,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: 'BIN 6 chữ số *'),
              validator: (value) => RegExp(r'^\d{6}$').hasMatch(value ?? '')
                  ? null
                  : 'BIN phải có 6 chữ số.',
            ),
            const SizedBox(height: AppSpacing.md),
            TextFormField(
              controller: bank,
              decoration: const InputDecoration(labelText: 'Tên ngân hàng *'),
              validator: _required,
            ),
            const SizedBox(height: AppSpacing.md),
            TextFormField(
              controller: number,
              keyboardType: TextInputType.number,
              decoration: const InputDecoration(labelText: 'Số tài khoản *'),
              validator: (value) => RegExp(r'^\d{6,30}$').hasMatch(value ?? '')
                  ? null
                  : 'Cần 6–30 chữ số.',
            ),
            const SizedBox(height: AppSpacing.md),
            TextFormField(
              controller: name,
              decoration: const InputDecoration(
                labelText: 'Tên chủ tài khoản *',
              ),
              validator: _required,
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
          if (!(key.currentState?.validate() ?? false)) return;
          Navigator.pop(context, {
            'bank_bin': bin.text.trim(),
            'bank_name': bank.text.trim(),
            'account_number': number.text.trim(),
            'account_name': name.text.trim(),
          });
        },
        child: const Text('Tạo'),
      ),
    ],
  );
}

class _AuditDetail extends StatelessWidget {
  const _AuditDetail({required this.item});
  final Map<String, dynamic> item;

  String pretty(Object? value) {
    if (value == null) return '—';
    try {
      return const JsonEncoder.withIndent('  ').convert(value);
    } catch (_) {
      return valueText(value);
    }
  }

  @override
  Widget build(BuildContext context) => AlertDialog(
    title: Text('Audit #${valueText(item['id'])}'),
    content: SizedBox(
      width: 760,
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Wrap(
              spacing: AppSpacing.xl,
              children: [
                DetailField(
                  label: 'Thời gian',
                  value: formatDateTime(item['occurred_at']),
                ),
                DetailField(
                  label: 'Người thực hiện',
                  value: valueText(item['actor_name']),
                ),
                DetailField(
                  label: 'Đối tượng',
                  value: valueText(item['target_label']),
                ),
                DetailField(label: 'Phân hệ', value: valueText(item['module'])),
                DetailField(
                  label: 'Action',
                  value: valueText(item['action_code']),
                ),
                DetailField(
                  label: 'Kết quả',
                  value: valueText(item['outcome']),
                  status: true,
                ),
                DetailField(
                  label: 'Request ID',
                  value: valueText(item['request_id']),
                ),
                DetailField(
                  label: 'Correlation ID',
                  value: valueText(item['correlation_id']),
                ),
              ],
            ),
            for (final field in const [
              ('metadata', 'Metadata'),
              ('before_snapshot', 'Before snapshot'),
              ('after_snapshot', 'After snapshot'),
            ]) ...[
              Text(field.$2, style: Theme.of(context).textTheme.titleSmall),
              const SizedBox(height: AppSpacing.xs),
              Container(
                padding: const EdgeInsets.all(AppSpacing.md),
                color: AppColors.surfaceSubtle,
                child: SelectableText(pretty(item[field.$1])),
              ),
              const SizedBox(height: AppSpacing.md),
            ],
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
  );
}

class _NumberSetting extends StatelessWidget {
  const _NumberSetting({required this.controller, required this.label});
  final TextEditingController controller;
  final String label;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: 230,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ExcludeSemantics(child: Text(label)),
        const SizedBox(height: AppSpacing.xs),
        Semantics(
          label: label,
          child: TextField(
            controller: controller,
            keyboardType: TextInputType.number,
            decoration: const InputDecoration(hintText: 'Nhập số'),
          ),
        ),
      ],
    ),
  );
}

class _TextFilter extends StatelessWidget {
  const _TextFilter({required this.controller, required this.label});
  final TextEditingController controller;
  final String label;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: 180,
    child: TextField(
      controller: controller,
      keyboardType: TextInputType.text,
      decoration: InputDecoration(labelText: label),
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

String? _required(String? value) =>
    value == null || value.trim().isEmpty ? 'Bắt buộc.' : null;

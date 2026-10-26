import 'package:flutter/material.dart';

import '../design_system/components/layout.dart';
import '../design_system/theme/app_theme.dart';
import '../features/appointments/appointments_screen.dart';
import '../features/billing/billing_screen.dart';
import '../features/catalog/services_screen.dart';
import '../features/clinic/clinic_master_screen.dart';
import '../features/encounters/encounters_screen.dart';
import '../features/inventory/inventory_screen.dart';
import '../features/patients/patients_screen.dart';
import '../features/prescriptions/prescriptions_screen.dart';
import '../features/scheduling/schedules_screen.dart';
import '../features/settings/settings_audit_screen.dart';
import '../features/shared/api_helpers.dart';
import '../features/staff/staff_screen.dart';
import '../session_controller.dart';
import '../ui/responsive.dart';
import '../design_system/formatters/app_formatters.dart';

class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.session});
  final SessionController session;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  int index = 0;
  final scaffoldKey = GlobalKey<ScaffoldState>();

  @override
  Widget build(BuildContext context) {
    if (widget.session.mustChangePassword) {
      return _ChangePasswordScreen(session: widget.session);
    }
    final destinations = _destinations();
    if (index >= destinations.length) index = 0;
    final desktop = isDesktop(context);
    final content = IndexedStack(
      index: index,
      children: destinations.map((item) => item.page).toList(),
    );
    return Scaffold(
      key: scaffoldKey,
      appBar: desktop
          ? null
          : AppBar(
              leading: IconButton(
                tooltip: 'Mở điều hướng',
                onPressed: () => scaffoldKey.currentState?.openDrawer(),
                icon: const Icon(Icons.menu),
              ),
              title: Text(destinations[index].label),
              actions: _headerActions(),
            ),
      drawer: desktop
          ? null
          : Drawer(
              child: SafeArea(
                child: _AppNavigation(
                  destinations: destinations,
                  selectedIndex: index,
                  session: widget.session,
                  onSelected: (value) {
                    setState(() => index = value);
                    Navigator.pop(context);
                  },
                ),
              ),
            ),
      body: desktop
          ? Row(
              children: [
                SizedBox(
                  width: 280,
                  child: _AppNavigation(
                    destinations: destinations,
                    selectedIndex: index,
                    session: widget.session,
                    onSelected: (value) => setState(() => index = value),
                  ),
                ),
                const VerticalDivider(width: 1),
                Expanded(
                  child: Column(
                    children: [
                      _DesktopHeader(
                        session: widget.session,
                        title: destinations[index].label,
                      ),
                      const Divider(height: 1),
                      Expanded(child: content),
                    ],
                  ),
                ),
              ],
            )
          : content,
    );
  }

  List<_AppDestination> _destinations() {
    final session = widget.session;
    bool any(List<String> values) => hasAnyPermission(session, values);
    final result = <_AppDestination>[
      _AppDestination(
        group: 'Tổng quan',
        label: 'Tổng quan',
        icon: Icons.dashboard_outlined,
        page: _Dashboard(session: session),
      ),
    ];
    if (session.accountType == 'PATIENT' ||
        any(const [
          'patient.view.all',
          'patient.manage.all',
          'patient.view.assigned_or_history',
          'patient.view.own',
        ])) {
      result.add(
        _AppDestination(
          group: 'Tiếp nhận',
          label: session.accountType == 'PATIENT'
              ? 'Hồ sơ của tôi'
              : 'Bệnh nhân',
          icon: Icons.personal_injury_outlined,
          page: PatientsScreen(session: session),
        ),
      );
    }
    if (any(const ['staff.view.all', 'staff.view.own', 'staff.manage'])) {
      result.add(
        _AppDestination(
          group: 'Quản trị',
          label: 'Nhân sự',
          icon: Icons.badge_outlined,
          page: StaffScreen(session: session),
        ),
      );
    }
    if (any(const [
      'specialty.view',
      'specialty.manage',
      'room.view',
      'room.manage',
      'doctor_specialty.manage',
    ])) {
      result.add(
        _AppDestination(
          group: 'Danh mục',
          label: session.accountType == 'PATIENT'
              ? 'Thông tin phòng khám'
              : 'Danh mục phòng khám',
          icon: Icons.apartment_outlined,
          page: ClinicMasterScreen(session: session),
        ),
      );
    }
    if (any(const [
      'doctor_schedule.view.all',
      'doctor_schedule.view.own',
      'doctor_schedule.manage',
    ])) {
      result.add(
        _AppDestination(
          group: 'Vận hành',
          label: 'Lịch bác sĩ',
          icon: Icons.calendar_month_outlined,
          page: SchedulesScreen(session: session),
        ),
      );
    }
    if (any(const [
      'appointment.view.all',
      'appointment.view.doctor_own',
      'appointment.view.patient_own',
      'appointment.book.at_counter',
      'appointment.book.patient_own',
    ])) {
      result.add(
        _AppDestination(
          group: 'Tiếp nhận',
          label: 'Lịch hẹn',
          icon: Icons.event_available_outlined,
          page: AppointmentsScreen(session: session),
        ),
      );
    }
    if (any(const [
      'encounter.view.all',
      'encounter.view.assigned_or_history',
      'encounter.view.patient_own',
      'encounter.create.assigned',
    ])) {
      result.add(
        _AppDestination(
          group: 'Khám bệnh',
          label: session.accountType == 'PATIENT' ? 'Lịch sử khám' : 'Bệnh án',
          icon: Icons.medical_information_outlined,
          page: EncountersScreen(session: session),
        ),
      );
    }
    if (any(const [
      'prescription.view.all',
      'prescription.view.assigned_or_history',
      'prescription.view.patient_own',
      'prescription.draft.manage.assigned',
    ])) {
      result.add(
        _AppDestination(
          group: 'Khám bệnh',
          label: 'Đơn thuốc',
          icon: Icons.medication_outlined,
          page: PrescriptionsScreen(session: session),
        ),
      );
    }
    if (any(const [
      'invoice.view.all',
      'invoice.view.patient_own',
      'invoice.create',
    ])) {
      result.add(
        _AppDestination(
          group: 'Thu ngân',
          label: 'Hóa đơn & thanh toán',
          icon: Icons.receipt_long_outlined,
          page: BillingScreen(session: session),
        ),
      );
    }
    if (any(const ['service.view', 'service.manage'])) {
      result.add(
        _AppDestination(
          group: 'Danh mục',
          label: session.accountType == 'PATIENT'
              ? 'Bảng giá dịch vụ'
              : 'Dịch vụ',
          icon: Icons.medical_services_outlined,
          page: ServicesScreen(session: session),
        ),
      );
    }
    if (any(const [
      'medicine.view',
      'medicine.manage',
      'inventory.view',
      'inventory.receipt.manage',
      'inventory.adjust',
      'inventory.movement.view',
    ])) {
      result.add(
        _AppDestination(
          group: 'Vận hành',
          label: 'Thuốc & kho',
          icon: Icons.inventory_2_outlined,
          page: InventoryScreen(session: session),
        ),
      );
    }
    if (any(const [
      'settings.appointment.manage',
      'settings.inventory_alert.manage',
      'settings.bank_account.manage',
      'audit_log.view',
    ])) {
      result.add(
        _AppDestination(
          group: 'Quản trị',
          label: 'Cấu hình & nhật ký',
          icon: Icons.settings_outlined,
          page: SettingsAuditScreen(session: session),
        ),
      );
    }
    return result;
  }

  List<Widget> _headerActions() => [
    Padding(
      padding: const EdgeInsets.symmetric(horizontal: AppSpacing.xs),
      child: Center(
        child: Chip(label: Text(enumLabel(widget.session.roleCode))),
      ),
    ),
    IconButton(
      tooltip: 'Đăng xuất',
      onPressed: widget.session.logout,
      icon: const Icon(Icons.logout),
    ),
  ];
}

class _AppDestination {
  const _AppDestination({
    required this.group,
    required this.label,
    required this.icon,
    required this.page,
  });
  final String group;
  final String label;
  final IconData icon;
  final Widget page;
}

class _AppNavigation extends StatelessWidget {
  const _AppNavigation({
    required this.destinations,
    required this.selectedIndex,
    required this.session,
    required this.onSelected,
  });

  final List<_AppDestination> destinations;
  final int selectedIndex;
  final SessionController session;
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: Theme.of(context).colorScheme.surface,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const Padding(
            padding: EdgeInsets.fromLTRB(
              AppSpacing.lg,
              AppSpacing.xl,
              AppSpacing.lg,
              AppSpacing.lg,
            ),
            child: _BrandMark(),
          ),
          const Divider(height: 1),
          Expanded(
            child: ListView(
              padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
              children: [
                Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    ListTile(
                      leading: Icon(destinations.first.icon),
                      title: Text(destinations.first.label),
                      selected: selectedIndex == 0,
                      onTap: () => onSelected(0),
                    ),
                    for (final group in const [
                      'Tiếp nhận',
                      'Khám bệnh',
                      'Thu ngân',
                      'Danh mục',
                      'Vận hành',
                      'Quản trị',
                    ])
                      if (destinations.any((item) => item.group == group))
                        ExpansionTile(
                          key: PageStorageKey(group),
                          title: Text(group),
                          initiallyExpanded:
                              destinations[selectedIndex].group == group,
                          children: [
                            for (
                              var itemIndex = 1;
                              itemIndex < destinations.length;
                              itemIndex++
                            )
                              if (destinations[itemIndex].group == group)
                                ListTile(
                                  contentPadding: const EdgeInsets.only(
                                    left: 28,
                                    right: 12,
                                  ),
                                  selected: itemIndex == selectedIndex,
                                  leading: Icon(destinations[itemIndex].icon),
                                  title: Text(destinations[itemIndex].label),
                                  onTap: () => onSelected(itemIndex),
                                ),
                          ],
                        ),
                  ],
                ),
              ],
            ),
          ),
          const Divider(height: 1),
          Padding(
            padding: const EdgeInsets.all(AppSpacing.md),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  '${enumLabel(session.roleCode)} · ${enumLabel(session.accountType)}',
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.labelMedium,
                ),
                const SizedBox(height: AppSpacing.xs),
                OutlinedButton.icon(
                  onPressed: session.logout,
                  icon: const Icon(Icons.logout),
                  label: const Text('Đăng xuất'),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _BrandMark extends StatelessWidget {
  const _BrandMark();

  @override
  Widget build(BuildContext context) => Row(
    children: [
      Container(
        width: 42,
        height: 42,
        decoration: BoxDecoration(
          color: Theme.of(context).colorScheme.primary,
          borderRadius: BorderRadius.circular(AppRadius.medium),
        ),
        child: Icon(
          Icons.local_hospital_outlined,
          color: Theme.of(context).colorScheme.onPrimary,
        ),
      ),
      const SizedBox(width: AppSpacing.sm),
      const Expanded(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('DNCS Clinic', style: TextStyle(fontWeight: FontWeight.w700)),
            Text('Quản lý phòng khám', style: TextStyle(fontSize: 12)),
          ],
        ),
      ),
    ],
  );
}

class _DesktopHeader extends StatelessWidget {
  const _DesktopHeader({required this.session, required this.title});
  final SessionController session;
  final String title;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(
      horizontal: AppSpacing.xxl,
      vertical: AppSpacing.md,
    ),
    child: Row(
      children: [
        Expanded(
          child: Semantics(
            header: true,
            child: Text(title, style: Theme.of(context).textTheme.titleLarge),
          ),
        ),
        const SizedBox(width: AppSpacing.sm),
        Chip(
          avatar: const Icon(Icons.verified_user_outlined, size: 18),
          label: Text(
            '${enumLabel(session.roleCode)} · ${enumLabel(session.accountType)}',
          ),
        ),
      ],
    ),
  );
}

class _Dashboard extends StatelessWidget {
  const _Dashboard({required this.session});
  final SessionController session;

  @override
  Widget build(BuildContext context) => AppPageScaffold(
    title: 'Xin chào',
    description: 'Tổng quan phiên làm việc và các phân hệ được cấp quyền.',
    children: [
      LayoutBuilder(
        builder: (context, constraints) {
          final columns = constraints.maxWidth >= 1000
              ? 3
              : constraints.maxWidth >= 620
              ? 2
              : 1;
          const gap = AppSpacing.md;
          final cardWidth =
              (constraints.maxWidth - gap * (columns - 1)) / columns;
          return Wrap(
            spacing: gap,
            runSpacing: gap,
            children: [
              _StatusCard(
                width: cardWidth,
                icon: Icons.verified_user_outlined,
                title:
                    '${enumLabel(session.roleCode)} · ${enumLabel(session.accountType)}',
                detail: 'Chọn chức năng trong menu để bắt đầu công việc.',
              ),
              _StatusCard(
                width: cardWidth,
                icon: Icons.security_outlined,
                title: 'Phiên đăng nhập an toàn',
                detail: 'Đăng xuất khi kết thúc công việc trên máy dùng chung.',
              ),
              _StatusCard(
                width: cardWidth,
                icon: Icons.route_outlined,
                title: 'Luồng phòng khám',
                detail: 'Từ lịch hẹn, khám bệnh, đơn thuốc đến hóa đơn và kho.',
              ),
            ],
          );
        },
      ),
    ],
  );
}

class _StatusCard extends StatelessWidget {
  const _StatusCard({
    required this.width,
    required this.icon,
    required this.title,
    required this.detail,
  });
  final double width;
  final IconData icon;
  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) => SizedBox(
    width: width,
    child: Card(
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.lg),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, color: Theme.of(context).colorScheme.primary),
            const SizedBox(height: AppSpacing.lg),
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: AppSpacing.xs),
            Text(detail),
          ],
        ),
      ),
    ),
  );
}

class _ChangePasswordScreen extends StatefulWidget {
  const _ChangePasswordScreen({required this.session});
  final SessionController session;

  @override
  State<_ChangePasswordScreen> createState() => _ChangePasswordScreenState();
}

class _ChangePasswordScreenState extends State<_ChangePasswordScreen> {
  final current = TextEditingController();
  final next = TextEditingController();
  final confirm = TextEditingController();
  bool busy = false;
  String? error;

  @override
  void dispose() {
    current.dispose();
    next.dispose();
    confirm.dispose();
    super.dispose();
  }

  Future<void> submit() async {
    if (next.text != confirm.text) {
      setState(() => error = 'Mật khẩu xác nhận không khớp.');
      return;
    }
    setState(() {
      busy = true;
      error = null;
    });
    try {
      await widget.session.changePassword(current.text, next.text);
    } catch (value) {
      if (mounted) setState(() => error = value.toString());
    } finally {
      if (mounted) setState(() => busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    body: Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(AppSpacing.xl),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 440),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(AppSpacing.xl),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(
                    'Đổi mật khẩu tạm',
                    style: Theme.of(context).textTheme.headlineSmall,
                  ),
                  const SizedBox(height: AppSpacing.xs),
                  const Text(
                    'Bạn phải đổi mật khẩu trước khi truy cập nghiệp vụ.',
                  ),
                  const SizedBox(height: AppSpacing.lg),
                  TextField(
                    controller: current,
                    obscureText: true,
                    decoration: const InputDecoration(
                      labelText: 'Mật khẩu hiện tại',
                    ),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  TextField(
                    controller: next,
                    obscureText: true,
                    decoration: const InputDecoration(
                      labelText: 'Mật khẩu mới',
                    ),
                  ),
                  const SizedBox(height: AppSpacing.sm),
                  TextField(
                    controller: confirm,
                    obscureText: true,
                    decoration: const InputDecoration(
                      labelText: 'Xác nhận mật khẩu mới',
                    ),
                  ),
                  if (error != null)
                    Padding(
                      padding: const EdgeInsets.only(top: AppSpacing.sm),
                      child: Text(
                        error!,
                        style: TextStyle(
                          color: Theme.of(context).colorScheme.error,
                        ),
                      ),
                    ),
                  const SizedBox(height: AppSpacing.lg),
                  FilledButton(
                    onPressed: busy ? null : submit,
                    child: const Text('Đổi mật khẩu'),
                  ),
                  TextButton(
                    onPressed: busy ? null : widget.session.logout,
                    child: const Text('Đăng xuất'),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    ),
  );
}

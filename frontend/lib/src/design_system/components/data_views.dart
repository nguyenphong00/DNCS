import 'package:flutter/material.dart';

import '../../ui/responsive.dart';
import '../theme/app_theme.dart';
import 'feedback.dart';

class FilterBar extends StatelessWidget {
  const FilterBar({
    super.key,
    required this.searchController,
    required this.onSearch,
    this.hintText = 'Tìm kiếm',
    this.filters = const [],
    this.onClear,
  });

  final TextEditingController searchController;
  final ValueChanged<String> onSearch;
  final String hintText;
  final List<Widget> filters;
  final VoidCallback? onClear;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final search = TextField(
        controller: searchController,
        textInputAction: TextInputAction.search,
        onSubmitted: onSearch,
        decoration: InputDecoration(
          labelText: hintText,
          prefixIcon: const Icon(Icons.search),
          suffixIcon: searchController.text.isEmpty
              ? null
              : IconButton(
                  tooltip: 'Xóa tìm kiếm',
                  onPressed: () {
                    searchController.clear();
                    onSearch('');
                  },
                  icon: const Icon(Icons.close),
                ),
        ),
      );
      if (constraints.maxWidth < compactBreakpoint) {
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            search,
            if (filters.isNotEmpty) ...[
              const SizedBox(height: AppSpacing.sm),
              Wrap(
                spacing: AppSpacing.xs,
                runSpacing: AppSpacing.xs,
                children: filters,
              ),
            ],
            if (onClear != null)
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  onPressed: onClear,
                  child: const Text('Xóa bộ lọc'),
                ),
              ),
          ],
        );
      }
      return Row(
        children: [
          Expanded(flex: 2, child: search),
          if (filters.isNotEmpty) ...[
            const SizedBox(width: AppSpacing.sm),
            Expanded(
              flex: 3,
              child: Wrap(
                alignment: WrapAlignment.end,
                spacing: AppSpacing.xs,
                runSpacing: AppSpacing.xs,
                children: filters,
              ),
            ),
          ],
          if (onClear != null) ...[
            const SizedBox(width: AppSpacing.xs),
            TextButton(onPressed: onClear, child: const Text('Xóa bộ lọc')),
          ],
        ],
      );
    },
  );
}

class DataTableShell extends StatelessWidget {
  const DataTableShell({
    super.key,
    required this.loading,
    required this.columns,
    required this.rows,
    this.error,
    this.onRetry,
    this.mobileCards,
    this.emptyTitle = 'Chưa có dữ liệu',
    this.emptyMessage = 'Không có bản ghi phù hợp.',
    this.dataRowMaxHeight,
    this.resultLimit,
    this.page,
    this.hasMore = false,
    this.onPrevious,
    this.onNext,
  });

  final bool loading;
  final String? error;
  final VoidCallback? onRetry;
  final List<DataColumn> columns;
  final List<DataRow> rows;
  final List<Widget>? mobileCards;
  final String emptyTitle;
  final String emptyMessage;
  final double? dataRowMaxHeight;
  final int? resultLimit;
  final int? page;
  final bool hasMore;
  final VoidCallback? onPrevious;
  final VoidCallback? onNext;

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      AsyncContent(
        loading: loading,
        error: error,
        keepChildOnError: rows.isNotEmpty,
        onRetry: onRetry,
        empty: rows.isEmpty,
        emptyTitle: emptyTitle,
        emptyMessage: emptyMessage,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (page == null &&
                resultLimit != null &&
                rows.length >= resultLimit!)
              Padding(
                padding: const EdgeInsets.only(bottom: 12),
                child: InlineMessage(
                  message:
                      'Đang hiện $resultLimit dòng đầu, hãy lọc hoặc tìm thêm.',
                  tone: AppStatusTone.warning,
                ),
              ),
            LayoutBuilder(
              builder: (context, constraints) {
                final largeText =
                    MediaQuery.textScalerOf(context).scale(14) > 21;
                if (MediaQuery.sizeOf(context).width < 1366 ||
                    constraints.maxWidth < compactBreakpoint ||
                    largeText) {
                  final cards = !largeText && mobileCards != null
                      ? mobileCards!
                      : [
                          for (final row in rows)
                            _RowCard(columns: columns, row: row),
                        ];
                  return Column(
                    children: [
                      for (var i = 0; i < cards.length; i++) ...[
                        cards[i],
                        if (i < cards.length - 1)
                          const SizedBox(height: AppSpacing.sm),
                      ],
                    ],
                  );
                }
                return _HorizontalDataTable(
                  columns: columns,
                  rows: rows,
                  dataRowMaxHeight: dataRowMaxHeight,
                );
              },
            ),
          ],
        ),
      ),
      if (page != null) ...[
        const SizedBox(height: AppSpacing.md),
        Wrap(
          alignment: WrapAlignment.center,
          crossAxisAlignment: WrapCrossAlignment.center,
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.xs,
          children: [
            OutlinedButton.icon(
              onPressed: loading || page! <= 1 ? null : onPrevious,
              icon: const Icon(Icons.chevron_left),
              label: const Text('Trước'),
            ),
            Semantics(liveRegion: true, child: Text('Trang $page')),
            OutlinedButton.icon(
              onPressed: loading || !hasMore ? null : onNext,
              icon: const Icon(Icons.chevron_right),
              label: const Text('Tiếp'),
            ),
          ],
        ),
      ],
    ],
  );
}

// A table's labels and actions remain available when no custom compact card is
// supplied, and at large text sizes where ListTile's trailing slot is too narrow.
class _RowCard extends StatelessWidget {
  const _RowCard({required this.columns, required this.row});
  final List<DataColumn> columns;
  final DataRow row;
  @override
  Widget build(BuildContext context) => Card(
    child: InkWell(
      onTap: row.onSelectChanged == null
          ? null
          : () => row.onSelectChanged!(!row.selected),
      borderRadius: BorderRadius.circular(AppRadius.medium),
      child: Padding(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (var i = 0; i < columns.length; i++)
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    DefaultTextStyle.merge(
                      style: Theme.of(context).textTheme.labelMedium,
                      child: columns[i].label,
                    ),
                    const SizedBox(height: AppSpacing.xxs),
                    if (row.cells[i].onTap != null)
                      InkWell(
                        onTap: row.cells[i].onTap,
                        child: row.cells[i].child,
                      )
                    else
                      row.cells[i].child,
                  ],
                ),
              ),
          ],
        ),
      ),
    ),
  );
}

class DetailField extends StatelessWidget {
  const DetailField({
    super.key,
    required this.label,
    required this.value,
    this.status = false,
  });

  final String label;
  final String value;
  final bool status;

  @override
  Widget build(BuildContext context) => ConstrainedBox(
    constraints: const BoxConstraints(minWidth: 180, maxWidth: 320),
    child: Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: Theme.of(
              context,
            ).textTheme.labelMedium?.copyWith(color: AppColors.textMuted),
          ),
          const SizedBox(height: AppSpacing.xxs),
          if (status)
            StatusBadge(status: value)
          else
            SelectableText(
              value.isEmpty ? '—' : value,
              // Expose the value as text: Flutter Web's readonly textbox
              // semantics otherwise omit it from the browser accessibility tree.
              semanticsLabel: value.isEmpty ? '—' : value,
              style: Theme.of(context).textTheme.bodyLarge,
            ),
        ],
      ),
    ),
  );
}

class _HorizontalDataTable extends StatefulWidget {
  const _HorizontalDataTable({
    required this.columns,
    required this.rows,
    this.dataRowMaxHeight,
  });
  final List<DataColumn> columns;
  final List<DataRow> rows;
  final double? dataRowMaxHeight;
  @override
  State<_HorizontalDataTable> createState() => _HorizontalDataTableState();
}

class _HorizontalDataTableState extends State<_HorizontalDataTable> {
  final controller = ScrollController();
  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Scrollbar(
    controller: controller,
    thumbVisibility: true,
    child: SingleChildScrollView(
      controller: controller,
      scrollDirection: Axis.horizontal,
      child: DataTable(
        columnSpacing: 24,
        dataRowMaxHeight: widget.dataRowMaxHeight,
        columns: widget.columns,
        rows: widget.rows,
      ),
    ),
  );
}

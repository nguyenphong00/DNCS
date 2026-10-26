import 'package:flutter/material.dart';

import '../../ui/responsive.dart';
import '../theme/app_theme.dart';

class AppPageScaffold extends StatelessWidget {
  const AppPageScaffold({
    super.key,
    required this.title,
    required this.children,
    this.description,
    this.actions = const [],
    this.maxWidth = 1440,
    this.breadcrumbs = const [],
  });

  final String title;
  final String? description;
  final List<Widget> actions;
  final List<Widget> children;
  final double maxWidth;
  final List<String> breadcrumbs;

  @override
  Widget build(BuildContext context) => Center(
    child: ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth),
      child: ListView(
        padding: pagePadding(context),
        children: [
          PageHeader(
            title: title,
            description: description,
            actions: actions,
            breadcrumbs: breadcrumbs,
          ),
          const SizedBox(height: AppSpacing.xl),
          ..._separate(children, const SizedBox(height: AppSpacing.md)),
        ],
      ),
    ),
  );
}

class PageHeader extends StatelessWidget {
  const PageHeader({
    super.key,
    required this.title,
    this.description,
    this.actions = const [],
    this.breadcrumbs = const [],
  });

  final String title;
  final String? description;
  final List<Widget> actions;
  final List<String> breadcrumbs;

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final compact =
          constraints.maxWidth < 720 ||
          MediaQuery.textScalerOf(context).scale(14) > 21;
      final heading = Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (breadcrumbs.isNotEmpty) ...[
            Text(
              breadcrumbs.join(' / '),
              style: Theme.of(
                context,
              ).textTheme.bodySmall?.copyWith(color: AppColors.textMuted),
            ),
            const SizedBox(height: AppSpacing.xs),
          ],
          Semantics(
            header: true,
            child: Text(
              title,
              style: compact
                  ? Theme.of(context).textTheme.headlineMedium
                  : Theme.of(context).textTheme.headlineLarge,
            ),
          ),
          if (description != null && description!.isNotEmpty) ...[
            const SizedBox(height: AppSpacing.xs),
            Text(
              description!,
              style: Theme.of(
                context,
              ).textTheme.bodyLarge?.copyWith(color: AppColors.textSecondary),
            ),
          ],
        ],
      );
      if (compact || actions.isEmpty) {
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            heading,
            if (actions.isNotEmpty) ...[
              const SizedBox(height: AppSpacing.md),
              Wrap(
                spacing: AppSpacing.xs,
                runSpacing: AppSpacing.xs,
                children: actions,
              ),
            ],
          ],
        );
      }
      return Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(child: heading),
          const SizedBox(width: AppSpacing.xl),
          Wrap(
            spacing: AppSpacing.xs,
            runSpacing: AppSpacing.xs,
            children: actions,
          ),
        ],
      );
    },
  );
}

class SectionCard extends StatelessWidget {
  const SectionCard({
    super.key,
    required this.child,
    this.title,
    this.description,
    this.trailing,
    this.padding = const EdgeInsets.all(AppSpacing.xl),
  });

  final String? title;
  final String? description;
  final Widget? trailing;
  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) => Card(
    child: Padding(
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (title != null || trailing != null) ...[
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      if (title != null)
                        Text(
                          title!,
                          style: Theme.of(context).textTheme.titleLarge,
                        ),
                      if (description != null) ...[
                        const SizedBox(height: AppSpacing.xxs),
                        Text(
                          description!,
                          style: Theme.of(context).textTheme.bodyMedium
                              ?.copyWith(color: AppColors.textSecondary),
                        ),
                      ],
                    ],
                  ),
                ),
                if (trailing != null) ...[
                  const SizedBox(width: AppSpacing.md),
                  trailing!,
                ],
              ],
            ),
            const SizedBox(height: AppSpacing.lg),
          ],
          child,
        ],
      ),
    ),
  );
}

List<Widget> _separate(List<Widget> values, Widget separator) {
  if (values.length < 2) return values;
  return [
    for (var index = 0; index < values.length; index++) ...[
      values[index],
      if (index < values.length - 1) separator,
    ],
  ];
}

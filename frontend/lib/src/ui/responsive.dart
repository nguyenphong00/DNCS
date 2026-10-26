import 'package:flutter/material.dart';

const double compactBreakpoint = 720;
const double desktopBreakpoint = 960;
const double wideBreakpoint = 1240;

bool isDesktop(BuildContext context) =>
    MediaQuery.sizeOf(context).width /
        (MediaQuery.textScalerOf(context).scale(14) / 14) >=
    desktopBreakpoint;

EdgeInsets pagePadding(BuildContext context) {
  final width = MediaQuery.sizeOf(context).width;
  if (width >= wideBreakpoint) return const EdgeInsets.all(36);
  if (width >= compactBreakpoint) return const EdgeInsets.all(28);
  return const EdgeInsets.all(16);
}

class ResponsivePage extends StatelessWidget {
  const ResponsivePage({super.key, required this.child, this.maxWidth = 1440});

  final Widget child;
  final double maxWidth;

  @override
  Widget build(BuildContext context) => Center(
    child: ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth),
      child: child,
    ),
  );
}

import 'package:flutter/material.dart';

abstract final class AppColors {
  static const brand = Color(0xFF006A60);
  static const brandHover = Color(0xFF005A52);
  static const brandPressed = Color(0xFF004A44);
  static const page = Color(0xFFF6F8F8);
  static const surface = Colors.white;
  static const surfaceSubtle = Color(0xFFEEF3F2);
  static const border = Color(0xFFD8E0DE);
  static const borderStrong = Color(0xFFAAB8B5);
  static const text = Color(0xFF17201E);
  static const textSecondary = Color(0xFF4D5C59);
  static const textMuted = Color(0xFF6D7B78);
  static const textDisabled = Color(0xFF9AA5A3);
  static const info = Color(0xFF0B57D0);
  static const infoContainer = Color(0xFFE8F0FE);
  static const success = Color(0xFF137333);
  static const successContainer = Color(0xFFE6F4EA);
  static const warning = Color(0xFF7A4700);
  static const warningContainer = Color(0xFFFFF4D6);
  static const danger = Color(0xFFB3261E);
  static const dangerContainer = Color(0xFFFCE8E6);
  static const clinical = Color(0xFF6750A4);
  static const clinicalContainer = Color(0xFFF0EAFB);
}

abstract final class AppSpacing {
  static const xxs = 4.0;
  static const xs = 8.0;
  static const sm = 12.0;
  static const md = 16.0;
  static const lg = 20.0;
  static const xl = 24.0;
  static const xxl = 32.0;
  static const pageWide = 36.0;
  static const section = 48.0;
}

abstract final class AppRadius {
  static const small = 8.0;
  static const medium = 12.0;
  static const large = 16.0;
  static const card = 20.0;
  static const pill = 999.0;
}

abstract final class AppTheme {
  static ThemeData get light {
    final scheme = ColorScheme.fromSeed(
      seedColor: AppColors.brand,
      brightness: Brightness.light,
      surface: AppColors.surface,
      error: AppColors.danger,
    );
    const textTheme = TextTheme(
      displaySmall: TextStyle(
        fontSize: 32,
        height: 1.25,
        fontWeight: FontWeight.w700,
      ),
      headlineLarge: TextStyle(
        fontSize: 28,
        height: 1.29,
        fontWeight: FontWeight.w700,
      ),
      headlineMedium: TextStyle(
        fontSize: 24,
        height: 1.33,
        fontWeight: FontWeight.w700,
      ),
      headlineSmall: TextStyle(
        fontSize: 22,
        height: 1.27,
        fontWeight: FontWeight.w700,
      ),
      titleLarge: TextStyle(
        fontSize: 18,
        height: 1.33,
        fontWeight: FontWeight.w600,
      ),
      titleMedium: TextStyle(
        fontSize: 16,
        height: 1.38,
        fontWeight: FontWeight.w600,
      ),
      bodyLarge: TextStyle(fontSize: 14, height: 1.43),
      bodyMedium: TextStyle(fontSize: 14, height: 1.43),
      bodySmall: TextStyle(fontSize: 12, height: 1.33),
      labelLarge: TextStyle(
        fontSize: 13,
        height: 1.38,
        fontWeight: FontWeight.w600,
      ),
      labelMedium: TextStyle(
        fontSize: 12,
        height: 1.33,
        fontWeight: FontWeight.w500,
      ),
    );

    final controlShape = RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(AppRadius.medium),
    );
    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      fontFamily: 'Roboto',
      textTheme: textTheme.apply(
        bodyColor: AppColors.text,
        displayColor: AppColors.text,
      ),
      scaffoldBackgroundColor: AppColors.page,
      visualDensity: VisualDensity.standard,
      appBarTheme: const AppBarTheme(
        centerTitle: false,
        scrolledUnderElevation: 0,
        elevation: 0,
        backgroundColor: AppColors.page,
        foregroundColor: AppColors.text,
      ),
      cardTheme: CardThemeData(
        color: AppColors.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.card),
          side: const BorderSide(color: AppColors.border),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        border: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.medium),
          borderSide: const BorderSide(color: AppColors.border),
        ),
        enabledBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.medium),
          borderSide: const BorderSide(color: AppColors.border),
        ),
        focusedBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.medium),
          borderSide: const BorderSide(color: AppColors.brand, width: 2),
        ),
        errorBorder: OutlineInputBorder(
          borderRadius: BorderRadius.circular(AppRadius.medium),
          borderSide: const BorderSide(color: AppColors.danger),
        ),
        filled: true,
        fillColor: AppColors.surface,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md,
          vertical: AppSpacing.md,
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: const Size(48, 48),
          shape: controlShape,
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.lg),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: const Size(48, 48),
          shape: controlShape,
          side: const BorderSide(color: AppColors.borderStrong),
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.lg),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          minimumSize: const Size(48, 48),
          shape: controlShape,
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(minimumSize: const Size.square(48)),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: AppColors.surface,
        surfaceTintColor: Colors.transparent,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.large),
        ),
      ),
      dividerTheme: const DividerThemeData(
        color: AppColors.border,
        thickness: 1,
        space: 1,
      ),
      chipTheme: ChipThemeData(
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.pill),
          side: const BorderSide(color: AppColors.border),
        ),
      ),
      snackBarTheme: const SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        showCloseIcon: true,
      ),
      navigationRailTheme: const NavigationRailThemeData(
        backgroundColor: AppColors.surface,
        indicatorColor: AppColors.surfaceSubtle,
        minWidth: 76,
      ),
      navigationBarTheme: const NavigationBarThemeData(
        backgroundColor: AppColors.surface,
        indicatorColor: AppColors.surfaceSubtle,
        height: 72,
      ),
      dataTableTheme: const DataTableThemeData(
        headingRowColor: WidgetStatePropertyAll(AppColors.surfaceSubtle),
        dividerThickness: 1,
        dataRowMinHeight: 48,
        headingRowHeight: 48,
      ),
      progressIndicatorTheme: const ProgressIndicatorThemeData(
        color: AppColors.brand,
      ),
    );
  }
}

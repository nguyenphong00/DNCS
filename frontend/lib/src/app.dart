import 'package:flutter/material.dart';

import 'design_system/theme/app_theme.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';
import 'session_controller.dart';

class DncsApp extends StatelessWidget {
  const DncsApp({super.key, required this.session});

  final SessionController session;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'DNCS Clinic',
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light,
      home: ListenableBuilder(
        listenable: session,
        builder: (context, _) {
          if (!session.initialized) {
            return const Scaffold(
              body: Center(child: CircularProgressIndicator()),
            );
          }
          if (!session.isAuthenticated) return LoginScreen(session: session);
          return HomeScreen(session: session);
        },
      ),
    );
  }
}

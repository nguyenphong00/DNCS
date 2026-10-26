import 'package:flutter/material.dart';

import 'src/app.dart';
import 'src/session_controller.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final session = SessionController(
    baseUrl: const String.fromEnvironment(
      'API_BASE_URL',
      defaultValue: 'http://localhost:8080',
    ),
  );
  await session.initialize();
  runApp(DncsApp(session: session));
}

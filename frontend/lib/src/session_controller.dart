import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

class ApiException implements Exception {
  ApiException(
    this.statusCode,
    this.code,
    this.message, {
    this.requestId,
    this.fields = const {},
  });
  final int statusCode;
  final String code;
  final String message;
  final String? requestId;
  final Map<String, String> fields;
  @override
  String toString() => message;
}

class SessionController extends ChangeNotifier {
  SessionController({
    required String baseUrl,
    FlutterSecureStorage? storage,
    http.Client? client,
  }) : baseUri = Uri.parse(baseUrl.replaceAll(RegExp(r'/$'), '')),
       _storage = storage ?? const FlutterSecureStorage(),
       _client = client ?? http.Client();

  static const _accessKey = 'dncs_access_token';
  static const _refreshKey = 'dncs_refresh_token';
  static const _mustChangeKey = 'dncs_must_change_password';
  static const _accountTypeKey = 'dncs_account_type';
  final Uri baseUri;
  final FlutterSecureStorage _storage;
  final http.Client _client;

  String? _accessToken;
  String? _refreshToken;
  Map<String, dynamic>? principal;
  Set<String> permissions = <String>{};
  bool initialized = false;

  bool get isAuthenticated => _accessToken != null && principal != null;
  bool get mustChangePassword => principal?['must_change_password'] == true;
  String get roleCode => principal?['role_code']?.toString() ?? '';
  String get accountType => principal?['account_type']?.toString() ?? '';

  Future<void> initialize() async {
    _accessToken = await _storage.read(key: _accessKey);
    _refreshToken = await _storage.read(key: _refreshKey);
    final storedAccountType = await _storage.read(key: _accountTypeKey);
    final storedMustChange = await _storage.read(key: _mustChangeKey) == 'true';
    if (_accessToken != null) {
      if (storedMustChange) {
        _setPasswordChangePrincipal(storedAccountType ?? 'STAFF');
      } else {
        try {
          await loadMe();
        } catch (_) {
          try {
            await _refresh();
            if (mustChangePassword) {
              _setPasswordChangePrincipal(storedAccountType ?? 'STAFF');
            } else {
              await loadMe();
            }
          } catch (_) {
            await _clear();
          }
        }
      }
    }
    initialized = true;
    notifyListeners();
  }

  Future<void> login({
    required bool staff,
    required String identifier,
    required String password,
  }) async {
    final data = await requestJson(
      'POST',
      staff ? '/api/v1/auth/staff/login' : '/api/v1/auth/patient/login',
      authenticated: false,
      body: staff
          ? {'username': identifier, 'password': password}
          : {'phone_number': identifier, 'password': password},
    );
    final accountType = staff ? 'STAFF' : 'PATIENT';
    await _acceptTokens(data, accountType: accountType);
    if (data['must_change_password'] == true) {
      _setPasswordChangePrincipal(accountType);
      notifyListeners();
    } else {
      await loadMe();
    }
  }

  Future<void> registerPatient(Map<String, dynamic> body) async {
    final data = await requestJson(
      'POST',
      '/api/v1/auth/patient/register',
      authenticated: false,
      body: body,
    );
    await _acceptTokens(
      Map<String, dynamic>.from(data['tokens'] as Map),
      accountType: 'PATIENT',
    );
    await loadMe();
  }

  Future<void> changePassword(
    String currentPassword,
    String newPassword,
  ) async {
    final data = await requestJson(
      'POST',
      '/api/v1/auth/change-password',
      body: {'current_password': currentPassword, 'new_password': newPassword},
    );
    await _acceptTokens(data);
    await loadMe();
  }

  Future<void> loadMe() async {
    final data = await requestJson('GET', '/api/v1/me');
    principal = Map<String, dynamic>.from(data['principal'] as Map);
    permissions = (data['permissions'] as List<dynamic>? ?? const [])
        .map((item) => item.toString())
        .toSet();
    await _storage.write(key: _accountTypeKey, value: accountType);
    await _storage.write(
      key: _mustChangeKey,
      value: mustChangePassword.toString(),
    );
    notifyListeners();
  }

  Future<void> logout() async {
    final refresh = _refreshToken;
    if (refresh != null) {
      try {
        await requestJson(
          'POST',
          '/api/v1/auth/logout',
          authenticated: false,
          body: {'refresh_token': refresh},
        );
      } catch (_) {
        // Local logout remains available when the API is unreachable.
      }
    }
    await _clear();
    notifyListeners();
  }

  Future<Map<String, dynamic>> requestJson(
    String method,
    String path, {
    Map<String, dynamic>? body,
    Map<String, String>? query,
    Map<String, String>? headers,
    bool authenticated = true,
    bool retryOnUnauthorized = true,
  }) async {
    final uri = baseUri.replace(
      path: '${baseUri.path}$path',
      queryParameters: query?.isEmpty == true ? null : query,
    );
    final requestHeaders = <String, String>{
      'Accept': 'application/json',
      'Content-Type': 'application/json',
      'X-Request-ID': _uuid(),
      ...?headers,
    };
    if (authenticated && _accessToken != null) {
      requestHeaders['Authorization'] = 'Bearer $_accessToken';
    }
    final response = await _send(method, uri, requestHeaders, body);
    if (response.statusCode == 401 &&
        authenticated &&
        retryOnUnauthorized &&
        _refreshToken != null) {
      await _refresh();
      return requestJson(
        method,
        path,
        body: body,
        query: query,
        headers: headers,
        retryOnUnauthorized: false,
      );
    }
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw _decodeError(response);
    }
    if (response.statusCode == 204 || response.body.trim().isEmpty) {
      return <String, dynamic>{};
    }
    final decoded = jsonDecode(response.body);
    return decoded is Map<String, dynamic>
        ? decoded
        : <String, dynamic>{'items': decoded};
  }

  Future<http.Response> _send(
    String method,
    Uri uri,
    Map<String, String> headers,
    Map<String, dynamic>? body,
  ) {
    final encoded = body == null ? null : jsonEncode(body);
    return switch (method) {
      'GET' => _client.get(uri, headers: headers),
      'POST' => _client.post(uri, headers: headers, body: encoded),
      'PATCH' => _client.patch(uri, headers: headers, body: encoded),
      'PUT' => _client.put(uri, headers: headers, body: encoded),
      'DELETE' => _client.delete(uri, headers: headers, body: encoded),
      _ => throw ArgumentError.value(method, 'method'),
    };
  }

  Future<void> _refresh() async {
    final refresh = _refreshToken;
    if (refresh == null) {
      throw ApiException(401, 'AUTH_REQUIRED', 'Yêu cầu đăng nhập.');
    }
    final data = await requestJson(
      'POST',
      '/api/v1/auth/refresh',
      authenticated: false,
      body: {'refresh_token': refresh},
    );
    await _acceptTokens(data, accountType: accountType);
  }

  Future<void> _acceptTokens(
    Map<String, dynamic> data, {
    String? accountType,
  }) async {
    final access = data['access_token']?.toString();
    final refresh = data['refresh_token']?.toString();
    if (access == null || refresh == null) {
      throw ApiException(
        500,
        'INVALID_TOKEN_RESPONSE',
        'API trả token không hợp lệ.',
      );
    }
    _accessToken = access;
    _refreshToken = refresh;
    await _storage.write(key: _accessKey, value: access);
    await _storage.write(key: _refreshKey, value: refresh);
    if (accountType != null && accountType.isNotEmpty) {
      await _storage.write(key: _accountTypeKey, value: accountType);
    }
    await _storage.write(
      key: _mustChangeKey,
      value: (data['must_change_password'] == true).toString(),
    );
  }

  Future<void> _clear() async {
    _accessToken = null;
    _refreshToken = null;
    principal = null;
    permissions = <String>{};
    await _storage.delete(key: _accessKey);
    await _storage.delete(key: _refreshKey);
    await _storage.delete(key: _mustChangeKey);
    await _storage.delete(key: _accountTypeKey);
  }

  void _setPasswordChangePrincipal(String accountType) {
    principal = <String, dynamic>{
      'account_type': accountType,
      'role_code': '',
      'must_change_password': true,
    };
    permissions = <String>{};
  }

  ApiException _decodeError(http.Response response) {
    try {
      final body = jsonDecode(response.body) as Map<String, dynamic>;
      final error = Map<String, dynamic>.from(body['error'] as Map);
      return ApiException(
        response.statusCode,
        error['code']?.toString() ?? 'HTTP_ERROR',
        error['message']?.toString() ?? 'Không thể xử lý yêu cầu.',
        requestId: body['request_id']?.toString(),
        fields: error['fields'] is Map
            ? (error['fields'] as Map).map(
                (key, value) => MapEntry(key.toString(), value.toString()),
              )
            : const {},
      );
    } catch (_) {
      return ApiException(
        response.statusCode,
        'HTTP_ERROR',
        'Không thể kết nối dịch vụ.',
      );
    }
  }

  String _uuid() {
    final random = Random.secure();
    final bytes = List<int>.generate(16, (_) => random.nextInt(256));
    bytes[6] = (bytes[6] & 0x0f) | 0x40;
    bytes[8] = (bytes[8] & 0x3f) | 0x80;
    final hex = bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
    return '${hex.substring(0, 8)}-${hex.substring(8, 12)}-${hex.substring(12, 16)}-${hex.substring(16, 20)}-${hex.substring(20)}';
  }
}

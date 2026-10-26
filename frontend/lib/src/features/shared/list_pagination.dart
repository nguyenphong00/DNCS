import 'package:flutter/foundation.dart';

/// Keeps applied filters and page position until a request succeeds. Stale
/// responses (including after disposal) must not replace the current list.
class ListPagination {
  ListPagination({
    this.cursor = false,
    this.pageSize = 100,
    this.sizeParameter,
  });
  final bool cursor;
  final int pageSize;
  final String? sizeParameter;
  int page = 1;
  bool hasMore = false;
  int _generation = 0;
  Map<String, String> _filters = {};
  List<String> _cursors = [''];

  PageRequest start(Map<String, String> filters, {int? targetPage}) {
    final applied = targetPage == null ? filters : _filters;
    final reset = !mapEquals(applied, _filters);
    final requested = targetPage ?? (reset ? 1 : page);
    return PageRequest(++_generation, requested, Map.of(applied), {
      ...applied,
      sizeParameter ?? (cursor ? 'limit' : 'page_size'): '$pageSize',
      if (!cursor) 'page': '$requested',
      if (cursor && requested > 1) 'cursor': _cursors[requested - 1],
    }, reset);
  }

  bool isCurrent(PageRequest request) => request.generation == _generation;
  bool accept(PageRequest request, Map<String, dynamic> response) {
    if (!isCurrent(request)) return false;
    page = request.page;
    _filters = request.filters;
    if (cursor) {
      if (request.reset) _cursors = [''];
      final next = response['next_cursor']?.toString() ?? '';
      hasMore = next.isNotEmpty;
      _cursors = _cursors.take(page).toList();
      if (hasMore) _cursors.add(next);
    } else {
      hasMore = response['has_more'] == true;
    }
    return true;
  }

  void dispose() => _generation++;
}

class PageRequest {
  const PageRequest(
    this.generation,
    this.page,
    this.filters,
    this.query,
    this.reset,
  );
  final int generation;
  final int page;
  final Map<String, String> filters;
  final Map<String, String> query;
  final bool reset;
}

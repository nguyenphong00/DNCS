import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import '../theme/app_theme.dart';

class EntityOption {
  const EntityOption({
    required this.id,
    required this.label,
    this.subtitle = '',
  });
  final int id;
  final String label;
  final String subtitle;
}

/// A selected identity is cleared whenever the search changes. Old responses
/// never overwrite newer input, including after selection or disposal.
class EntityPicker extends StatefulWidget {
  const EntityPicker({
    super.key,
    required this.label,
    required this.search,
    required this.onSelected,
    this.initial,
    this.enabled = true,
    this.validator,
    this.errorText,
  });
  final String label;
  final Future<List<EntityOption>> Function(String) search;
  final ValueChanged<EntityOption?> onSelected;
  final EntityOption? initial;
  final bool enabled;
  final FormFieldValidator<String>? validator;
  final String? errorText;
  @override
  State<EntityPicker> createState() => _EntityPickerState();
}

class _EntityPickerState extends State<EntityPicker> {
  late final TextEditingController controller;
  final inputFocus = FocusNode();
  final scroll = ScrollController();
  int active = -1;
  Timer? timer;
  int generation = 0;
  List<EntityOption> options = [];
  bool busy = false;
  String? error;
  @override
  void initState() {
    super.initState();
    controller = TextEditingController(text: widget.initial?.label ?? '');
  }

  @override
  void dispose() {
    timer?.cancel();
    generation++;
    inputFocus.dispose();
    scroll.dispose();
    controller.dispose();
    super.dispose();
  }

  void changed(String value) {
    timer?.cancel();
    final current = ++generation;
    widget.onSelected(null);
    setState(() {
      options = [];
      active = -1;
      error = null;
      busy = true;
    });
    timer = Timer(const Duration(milliseconds: 400), () async {
      try {
        final result = await widget.search(value.trim());
        if (mounted && generation == current) {
          setState(() {
            options = result;
            busy = false;
          });
        }
      } catch (_) {
        if (mounted && generation == current) {
          setState(() {
            error = 'Không tải được danh sách. Nhập lại để thử.';
            busy = false;
          });
        }
      }
    });
  }

  void select(EntityOption option) {
    timer?.cancel();
    generation++;
    setState(() {
      controller.text = option.label;
      options = [];
      active = -1;
      busy = false;
    });
    widget.onSelected(option);
    inputFocus.requestFocus();
  }

  KeyEventResult key(KeyEvent event) {
    if (event is! KeyDownEvent || !widget.enabled) {
      return KeyEventResult.ignored;
    }
    if (event.logicalKey == LogicalKeyboardKey.escape &&
        (options.isNotEmpty || busy)) {
      timer?.cancel();
      generation++;
      setState(() {
        options = [];
        active = -1;
        busy = false;
      });
      return KeyEventResult.handled;
    }
    if (options.isEmpty) return KeyEventResult.ignored;
    if (event.logicalKey == LogicalKeyboardKey.arrowDown ||
        event.logicalKey == LogicalKeyboardKey.arrowUp) {
      setState(() {
        final down = event.logicalKey == LogicalKeyboardKey.arrowDown;
        active = active < 0
            ? (down ? 0 : options.length - 1)
            : (active + (down ? 1 : -1)) % options.length;
      });
      if (scroll.hasClients) {
        final offset = (active * 72.0).clamp(
          0.0,
          scroll.position.maxScrollExtent,
        );
        scroll.jumpTo(offset);
      }
      return KeyEventResult.handled;
    }
    if (event.logicalKey == LogicalKeyboardKey.enter && active >= 0) {
      select(options[active]);
      return KeyEventResult.handled;
    }
    return KeyEventResult.ignored;
  }

  @override
  Widget build(BuildContext context) => Column(
    mainAxisSize: MainAxisSize.min,
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Focus(
        onKeyEvent: (_, event) => key(event),
        child: TextFormField(
          focusNode: inputFocus,
          controller: controller,
          enabled: widget.enabled,
          validator: widget.validator,
          onChanged: changed,
          onTap: () {
            if (controller.text.isEmpty) changed('');
          },
          decoration: InputDecoration(
            labelText: widget.label,
            prefixIcon: const Icon(Icons.search),
            errorText: error ?? widget.errorText,
            suffixIcon: busy
                ? const Padding(
                    padding: EdgeInsets.all(AppSpacing.sm),
                    child: SizedBox.square(
                      dimension: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  )
                : null,
          ),
        ),
      ),
      if (busy || options.isNotEmpty)
        Semantics(
          liveRegion: true,
          child: Text(
            busy
                ? 'Đang tìm…'
                : options.isEmpty
                ? ''
                : '${options.length} kết quả. ${active >= 0 ? 'Đang chọn ${options[active].label}. ' : ''}Dùng phím lên/xuống và Enter để chọn.',
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ),
      if (options.isNotEmpty)
        ConstrainedBox(
          constraints: const BoxConstraints(maxHeight: 180),
          child: ListView(
            controller: scroll,
            shrinkWrap: true,
            children: [
              for (var i = 0; i < options.length; i++)
                Semantics(
                  selected: i == active,
                  button: true,
                  child: ListTile(
                    selected: i == active,
                    title: Text(options[i].label),
                    subtitle: options[i].subtitle.isEmpty
                        ? null
                        : Text(options[i].subtitle),
                    onTap: () => select(options[i]),
                  ),
                ),
            ],
          ),
        ),
    ],
  );
}

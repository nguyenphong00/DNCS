import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

DateTime clinicNow([DateTime? instant]) =>
    (instant ?? DateTime.now()).toUtc().add(const Duration(hours: 7));
String clinicDate(DateTime date) =>
    '${date.year.toString().padLeft(4, '0')}-${date.month.toString().padLeft(2, '0')}-${date.day.toString().padLeft(2, '0')}';
String clinicDayEnd(DateTime date) =>
    '${clinicDate(date)}T23:59:59.999999+07:00';

String clinicTimestamp(DateTime date, TimeOfDay time) =>
    '${clinicDate(date)}T${time.hour.toString().padLeft(2, '0')}:${time.minute.toString().padLeft(2, '0')}:00+07:00';

class DateField extends StatelessWidget {
  const DateField({
    super.key,
    required this.controller,
    required this.label,
    this.onChanged,
    this.errorText,
    this.firstDate,
    this.lastDate,
  });
  final TextEditingController controller;
  final String label;
  final ValueChanged<String>? onChanged;
  final String? errorText;
  final DateTime? firstDate;
  final DateTime? lastDate;
  @override
  Widget build(BuildContext context) =>
      ValueListenableBuilder<TextEditingValue>(
        valueListenable: controller,
        builder: (context, value, _) {
          Future<void> pick() async {
            final first = firstDate ?? DateTime(1900);
            final last = lastDate ?? DateTime(2100);
            var initial = DateTime.tryParse(controller.text) ?? clinicNow();
            if (initial.isBefore(first)) initial = first;
            if (initial.isAfter(last)) initial = last;
            final selected = await showDatePicker(
              context: context,
              initialDate: initial,
              firstDate: first,
              lastDate: last,
            );
            if (selected != null && context.mounted) {
              controller.text = clinicDate(selected);
              onChanged?.call(controller.text);
            }
          }

          return Focus(
            onKeyEvent: (_, event) {
              if (event is KeyDownEvent &&
                  (event.logicalKey == LogicalKeyboardKey.enter ||
                      event.logicalKey == LogicalKeyboardKey.space)) {
                pick();
                return KeyEventResult.handled;
              }
              return KeyEventResult.ignored;
            },
            child: TextFormField(
              key: ValueKey(value.text),
              initialValue: displayClinicDate(value.text),
              readOnly: true,
              decoration: InputDecoration(
                labelText: label,
                errorText: errorText,
                suffixIcon: IconButton(
                  tooltip: 'Chọn $label',
                  onPressed: pick,
                  icon: const Icon(Icons.calendar_today),
                ),
              ),
              onTap: pick,
            ),
          );
        },
      );
}

class DateTimeField extends StatelessWidget {
  const DateTimeField({
    super.key,
    required this.controller,
    required this.label,
    this.onChanged,
    this.errorText,
  });
  final TextEditingController controller;
  final String label;
  final ValueChanged<String>? onChanged;
  final String? errorText;
  @override
  Widget build(BuildContext context) =>
      ValueListenableBuilder<TextEditingValue>(
        valueListenable: controller,
        builder: (context, value, _) {
          Future<void> pick() async {
            final parsed = DateTime.tryParse(controller.text);
            final initial = parsed == null ? clinicNow() : clinicNow(parsed);
            final date = await showDatePicker(
              context: context,
              initialDate: initial,
              firstDate: DateTime(1900),
              lastDate: DateTime(2100),
            );
            if (date == null || !context.mounted) return;
            final time = await showTimePicker(
              context: context,
              initialTime: TimeOfDay(
                hour: initial.hour,
                minute: initial.minute,
              ),
            );
            if (time != null && context.mounted) {
              controller.text = clinicTimestamp(date, time);
              onChanged?.call(controller.text);
            }
          }

          return Focus(
            onKeyEvent: (_, event) {
              if (event is KeyDownEvent &&
                  (event.logicalKey == LogicalKeyboardKey.enter ||
                      event.logicalKey == LogicalKeyboardKey.space)) {
                pick();
                return KeyEventResult.handled;
              }
              return KeyEventResult.ignored;
            },
            child: TextFormField(
              key: ValueKey(value.text),
              initialValue: displayClinicDateTime(value.text),
              readOnly: true,
              decoration: InputDecoration(
                labelText: label,
                errorText: errorText,
                suffixIcon: IconButton(
                  tooltip: 'Chọn $label',
                  onPressed: pick,
                  icon: const Icon(Icons.schedule),
                ),
              ),
              onTap: pick,
            ),
          );
        },
      );
}

String displayClinicDate(String value) {
  final parsed = DateTime.tryParse(value);
  if (parsed == null) return '';
  return '${parsed.day.toString().padLeft(2, '0')}/${parsed.month.toString().padLeft(2, '0')}/${parsed.year}';
}

String displayClinicDateTime(String value) {
  final parsed = DateTime.tryParse(value);
  if (parsed == null) return '';
  final local = clinicNow(parsed);
  return '${displayClinicDate(clinicDate(local))}, ${local.hour.toString().padLeft(2, '0')}:${local.minute.toString().padLeft(2, '0')}';
}

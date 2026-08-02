# Происхождение и совместимость с IRIS

Проверено 2 августа 2026 года по официальному репозиторию
[versenilvis/IRIS](https://github.com/versenilvis/IRIS), commit
[`d669e97423a7ca9326d17b1289d06ba90942bd77`](https://github.com/versenilvis/IRIS/commit/d669e97423a7ca9326d17b1289d06ba90942bd77).

## Лицензия

IRIS опубликован под
[BSD Zero Clause License (0BSD)](https://github.com/versenilvis/IRIS/blob/main/LICENSE).
Она прямо разрешает использовать, копировать, изменять и распространять код
для любых целей, с оплатой или без неё. Поэтому перенос реализации разрешён.

Metuur сохраняет полный текст 0BSD и точный upstream commit в
[`THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md). Ограничения собственной
Metuur Free Use — No Sale License относятся только к оригинальным частям
Metuur и не отменяют 0BSD-права на IRIS-derived material.

## Что перенесено

- архитектура прозрачной PTY-обёртки: настоящий shell работает постоянно, а
  обычный ввод передаётся ему без задержки;
- логика клавиш `Tab`, `Shift+Tab`, `Esc`, `Ctrl+R`, стрелок и ghost text;
- структура inline-overlay: 76 колонок, максимум 6 строк, responsive position,
  `▶`, scroll counter и footer;
- modern-палитра IRIS и принципы ANSI save/restore cursor.

## Windows-адаптации Metuur

- Unix PTY и сигналы заменены на Windows ConPTY через
  `github.com/charmbracelet/x/xpty`;
- bash/zsh/fish hooks заменены приватным PowerShell OSC prompt marker;
- добавлены PowerShell/PSReadLine, Windows path semantics и UTF-8 VT input;
- добавлены активный файл VS Code, анализ Go-модулей и локальный completion;
- вставка строки работает в Windows PowerShell 5.1 и pwsh 7 без зависимости от
  Unix `Ctrl+U` binding.

## Название и связь с upstream

Metuur не использует название или логотип IRIS, не заявляет официальный статус
и не подразумевает одобрение со стороны авторов IRIS. Основной разработчик
IRIS — [@versenilvis](https://github.com/versenilvis); благодарность и ссылка
указаны в README и release/PR Metuur.

Это техническая проверка происхождения и условий лицензии, а не индивидуальная
юридическая консультация.

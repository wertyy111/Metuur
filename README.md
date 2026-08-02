# Metuur

<p align="center">
  <strong>Неофициальный Windows-порт интерфейса и поведения IRIS для Go-команд.</strong><br>
  Подсказки во время ввода, активный Go-файл VS Code и интерактивные программы без захвата клавиатуры.
</p>

<p align="center">
  <a href="https://github.com/wertyy111/Metuur/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/wertyy111/Metuur/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/wertyy111/Metuur/releases"><img alt="Release" src="https://img.shields.io/github/v/release/wertyy111/Metuur?display_name=tag"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Metuur_no--sale_%2B_0BSD-blue"></a>
  <img alt="Platform" src="https://img.shields.io/badge/platform-Windows-0078D4">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.25%2B-00ADD8">
</p>

Metuur — неофициальная Windows-адаптация
[IRIS](https://github.com/versenilvis/IRIS), созданного и поддерживаемого
[@versenilvis](https://github.com/versenilvis), при участии
[сообщества IRIS](https://github.com/versenilvis/IRIS/graphs/contributors).
Интерфейс и модель прозрачной PTY-обёртки перенесены с upstream-коммита
[`d669e97`](https://github.com/versenilvis/IRIS/commit/d669e97423a7ca9326d17b1289d06ba90942bd77).

Metuur не связан с авторами IRIS, не является официальным портом и не
подразумевает их спонсорство или одобрение.

## Интерфейс IRIS

Меню повторяет modern-overlay IRIS: ширина до 76 колонок, шесть видимых строк,
ghost text, `▶`, прокрутка, responsive-позиция и footer
`<Tab> Accept • <Ctrl+R> Mode`. Бренд, Windows-интеграция и Go-контекст остаются
собственными частями Metuur.

## Почему клавиатура больше не блокируется

Версии 0.3.x были отдельным REPL: Metuur сам редактировал строку, затем запускал
команду в другом процессе. Такой подход конфликтовал с VS Code ConPTY и
PSReadLine.

Начиная с 0.4.0 схема такая же, как у IRIS:

```text
клавиши VS Code → Metuur (перехватывает только служебные клавиши)
               → настоящий PowerShell внутри Windows ConPTY
               → ANSI-вывод и overlay обратно в VS Code
```

Обычный символ немедленно передаётся PowerShell ровно один раз. Пока выполняется
`go run`, отладчик или другая интерактивная программа, весь ввод проходит в неё
без обработки. После следующего PowerShell prompt подсказки включаются снова.

## Возможности

- продолжает начатые команды `go r`, `go bui`, `go tes`, `gofm`;
- локально определяет активный `.go`-файл VS Code и ставит команду для него
  первой, не меняя установленные расширения;
- находит реальные `main`-пакеты, тесты, вложенные `go.mod`/`go.work`, файлы и папки;
- знает `go`, `gofmt`, `goimports`, `gopls`, `dlv`, `staticcheck`,
  `golangci-lint`, `gotestsum`, `govulncheck`, `air`, `mockgen` и другие Go-инструменты;
- сохраняет одну настоящую PowerShell-сессию: работают `cd`, переменные, функции,
  aliases, PSReadLine и программы с вводом из stdin;
- понимает русские и английские намерения, опечатки и текст в неверной раскладке;
- объединяет specs, проект, активный файл, историю и необязательную локальную модель;
- адаптируется к изменению размера терминала и не перерисовывает строку PowerShell.

## Быстрый старт

Требования: Windows 10 1809+ или Windows 11, Go 1.25+, PowerShell 5.1 или 7.

```powershell
git clone https://github.com/wertyy111/Metuur.git
cd Metuur
go build -buildvcs=false -trimpath -o .\bin\metuur.exe .\cmd\metuur
.\bin\metuur.exe
```

Копируйте только команды внутри блоков. Текст приглашения вроде
`PS D:\папка>` вводить не нужно.

Для установки в `%LOCALAPPDATA%\Programs\Metuur` и добавления `metuur` в PATH:

```powershell
.\scripts\install.ps1
```

## VS Code и автозапуск

Задача `Metuur: auto start` запускается при открытии папки. Если VS Code один раз
спросит разрешение, выберите **Allow Automatic Tasks**. Задача открывает
настоящий PowerShell внутри Metuur и больше не отключает подсказки редактора Go.

Metuur читает локальное состояние открытой рабочей области VS Code. Он не
устанавливает и не отключает расширения редактора, не меняет их каталог и не
отправляет исходный код в сеть. На Windows это работает без отдельного плагина.

Необязательный bridge для мгновенного отслеживания очень быстрых переключений
между одноимёнными файлами можно установить только по явному запросу:

```powershell
.\scripts\install.ps1 -InstallVSCodeBridge
```

Если обычный каталог расширений защищён от записи, bridge следует пропустить:
основное локальное определение файла продолжит работать, а ваши плагины останутся
в исходном каталоге.

Чтобы запускать Metuur во всех новых терминалах VS Code через профиль PowerShell:

```powershell
.\scripts\enable-vscode-autostart.ps1
```

Отключение:

```powershell
.\scripts\disable-vscode-autostart.ps1
```

## Управление

| Клавиша | Действие |
|---|---|
| `Tab` | вставить выбранную подсказку в PSReadLine |
| `→` в конце строки | принять только inline ghost text |
| `Enter` | выполнить именно текущую строку; выбранный пункт — после навигации |
| `↑` / `↓` | выбрать подсказку или открыть историю |
| `Shift+Tab` / `Ctrl+Space` | включить или выключить подсказки |
| `Esc` | скрыть меню до следующего ввода |
| `Ctrl+R` | переключить `SPEC` / `HISTORY` |
| `Ctrl+A` / `Ctrl+E` | начало / конец строки |
| `Ctrl+W` / `Ctrl+U` | удалить слово / очистить строку |
| `Ctrl+Y` | скопировать всю текущую строку |
| `Ctrl+C` / `Ctrl+L` | отменить ввод / очистить экран |

## Необязательное локальное понимание намерения

Встроенные подсказки работают без модели. Для фраз вроде `проверь все тесты`
можно один раз установить маленький локальный completion-движок:

```powershell
metuur ai setup
metuur ai status
```

Будут загружены Windows CPU-бэкенд llama.cpp и квантованная
Qwen2.5-Coder 0.5B (около 409 МБ). Ответ модели принимается только тогда, когда
он совпадает с безопасной командой, уже построенной по реальным файлам проекта.

Папку модели можно перенести на другой диск:

```powershell
metuur ai setup "D:\MetuurAI"
```

## Проверка

```powershell
go test -count=1 ./...
go vet ./...
go build -buildvcs=false -trimpath -o .\bin\metuur.exe .\cmd\metuur
.\bin\metuur.exe doctor
```

Тесты включают настоящий вложенный сценарий ConPTY: ввод при открытом меню,
`Tab`, PowerShell `Read-Host`, возврат к prompt и завершение shell.

## Происхождение и лицензии

Части поведения shell-wrapper и overlay, перенесённые из IRIS, доступны по его
лицензии [0BSD](https://github.com/versenilvis/IRIS/blob/main/LICENSE). Точная
версия upstream, текст 0BSD и лицензии ConPTY/Unicode-зависимостей находятся в
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

Оригинальные части Metuur распространяются по
[Metuur Free Use — No Sale License 1.0](LICENSE): бесплатные использование,
изменение и распространение разрешены, продажа копий запрещена, а перед первой
публичной публикацией изменённой версии требуется уведомление автора. Эти
условия не отменяют права, уже выданные авторами стороннего кода, включая IRIS.

Подробнее: [русское пояснение](LICENSE-RU.md) и
[проверка происхождения](docs/IRIS-COMPATIBILITY.md).

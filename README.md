# Metuur

<p align="center">
  <strong>Интеллектуальный помощник Go-команд для терминала Windows.</strong><br>
  Готовые команды для открытого проекта и активного файла VS Code — прямо во время ввода.
</p>

<p align="center">
  <a href="https://github.com/wertyy111/Metuur/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/wertyy111/Metuur/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/wertyy111/Metuur/releases"><img alt="Release" src="https://img.shields.io/github/v/release/wertyy111/Metuur?display_name=tag"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-free_use_%C2%B7_no_sale-blue"></a>
  <img alt="Platform" src="https://img.shields.io/badge/platform-Windows-0078D4">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.24%2B-00ADD8">
</p>

Metuur — нативная интерактивная оболочка на Go, вдохновлённая идеей
[IRIS](https://github.com/versenilvis/IRIS) и адаптированная под Windows,
PowerShell и встроенный терминал VS Code. Она не просто дописывает название
команды, а анализирует открытую папку и предлагает готовый корректный запуск.

```text
λ go
╭─ 1/24 ───────────────────────────────────────────────────────────────────╮
│ ▶ ▷ go run .\task2.go                   открытый файл VS Code             │
│   ⬡ go build .\task2.go                 открытый файл VS Code             │
│   ◇ gofmt -w .\task2.go                 открытый файл VS Code             │
│   ◆ go test ./...                        все тесты Go-модуля               │
│   ◆ go vet .\task2.go                   открытый файл VS Code             │
╰─ <Tab> вставить · <Enter> запуск · ↑↓ · <Esc> скрыть · SPEC ─────────────╯
```

## Возможности

- определение активного `.go`-файла текущего окна VS Code без расширений;
- поиск настоящих `main`-файлов, пакетов, тестов и вложенных `go.mod`/`go.work`;
- готовые варианты для `run`, `build`, `fmt`, `test`, `vet`, `generate`,
  `fix`, `list`, `install`, `doc`, `clean`, `mod` и `work`;
- сокращённые намерения: `go b`, `go t`, `go mo t`, `dlv d`, `gopls ch`;
- встроенная база Go-инструментов: `dlv`, `gopls`, `gofmt`, `goimports`,
  `golangci-lint`, `staticcheck`, `air`, `gotestsum`, `govulncheck` и другие;
- прокручиваемое меню, ghost text, fuzzy-поиск и отдельный режим истории;
- постоянная PowerShell-сессия: `cd`, переменные, функции и aliases сохраняются;
- автоматический запуск при открытии папки в VS Code.

## Быстрый старт

Требования: Windows 10/11, Go 1.24+ и PowerShell 5.1 или PowerShell 7.

Скачайте `metuur-windows-amd64.exe` со страницы
[Releases](https://github.com/wertyy111/Metuur/releases), переименуйте его в
`metuur.exe` и запустите в терминале проекта:

```powershell
.\metuur.exe
```

Или соберите проект самостоятельно:

```powershell
git clone https://github.com/wertyy111/Metuur.git
cd Metuur
go build -buildvcs=false -trimpath -o .\bin\metuur.exe .\cmd\metuur
.\bin\metuur.exe
```

Для установки в `%LOCALAPPDATA%\Programs\Metuur` и добавления команды `metuur`
в пользовательский `PATH`:

```powershell
.\scripts\install.ps1
```

## VS Code

Задача `Metuur: auto start` запускается при открытии папки. Если VS Code спросит
разрешение на автоматические задачи, выберите **Allow Automatic Tasks**.

Чтобы включить Metuur во всех новых терминалах VS Code:

```powershell
.\scripts\enable-vscode-autostart.ps1
```

После смены активной вкладки достаточно написать `go`: команда запуска открытого
файла появится первой. Если файл не является запускаемым `package main`, Metuur
выберет подходящий `main` из проекта.

## Управление

| Клавиша | Действие |
|---|---|
| `Tab` / `→` | вставить выбранную подсказку |
| `Enter` | выполнить готовую команду |
| `↑` / `↓` | перемещаться по всему списку |
| `Esc` | скрыть меню |
| `Shift+Tab` / `Ctrl+Space` | показать или скрыть меню |
| `Ctrl+R` | переключить `SPEC` / `HISTORY` |
| `Ctrl+Y` / `Ctrl+Shift+C` | скопировать всю строку |
| `Ctrl+C` | отменить текущий ввод |
| `Ctrl+L` | очистить экран |
| `exit` | закрыть Metuur |

## Проверка проекта

```powershell
go test ./...
go vet ./...
go build -buildvcs=false -trimpath -o .\bin\metuur.exe .\cmd\metuur
.\bin\metuur.exe doctor
```

## Структура

```text
cmd/metuur        CLI и точка входа
internal/app      цикл ввода и состояние командной строки
internal/console  Windows Console API и обработка клавиш
internal/suggest  анализ проекта, база команд и ранжирование
internal/shell    постоянная PowerShell-сессия
internal/ui       ANSI-интерфейс и меню подсказок
specs             встроенные спецификации и рецепты команд
scripts           установка, удаление и автозапуск
```

## Лицензия

[Metuur Free Use — No Sale License 1.0](LICENSE) разрешает бесплатно
использовать, изменять и распространять проект, но запрещает продавать
исходники, бинарные файлы и изменённые копии. Коммерчески использовать Metuur
как инструмент и брать деньги за собственную работу или поддержку разрешено.
Доступно [русское пояснение](LICENSE-RU.md).

Из-за запрета продажи это source-available, а не OSI open source. Metuur
является самостоятельной реализацией; IRIS использован как продуктовый и
архитектурный ориентир.

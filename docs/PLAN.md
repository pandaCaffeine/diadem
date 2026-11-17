# Diadem — план реализации

Консольный DLNA (UPnP AV) медиа-сервер: запускается из папки с медиафайлами и делает её содержимое видимым для VLC на телевизоре в гостиной. TUI — в стиле charm-экосистемы (Bubble Tea/Lipgloss), дружелюбный к не-программистам.

## Зафиксированные решения

| Область | Решение |
|---|---|
| Стратегия | Собственное подмножество UPnP: SSDP + device/service XML + ContentDirectory:1 (Browse) + HTTP с Range. Референс по совместимости — `anacrolix/dms` (ADR-0001) |
| Стек | Go + Bubble Tea / Lipgloss (ADR-0002) |
| Go-модуль | `github.com/pandaCaffeine/diadem` |
| Контент | Video + Audio + Photo; фильтр по расширениям; dot-файлы скрыты |
| Каталог | Зеркало реального дерева папок; папки сверху, natural sort (`S02E10` после `S02E9`) |
| Транскодинг | Нет — отдаём оригинальные файлы |
| Сеть | SSDP на всех IPv4-интерфейсах; HTTP-порт 8200 по умолчанию (`--port`) |
| Язык UI | Английский (перевод — потом, если приспичит) |
| Раскладка TUI | Шапка (лого, Friendly Name, путь, IP/порт) / основная зона / футер с подсказками клавиш; вкладки **Library / Clients / Logs** по Tab |
| Навигация | ↑/↓ прокрутка, Enter свернуть/развернуть Container, попап с деталями Item (Media Kind, размер, путь) |
| Живой каталог | fsnotify + debounce ~500 мс — файлы появляются/исчезают без перезапуска |
| Диагностика | `--debug`: подробный лог в файл + вкладка Logs |
| Флаги CLI v1 | `--path` (cwd), `--name` (`Diadem`), `--port` (8200) — и всё |
| Тесты | Unit на чистую логику в каждом шаге + ручной чеклист проверки шага |
| Публикация | Локальная отладка → публичный GitHub (`pandaCaffeine/diadem`) после стабилизации |
| Процесс | Коммитит только пользователь; каждый шаг = отдельный коммит ревью-сайза |

## Non-goals (v1)

- Транскодинг, ремукс
- Метаданные через ffprobe — реализовано иначе: парсеры заголовков на чистом Go (ADR-0003)
- Симлинки — не следуем (кандидат на v1.1, с защитой от циклов)
- Внешние субтитры (.srt/.ass) — не показываем в дереве клиента
- ContentDirectory Search — VLC использует только Browse
- AVTransport, push, GENA-eventing — VLC это pull-клиент, не нужно
- IPv6, авторизация, конфиг-файл, выбор сетевого интерфейса (`--interface` — на потом)

## Архитектура

Пакеты:

- `internal/library` — скан файловой системы → дерево Container/Item; natural sort; watcher (fsnotify + debounce); Store — снапшот дерева как единый источник правды (ADR-0004)
- `internal/format` — парсеры заголовков видео-контейнеров MP4/MKV/AVI: Metadata (ADR-0003)
- `internal/dlna` — SSDP (UDP multicast 239.255.255.250:1900), XML-описания устройства и сервисов, SOAP ContentDirectory, генерация DIDL-Lite, таблица MIME + protocolInfo по Media Kind
- `internal/httpd` — общий HTTP-мукс на 8200: раздача файлов (Range), описания, SOAP-эндпоинты
- `internal/clients` — трекер клиентов и их действий (события из SSDP/HTTP)
- `internal/tui` — Bubble Tea: вкладки Library/Clients/Logs, шапка/футер
- `main.go` — флаги, сборка компонентов, запуск

Тонкость UPnP: в device description один `<URLBase>` — адрес, который получают все клиенты. Выбираем «лучший» IPv4 (не loopback); выбранный IP всегда показываем в шапке TUI.

## Шаги (каждый = отдельный коммит на ревью)

### Шаг 1 — Каркас: скан библиотеки + TUI-дерево
- `go.mod` (`github.com/pandaCaffeine/diadem`); зависимости: bubbletea, lipgloss, bubbles
- Флаги `--path/--name/--port` + валидация
- `library`: рекурсивный скан в горутине (TUI не блокируется), фильтр расширений, natural sort, папки сверху
- `tui`: шапка с лого (корона — Diadem же), дерево: ↑/↓/Enter/PgUp/PgDn, попап деталей Item
- Тесты: media-фильтр, natural sort
- ✅ Проверка: `go run . --path <папка>` — дерево листается, детали открываются

### Шаг 2 — HTTP-раздача файлов
- `httpd`: файлы по URL-safe путям, MIME по расширению, `Accept-Ranges`/`Range`/206, `Content-Length`, защита от path traversal
- Тесты: MIME-таблица, парсинг Range (включая суффиксные и open-ended), traversal; интеграция через httptest
- ✅ Проверка: `curl -r 0-1023 http://localhost:8200/...` отдаёт ровно 1024 байта; VLC → Open Network Stream — играет и перематывает

### Шаг 3 — UPnP-устройство и ContentDirectory
- `dlna`: device description XML (MediaServer:1), SCPD ContentDirectory:1, ConnectionManager (обязательная заглушка)
- SOAP `Browse` (BrowseDirectChildren + BrowseMetadata), генерация DIDL-Lite, схема ObjectID (0 = корень), MIME + protocolInfo по Media Kind
- Выбор primary IP → `<URLBase>`
- Тесты: golden-XML для DIDL-Lite (включая кириллицу в именах), парсинг SOAP, интеграционный Browse через httptest
- ✅ Проверка: curl-запрос SOAP Browse — валидный DIDL-Lite в ответе

### Шаг 4 — SSDP: сервер становится видимым
- SSDP: периодические `ssdp:alive`, ответы на `M-SEARCH` (upnp:rootdevice, MediaServer:1, uuid, ContentDirectory), `ssdp:byebye` при выключении
- Тесты: парсинг M-SEARCH, формирование ответов
- ✅ Проверка: 🎉 VLC на ноутбуке → Local Network → Universal Plug'n'Play → **Diadem** → дерево → воспроизведение и перемотка. Затем — телевизор в гостиной

### Шаг 5 — Вкладка Clients
- `clients`: трекер из SSDP M-SEARCH + HTTP-запросов (IP, User-Agent); события действий: «browse <Container>», «plays <Item>» с таймстампами
- `tui`: вкладка Clients — список клиентов и их последнее действие
- Тесты: логика трекера
- ✅ Проверка: листаешь/играешь в VLC — действия видны во вкладке почти мгновенно

### Шаг 6 — Живое обновление библиотеки
- `library`: fsnotify + debounce 500 мс, инкрементальный rescan; TUI сохраняет свёрнутость/позицию при обновлении
- Тесты: debounce-логика
- ✅ Проверка: при работающем сервере скопировать файл в папку → появился в TUI и в VLC без перезапуска

### Шаг 7 — Диагностика и полировка
- `--debug`: подробный лог (SSDP/SOAP/HTTP) в файл + вкладка Logs
- Graceful shutdown: q/Ctrl+C → `ssdp:byebye` → выход
- Внятные ошибки: порт занят (с подсказкой), папка пуста / нет медиа-файлов
- README (установка, запуск, macOS-файрвол, FAQ), `.gitignore`
- ✅ Проверка: чеклист «первого запуска с нуля» на ноутбуке, затем на телевизоре

## Риски и меры

- **URLBase на мультиинтерфейсной машине** (Wi-Fi + VPN и т.п.): выберется не тот IP → выбранный IP всегда в шапке TUI; при реальных проблемах добавим `--interface`
- **Причуды protocolInfo у ТВ-клиентов**: сверяем строки с dms, где они выстраданы годами
- **Большие библиотеки**: полный скан на старте — в горутине, дерево наполняется по мере скана
- **macOS-файрвол** может заблокировать UDP multicast при первом запуске: подсказка в README и в TUI при старте

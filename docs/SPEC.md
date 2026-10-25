# bblv — Bambu Live View: specyfikacja

Podgląd bieżącego wydruku drukarek Bambu Lab w przeglądarce: widok 3D wydrukowanych warstw, postęp, parametry, kamera.
Serwer w Go, frontend Vue 3 + Element Plus. Moduł: `github.com/marektews/bblv`, licencja MIT.

## 1. Zakres

### W zakresie
- Wiele drukarek: pulpit (kafelki) + widok szczegółowy `/printer/{id}`.
- Wszystkie modele Bambu. **A1 — testowana**, pozostałe (A1 mini, P1P/P1S, X1C/X1E, H2D) — wg dokumentacji społeczności (OpenBambuAPI, ha-bambulab), oznaczone w UI i README jako **„eksperymentalne”**.
- Widok 3D z G-code bieżącego wydruku, panel parametrów, kamera.
- Konfiguracja przez WWW z wykrywaniem drukarek w sieci.
- Nagrywanie / odtwarzanie raportów MQTT.

### Poza zakresem (na teraz)
- Sterowanie drukarką (pauza/stop itp.) — aplikacja tylko czyta.
- Historia wydruków, wykresy historii temperatur.
- Połączenie przez chmurę Bambu.
- Docker.
- Postęp wewnątrz bieżącej warstwy (drukarka go nie raportuje).

## 2. Komunikacja z drukarką (LAN)

Drukarka pozostaje w **trybie chmurowym** — LAN Only Mode i Developer Mode **nie są wymagane**. Firmware blokuje w tym trybie tylko sterowanie przez lokalny MQTT; odczyt statusu, FTPS i kamera działają (zweryfikowane na A1, firmware 01.08.01.00).

| Kanał | Port | Użycie |
|---|---|---|
| MQTT over TLS | 8883 | raporty stanu (tylko odczyt); login `bblp`, hasło = Access Code; topic `device/{serial}/report`, żądania `get_version` i `pushall` na `device/{serial}/request` |
| FTPS (implicit TLS) | 990 | pobranie plików wydruku z karty SD; **tylko ścieżki względne** (`cache/...`) — absolutne (`/cache/...`) zwracają `550`; PASV (EPSV wyłączone) |
| Kamera A1/P1 | 6000 | TLS, pakiet autoryzacji 80 B (`bblp` + Access Code), ramki: nagłówek 16 B (rozmiar LE) + JPEG 1536×1080 (~150 KB); zmierzone ~0,7 kl./s po zakończonym wydruku, pierwsza klatka po ~4,5 s |
| Kamera X1/H2D | 322 | RTSPS, H.264 — przez zewnętrzny `ffmpeg` (jeśli w PATH) → MJPEG; bez ffmpeg brak kamery |
| SSDP | UDP 2021 | wykrywanie: drukarka wysyła `NOTIFY` co ~10 s (z portu 1900); nagłówki `Location` (IP), `USN` (serial), `DevModel.bambu.com` (kod modelu, np. `N2S` = A1), `DevName.bambu.com`, `DevConnect.bambu.com`, `DevVersion.bambu.com`; port współdzielony z Bambu Studio (`SO_REUSEADDR`); serwer nasłuchuje w tle od startu |

- Po połączeniu `pushall`, następnie scalanie raportów przyrostowych w pełny stan.
- Jedno połączenie MQTT na drukarkę; kamera otwierana tylko gdy ktoś ogląda, jedno połączenie upstream rozdzielane na wszystkich widzów (`internal/camera.Hub`): zamykane 10 s po odejściu ostatniego widza, nowy widz dostaje od razu ostatnią klatkę, wolny widz gubi klatki zamiast blokować innych. Kamera RTSP: `ffmpeg -rtsp_transport tcp … -r 2 -f image2pipe -c:v mjpeg`.
- Developer Mode / LAN Only i wersja firmware — weryfikacja przy pierwszym teście (etap 1).

## 3. Geometria i widok 3D

### Źródło G-code
1. **Automatycznie:** przy każdym wydruku drukarka zapisuje w `cache/` na karcie SD:
   - `<subtask>.3mf` — projekt; z niego miniatura `Metadata/plate_N.png`,
   - `<subtask>_plate_N.gcode` — gotowy G-code płyty (bez rozpakowywania `.3mf`),
   - `N_<subtask>.bbl` — JSON: numer płyty (prefiks nazwy), `file path` do `.3mf`, `md5`, `ams mapping` (indeks filamentu → slot AMS, `-1` = brak).

   `subtask_name` z MQTT → odszukanie `.bbl` w `cache/` → pobranie **`.3mf`** (skompresowany, zawiera też `Metadata/plate_N.gcode`; ~5× mniejszy niż goły G-code) → gdy `.3mf` nie ma G-code lub się nie pobierze — goły `.gcode`. Pole `file_size` w `.bbl` bywa nieaktualne — nie polegać na nim.
2. **Awaryjnie:** ręczny upload `.gcode` / `.3mf` przez stronę.

### Przetwarzanie (serwer, Go)
- Parser G-code: warstwy, typy ścieżek (`; FEATURE:`), zmiany narzędzia (`T0…T3`), kolory filamentów z nagłówka (`filament_colour`), szerokość ekstruzji i wysokość warstwy.
- Pomijanie ruchów jałowych (travel).
- Upraszczanie: scalanie współliniowych odcinków.
- Szerokość ścieżki z `; LINE_WIDTH:`, a gdy brak — z objętości wytłoczenia. Łuki G2/G3 linearyzowane (odchyłka ≤ 0,01 mm). Pod uwagę brane tylko ruchy między pierwszym `; CHANGE_LAYER` a `; MACHINE_END_GCODE_START`.
- Wynik — format binarny (little endian):
  ```
  "BBLG" | uint32 wersja | uint32 n | n bajtów JSON (dopełnione spacjami do 4 B) | punkty
  JSON:  {version, points, layers:[{z,height,first,count}], features:[nazwy], filaments:[#RRGGBB], bounds:[6]}
  punkt: 7 × float32 = x, y, z, width, height, layer, tool*256+feature
  ```
  Punkty tworzą polilinie: odcinek kończący się w punkcie `i` ma atrybuty punktu `i`; `width = 0` oznacza „przejdź do” (bez odcinka). Na wydruku testowym: 197 tys. odcinków → 5,3 MB (gzip 1,2 MB), parsowanie 80 ms.
- Cache na dysku, klucz = ścieżki + rozmiary + daty plików na karcie, limit rozmiaru (domyślnie 1 GB, LRU).

### Render (przeglądarka)
- Czysty Three.js w komponencie Vue `<PrintViewer>`; scena i bufory poza reaktywnością (`markRaw`).
- Bryły o realnej szerokości/wysokości ścieżki — instancje + własny shader.
- Automatyczny fallback do linii powyżej 2,5 mln odcinków (urządzenia dotykowe: 400 tys.); `?lines` w adresie strony wymusza linie (test).
- Odcinek = instancja graniastosłupa o przekroju sześciokąta; start z punktu `i`, koniec i atrybuty z punktu `i+1` tego samego bufora (jeden upload na GPU). Dwa przejścia: nieprzezroczyste (wydrukowane + bieżąca) i przezroczysty „duch”.
- Warstwy wydrukowane: pełne; bieżąca: wyróżniona (w całości jako „w trakcie”); pozostałe: półprzezroczysty „duch”.
- Suwak warstw: tryb „na żywo” (podąża za wydrukiem) i ręczny, przycisk „wróć na żywo”.
- Kolorowanie: domyślnie kolory filamentów, przełącznik na typy ścieżek.
- Legenda filamentów: kolor ze slicera, slot AMS z `ams mapping` w pliku `.bbl` (np. „Filament 1 → slot 4 · PETG”; `-1` = brak przypisania, `254` = szpula zewnętrzna) i podświetlenie filamentu drukowanego teraz (`ams.tray_now` z raportu = slot z mapowania). Bez pliku `.bbl` (upload, replay) legenda pokazuje same filamenty.
- Po zakończeniu wydruku widok zostaje na ostatnim wydruku do startu następnego.

## 4. Panel parametrów
Stan (przygotowanie/druk/pauza/koniec/błąd), %, warstwa X/Y, pozostały czas + godzina zakończenia, temperatury dyszy i stołu (aktualna/docelowa), nazwa pliku, profil prędkości, wentylatory, sloty AMS (kolor, typ, aktywny, który filament wydruku jest z danego slotu — to samo mapowanie co w legendzie 3D), bieżący etap, błędy HMS, sygnał Wi-Fi.

**HMS:** snapshot bazy opisów Bambu wbudowany w binarkę + link do wiki Bambu. Brak polskiego opisu → angielski.
- Źródło: `https://e.bambulab.com/query.php?lang={en|pl}&d={prefiks numeru seryjnego}` (to samo, czego używa Bambu Studio) → `device_hms` (kody 16 hex) i `device_error` (kody `print_error`, 8 hex). Odświeżanie: `go run ./internal/hms/gen` (18 zapytań, ~7 MB) → `internal/hms/data/hms_{en,pl}.json.gz` (~60–70 KB). Teksty różne dla modeli są zachowane per prefiks (A1 = `039`).
- Link „więcej”: `https://e.bambulab.com/index.php?e={kod}&d={prefiks}&s=device_hms|device_error&lang=…` — przekierowuje na stronę kodu w wiki albo na przegląd HMS.
- `GET /api/printers/{id}/alerts?lang=pl|en` → `[{kind, code, severity, text, url}]`; waga HMS z bitów kodu: 1 fatal, 2 serious, 3 common, 4 info.

**Etapy (`stg_cur`):** nazwy PL/EN dla 78 numerów wg listy ha-bambulab (`web/src/locales/stages.ts`); 0 = drukowanie i -1/255 = bezczynna nie są pokazywane. Etap widoczny w panelu i na kafelku w trakcie wydruku.

## 5. Serwer HTTP / API (szkic)

| Metoda | Ścieżka | Opis |
|---|---|---|
| GET | `/api/printers` | lista drukarek + skrócony stan |
| GET | `/api/printers/{id}/state` | pełny stan |
| GET | `/api/events` | SSE: zdarzenie `printer` z pełnym opisem drukarki (`id`, `name`, `model`, `experimental`, `online`, `status`, `geometry`) — po połączeniu dla każdej drukarki, potem przy każdej zmianie; nowy `geometry.key` = przeładuj geometrię |
| GET | `/api/printers/{id}/geometry` | geometria binarna (z cache), gzip, `ETag` = `geometry.key` |
| GET | `/api/printers/{id}/thumbnail` | miniatura z `.3mf` |
| GET | `/api/printers/{id}/camera` | MJPEG (`multipart/x-mixed-replace`); `camera: true` w opisie drukarki, gdy dostępna |
| POST | `/api/printers/{id}/upload` | ręczny upload G-code / 3MF |
| GET | `/api/auth` | `{required, authenticated}` |
| GET | `/api/config` | drukarki (bez Access Code, tylko `hasAccessCode`), `passwordSet`, ścieżka `BambuStudio.conf` |
| POST/PUT/DELETE | `/api/config/printers[/{id}]` | dodanie / zmiana (pusty kod = bez zmiany) / usunięcie; zmiany od razu w monitorze |
| GET | `/api/config/discovered` | drukarki wykryte przez SSDP + `configuredAs` |
| POST | `/api/config/import-codes` | kody z `BambuStudio.conf` (wgrany plik albo lokalny) dla drukarek o pasującym numerze seryjnym |
| PUT | `/api/config/password` | `{current, password}`; puste hasło = usunięcie |
| POST | `/api/login`, `/api/logout` | sesja (gdy ustawione hasło) |

Stany `geometry.state`: `none`, `loading`, `ready`, `missing` (brak plików na karcie → upload), `error`. Przy `missing`/`error` w trakcie wydruku serwer ponawia pobranie co 30 s (max 10 razy) — drukarka może jeszcze pobierać plik.

Frontend wbudowany przez `go:embed` (Vite buduje do `internal/webui/dist/`; bez zbudowanego frontendu serwer pokazuje stronę zastępczą). W trybie dev serwer Vite proxy'uje `/api` do Go.

`bblv serve -replay nagranie.jsonl [-replay-model plik.3mf] [-speed N]` — wirtualna drukarka `replay` z nagrania, do pracy nad frontendem bez drukarki.

## 6. Bezpieczeństwo
- Praca w sieci lokalnej; dostęp zdalny przez własny VPN.
- Jedno opcjonalne wspólne hasło (hash bcrypt). Puste hasło = otwarty dostęp do wszystkiego, łącznie z konfiguracją.
  - Sesja: ciasteczko `HttpOnly`, `SameSite=Strict`, ważne 30 dni, podpisane HMAC sekretem z `config.json`; zmiana hasła zmienia sekret (wylogowuje inne sesje, bieżąca dostaje nowe ciasteczko).
  - Błędne hasło: odpowiedź opóźniona o 1 s.
  - Zmiany (POST/PUT/DELETE) z nagłówkiem `Origin` innej strony są odrzucane; endpointy JSON wymagają `Content-Type: application/json` — bez hasła chroni to przed formularzami z obcych stron.
- Access Code nigdy nie wraca do przeglądarki (pole `••••`, tylko nadpisanie).
- **Źródło Access Code** (ekran A1 pokazuje go tylko w LAN Only Mode):
  - ręczne wpisanie, z podpowiedzią gdzie go znaleźć,
  - **import z Bambu Studio**: serwer czyta `BambuStudio.conf` (klucz `access_code`: serial → kod) lokalnie, gdy działa na tym samym komputerze (`%APPDATA%\BambuStudio\` / `~/.config/BambuStudio/` / `~/Library/Application Support/BambuStudio/`), albo z pliku wgranego przez formularz; wgrany plik nie jest zapisywany. Usługa działająca na koncie systemowym nie widzi pliku użytkownika — wtedy upload.

## 7. Dane
Katalog danych: domyślnie `%ProgramData%\bblv` (Windows) / `/var/lib/bblv` (Linux), nadpisywany flagą `--data`.
```
config.json     drukarki, hash hasła — zapis atomowy, uprawnienia 0600
cache/          geometria + miniatury
recordings/     nagrania MQTT (.jsonl)
```

## 8. Nagrywanie / odtwarzanie
- Nagrywanie surowych raportów MQTT (ze znacznikiem czasu) i nazwy pliku wydruku do `.jsonl`.
- Odtwarzanie: serwer czyta plik zamiast drukarki, z przyspieszeniem (np. ×10).
- Cel: rozwój bez drukarki, testy automatyczne, diagnoza modeli „eksperymentalnych” z nagrań od innych.

## 9. Frontend
- Vue 3 + Element Plus + TypeScript + Vite (projekt w `web/`, build do `internal/webui/dist/`).
  - TypeScript 6.x — TS 7 (natywny, w Go) nie ma API JS, którego wymaga `vue-tsc`.
  - Element Plus importowany w całości (~350 KB JS po gzip); serwer wysyła pliki tekstowe skompresowane gzip.
  - Paczki npm instalowane z `--before` (wersje co najmniej 2 tygodnie po publikacji).
- i18n PL/EN (`vue-i18n` + lokalizacje Element Plus), język wg przeglądarki + przełącznik.
- Motyw jasny/ciemny wg systemu.
- Responsywny pulpit i widok szczegółowy (telefon); konfiguracja dopracowana pod komputer.
- Three.js trzymany lokalnie (bez CDN).

## 10. Budowa i wdrożenie
- `CGO_ENABLED=0` — twarda zasada (cross-kompilacja jedną komendą).
- Go 1.24 (`go.mod`); zależności dodawać z `GOTOOLCHAIN=local`, bo `go get …@latest` potrafi podnieść wymaganą wersję Go i pobrać nowszy toolchain.
- `dist/` poza repo; skrypty `build.ps1` i `build.sh`: `npm ci --prefer-offline && npm run build`, następnie `go build -trimpath -ldflags "-s -w -X main.version=…"` dla `windows/amd64`, `linux/amd64`, `linux/arm64` (~9,5 MB każda); wersja z `git describe`.
- `bblv service install|uninstall|start|stop|restart|status [-data … -listen …]` (`kardianos/service`): usługa Windows (restart po awarii, log w `<data>/bblv.log`) / systemd (log w journalu); ścieżka danych zapisywana jako bezwzględna.
- `bblv record` domyślnie anonimizuje nagranie (numery seryjne, `chip_id`, `tag_uid`, `tray_uuid`, IP/maska → `REDACTED`/`SERIAL`); `-raw` zachowuje dane; `bblv anonymize` czyści starsze nagrania.
- Testy w Go: parser G-code, scalanie stanu MQTT (na nagraniach z A1).

## 11. Struktura kodu (propozycja)
```
cmd/bblv/            main, komendy CLI (serve, service, record, replay, geometry)
internal/printer/    klient MQTT, model stanu, scalanie raportów, profile modeli
internal/ftps/       sesja FTPS z kartą SD
internal/printjob/   wyszukanie plików wydruku na karcie, budowa geometrii i miniatury
internal/gcode/      parser, upraszczanie, format binarny
internal/cache/      cache geometrii i miniatur (LRU)
internal/camera/     źródło JPEG:6000, źródło ffmpeg, fan-out MJPEG
internal/discovery/  SSDP
internal/config/     config.json
internal/auth/       hasło, sesje
internal/recording/  nagrywanie / odtwarzanie
internal/hms/        opisy HMS i kodów błędów (gen/ = generator tabel)
internal/server/     HTTP, SSE, routing, embed frontendu
web/                 projekt Vue (Vite)
docs/                specyfikacja
```

## 12. Etapy
1. **Szkielet + MQTT A1:** `go.mod`, struktura, klient MQTT, `pushall`, scalanie stanu, nagrywanie do `.jsonl`, `bblv record`. Weryfikacja Developer Mode / firmware.
2. **G-code:** FTPS, rozpakowanie `.3mf`, parser, upraszczanie, format binarny, cache, testy. Odtwarzanie nagrań.
3. **Serwer HTTP:** API, SSE, ręczny upload, embed frontendu.
4. **Frontend — szkielet:** Vite + Vue + Element Plus + i18n, pulpit, widok szczegółowy, panel parametrów.
5. **`<PrintViewer>`:** bryły instancjami, duch, suwak, tryby kolorów, fallback do linii.
6. **Kamera:** A1/P1 (6000) → MJPEG, fan-out; ścieżka ffmpeg dla RTSPS.
7. **Konfiguracja WWW:** SSDP, formularze, hasło/sesje.
8. **HMS + dopracowanie:** snapshot HMS, etapy wydruku, tłumaczenia.
9. **Wdrożenie:** usługa, skrypty budowania, README (EN), profile modeli eksperymentalnych.

## 13. Do weryfikacji w trakcie
- ~~Czy odczyt MQTT na A1 wymaga Developer Mode~~ — nie, działa w trybie chmurowym.
- ~~Numer płyty i ścieżka pliku~~ — z plików `cache/N_<subtask>.bbl` (sekcja 3).
- ~~Wymagania FTPS~~ — ścieżki względne, PASV, cache sesji TLS włączony.
- ~~Rozdzielczość kamery A1~~ — 1536×1080, ~150 KB/klatkę, ~0,7 kl./s (pomiar bez trwającego wydruku).
- A1 nie raportuje numeru linii G-code (`mc_print_line_number` = 0) — postęp wewnątrz warstwy pozostaje poza zakresem.
- ~~Dostępne języki w bazie HMS Bambu~~ — polski jest (prawdziwe tłumaczenia).
- Ścieżka `BambuStudio.conf` na Linuxie/macOS (przyjęta wg konwencji, niesprawdzona).
- Kody modeli w SSDP poza `N2S` (A1) — wg dokumentacji społeczności.

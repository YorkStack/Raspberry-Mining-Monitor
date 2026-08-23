# LLM-Prompt: AxeOS-kompatibler Mess-Agent für einen Mac-Miner

Dieser Prompt lässt eine Coding-LLM einen kleinen Netzwerk-Agenten für macOS
bauen. Der Agent liest die Werte eines experimentellen Miners auf einem MacBook
Pro (M2) aus und stellt sie **im AxeOS-Format** bereit. Dadurch liest ihn der
Raspberry Mining Monitor ohne jede Änderung — er wird wie ein Bitaxe als
`type: axeos` in `config.yaml` eingetragen.

Kopiere alles unterhalb der Linie in die LLM.

---

Du baust einen kleinen, read-only HTTP-Agenten für macOS (Apple Silicon, M2).
Er stellt die Telemetrie eines lokal laufenden, experimentellen Bitcoin-Miners
so bereit, dass ein bestehendes Monitoring-System sie wie einen AxeOS-Bitaxe
liest.

## Ziel

Ein HTTP-Server, der die AxeOS-Upstream-API (`bitaxeorg/ESP-Miner`) so weit
nachbildet, dass ein Client, der `GET /api/system/info` abfragt, gültige,
korrekt einheitenbehaftete Werte bekommt.

## Zwingende Endpunkte

1. `GET /api/system/info`
   - Antwortet mit HTTP 200 und `Content-Type: application/json`.
   - Liefert das unten definierte JSON.
2. `GET /api/v2/dashboard`
   - Antwortet mit **HTTP 404**.
   - Das ist nicht optional: der Client erkennt an genau diesem 404, dass es
     sich um die Upstream-Variante handelt und nicht um die NerdQAxe-Variante.
     Gib hier niemals 200 zurück.
3. Alle anderen Pfade: 404.
4. Nur `GET` und `HEAD`. Jede andere Methode: 405. Der Agent ist strikt
   lesend und darf den Miner nicht steuern.

## JSON-Vertrag für `/api/system/info`

Exakte Feldnamen und Einheiten. Fehlt ein Wert real (z. B. Temperatur nicht
auslesbar), lass das Feld **weg** statt eine 0 zu senden — der Client behandelt
fehlende Felder korrekt als „nicht verfügbar".

```json
{
  "hashRate": 1234.5,
  "expectedHashrate": 1300,
  "power": 22.4,
  "voltage": 5000,
  "current": 4480,
  "coreVoltage": 1150,
  "frequency": 600,
  "temp": 58.1,
  "vrTemp": 61.0,
  "fanrpm": 0,
  "fanspeed": 0,
  "sharesAccepted": 128,
  "sharesRejected": 0,
  "bestDiff": 4200000000,
  "bestSessionDiff": 1200000000,
  "uptimeSeconds": 3600,
  "ASICModel": "MAC-M2",
  "version": "mac-agent-1.0",
  "hostname": "macbook-miner",
  "stratumURL": "public-pool.io",
  "stratumPort": 3333,
  "stratumUser": "bc1q...deine_adresse.mac",
  "isUsingFallbackStratum": 0
}
```

**Einheiten, unbedingt einhalten** (so erwartet die Upstream-API):

| Feld | Einheit |
|---|---|
| `hashRate`, `expectedHashrate` | **GH/s** (Gigahash pro Sekunde). 1 TH/s = 1000. |
| `power` | Watt |
| `voltage`, `coreVoltage` | **Millivolt** (5 V → 5000) |
| `current` | **Milliampere** (4,48 A → 4480) |
| `frequency` | MHz |
| `temp`, `vrTemp` | Grad Celsius |
| `fanspeed` | Prozent, `fanrpm` | Umdrehungen/min |
| `bestDiff`, `bestSessionDiff` | Difficulty (Zahl) |
| `uptimeSeconds` | Sekunden |

J/TH wird **nicht** gesendet. Das Monitoring rechnet es selbst aus
`power / (hashRate / 1000)`. Es genügt, `power` und `hashRate` korrekt zu liefern.

## Woher die Werte auf macOS kommen

- **hashRate**: aus dem experimentellen Miner selbst. Nutze dessen eigene
  Ausgabe: parse seine Log-Zeilen oder frage seine API/RPC ab, falls vorhanden.
  Beschreibe im Code klar, wo diese Quelle angebunden wird, damit ich sie an
  meinen konkreten Miner anpassen kann. Falls der Miner Shares/Best-Difficulty
  meldet, gib sie in `sharesAccepted`/`sharesRejected`/`bestDiff` weiter.

- **power (Watt)**: auf Apple Silicon liefert
  `sudo powermetrics --samplers cpu_power -n 1 -i 1000` die kombinierte
  Package-Leistung (CPU + GPU + ANE) in Milliwatt. Rechne in Watt um.
  `powermetrics` braucht root. Löse das über einen einmal eingerichteten
  sudoers-Eintrag nur für `powermetrics` oder einen kleinen Helper — beschreibe
  die Einrichtung im README. Wenn keine Leistung ermittelbar ist, lass `power`
  weg, statt zu raten.

- **temp (°C)**: auf Apple Silicon nicht über eine offizielle API abrufbar.
  Praktikable Optionen, in dieser Reihenfolge:
  1. `powermetrics --samplers smc` (root) — liefert je nach Modell Die-Temperatur.
  2. Das Tool `istats` (Ruby-Gem) liest SMC-Sensoren ohne root.
  3. `ioreg`/IOKit AppleSMC-Keys direkt.
  Ist keine verlässliche Temperatur verfügbar, lass `temp` weg. Erfinde keinen
  Wert. Dokumentiere, welche Methode gewählt wurde.

- **voltage/current/frequency/coreVoltage**: existieren auf einem Mac nicht als
  echte ASIC-Werte. Lass sie entweder weg oder kennzeichne sie klar als
  synthetisch. Erfinde keine plausiblen Fake-Werte, die wie echte Messwerte
  aussehen.

- **ASICModel**: eine feste Kennung wie `"MAC-M2"`, damit im Dashboard erkennbar
  ist, dass dies kein ASIC-Miner ist.

## Technische Vorgaben

- Sprache: Python 3 (Standardbibliothek `http.server` genügt) **oder** Go.
  Keine schweren Frameworks.
- Bind-Adresse und Port konfigurierbar (Default `0.0.0.0:8080`), damit der
  Raspberry Pi den Mac im LAN erreicht.
- Aktualisiere die Messwerte höchstens etwa alle 2 Sekunden und cache sie, damit
  häufige Abfragen `powermetrics` nicht überlasten.
- Strikt read-only. Kein Endpunkt, der irgendetwas schreibt oder den Miner
  steuert.
- Keine Secrets ausgeben. Die Payout-Adresse in `stratumUser` ist öffentlich,
  aber gib keine Wallet-Passwörter, privaten Schlüssel oder Seeds aus — der
  Agent braucht sie ohnehin nicht.
- Robust gegen Fehler: wenn eine Quelle (Miner-Log, `powermetrics`) kurz
  ausfällt, liefere die zuletzt bekannten Werte oder lass das betroffene Feld
  weg, aber lass den Server weiterlaufen.
- Als Deliverables: die Server-Datei, ein kurzes README (inkl. sudoers-Hinweis
  für `powermetrics`) und eine `launchd`-plist, damit der Agent als
  Hintergrunddienst startet.

## Verifikation, die du selbst durchführst

- `curl http://localhost:8080/api/v2/dashboard` muss 404 liefern.
- `curl http://localhost:8080/api/system/info | python3 -m json.tool` muss
  gültiges JSON mit `hashRate` in GH/s zeigen.
- Ein `power`-Wert in Watt, der zur tatsächlichen Last passt (grob 5–40 W idle
  bis Last auf einem M2), keine 1000-fach zu großen oder zu kleinen Zahlen.

---

## Danach: Eintrag im Raspberry Mining Monitor

In `config.yaml` des Monitors:

```yaml
miners:
  - name: MacBook M2
    host: 192.168.x.y:8080     # IP des MacBooks im LAN
    type: axeos
    payout_address: bc1q...
```

Der Monitor erkennt über das 404 auf `/api/v2/dashboard` automatisch die
Upstream-Variante, liest `/api/system/info`, rechnet GH/s in TH/s und mV/mA in
V/A um und zeigt Hashrate, Watt, J/TH und, falls geliefert, Temperatur an —
genau wie bei einem NerdOctaxe.

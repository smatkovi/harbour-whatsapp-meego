# WhatsApp für MeeGo Harmattan (Nokia N9 / N950)

Ein WhatsApp-Client für ein Telefon von 2011. Er meldet sich als
verknüpftes Gerät an — so, wie WhatsApp Web es tut — und braucht dafür ein
Haupttelefon, auf dem WhatsApp läuft.

Der Port setzt auf [harbour-whatsapp](https://github.com/smatkovi/harbour-whatsapp)
für Sailfish auf. Geteilt wird das Go-Backend mit
[whatsmeow](https://github.com/tulir/whatsmeow); die Oberfläche ist neu
geschrieben, weil Silica auf Harmattan nicht existiert.

## Was geht

* Verknüpfen per Telefonnummer, Chatliste, Verlauf, Senden und Empfangen
* Bilder, Dokumente, Dateianhänge, Sprachnachrichten (Opus wird auf dem
  Gerät nach WAV gewandelt, weil Harmattan kein Opus kann)
* Profilbilder, Gruppen, Mitgliederliste einer Gruppe mit dem Weg in den
  Einzelchat
* Ältere Nachrichten vom Haupttelefon nachladen
* Sprachanrufe, wahlweise über eine eingebaute SIP-Brücke: dann klingelt es
  in der systemeigenen Anrufansicht, auch am Sperrbildschirm
* **Integration in die Nachrichten-App**: WhatsApp-Chats erscheinen neben
  SMS, Telegram und Matrix (siehe unten)
* Das Backend läuft als Dienst weiter, wenn die App zu ist, und verbindet
  sich nach einem Netzwechsel von selbst neu

## Installieren

```
dpkg -i harbour-whatsapp_<version>_armel.deb
```

Das Paket legt beim ersten Start der App zwei Konten an — eines für die
Nachrichten-App, eines für Anrufe — und entfernt sie bei der
Deinstallation wieder.

## Die Nachrichten-App

Die Anbindung hängt sich an [pybridge](https://openrepos.net/), den
Telepathy-Verbindungsmanager, der auf Harmattan schon Telegram und Matrix
in die Nachrichten-App trägt. Neu ist nur ein Daemon
(`/opt/pywhatsapp/whatsapp_daemon.py`), der auf der einen Seite pybridges
Zeilen-JSON spricht und auf der anderen die HTTP-Schnittstelle des
Backends.

pybridge selbst gehört einem fremden Paket und wird nicht ersetzt, sondern
an neun Stellen erweitert — überall dort, wo es nach Protokoll verzweigt,
wird aus `== 'telegram'` ein `in ('telegram', 'whatsapp')`. Das erledigt
`patch-pybridge.py`, das jede erwartete Stelle genau einmal vorfinden muss
und sonst abbricht, ohne etwas anzufassen. So lässt es sich nach einem
pybridge-Update erneut anwenden.

Ohne pybridge läuft die App trotzdem, nur eben mit eigener Oberfläche.

## Bauen

Gebraucht werden ein Cross-GCC für `arm-none-linux-gnueabi`, das
MADDE-Sysroot aus dem Harmattan-SDK (für Qt 4.7) und Go 1.26 oder neuer.

```
meego/build.sh          # -> build/meego/{harbour-whatsapp,wa-backend}
meego/build-deb.sh 1.7  # -> harbour-whatsapp_1.7_armel.deb
```

Zwei Link-Details sind nicht kosmetisch: `-Wl,--dynamic-linker=/lib/ld-linux.so.3`,
sonst verlangt das Binary den armhf-Lader, den Harmattan nicht hat; und
`-static-libstdc++ -static-libgcc` mit `--exclude-libs,ALL`, damit die
moderne C++-Laufzeit im Binary bleibt, statt sie an das mit GCC 4.4 gebaute
Qt zu exportieren.

## Was nicht geht

* **Die Lautstärketasten stellen im Gespräch den Klingelton**, nicht die
  Gesprächslautstärke. Dafür müsste sich die App über `com.nokia.mce` als
  Anruf anmelden, und das verweigert der Bus unsignierten Paketen.
* Die Nachrichtendatenbank liegt unverschlüsselt (Modus 0600). Auf Sailfish
  übernimmt das Sailfish Secrets; auf Harmattan gibt es keinen
  Schlüsseldienst, hinter dem ein Schlüssel besser aufgehoben wäre als in
  einer Datei daneben — und die Partition ist ohnehin nicht verschlüsselt.
  Lieber ehrlich unverschlüsselt als scheinverschlüsselt.
* Videoanrufe.

## Lizenz

Wie das Ursprungsprojekt.

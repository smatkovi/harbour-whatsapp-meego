#!/bin/sh
# Packt Oberflaeche, Backend und QML als Harmattan-.deb.
#
#   meego/build-deb.sh [version]
#
# Keine versionierten Abhaengigkeiten: "libqt4-gui (>= 4.7.4)" sieht harmlos
# aus und ist es nicht -- das installierte Qt ist 4.7.4~git20120327, und "~"
# sortiert in Debian UNTER der leeren Zeichenkette. Die Bedingung waere nie
# erfuellbar, das Paket bliebe halb installiert und der Starter malte ein
# rotes Rufzeichen ueber das Symbol.
set -e
cd "$(dirname "$0")/.."

VERSION=${1:-0.1}
STAGE=build/stage-meego
OUT=build/meego
rm -rf "$STAGE"

[ -x "$OUT/harbour-whatsapp" ] || { echo "$OUT/harbour-whatsapp fehlt -- erst meego/build.sh" >&2; exit 1; }
[ -x "$OUT/wa-backend" ] || { echo "$OUT/wa-backend fehlt -- erst meego/build.sh" >&2; exit 1; }

mkdir -p "$STAGE/opt/harbour-whatsapp/bin" "$STAGE/opt/harbour-whatsapp/qml" \
         "$STAGE/usr/share/applications" \
         "$STAGE/usr/share/icons/hicolor/80x80/apps" \
         "$STAGE/usr/share/themes/base/meegotouch/icons" \
         "$STAGE/DEBIAN"

cp "$OUT/harbour-whatsapp" "$STAGE/opt/harbour-whatsapp/bin/"
cp "$OUT/wa-backend" "$STAGE/opt/harbour-whatsapp/bin/"
chmod 755 "$STAGE/opt/harbour-whatsapp/bin/"*
cp meego/qml/*.qml "$STAGE/opt/harbour-whatsapp/qml/"
cp meego/harbour-whatsapp.desktop "$STAGE/usr/share/applications/"

# postinst/prerm tragen whatsapp.local in /etc/hosts ein und wieder aus:
# die Kontoverwaltung prueft die SIP-Adresse wie eine Mailadresse und nimmt
# weder einen nackten Namen noch eine IP.
# Der Sitzungs-Upstart-Job: damit laeuft das Backend nach dem Einschalten
# von selbst, nimmt Nachrichten und Anrufe entgegen und haelt die
# SIP-Bruecke offen, auch wenn die App zu ist. ~/.config/upstart laeuft als
# "user" -- pybridge liefert seinen Job auf demselben Weg aus.
# Daemon: D-Bus-Aktivierung plus ein Upstart-Job, der sie nur anstoesst.
# Jobs unter ~/.config/upstart liest auf diesem Geraet niemand (nachgesehen),
# und nach /etc/init/xsession/ kommt ein unsigniertes Paket nicht -- Aegis
# verweigert dort jede Datei ohne Referenz-Hash. Der Sitzungs-D-Bus ist der
# Weg, der bleibt; derselbe traegt den Mastodon-Feed.
mkdir -p "$STAGE/usr/share/dbus-1/services" "$STAGE/etc/init/apps"
cp meego/org.smatkovi.WhatsApp.service "$STAGE/usr/share/dbus-1/services/"
cp meego/harbour-whatsapp-trigger.conf "$STAGE/etc/init/apps/harbour-whatsapp.conf"

# --- Nachrichten-App: der pybridge-Anschluss --------------------------------
# pybridge ist der Telepathy-Verbindungsmanager, der auf diesem Geraet schon
# Telegram und Matrix in die Nachrichten-App traegt. WhatsApp haengt sich
# daran: ein Daemon, der auf der einen Seite pybridges Zeilen-JSON spricht
# und auf der anderen unsere HTTP-Schnittstelle. Der Manager selbst gehoert
# einem fremden Paket und wird nur an neun Stellen erweitert -- durch ein
# pruefendes Skript, das sich nach einem pybridge-Update erneut anwenden
# laesst.
mkdir -p "$STAGE/opt/pywhatsapp" \
         "$STAGE/usr/share/accounts/services" \
         "$STAGE/usr/share/accounts/providers" \
         "$STAGE/usr/share/themes/blanco/meegotouch/icons"
cp meego/pybridge/whatsapp_daemon.py "$STAGE/opt/pywhatsapp/"
cp meego/pybridge/patch-pybridge.py  "$STAGE/opt/pywhatsapp/"
cp meego/pybridge/whatsapp-setup     "$STAGE/opt/pywhatsapp/"
chmod 755 "$STAGE/opt/pywhatsapp/"*.py "$STAGE/opt/pywhatsapp/whatsapp-setup"
cp meego/pybridge/whatsapp.service  "$STAGE/usr/share/accounts/services/"
cp meego/pybridge/whatsapp.provider "$STAGE/usr/share/accounts/providers/"
cp meego/pybridge/icon-m-service-whatsapp.png \
   meego/pybridge/icon-s-service-whatsapp.png \
   "$STAGE/usr/share/themes/blanco/meegotouch/icons/"

# Der Ereignistyp fuer die Klingelmeldung. class=system darin sorgt
# dafuer, dass sie ueber allem erscheint, auch am Sperrbildschirm.
mkdir -p "$STAGE/usr/share/meegotouch/notifications/eventtypes"
cp meego/harbour-whatsapp.call.conf \
   "$STAGE/usr/share/meegotouch/notifications/eventtypes/"

cp meego/postinst meego/prerm "$STAGE/DEBIAN/"
chmod 755 "$STAGE/DEBIAN/postinst" "$STAGE/DEBIAN/prerm"

# Das Symbol traegt die exakte Silhouette der Standard-Apps, erzeugt mit
# ~/ps/meego-icon-tool/squircle.py --fill. Ein rundes Icon faellt im Raster
# des Startbildschirms sofort auf.
cp meego/icons/icon-80.png "$STAGE/usr/share/icons/hicolor/80x80/apps/harbour-whatsapp.png"
cp meego/icons/icon-80.png "$STAGE/usr/share/themes/base/meegotouch/icons/harbour-whatsapp-80.png"

VERSION="$VERSION" python3 - <<'PY'
import base64, io, os, textwrap
icon = base64.b64encode(open("meego/icons/icon-64.png", "rb").read()).decode("ascii")
text = io.open("meego/control.in", encoding="utf-8").read()
text = text.replace("@VERSION@", os.environ["VERSION"])
text = text.replace("@ICON@",
                    "\n".join(" " + line for line in textwrap.wrap(icon, 76)))
io.open("build/stage-meego/DEBIAN/control", "w", encoding="utf-8").write(text)
PY

DEB="harbour-whatsapp_${VERSION}_armel.deb"
python3 meego/mkdeb.py "$STAGE" "$DEB"

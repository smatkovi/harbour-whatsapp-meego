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

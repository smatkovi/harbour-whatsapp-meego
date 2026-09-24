#!/bin/sh
# Baut die Harmattan-Oberflaeche und das Go-Backend fuer ARM.
#
# Beides laeuft auf dem Build-Rechner: die Oberflaeche mit der GCC-14-Cross-
# Toolchain gegen das MADDE-Sysroot (Qt 4.7.4), das Backend mit Gos eigenem
# Cross-Compiler und dem Build-Tag "meego".
#
# Zwei Link-Details sind nicht kosmetisch:
#   * -Wl,--dynamic-linker=/lib/ld-linux.so.3 -- sonst verlangt das Binary den
#     armhf-Lader, den Harmattan nicht hat.
#   * -static-libstdc++ -static-libgcc mit --exclude-libs,ALL -- die moderne
#     C++-Laufzeit bleibt im Binary, statt sie an Qt zu exportieren, das gegen
#     die von GCC 4.4 gebaut ist.
#
#   meego/build.sh          # -> build/meego/{harbour-whatsapp,wa-backend}
set -e
cd "$(dirname "$0")/.."

REMOTE=/tmp/wa-meego-src
HOST=$(sh "$HOME/ps/nfsshift-sfos/tools/buildhost.sh")
echo "== Build-Rechner: $HOST"

rsync -a --delete --exclude build --exclude '*.deb' --exclude stage \
    --exclude .git ./ "$HOST:$REMOTE/"

cat > /tmp/wa-meego-remote.sh <<'REMOTE_EOF'
set -e
SRC=/tmp/wa-meego-src
XGCC=${XGCC:-/tmp/xgcc-harmattan}
SYSROOT=${SYSROOT:-$HOME/QtSDK/Madde/sysroots/harmattan_sysroot_10.2011.34-1_slim}
SIMQT=${SIMQT:-$HOME/QtSDK/Simulator/Qt/gcc}
OUT=$SRC/build/meego
mkdir -p "$OUT"

# --- Backend ---------------------------------------------------------------
# Die gepatchte Fassung von meowcaller: ohne sie braucht der MLow-Kodierer
# auf der N950 273 ms je 60-ms-Rahmen statt 90. Die replace-Zeile kommt in
# die KOPIE des Baums, nicht ins Repo -- der Pfad gilt nur hier.
FORK=$(sh "$SRC/meego/meowcaller-fork.sh" | tail -1)
cd "$SRC/backend"
grep -q '^replace github.com/purpshell/meowcaller' go.mod ||
    printf '\nreplace github.com/purpshell/meowcaller => %s\n' "$FORK" >> go.mod
GOFLAGS=-mod=mod GOTOOLCHAIN=local go mod tidy > /dev/null
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -tags meego \
    -ldflags "-s -w" -o "$OUT/wa-backend" .
echo "== wa-backend fertig ($(stat -c %s "$OUT/wa-backend") B)"

# --- Oberflaeche -----------------------------------------------------------
CXX=$XGCC/bin/arm-none-linux-gnueabi-g++
[ -x "$CXX" ] || { echo "Cross-Compiler fehlt: $CXX (meego/toolchain.sh aus harbour-snapszer)" >&2; exit 1; }
MOC=$SIMQT/bin/moc
QTINC=$SYSROOT/usr/include/qt4

CXXFLAGS="--sysroot=$SYSROOT -std=gnu++17 -O2 -Wall -Wno-register \
 -Wno-deprecated-declarations -DQT_NO_DEBUG -I$QTINC -I$SRC/meego"
for m in QtCore QtGui QtNetwork QtScript QtDeclarative; do
    CXXFLAGS="$CXXFLAGS -I$QTINC/$m"
done
LDFLAGS="--sysroot=$SYSROOT -static-libstdc++ -static-libgcc -Wl,-O1 \
 -Wl,--as-needed -Wl,--exclude-libs,ALL -Wl,--dynamic-linker=/lib/ld-linux.so.3"
LIBS="-lQtDeclarative -lQtScript -lQtNetwork -lQtGui -lQtCore -lpthread"

cd "$OUT"
$MOC "$SRC/meego/src/Backend.h" -o moc_Backend.cpp
OBJS=""
for s in moc_Backend; do
    $CXX $CXXFLAGS -c "$s.cpp" -o "$s.o"; OBJS="$OBJS $s.o"
done
for s in src/Json src/Backend; do
    n=$(basename "$s")
    $CXX $CXXFLAGS -c "$SRC/meego/$s.cpp" -o "$n.o"; OBJS="$OBJS $n.o"
done
$CXX $CXXFLAGS -c "$SRC/meego/main.cpp" -o main.o; OBJS="$OBJS main.o"
$CXX $LDFLAGS -o harbour-whatsapp $OBJS $LIBS
echo "== harbour-whatsapp fertig ($(stat -c %s harbour-whatsapp) B)"
REMOTE_EOF

scp -q /tmp/wa-meego-remote.sh "$HOST:/tmp/"
ssh "$HOST" "sh /tmp/wa-meego-remote.sh"

mkdir -p build/meego
rsync -a "$HOST:$REMOTE/build/meego/harbour-whatsapp" \
         "$HOST:$REMOTE/build/meego/wa-backend" build/meego/
echo "== build/meego bereit"

#!/bin/sh
# Legt auf dem Bau-Rechner eine gepatchte Kopie von meowcaller an.
#
# Warum ueberhaupt: der MLow-Kodierer der Bibliothek braucht auf der N950
# fuer einen 60-ms-Rahmen 273 ms. Die Patches in backend/patches/ bringen
# ihn auf 90 ms -- siehe die Messreihe im dortigen README. Sie gehoeren in
# die Bibliothek, nicht ins Backend, deshalb dieser Umweg.
#
# Aufgespielt wird auf eine Kopie aus dem Modul-Zwischenspeicher; der
# Zwischenspeicher selbst bleibt unangetastet (er ist schreibgeschuetzt und
# wird von anderen Projekten mitbenutzt).
#
#   meego/meowcaller-fork.sh [zielverzeichnis]
set -e
SRC=$(cd "$(dirname "$0")/.." && pwd)
ZIEL=${1:-/tmp/meowcaller-fix}

VERSION=$(awk '/github.com\/purpshell\/meowcaller /{print $2; exit}' "$SRC/backend/go.mod")
[ -n "$VERSION" ] || { echo "meowcaller steht nicht in backend/go.mod" >&2; exit 1; }

# Ohne diesen Schritt fehlt das Modul, wenn der Rechner es noch nie geholt hat.
( cd "$SRC/backend" && GOFLAGS=-mod=mod GOTOOLCHAIN=local go mod download github.com/purpshell/meowcaller )

CACHE="$(go env GOMODCACHE)/github.com/purpshell/meowcaller@$VERSION"
[ -d "$CACHE" ] || { echo "nicht im Zwischenspeicher: $CACHE" >&2; exit 1; }

rm -rf "$ZIEL"
cp -r "$CACHE" "$ZIEL"
chmod -R u+w "$ZIEL"

for p in "$SRC"/backend/patches/*.patch; do
    [ -f "$p" ] || continue
    ( cd "$ZIEL" && patch -p1 --quiet < "$p" )
    echo "== aufgespielt: $(basename "$p")"
done
echo "$ZIEL"

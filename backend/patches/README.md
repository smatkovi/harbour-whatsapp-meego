# Was hier liegt und warum

Änderungen an `github.com/purpshell/meowcaller`, die dieses Gerät braucht.
Sie gehören in die Bibliothek, nicht ins Backend, und sind deshalb Patches:
`meego/meowcaller-fork.sh` legt beim Bauen eine Kopie des Moduls aus dem
Zwischenspeicher an und spielt sie darauf.

## Warum überhaupt

Der MLow-Kodierer schafft auf der N950 keine Echtzeit. Ein 60-ms-Rahmen
muss in 60 ms fertig sein; gemessen wurde mit `bench2` (Stille, Dauerton
und ein sprachähnliches Signal, je 20 Rahmen), auf dem sonst untätigen
Gerät:

| Stand | sprachähnlich | Stille |
|---|---|---|
| unverändert | ~273 ms | — |
| Drehtabelle statt `sin`/`cos` je Schmetterling | 85,2 ms | 75,4 ms |
| kein Feld je Rekursionsknoten | — | — |
| reelle FFT über die halbe Länge | 75,8 ms | 66,2 ms |
| Basis 2 und 3 ausgeschrieben | 64,6 ms | 54,2 ms |
| Pulssuche: Zustand gegriffen, Merkfeld wiederverwendet | **63,4 ms** | **52,8 ms** |

Dekodieren kostet unverändert 3,4 ms — deshalb hört man die Gegenseite
einwandfrei, während sie von uns nur Bruchstücke bekam.

## Was der Patch macht

**Drehtabelle.** Das Profil zeigte `math.cos` mit 40 % und `math.sin` mit
10 % der Rechenzeit, beide aus `fftRec`: jeder Schmetterling rechnete
seinen Drehfaktor neu aus. Es gibt aber je Länge nur n verschiedene, und
sie ändern sich nie.

**Kein Feld je Rekursionsknoten.** `fftRec` legte an jedem Knoten ein Feld
an — bei Länge 512 mehrere hundert je FFT. Jetzt ein Kratzfeld von 2n aus
einem Pool.

**Reelle FFT.** Der reelle Eingang ging als komplexe Folge mit lauter
Nullen im Imaginärteil in eine FFT voller Länge. Jetzt werden je zwei
Abtastwerte zu einer komplexen Zahl gepackt, eine FFT halber Länge
gerechnet und die beiden Spektren in linearer Zeit wieder getrennt.

**Basis 2 und 3 ausgeschrieben.** Der allgemeine Weg rechnet je
Ausgabewert eine Schleife mit Modulo-Rechnung und einer Multiplikation
auch dann, wenn der Drehfaktor 1 ist. Bei 512 und 576 kommen nur die
Basen 2 und 3 vor.

**Pulssuche.** `sc.fcbStates[wi][idx].num[i]` sind drei Indizierungen mit
je einer Bereichsprüfung, achtzigmal je Puls, tausendfach je Teilrahmen.
Jetzt einmal gegriffen. Das Merkfeld in `celpGetMaxiK` wird
wiederverwendet statt je Aufruf neu angelegt (der Sammler stand mit 13 %
im Profil).

Geprüft ist die FFT gegen die schlichte Doppelsumme (`naiveDft`) für alle
vorkommenden Längen, in beiden Richtungen — nicht nur gegen die alte
Fassung. Die Tests liegen im Patch (`mlow/fft_*_test.go`) und laufen beim
Bauen mit.

## Was NICHT hilft

* **Suchtiefe absenken** (`smplFcbTotSurv20msMax`, `smplLsfSurv`): macht den
  Kodierer *langsamer*, weil der Ratenregler auf eine schlechtere Suche mit
  mehr Pulsen antwortet. 100 Überlebende 90 ms, 40 → 100 ms, 25 → 167 ms.
* **Polarform (Euler) für die komplexen Zahlen**: die Multiplikation würde
  billiger (Beträge mal, Winkel plus), aber die Addition bräuchte je
  Schmetterling ein Hin- und Herrechnen mit `sin`, `cos` und `atan2` — und
  eine FFT besteht überwiegend aus Additionen.

## Was noch offen ist

Bei 63,4 ms fehlen gut 5 % auf Echtzeit, mit Stille bei 52,8 ms holt der
Kodierer in Sprechpausen wieder auf. Weiter ginge es mit:

* **DTX**: Stille erkennen und die Pulssuche ganz überspringen. Ein
  Stille-Rahmen kostet heute 52,8 ms für 24 Byte Ergebnis.
* **NEON**: die Pulssuche ist jetzt der größte Posten (`addPulse` 24 %,
  `celpGetMaxiK` 10 %, `celpDotProd` 5 %). Gos Übersetzer vektorisiert
  nicht, und sein ARM-Assembler kennt NEON nur über rohe Wortkodierung —
  der bequeme Weg wäre cgo mit der MADDE-Werkzeugkette und
  `-mfpu=neon -O3`, was den statischen Bau dieses Ports aufgibt.

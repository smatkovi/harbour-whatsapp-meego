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
| Pulssuche: Zustand gegriffen, Merkfeld wiederverwendet | 63,4 ms | 52,8 ms |
| DCT-Tabellen einmal bauen, Drehtabelle durchgereicht | **52,8 ms** | **41,1 ms** |

Damit ist der Kodierer **unter der Grenze**: ein 60-ms-Rahmen braucht
52,8 ms, in Sprechpausen 41,1 ms. Vorher war er um den Faktor 4,5 zu
langsam.

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

**DCT-Tabellen einmal bauen.** `buildDctTables()` lief bei **jeder**
LPC-Analyse: 2048 Aufrufe von `math.Cos` und 16 KB, die als Rückgabewert
kopiert wurden — je Rahmen, obwohl die Tabellen nur an Konstanten hängen.
Im Profil eines Stille-Rahmens stand `math.cos` deshalb immer noch bei
9 %, lange nachdem die FFT ihre Winkelfunktionen los war.

**Drehtabelle durchreichen.** Jeder Rekursionsknoten holte sie aus einer
`sync.Map` — 9 % im selben Profil. Sie gilt aber für alle Ebenen: die
Drehfaktoren der halben Länge sind jeder zweite Eintrag, die der viertel
Länge jeder vierte. Ein Schrittmaß genügt, und die Tabelle wird einmal je
FFT geholt.

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

## NEON: gemessen, verworfen

Drei Befunde, jeder einzeln geprüft:

* **Gos ARM-Assembler kennt NEON nicht.** `VLD1`, `VADDF` und Verwandte
  werden abgelehnt; es bliebe rohe Wortkodierung je Befehl.
* **cgo kostet 1212 ns je Übergang** (auf dem Gerät gemessen, leerer
  Aufruf). Ein Skalarprodukt über 80 Werte dauert 286 ns — die Grenze
  frisst das Vierfache dessen, was sie beschleunigen soll.
* **Die NEON-Fassung ist nicht schneller.** clang vektorisiert sauber
  (`vld1.32`, `vmul.f32 q9`, `vadd.f32 q8` im Disassemblat), aber ohne
  Übergang gemessen braucht sie 333 ns gegen 286 ns in Go. Die Schleifen
  sind kurz und ladegebunden, nicht rechengebunden.

Der cgo-Bau selbst funktioniert übrigens: clang mit
`--target=armv7-unknown-linux-gnueabi -mfloat-abi=softfp` und `lld`
erzeugt ein Binär, das auf dem Gerät läuft (die GCC-14-Werkzeugkette
scheidet aus, sie ist `--with-float=hard` gebaut und kollidiert mit Gos
weichem Gleitkomma-ABI). Nur lohnt es nicht.

Auch geprüft: **GC-Einstellungen** bringen nichts (`GOGC=off`: 62,7 statt
63,2 ms).

## DTX: Sprechpausen kosten nichts mehr

Mit `MLOW_DTX=1` schickt der Kodierer in einer Pause nur das TOC-Byte
`0x10` ("16 kHz, 60 ms, nicht aktiv") und sonst nichts. Der Dekodierer
liest bei einem inaktiven Rahmen den Rumpf gar nicht erst, sondern gibt
Stille aus (`decoder.go`: "inactive/SID, emitting silence").

| | ohne DTX | mit DTX |
|---|---|---|
| Stille | 41,2 ms, 24 Byte | **0,5 ms, 1 Byte** |
| Sprache | 52,5 ms, 108 Byte | 52,5 ms, 108 Byte |

Im Backend hängt es an der Einstellung `call_dtx` (`/prefs/set?call_dtx=1`),
**standardmäßig aus**. Zwei Dinge sind ungeprüft:

* wie WhatsApps eigener Dekodierer auf einen Rahmen aus einem einzigen
  Byte reagiert — unserer ist nachgebaut, seiner nicht;
* ob das erste Wort nach einer Pause leidet. Die Analyse hält während der
  Pause ihren Zustand an, und der stammt dann noch aus der Zeit davor.
  Richtige DTX-Umsetzungen lassen die billigen Teile (Hochpass, Verlauf)
  weiterlaufen; das wäre der nächste Schritt, falls man es hört.

## Komfortrauschen: die Gegenrichtung

Mit `MLOW_CNG=1` füllt der Dekodierer einen inaktiven Rahmen mit leisem
Rauschen statt mit lauter Nullen.

WhatsApp hat DTX in seinen `voip_settings` stehen, schickt in
Sprechpausen also selbst inaktive Rahmen. Bisher gab der Dekodierer dafür
digitale Stille aus — und das klingt nicht nach einer Pause, sondern nach
einer abgerissenen Leitung: zwischen zwei Wörtern verschwindet auch das
Grundgeräusch des Raumes, das man die ganze Zeit gehört hat.

Die Höhe des Rauschens schätzt der Dekodierer selbst, aus den leisesten
Stellen der zuletzt dekodierten Sprache (Minimumstatistik: sofort herunter
auf ein neues Minimum, 2,5 % je Rahmen wieder hinauf). Die inaktiven
Rahmen tragen keine Angaben dazu — von ihnen wird nur das erste Byte
gelesen. Weißes Rauschen klingt scharf, deshalb ein einpoliger Tiefpass;
eingeblendet wird über den ersten Rahmen, sonst klickt der Übergang; und
nach oben ist hart gedeckelt (−46 dBFS), lieber zu leise als ein Zischen
über dem Gespräch.

Im Backend an der Einstellung `call_cng`, standardmäßig aus.

## Was noch offen ist

Die Grenze ist unterschritten, Reserve bleibt wenig: 12 % bei Sprache.
Wenn mehr gebraucht wird:

* **DTX**: Stille erkennen und die Pulssuche überspringen. Ein
  Stille-Rahmen kostet heute 41,1 ms für 24 Byte Ergebnis — da liegt noch
  viel.
* **`SmplMem.regionFor`** stand im letzten Profil bei 9 %; das ist eine
  Nachschlagefunktion, keine Rechnung.

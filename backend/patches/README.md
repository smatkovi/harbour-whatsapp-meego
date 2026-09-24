# Was hier liegt und warum

Änderungen an `github.com/purpshell/meowcaller`, die dieses Gerät braucht.
Sie gehören nicht ins Backend, sondern in die Bibliothek, und sind deshalb
Patches statt Quelltext: aufgespielt wird auf eine Kopie des Moduls aus dem
Zwischenspeicher, auf die eine `replace`-Zeile zeigt.

## mlow-fft-drehtabelle.patch

Der MLow-Kodierer schafft auf der N950 keine Echtzeit. Gemessen mit einem
Testton, auf dem sonst untätigen Gerät, 25 Rahmen am Stück:

| Stand | je 60-ms-Rahmen | erlaubt |
|-------|-----------------|---------|
| unverändert | 273 ms | 60 ms |
| mit Drehtabelle | 95 ms | 60 ms |
| dazu ohne Zwischenfelder | 90 ms | 60 ms |

Dekodieren kostet 3,4 ms — deshalb hört man die Gegenseite einwandfrei,
während sie von uns nur Bruchstücke bekommt.

Der Patch macht zwei Dinge:

**Drehtabelle.** Das Profil des Kodierers zeigte `math.cos` mit 40 % und
`math.sin` mit 10 % der gesamten Rechenzeit, aufgerufen aus `fftRec` (66 %
kumulativ): jeder einzelne Schmetterling rechnete seinen Drehfaktor neu
aus. Die Winkel sind aber immer `sign*2*pi*(k*j mod n)/n` — es gibt nur n
verschiedene, und sie ändern sich nie. Einmal je Länge gerechnet, danach
nachgeschlagen. Das allein bringt den Faktor 2,9.

**Kein Feld je Rekursionsknoten.** `fftRec` legte an jedem Knoten ein
eigenes Feld an; bei Länge 512 sind das mehrere hundert je FFT, und im
Kodierer laufen mehrere FFTs je Rahmen. Jetzt reicht ein Kratzfeld von 2n,
das über einen Pool wiederverwendet wird.

Die Tests des Moduls (`go test ./mlow/`) laufen mit dem Patch durch.

## Was NICHT hilft

Die Suchtiefe abzusenken (`smplFcbTotSurv20msMax`, `smplLsfSurv`) macht den
Kodierer **langsamer**, nicht schneller: der Ratenregler antwortet auf eine
schlechtere Suche mit mehr Pulsen. Gemessen: 100 Überlebende 90 ms,
40 → 100 ms, 25 → 167 ms.

## Was noch offen ist

Bis zur Echtzeit fehlt gut der Faktor 1,5. In Reichweite:

* echte Reell-FFT statt komplexer FFT über reelle Eingaben (halbiert die
  FFT-Arbeit; die FFT sind nach dem Patch noch 36 %)
* iterative Radix-2/3-FFT statt der rekursiven gemischten Basis
* NEON für die heißen Schleifen (komplexe Multiplikation, Skalarprodukte,
  Pulssuche). Der Cortex-A8 hat es, Gos Übersetzer nutzt es nicht von
  selbst — das hieße Assembler von Hand oder cgo, und cgo bricht den
  statischen Bau dieses Ports.

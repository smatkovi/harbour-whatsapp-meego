//go:build meego

// Was auf MeeGo Harmattan (Nokia N9/N950) gilt.
//
// Der entscheidende Unterschied ist CGO. Harmattan hat glibc 2.10 und einen
// 2.6.32-Kernel; ein Go-Binary laeuft dort einwandfrei, aber nur solange es
// statisch ist und nichts gegen die uralte C-Bibliothek bindet. SQLCipher ist
// C und braucht CGO, also tritt hier modernc.org/sqlite an - SQLite in reinem
// Go, auf dem N950 gemessen mit 3.53.4.
//
// Der Preis: die Nachrichtendatenbank liegt unverschluesselt. Auf Sailfish
// waere das inakzeptabel, weil es dort Sailfish Secrets gibt; auf Harmattan
// gibt es keinen Schluesseldienst, hinter dem der Schluessel besser aufgehoben
// waere als in einer Datei mit Modus 0600 - und die Partition ist ohnehin
// nicht verschluesselt. Lieber ehrlich unverschluesselt als
// scheinverschluesselt mit dem Schluessel daneben.
package main

import (
	_ "modernc.org/sqlite"
)

// Treiber, den modernc.org/sqlite registriert - "sqlite", nicht "sqlite3".
const dbDriverName = "sqlite"

// Hier ist die Klartext-Datenbank der Normalfall, kein Grund anzuhalten.
const enforceEncryptedDB = false

// Die Parameter sind nicht dieselben wie bei SQLCipher, und das ist keine
// Kosmetik: "_foreign_keys=on" ist mattn-Syntax, modernc.org/sqlite kennt
// nur "_pragma=...". Mit der uebernommenen Zeichenkette blieben die
// Fremdschluessel also aus -- und vor allem fehlte jede Wartezeit bei
// belegter Datenbank. whatsmeow schreibt den Verlauf, den App-State und die
// Signal-Sitzungen gleichzeitig; ohne busy_timeout scheitert der Zweite
// sofort mit SQLITE_BUSY. Auf dem N950 sah das so aus:
//
//     app state sync critical_block failed: ... database is locked (5)
//     Error decrypting message ...: failed to load session: database is locked
//
// Die Nachricht war nicht verloren, nur unentschluesselbar - whatsmeow
// schickt eine Wiederholungsquittung. Aber der Kontaktabgleich blieb leer.
//
// journal_mode(WAL) laesst Leser und Schreiber nebeneinander arbeiten,
// busy_timeout gibt dem Zweiten zehn Sekunden statt sofort aufzugeben, und
// _txlock=immediate nimmt die Schreibsperre gleich zu Beginn einer
// Transaktion statt mittendrin - das vermeidet die Verklemmung zweier
// Schreiber, die beide erst lesen.
func getDBConnectionString() string {
	return "file:wa.db?" +
		"_pragma=busy_timeout(10000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"
}

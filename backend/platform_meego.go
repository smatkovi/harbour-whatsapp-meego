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

func getDBConnectionString() string {
	return "file:wa.db?_foreign_keys=on"
}

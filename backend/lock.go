package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Eine exklusive Sperre auf dem Datenverzeichnis.
//
// Der Anlass war Datenverlust: waehrend der Entwicklung liefen zeitweise zwei
// Backends nebeneinander (das zweite fand die HTTP-Ports belegt, lief aber
// trotzdem weiter), und beim Beenden schrieb eines davon seinen leeren
// Nachrichtenspeicher ueber den vollen. Uebrig blieb eine Datei mit dem
// Inhalt "null" -- der ganze zwischengespeicherte Verlauf war weg.
//
// watchForDaemon reicht dagegen nicht: es fragt die HTTP-Ports ab und tritt
// zurueck, wenn es dort einen Daemon findet -- aber erst nach 15 Sekunden,
// und bis dahin hat die zweite Instanz die Dateien laengst geoeffnet.
//
// flock ist hier das richtige Mittel: es haengt am Dateideskriptor und
// verschwindet mit dem Prozess, auch wenn er abstuerzt. Kein verwaister
// Sperreintrag, um den sich jemand kuemmern muesste.
var sperrDatei *os.File

// sperreNehmen belegt das Datenverzeichnis. Gelingt das nicht, laeuft schon
// eine andere Instanz und diese hier soll sich beenden.
func sperreNehmen(verzeichnis string) error {
	pfad := filepath.Join(verzeichnis, ".lock")
	f, err := os.OpenFile(pfad, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		// Kein Verzeichnis, kein Platz -- lieber weiterlaufen als gar nicht
		// starten. Die Sperre ist eine Absicherung, keine Voraussetzung.
		fmt.Printf("⚠ Sperre nicht anlegbar (%v) - laufe ohne\n", err)
		return nil
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("eine andere Instanz haelt %s", pfad)
	}
	// Offen halten: mit dem Deskriptor faellt die Sperre.
	sperrDatei = f
	fmt.Fprintf(f, "%d\n", os.Getpid())
	return nil
}

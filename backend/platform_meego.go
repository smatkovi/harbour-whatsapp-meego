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
	"fmt"
	"os"
	"path/filepath"

	"github.com/godbus/dbus/v5"

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
//	app state sync critical_block failed: ... database is locked (5)
//	Error decrypting message ...: failed to load session: database is locked
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

// medienWurzel: auf Harmattan liegen Bilder, Videos, Musik und Dokumente
// unter MyDocs. Das ist die VFAT-Partition, die auch am USB-Kabel erscheint,
// und der Tracker indiziert ausschliesslich sie -- ein Anhang unterhalb von
// ~/Documents ist zwar heruntergeladen, aber fuer Galerie und Dokumente-App
// unsichtbar.
func medienWurzel(homeDir string) string {
	myDocs := filepath.Join(homeDir, "MyDocs")
	if st, err := os.Stat(myDocs); err == nil && st.IsDir() {
		return myDocs
	}
	return homeDir
}

// sipAnruf laesst das Telefon klingeln und gibt die beiden Tonenden zurueck.
//
// Gibt es keine Bruecke oder ist kein Telefon registriert, meldet es false
// und der Anruf laeuft wie bisher ueber PulseAudio in der App.
func sipAnruf(name, nummer string, beiAuflegen func()) (meowcallerQuelle, meowcallerSenke, bool) {
	if bruecke == nil {
		return nil, nil, false
	}
	bruecke.mu.Lock()
	bereit := bruecke.registriert
	bruecke.mu.Unlock()
	if !bereit {
		return nil, nil, false
	}
	if err := bruecke.klingeln(name, nummer, beiAuflegen); err != nil {
		fmt.Println("📞 SIP: klingeln:", err)
		return nil, nil, false
	}
	return sipQuelle{bruecke}, sipSenke{bruecke}, true
}

func sipAuflegen() {
	if bruecke != nil {
		bruecke.auflegen()
	}
}

func sipStarten() { sipBrueckeStarten() }

// policyMutesStreams sagt, ob eine Richtlinienschicht frisch angelegte
// Stroeme stummschaltet und deshalb periodisch zurueckgesetzt werden muss.
//
// Auf Harmattan nicht: hier gibt es keinen solchen Waechter -- und das
// Gegenmittel war schlimmer als die Krankheit. PulseAudio ist auf diesem
// Geraet 0.9.19, und SET_SOURCE_OUTPUT_MUTE kam erst Jahre spaeter dazu.
// Der Server findet das Kommando nicht in seiner Tabelle, wertet das als
// Protokollverstoss und wirft die Verbindung weg -- mitten im Anruf.
//
// Im Protokoll sah das so aus: keepUnmuted feuert nach 500 ms, der
// Mikrofonzaehler blieb bei 10240 Samples (0,64 s) stehen, und ab 700 ms
// beantwortete PulseAudio jede Latenzabfrage mit EOF. Beide Stroeme
// standen danach auf serverLost, also rec=false play=false: das Gegenueber
// war zu hoeren gewesen -- die RTP-Pakete kamen an und wurden dekodiert --,
// aber nichts davon erreichte je den Lautsprecher, und das Mikrofon lieferte
// nichts mehr.
func policyMutesStreams() bool { return false }

// --- Musikwiedergabe waehrend eines Anrufs -------------------------------
//
// Harmattan hat dafuer einen Richtliniendienst (org.maemo.resource.manager),
// der einer Anwendung mit Anruf-Klasse die Tonausgabe zuteilt und die Musik
// dabei anhaelt. Da hineinzukommen hiesse, sich als Anruf-Anwendung
// anzumelden -- und derselbe Weg ueber com.nokia.mce, den wir dafuer
// brauchten, wird uns vom Bus verweigert ("Rejected send message ...
// req_call_state_change"). Aegis vergibt diese Rechte nur an signierte
// Pakete.
//
// Was bleibt, ist der direkte Weg: MAFW ist der Wiedergabedienst hinter der
// Musik-App, und er nimmt Befehle von jedem auf dem Sitzungsbus entgegen.
// get_status liefert den Zustand als viertes Feld -- 1 heisst "spielt".
const (
	mafwDienst = "com.nokia.mafw.renderer.MafwGstRendererPlugin.mafw_gst_renderer"
	mafwPfad   = "/com/nokia/mafw/renderer/mafw_gst_renderer"
	mafwIface  = "com.nokia.mafw.renderer"
	mafwSpielt = int32(1)
)

// Nur fortsetzen, wenn wir es waren, die angehalten haben -- sonst faengt
// nach dem Auflegen Musik an, die vorher gar nicht lief.
var musikVonUnsPausiert bool

func mafwObjekt() (*dbus.Conn, dbus.BusObject) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, nil
	}
	return conn, conn.Object(mafwDienst, dbus.ObjectPath(mafwPfad))
}

func musikPausieren() {
	_, obj := mafwObjekt()
	if obj == nil {
		return
	}
	var playlist string
	var index uint32
	var zustand int32
	var objektId string
	if err := obj.Call(mafwIface+".get_status", 0).Store(
		&playlist, &index, &zustand, &objektId); err != nil {
		// Laeuft die Musik-App gar nicht, gibt es auch nichts anzuhalten.
		return
	}
	if zustand != mafwSpielt {
		return
	}
	if err := obj.Call(mafwIface+".pause", 0).Err; err != nil {
		fmt.Printf("🎵 Musik nicht anzuhalten: %v\n", err)
		return
	}
	musikVonUnsPausiert = true
	fmt.Println("🎵 Musikwiedergabe fuer den Anruf angehalten")
}

func musikFortsetzen() {
	if !musikVonUnsPausiert {
		return
	}
	musikVonUnsPausiert = false
	_, obj := mafwObjekt()
	if obj == nil {
		return
	}
	if err := obj.Call(mafwIface+".resume", 0).Err; err != nil {
		fmt.Printf("🎵 Musik nicht fortzusetzen: %v\n", err)
		return
	}
	fmt.Println("🎵 Musikwiedergabe fortgesetzt")
}

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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

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
func sipAnruf(name, nummer string, beiAnnahme, beiAuflegen func()) (meowcallerQuelle, meowcallerSenke, bool) {
	if bruecke == nil {
		return nil, nil, false
	}
	bruecke.mu.Lock()
	bereit := bruecke.registriert
	bruecke.mu.Unlock()
	if !bereit {
		return nil, nil, false
	}
	if err := bruecke.klingeln(name, nummer, beiAnnahme, beiAuflegen); err != nil {
		fmt.Println("📞 SIP: klingeln:", err)
		return nil, nil, false
	}
	return sipQuelle{bruecke}, sipSenke{bruecke}, true
}

// sipLaeuft sagt, ob gerade ein SIP-Gespraech steht -- dann sind die
// Tonenden schon da und es waere falsch, ein zweites Mal zu klingeln.
func sipLaeuft() bool {
	if bruecke == nil {
		return false
	}
	bruecke.mu.Lock()
	defer bruecke.mu.Unlock()
	return bruecke.laeuft
}

// sipStroeme gibt die Tonenden einer bereits stehenden Verbindung.
func sipStroeme() (meowcallerQuelle, meowcallerSenke) {
	return sipQuelle{bruecke}, sipSenke{bruecke}
}

func sipAuflegen() {
	if bruecke != nil {
		bruecke.auflegen()
	}
}

func sipStarten() {
	sipBrueckeStarten()
	// Nach dem Start der Bruecke nachsehen, ob sich ein Telefon anmeldet.
	// Tut es das nicht, klingelt kein eingehender Anruf -- und das faellt
	// erst auf, wenn einer verpasst wurde. Der Anstoss kostet nichts,
	// wenn ohnehin alles steht.
	go func() {
		time.Sleep(20 * time.Second)
		if bruecke == nil {
			return
		}
		bruecke.mu.Lock()
		bereit := bruecke.registriert
		bruecke.mu.Unlock()
		if bereit {
			return
		}
		fmt.Println("📞 SIP: nach 20 s kein Telefon angemeldet")
		sipKontoAnstossen()
	}()
}

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

// sipKontoAnstossen bittet Mission Control, das SIP-Konto neu anzumelden.
//
// Die Bruecke verliert bei jedem Neustart des Dienstes ihren registrierten
// Kontakt; sofiasip merkt das nicht und meldet sich erst zur naechsten
// Erneuerung wieder an. Bis dahin klingelt kein eingehender Anruf -- genau
// das war im Feld zu sehen, nachdem ein Upgrade den Dienst neu gestartet
// hatte.
//
// Der Anstoss ist ein Umweg ueber die gewuenschte Anwesenheit: erst
// offline, dann verfuegbar. Das laesst Mission Control die Verbindung neu
// aufbauen, und damit kommt ein frisches REGISTER.
func sipKontoAnstossen() {
	conn, err := dbus.SessionBus()
	if err != nil {
		return
	}
	verwalter := conn.Object("org.freedesktop.Telepathy.AccountManager",
		dbus.ObjectPath("/org/freedesktop/Telepathy/AccountManager"))
	var konten dbus.Variant
	if err := verwalter.Call("org.freedesktop.DBus.Properties.Get", 0,
		"org.freedesktop.Telepathy.AccountManager", "ValidAccounts").Store(&konten); err != nil {
		return
	}
	pfade, ok := konten.Value().([]dbus.ObjectPath)
	if !ok {
		return
	}
	for _, p := range pfade {
		if !strings.Contains(string(p), "sofiasip/sip/whatsapp") {
			continue
		}
		konto := conn.Object("org.freedesktop.Telepathy.AccountManager", p)
		setzen := func(art uint32, name string) {
			_ = konto.Call("org.freedesktop.DBus.Properties.Set", 0,
				"org.freedesktop.Telepathy.Account", "RequestedPresence",
				dbus.MakeVariant(struct {
					Art     uint32
					Name    string
					Meldung string
				}{art, name, ""})).Err
		}
		fmt.Println("📞 SIP: stosse Konto zur Neuanmeldung an")
		setzen(1, "offline")
		time.Sleep(2 * time.Second)
		setzen(2, "available")
		return
	}
}

// --- Klingelmeldung ------------------------------------------------------
//
// Der Benachrichtigungsdienst von Harmattan heisst nicht
// org.freedesktop.Notifications -- den gibt es hier gar nicht, und der
// Versuch endete mit "was not provided by any .service files". Er heisst
// com.meego.core.MNotificationManager, und welche Art Meldung daraus wird,
// entscheidet der Ereignistyp: unser harbour-whatsapp.call traegt
// class=system, und das erscheint ueber allem, auch am Sperrbildschirm.

const meldungsTyp = "harbour-whatsapp.call"

func plattformKlingelmeldung(titel, name string) (uint32, bool) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return 0, false
	}
	obj := conn.Object("com.meego.core.MNotificationManager",
		dbus.ObjectPath("/notificationmanager"))

	var kennung uint32
	if err := obj.Call("com.meego.core.MNotificationManager.notificationUserId",
		0).Store(&kennung); err != nil {
		fmt.Printf("📞 Meldung: keine Kennung (%v)\n", err)
		return 0, false
	}

	// Beim Antippen holt der Dienst die App nach vorn. Die Aktion ist ein
	// D-Bus-Aufruf in Textform: Dienst, Pfad, Schnittstelle, Methode.
	aktion := dienstName + " / " + dienstName + " Anzeigen"

	var id uint32
	ruf := obj.Call("com.meego.core.MNotificationManager.addNotification", 0,
		kennung, uint32(0), meldungsTyp, titel, name, aktion,
		"icon-m-telephony-call-ongoing", uint32(1), "whatsapp-anruf")
	if ruf.Err != nil {
		fmt.Printf("📞 Meldung nicht absetzbar: %v\n", ruf.Err)
		return 0, false
	}
	if err := ruf.Store(&id); err != nil {
		return 0, false
	}
	fmt.Printf("📞 Klingelmeldung %d abgesetzt\n", id)
	return id, true
}

func plattformMeldungSchliessen(id uint32) bool {
	if id == 0 {
		return true
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	obj := conn.Object("com.meego.core.MNotificationManager",
		dbus.ObjectPath("/notificationmanager"))
	var kennung uint32
	if err := obj.Call("com.meego.core.MNotificationManager.notificationUserId",
		0).Store(&kennung); err != nil {
		return false
	}
	_ = obj.Call("com.meego.core.MNotificationManager.removeNotification", 0,
		kennung, id).Err
	return true
}

// --- Anrufzustand fuer MCE ----------------------------------------------
//
// req_call_state_change verweigert der Bus unsignierten Paketen: das Recht
// heisst mce::CallStateControl, und Aegis vergibt es hier nicht -- auch im
// Open Mode nicht, aegis-exec -a laesst die Rechteliste unveraendert.
//
// mcetool hat es, weil es als root laeuft. Und MCE bindet den Zustand an
// die Verbindung dessen, der ihn setzt: ein mcetool, das sich sofort
// beendet, aendert nichts. Deshalb wird eines mit --block gehalten,
// solange der Anruf dauert, und beim Ende beendet.
var mceHalter *exec.Cmd
var mceMu sync.Mutex

func plattformAnrufZustand(zustand string) bool {
	mceMu.Lock()
	defer mceMu.Unlock()

	// Den bisherigen Halter loslassen -- ein zweiter waere ein zweiter
	// Anspruch auf denselben Zustand.
	if mceHalter != nil && mceHalter.Process != nil {
		_ = mceHalter.Process.Kill()
		go func(c *exec.Cmd) { _ = c.Wait() }(mceHalter)
		mceHalter = nil
	}
	if zustand == "none" || zustand == "" {
		return true
	}

	befehl := exec.Command("sudo", "mcetool",
		"--set-call-state="+zustand+":normal", "--block")
	// Der Halter muss mit uns sterben. Ueberlebt er einen Absturz, glaubt
	// das Telefon fuer immer, es klingle -- der Bildschirm bliebe an, die
	// Tastensperre aus, und kein echter Anruf kaeme mehr richtig durch.
	// Pdeathsig sorgt dafuer, dass der Kern ihn mitnimmt.
	befehl.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := befehl.Start(); err != nil {
		fmt.Printf("📞 mcetool nicht startbar (%v)\n", err)
		return false
	}
	mceHalter = befehl
	fmt.Printf("📞 MCE-Anrufzustand %q wird gehalten (pid %d)\n",
		zustand, befehl.Process.Pid)
	return true
}

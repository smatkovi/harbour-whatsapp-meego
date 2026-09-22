//go:build meego

// Netzwaechter fuer Harmattan.
//
// connman gibt es hier nicht -- der Verbindungsdienst heisst icd2. Der
// vorhandene Waechter meldet deshalb "state=unknown", und weil
// watchdogShouldConnect nur bei "offline" und "idle" abbricht, versucht das
// Backend die Verbindung trotzdem. Es funktioniert also auch ohne diese
// Datei, nur eben erst beim naechsten Durchlauf des periodischen Waechters:
// bis zu 60 Sekunden, nachdem WLAN oder GPRS zurueck sind.
//
// icd2 liefert den Anstoss sofort. Es sitzt auf dem System-Bus, kennt
// state_req zum Abfragen und schickt state_sig bei jeder Aenderung.
package main

import (
	"fmt"
	"time"

	"github.com/godbus/dbus/v5"
)

// icd2-Zustaende, wie sie state_sig meldet.
const (
	icdDisconnected = 0
	icdConnecting   = 1
	icdConnected    = 2
)

func watchNetworkPlatform() {
	conn, err := dbus.SystemBus()
	if err != nil {
		fmt.Printf("⚠ icd2: kein System-Bus (%v) - nur periodischer Waechter\n", err)
		return
	}
	if merr := conn.AddMatchSignal(
		dbus.WithMatchInterface("com.nokia.icd2"),
		dbus.WithMatchMember("state_sig"),
	); merr != nil {
		fmt.Printf("⚠ icd2: Signal nicht abonnierbar (%v) - nur periodischer Waechter\n", merr)
		return
	}

	// Einmal den aktuellen Zustand anstossen, damit die Antwort als
	// state_sig hereinkommt und netState gleich stimmt.
	obj := conn.Object("com.nokia.icd2", dbus.ObjectPath("/com/nokia/icd2"))
	_ = obj.Call("com.nokia.icd2.state_req", 0).Err

	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)
	fmt.Println("📶 icd2-Waechter aktiv")

	// Der zuletzt gesehene Zustand. Er entscheidet, ob ein "verbunden"
	// eine Bestaetigung ist oder ein Wechsel -- und nur beim Wechsel muss
	// etwas geschehen.
	vorher := -1
	var letzterAbriss time.Time

	for sig := range ch {
		if sig.Name != "com.nokia.icd2.state_sig" {
			continue
		}
		// state_sig traegt mehrere Felder; der Zustand ist das letzte
		// uint32. Die Signatur unterscheidet sich je nach Variante des
		// Signals, deshalb wird von hinten gesucht statt fest indiziert.
		zustand := -1
		for i := len(sig.Body) - 1; i >= 0; i-- {
			if u, ok := sig.Body[i].(uint32); ok {
				zustand = int(u)
				break
			}
		}
		if zustand < 0 {
			continue
		}
		switch zustand {
		case icdConnected:
			netState = "online"
			if client == nil {
				break
			}
			if !client.IsConnected() {
				nudgeConnect("icd2: Netz wieder da")
				break
			}
			// Hier lag der Fehler: steht die alte Verbindung scheinbar
			// noch, geschah nichts. Nach einem Wechsel von WLAN auf GPRS
			// ist das aber der Normalfall -- die Gegenstelle ist weg, doch
			// der Rechner merkt es nicht, weil TCP von sich aus nichts
			// sagt. Bis der Keepalive zweimal ausfiel und der periodische
			// Waechter zugriff, vergingen Minuten; im Feldprotokoll stand
			// dann "connecting (watchdog)" statt eines icd2-Anstosses.
			//
			// Ein Wechsel von "nicht verbunden" nach "verbunden" heisst,
			// dass sich der Weg ins Netz geaendert hat. Dann wird aktiv
			// abgerissen statt nachgefragt.
			if vorher != -1 && vorher != icdConnected &&
				time.Since(letzterAbriss) > 20*time.Second {
				letzterAbriss = time.Now()
				fmt.Println("📶 icd2: anderer Weg ins Netz - Verbindung wird erneuert")
				go func() {
					client.Disconnect()
					time.Sleep(500 * time.Millisecond)
					nudgeConnect("icd2: Netzwechsel")
				}()
			}
		case icdDisconnected:
			netState = "idle"
		case icdConnecting:
			// Nichts tun: erst wenn die Verbindung steht, lohnt ein Versuch.
		}
		vorher = zustand
	}
}

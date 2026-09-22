//go:build meego

// Der Dienstname auf dem Sitzungsbus -- damit laeuft das Backend als Daemon.
//
// Harmattan hat keinen brauchbaren Weg, einen Benutzerdienst beim Start
// hochzuziehen: Jobs unter ~/.config/upstart liest niemand (nachgesehen:
// /etc/init enthaelt keinen Verweis darauf, und initctl kennt kein
// --session), und nach /etc/init/xsession/ kommt ein unsigniertes Paket
// nicht -- Aegis verweigert dort jede Datei ohne Referenz-Hash.
//
// Was geht, ist die Aktivierung ueber den Sitzungs-D-Bus. Ein darueber
// gestarteter Dienst laeuft als Besitzer des Busses, also als "user", mit
// HOME und Zugriff aufs Datenverzeichnis; und /usr/share/dbus-1/services/
// steht fremden Paketen offen. Dasselbe Muster traegt seit Monaten den
// Mastodon-Feed auf diesem Geraet.
//
// Dafuer muss der Dienst den Namen auch beanspruchen: ohne das gilt die
// Aktivierung als gescheitert, und D-Bus startet ihn beim naechsten Anstoss
// wieder -- eine Instanz nach der anderen.
package main

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

const dienstName = "org.smatkovi.WhatsApp"

// sitzungsNamenBeanspruchen meldet den Dienst am Sitzungsbus an.
//
// Scheitert es, laeuft das Backend trotzdem weiter: gestartet wurde es dann
// eben von der App und nicht von D-Bus, und das ist kein Fehler.
func sitzungsNamenBeanspruchen() {
	conn, err := dbus.SessionBus()
	if err != nil {
		fmt.Printf("⚠ Sitzungsbus nicht erreichbar (%v) - laeuft ohne Dienstnamen\n", err)
		return
	}
	antwort, err := conn.RequestName(dienstName, dbus.NameFlagDoNotQueue)
	if err != nil {
		fmt.Printf("⚠ Dienstname nicht zu haben (%v)\n", err)
		return
	}
	if antwort != dbus.RequestNameReplyPrimaryOwner {
		// Jemand haelt ihn schon -- zusammen mit der Dateisperre ist das der
		// zweite Riegel gegen zwei Instanzen.
		fmt.Printf("⚠ %s gehoert bereits einer anderen Instanz\n", dienstName)
		return
	}
	fmt.Printf("🔌 Dienstname %s beansprucht\n", dienstName)
}

//go:build !meego

// Was auf Sailfish gilt: verschluesselte Datenbank ueber SQLCipher, und der
// Schluessel kommt ausschliesslich aus Sailfish Secrets.
//
// Diese drei Werte sind der einzige Unterschied zur MeeGo-Fassung. Sie stehen
// hier statt in main.go, damit der Rest des Backends fuer beide Plattformen
// derselbe Quelltext bleibt.
package main

import (
	"encoding/hex"
	"fmt"

	_ "github.com/mutecomm/go-sqlcipher/v4"
)

// Treiber, den go-sqlcipher registriert. Braucht CGO.
const dbDriverName = "sqlite3"

// Eine ohne Secrets angelegte Klartext-Datenbank wird nicht weiterbetrieben.
const enforceEncryptedDB = true

func getDBConnectionString() string {
	if encryptionKey == nil || len(encryptionKey) == 0 {
		return "file:wa.db?_foreign_keys=on"
	}
	keyHex := hex.EncodeToString(encryptionKey)
	return fmt.Sprintf("file:wa.db?_foreign_keys=on&_pragma_key=x'%s'&_pragma_cipher_page_size=4096", keyHex)
}

// medienWurzel: auf Sailfish sind ~/Pictures und Konsorten die richtigen
// Orte, der Tracker kennt sie.
func medienWurzel(homeDir string) string { return homeDir }

// Auf Sailfish gibt es die SIP-Bruecke nicht: dort zeigt die App ihre eigene
// Anrufansicht, und ein lokaler SIP-Server haette keinen Abnehmer.
func sipLaeuft() bool { return false }

func sipStroeme() (meowcallerQuelle, meowcallerSenke) { return nil, nil }

func sipAnruf(name, nummer string, beiAnnahme, beiAuflegen func()) (meowcallerQuelle, meowcallerSenke, bool) {
	return nil, nil, false
}

func sipAuflegen() {}
func sipStarten()  {}

// Auf Sailfish uebernimmt der connman-Waechter in main.go; hier ist nichts
// zusaetzlich zu tun.
func watchNetworkPlatform() {}

// Auf Sailfish startet systemd den Dienst; ein D-Bus-Name ist nicht noetig.
func sitzungsNamenBeanspruchen() {}

// policyMutesStreams: auf Sailfish ja -- die Richtlinienschicht legt
// gewoehnliche Stroeme im Anrufmodus stumm. Siehe keepUnmuted.
func policyMutesStreams() bool { return true }

// Auf Sailfish regelt das die Richtlinienschicht selbst.
func musikPausieren()  {}
func musikFortsetzen() {}

// Auf Sailfish gibt es keine SIP-Bruecke.
func sipKontoAnstossen() {}

// Auf Sailfish klingelt die gewoehnliche Meldung ueber
// org.freedesktop.Notifications -- siehe notifyRinging.
func plattformKlingelmeldung(titel, name string) (uint32, bool) { return 0, false }

func plattformMeldungSchliessen(id uint32) bool { return false }

// Auf Sailfish geht der Anrufzustand direkt ueber den Bus.
func plattformAnrufZustand(zustand string) bool { return false }

// Auf Sailfish waehlt die Telefon-App nicht fuer uns -- es gibt keine
// SIP-Bruecke, ueber die der Anruf zurueckkaeme.
func plattformTelefonWaehlt(nummer string) bool { return false }

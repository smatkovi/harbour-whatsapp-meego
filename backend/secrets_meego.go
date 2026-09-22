//go:build meego

// Schluesselablage fuer MeeGo Harmattan.
//
// Sailfish Secrets gibt es auf dem N9/N950 nicht - der Dienst heisst dort
// org.sailfishos.secrets.daemon.discovery und wird von nichts angeboten. Das
// Backend wartet sonst ewig auf ihn; das war beim ersten Startversuch auf dem
// N950 exakt die einzige Meldung, die kam.
//
// Harmattan hat keinen vergleichbaren Dienst. aegis-crypto kann Schluessel
// verwahren, gibt sie aber nur signierten Paketen heraus - ein per dpkg
// installiertes Paket bekommt dort nichts. Bleibt eine Datei mit Modus 0600
// im Datenverzeichnis, so wie mastodon-feed seinen Token haelt.
//
// Die Schnittstelle ist Wort fuer Wort dieselbe wie in secrets.go, damit
// main.go fuer beide Plattformen derselbe Quelltext bleibt. Was hier fehlt,
// ist alles, was nur auf Sailfish einen Sinn hat: Besitzkonflikte zwischen
// App-Identitaeten und die Schluesseluebergabe zwischen ihnen. Auf Harmattan
// gibt es diese Identitaeten nicht, also koennen die Faelle nicht eintreten -
// IsOwnershipError meldet immer false, und die Uebergabe ist ein Nichts.
package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
)

// Nachbildung des Typs aus secrets.go. main.go liest secrets.collectionName,
// wenn es einen Schluessel uebergeben will; hier steht nur ein Name drin.
type SailfishSecrets struct {
	collectionName string
}

var secrets *SailfishSecrets
var encryptionKey []byte

// Auf Sailfish beenden diese beiden Zustaende die Startschleife. Hier treten
// sie nie ein, muessen aber existieren, weil main.go sie vergleicht.
var ErrKeyHandoverRequested = fmt.Errorf("key handover requested")
var ErrKeyExported = fmt.Errorf("key exported for handover")

const keyFile = ".dbkey"

func IsOwnershipError(err error) bool { return false }

func InitSecrets() error {
	secrets = &SailfishSecrets{collectionName: "harbourwhatsapp"}
	return nil
}

// GetOrCreateKey liefert 32 Bytes aus .dbkey und legt sie beim ersten Mal an.
//
// Mit modernc.org/sqlite verschluesselt der Schluessel nichts - siehe
// platform_meego.go. Er wird trotzdem erzeugt und gehalten: das Backend
// erwartet an mehreren Stellen einen, und wenn spaeter doch einmal eine
// verschluesselnde Ablage dazukommt, liegt er schon bereit.
func GetOrCreateKey() ([]byte, error) {
	if data, err := os.ReadFile(keyFile); err == nil && len(data) == 32 {
		encryptionKey = data
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("kein Zufall fuer den Schluessel: %v", err)
	}
	if err := os.WriteFile(keyFile, key, 0600); err != nil {
		return nil, fmt.Errorf("Schluessel laesst sich nicht ablegen: %v", err)
	}
	encryptionKey = key
	return key, nil
}

func RegenerateKey() ([]byte, error) {
	ClearAllSecrets()
	return GetOrCreateKey()
}

func ClearAllSecrets() {
	os.Remove(keyFile)
	encryptionKey = nil
}

// exportKeyForHandover hat auf Harmattan nichts zu tun: es gibt keine zweite
// App-Identitaet, an die etwas zu uebergeben waere.
func exportKeyForHandover(ownCollection string, key []byte) (string, error) {
	return "", nil
}

// Wie auf Sailfish: reines JSON, keine Verschluesselung. Der Name taeuscht
// dort schon und hier ebenso - er bleibt, damit die Aufrufer gleich bleiben.
func LoadEncrypted(filename string, v interface{}) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func SaveEncrypted(filename string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0600)
}

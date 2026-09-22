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

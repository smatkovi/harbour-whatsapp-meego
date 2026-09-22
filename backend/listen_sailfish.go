//go:build !meego

package main

import (
	"fmt"
	"net"
)

// listenLocal oeffnet 127.0.0.1:port. Auf Sailfish ist das einfach net.Listen;
// die MeeGo-Fassung muss accept4 umgehen, siehe listen_meego.go.
func listenLocal(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
}

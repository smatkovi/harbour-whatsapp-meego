//go:build meego

// Eingehende Verbindungen auf einem 2.6.32-Kernel.
//
// Das ist die einzige Stelle, an der Gos Mindestanforderung "Kernel >= 3.2"
// auf Harmattan wirklich beisst. Ausgehend geht alles -- die Verbindung zu
// WhatsApp steht, TLS 1.3 inklusive. Aber beim Annehmen:
//
//     http.Serve kehrte zurueck: accept tcp 127.0.0.1:8095:
//     accept4: function not implemented
//
// accept4 kam in den ARM-Syscall-Tisch erst nach 2.6.32, und modernes Go hat
// den Rueckfall auf das alte accept entfernt, als es die Mindestanforderung
// anhob. syscall.Accept hilft nicht: es ruft ebenfalls nur accept4.
//
// Bleibt der rohe Aufruf. accept ist auf ARM-EABI die Nummer 285; der
// angenommene Deskriptor wird nichtblockierend gemacht und ueber
// net.FileConn in eine gewoehnliche net.Conn verwandelt, die dann wieder im
// ueblichen Netzpoller haengt. Auf dem N950 gemessen: angenommen, gelesen,
// alles wie erwartet.
//
// Der Preis ist ein OS-Thread, der im accept blockiert. Fuer eine lokale
// Schnittstelle mit einer Handvoll Verbindungen ist das kein Thema; Gos
// Scheduler legt einfach einen weiteren Thread an.
package main

import (
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

const sysAcceptARM = 285

type meegoListener struct {
	fd   int
	addr net.Addr
	once sync.Once
	zu   bool
	mu   sync.Mutex
}

func (l *meegoListener) Accept() (net.Conn, error) {
	for {
		l.mu.Lock()
		zu := l.zu
		l.mu.Unlock()
		if zu {
			return nil, net.ErrClosed
		}
		var rsa syscall.RawSockaddrAny
		size := uint32(syscall.SizeofSockaddrAny)
		nfd, _, errno := syscall.Syscall(sysAcceptARM, uintptr(l.fd),
			uintptr(unsafe.Pointer(&rsa)), uintptr(unsafe.Pointer(&size)))
		if errno != 0 {
			if errno == syscall.EINTR {
				continue // Signal waehrend des Wartens -- einfach nochmal
			}
			return nil, fmt.Errorf("accept: %v", errno)
		}
		syscall.SetNonblock(int(nfd), true)
		syscall.CloseOnExec(int(nfd))
		f := os.NewFile(nfd, "verbindung")
		conn, err := net.FileConn(f)
		f.Close() // FileConn dupliziert; das Original wird nicht mehr gebraucht
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

func (l *meegoListener) Close() error {
	var err error
	l.once.Do(func() {
		l.mu.Lock()
		l.zu = true
		l.mu.Unlock()
		err = syscall.Close(l.fd)
	})
	return err
}

func (l *meegoListener) Addr() net.Addr { return l.addr }

// listenLocal oeffnet 127.0.0.1:port ohne accept4.
func listenLocal(port int) (net.Listener, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(fd)
	if err = syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	sa := &syscall.SockaddrInet4{Port: port, Addr: [4]byte{127, 0, 0, 1}}
	if err = syscall.Bind(fd, sa); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if err = syscall.Listen(fd, 64); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &meegoListener{
		fd:   fd,
		addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port},
	}, nil
}

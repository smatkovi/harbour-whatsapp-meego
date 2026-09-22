//go:build !cgo

// Echounterdrueckung, wenn ohne CGO gebaut wird.
//
// speexdsp ist C und braucht deshalb CGO. Auf MeeGo Harmattan wird das
// Backend absichtlich ohne CGO gebaut: ein statisches Go-Binary laeuft dort
// einwandfrei, sobald es nicht gegen die glibc 2.10 des Geraets bindet.
//
// Betroffen sind nur Sprachanrufe, und auch die nur beim Freisprechen: ohne
// Unterdrueckung geht die Aufnahme unveraendert durch, das Gegenueber hoert
// sich selbst. Mit Headset faellt es nicht auf. Lieber diese Einschraenkung
// als eine Cross-Toolchain im Bauweg, nur damit ein Filter mitkommt.
package speexdsp

import "errors"

type Canceller struct{ frame int }

func New(rate, frame, tail int) (*Canceller, error) {
	if frame <= 0 || tail <= frame {
		return nil, errors.New("speexdsp: ungueltige Rahmenlaenge")
	}
	return &Canceller{frame: frame}, nil
}

func (c *Canceller) Frame() int                  { return c.frame }
func (c *Canceller) Process(rec, play []float32) {}
func (c *Canceller) Close()                      {}

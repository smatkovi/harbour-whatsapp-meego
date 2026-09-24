//go:build meego

package main

// Die Tonquelle der SIP-Bruecke darf sich nicht selbst takten.
//
// meowcaller holt die Rahmen aus einer getakteten Sendeschleife: alle 60 ms
// einen, und im selben Durchgang wird kodiert, verschluesselt und
// verschickt. Wer in ReadFrame schlaeft, haelt diese Schleife an -- der
// Sendetakt wird dann laenger als 60 ms, es geht nur noch ein Teil der
// Rahmen hinaus, und die Gegenseite hoert Bruchstuecke oder gar nichts.
// Genau das war der Fehler "der andere hoert mich nicht" auf dem N950.

import (
	"testing"
	"time"
)

func TestSipQuelleTaktetNichtSelbst(t *testing.T) {
	b := &sipBruecke{vonTelefon: neuerTonPuffer(rahmenSamples*6, rahmenSamples*2)}
	q := sipQuelle{b}

	// Zehn Rahmen holen und die Zeit messen. Ohne eigene Taktung ist das
	// eine Sache von Mikrosekunden; mit 60 ms Schlaf je Rahmen waeren es
	// ueber 600 ms.
	start := time.Now()
	for i := 0; i < 10; i++ {
		rahmen, err := q.ReadFrame()
		if err != nil {
			t.Fatalf("ReadFrame: %v", err)
		}
		if len(rahmen) != rahmenSamples {
			t.Fatalf("Rahmenlaenge %d, erwartet %d", len(rahmen), rahmenSamples)
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("zehn Rahmen brauchten %v -- die Quelle taktet sich selbst", d)
	}
}

// Ein halber Rahmen wird nicht halb ausgeliefert: genau das zerschnitt die
// Woerter. Er bleibt liegen, bis er voll ist -- und kommt dann am Stueck.
func TestSipQuelleZerschneidetNichts(t *testing.T) {
	b := &sipBruecke{vonTelefon: neuerTonPuffer(rahmenSamples*6, rahmenSamples*2)}
	q := sipQuelle{b}

	halb := make([]float32, rahmenSamples/2)
	for i := range halb {
		halb[i] = 0.5
	}
	b.vonTelefon.schreiben(halb)

	rahmen, err := q.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	for i, w := range rahmen {
		if w != 0 {
			t.Fatalf("Sample %d ist %v -- der halbe Rahmen wurde zerschnitten", i, w)
		}
	}

	// Genug nachschieben, dass der Vorlauf steht: jetzt kommt der Ton am
	// Stueck, mit dem ersten Sample an erster Stelle.
	rest := make([]float32, rahmenSamples*2)
	for i := range rest {
		rest[i] = 0.5
	}
	b.vonTelefon.schreiben(rest)
	rahmen, err = q.ReadFrame()
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}
	for i, w := range rahmen {
		if w != 0.5 {
			t.Fatalf("Sample %d ist %v, erwartet durchgehenden Ton", i, w)
		}
	}
}

// Nach einer Luecke laeuft die Ausgabe erst wieder an, wenn sich der
// Vorlauf angesammelt hat -- sonst haengt sie am Rand des Leerlaufs und
// jeder zweite Rahmen faellt aus.
func TestSipQuelleSammeltNachEinerLuecke(t *testing.T) {
	b := &sipBruecke{vonTelefon: neuerTonPuffer(rahmenSamples*6, rahmenSamples*2)}
	q := sipQuelle{b}

	voll := make([]float32, rahmenSamples)
	for i := range voll {
		voll[i] = 0.25
	}
	// Genau ein Rahmen: das ist weniger als der Vorlauf, also noch Stille.
	b.vonTelefon.schreiben(voll)
	rahmen, _ := q.ReadFrame()
	if rahmen[0] != 0 {
		t.Fatal("die Ausgabe lief ohne Vorlauf an")
	}
	b.vonTelefon.schreiben(voll)
	rahmen, _ = q.ReadFrame()
	if rahmen[0] != 0.25 {
		t.Fatalf("erstes Sample %v, erwartet 0.25 -- der Vorlauf stand", rahmen[0])
	}
}

// Der Puffer deckelt den Rueckstand: gelesen wird genau so schnell, wie
// geschrieben wird, also holt die Leseseite einen einmal entstandenen
// Rueckstand nie wieder auf -- er bliebe als Verzoegerung stehen.
func TestTonPufferDeckeltDenRueckstand(t *testing.T) {
	p := neuerTonPuffer(rahmenSamples*3, rahmenSamples)
	viel := make([]float32, rahmenSamples*10)
	for i := range viel {
		viel[i] = float32(i)
	}
	p.schreiben(viel)

	p.mu.Lock()
	n := len(p.dat)
	erstes := p.dat[0]
	p.mu.Unlock()
	if n != rahmenSamples*3 {
		t.Fatalf("Puffer haelt %d Samples, erwartet %d", n, rahmenSamples*3)
	}
	// Das Aelteste faellt weg, nicht das Neueste.
	if erstes != float32(rahmenSamples*10-rahmenSamples*3) {
		t.Fatalf("Puffer hat das Falsche verworfen: erstes Sample %v", erstes)
	}
}

// muLaw hin und zurueck: kein Vorzeichenfehler, kein Versatz.
func TestMuLawHinUndZurueck(t *testing.T) {
	for _, wert := range []int16{0, 100, -100, 1000, -1000, 8000, -8000, 30000, -30000} {
		zurueck := muLawDekodieren(muLawKodieren(wert))
		abstand := int(zurueck) - int(wert)
		if abstand < 0 {
			abstand = -abstand
		}
		// G.711 ist logarithmisch: nahe null sehr genau, oben grober.
		grenze := int(wert) / 8
		if grenze < 0 {
			grenze = -grenze
		}
		if grenze < 8 {
			grenze = 8
		}
		if abstand > grenze {
			t.Fatalf("%d wurde zu %d (Abstand %d, erlaubt %d)", wert, zurueck, abstand, grenze)
		}
	}
}

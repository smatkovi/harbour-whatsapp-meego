//go:build meego

// SIP-Bruecke: WhatsApp-Anrufe klingeln in Harmattans eigener Anrufoberflaeche.
//
// Der Gedanke stammt vom Geraet selbst. Harmattan bringt telepathy-sofiasip
// mit, also kann das N9/N950 SIP-Teilnehmer sein -- und ein eingehender
// SIP-Anruf laeuft durch die *systemeigene* Anrufansicht: Klingeln am
// Sperrbildschirm, Annehmen und Ablehnen ohne die App zu oeffnen,
// Naeherungssensor, Hoermuschel statt Lautsprecher. Das alles selbst zu
// bauen waere aussichtslos; es zu benutzen kostet einen kleinen SIP-Server.
//
// Aufbau:
//
//	Telefon (telepathy-sofiasip)  <--SIP/RTP-->  diese Bruecke  <-->  whatsmeow
//	        registriert sich bei 127.0.0.1:5060
//
// Bei einem eingehenden WhatsApp-Anruf schickt die Bruecke ein INVITE an das
// registrierte Telefon. Nimmt der Nutzer an, laeuft der Ton als RTP zwischen
// Telefon und Bruecke; die rechnet zwischen G.711 (8 kHz, was sofia-sip
// spricht) und den 16-kHz-Gleitkommarahmen von meowcaller um.
//
// Bewusst nur auf 127.0.0.1: die Bruecke nimmt jede Registrierung an, ohne
// Passwort. Auf der Loopback-Schnittstelle ist das vertretbar -- wer dort
// Pakete schicken kann, laeuft ohnehin schon als Benutzer auf dem Geraet.
// Auf einer echten Schnittstelle waere es das nicht, deshalb wird dort auch
// nicht gelauscht.
package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
)

const (
	sipHost      = "127.0.0.1"
	sipPort      = 5060
	rtpPort      = 40100
	sipRate      = 8000  // G.711 ist immer 8 kHz
	waRate       = 16000 // meowcaller.SampleRate
	rtpFrame     = 160   // 20 ms bei 8 kHz
	nutzlastPCMU = 0
)

// ---------------------------------------------------------------- G.711 µ-law

// muLawKodieren wandelt ein Sample nach G.711 µ-law.
//
// Von Hand statt per Bibliothek: es sind fuenfzehn Zeilen, und eine
// Abhaengigkeit mehr waere auf diesem Geraet teurer als der Code.
func muLawKodieren(wert int16) byte {
	const bias = 0x84
	const clip = 32635
	vorzeichen := byte(0)
	if wert < 0 {
		wert = -wert
		vorzeichen = 0x80
	}
	if wert > clip {
		wert = clip
	}
	wert += bias
	exponent := byte(7)
	for maske := int16(0x4000); wert&maske == 0 && exponent > 0; maske >>= 1 {
		exponent--
	}
	mantisse := byte((wert >> (exponent + 3)) & 0x0F)
	return ^(vorzeichen | (exponent << 4) | mantisse)
}

var muLawTabelle [256]int16

func init() {
	for i := 0; i < 256; i++ {
		u := ^byte(i)
		vorzeichen := u & 0x80
		exponent := (u >> 4) & 0x07
		mantisse := u & 0x0F
		wert := (int16(mantisse) << 3) + 0x84
		wert <<= exponent
		wert -= 0x84
		if vorzeichen != 0 {
			wert = -wert
		}
		muLawTabelle[i] = wert
	}
}

func muLawDekodieren(b byte) int16 { return muLawTabelle[b] }

// ---------------------------------------------------------------- Ringpuffer

// tonPuffer haelt Samples zwischen den beiden Seiten, die verschieden schnell
// takten: RTP kommt alle 20 ms, meowcaller will 60-ms-Rahmen.
type tonPuffer struct {
	mu  sync.Mutex
	dat []float32
	max int
}

func neuerTonPuffer(max int) *tonPuffer { return &tonPuffer{max: max} }

func (p *tonPuffer) schreiben(s []float32) {
	p.mu.Lock()
	p.dat = append(p.dat, s...)
	// Laeuft er ueber, faellt das Aelteste weg. Bei Ton ist eine Luecke
	// besser als wachsende Verzoegerung, die nie wieder aufholt.
	if len(p.dat) > p.max {
		p.dat = p.dat[len(p.dat)-p.max:]
	}
	p.mu.Unlock()
}

// lesen fuellt ziel; fehlende Samples werden zu Stille.
func (p *tonPuffer) lesen(ziel []float32) {
	p.mu.Lock()
	n := copy(ziel, p.dat)
	p.dat = p.dat[n:]
	p.mu.Unlock()
	for i := n; i < len(ziel); i++ {
		ziel[i] = 0
	}
}

// ---------------------------------------------------------------- Umrechnung

// hoch verdoppelt die Abtastrate (8 -> 16 kHz) durch lineare Interpolation.
func hoch(in []float32) []float32 {
	aus := make([]float32, len(in)*2)
	for i := range in {
		aus[i*2] = in[i]
		if i+1 < len(in) {
			aus[i*2+1] = (in[i] + in[i+1]) / 2
		} else {
			aus[i*2+1] = in[i]
		}
	}
	return aus
}

// runter halbiert die Abtastrate (16 -> 8 kHz). Der Mittelwert zweier
// Nachbarn ist ein einfacher Tiefpass -- ohne den klingt das Ergebnis
// blechern, weil alles oberhalb von 4 kHz zurueckfaltet.
func runter(in []float32) []float32 {
	aus := make([]float32, len(in)/2)
	for i := range aus {
		aus[i] = (in[i*2] + in[i*2+1]) / 2
	}
	return aus
}

// ---------------------------------------------------------------- Die Bruecke

type sipBruecke struct {
	mu sync.Mutex

	ua      *sipgo.UserAgent
	srv     *sipgo.Server
	cl      *sipgo.Client
	dc      *sipgo.DialogClientCache
	sitzung *sipgo.DialogClientSession
	// Bricht ein noch klingelndes INVITE ab (CANCEL statt BYE).
	abbruch context.CancelFunc
	angenommen bool

	// Wohin das Telefon erreichbar ist, aus seinem REGISTER.
	kontakt     *sip.Uri
	registriert bool

	rtp       *net.UDPConn
	gegen     *net.UDPAddr
	laeuft    bool
	sequenz   uint16
	zeitmarke uint32
	ssrc      uint32

	vonTelefon *tonPuffer // Mikrofon des Telefons, 16 kHz
	zumTelefon *tonPuffer // Ton von WhatsApp, 16 kHz

	// Wird gerufen, wenn das Telefon auflegt (BYE) oder ablehnt.
	beiAuflegen func()
	// Wird gerufen, sobald das Telefon abgehoben hat. Erst dann darf der
	// WhatsApp-Anruf angenommen werden.
	beiAnnahme func()
}

var bruecke *sipBruecke

// sipBrueckeStarten oeffnet den lokalen SIP-Server. Fehler sind nicht toedlich:
// ohne Bruecke laeuft alles wie bisher, nur eben ohne Klingeln am
// Sperrbildschirm.
func sipBrueckeStarten() {
	b := &sipBruecke{
		vonTelefon: neuerTonPuffer(waRate), // eine Sekunde
		zumTelefon: neuerTonPuffer(waRate),
		ssrc:       uint32(time.Now().UnixNano()),
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("harbour-whatsapp"))
	if err != nil {
		fmt.Println("📞 SIP: UA:", err)
		return
	}
	b.ua = ua
	if b.srv, err = sipgo.NewServer(ua); err != nil {
		fmt.Println("📞 SIP: Server:", err)
		return
	}
	if b.cl, err = sipgo.NewClient(ua); err != nil {
		fmt.Println("📞 SIP: Client:", err)
		return
	}
	// Die Dialogschicht von sipgo kuemmert sich um Tags, CSeq, ACK und BYE.
	// Von Hand ist das genau die Sorte Kleinkram, die man erst bemerkt,
	// wenn ein Geraet eigensinnig antwortet.
	b.dc = sipgo.NewDialogClientCache(b.cl,
		sip.ContactHeader{Address: sip.Uri{User: "whatsapp", Host: sipHost, Port: sipPort}})

	b.srv.OnRegister(b.beiRegister)
	// BYE und CANCEL laufen ueber die Dialogschicht; hier bleibt nur die
	// Antwort, damit das Telefon nicht wiederholt.
	b.srv.OnBye(b.beiBye)
	b.srv.OnCancel(b.beiBye)
	b.srv.OnAck(func(req *sip.Request, tx sip.ServerTransaction) {})
	// sofiasip schickt OPTIONS als Lebenszeichen an den Registrar. Bleibt
	// das unbeantwortet, haelt es die Bruecke irgendwann fuer tot und wirft
	// die Registrierung weg -- dann klingelt nichts mehr, ohne dass man
	// merkt warum.
	b.srv.OnOptions(func(req *sip.Request, tx sip.ServerTransaction) {
		_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	})

	adr := net.JoinHostPort(sipHost, strconv.Itoa(sipPort))
	go func() {
		if lerr := b.srv.ListenAndServe(context.Background(), "udp", adr); lerr != nil {
			fmt.Println("📞 SIP: lauschen:", lerr)
		}
	}()
	if err = b.rtpOeffnen(); err != nil {
		fmt.Println("📞 SIP: RTP:", err)
		return
	}
	bruecke = b
	fmt.Printf("📞 SIP-Bruecke laeuft auf %s (RTP %d)\n", adr, rtpPort)
}

func (b *sipBruecke) rtpOeffnen() error {
	adr := &net.UDPAddr{IP: net.ParseIP(sipHost), Port: rtpPort}
	c, err := net.ListenUDP("udp", adr)
	if err != nil {
		return err
	}
	b.rtp = c
	go b.rtpLesen()
	return nil
}

func (b *sipBruecke) beiRegister(req *sip.Request, tx sip.ServerTransaction) {
	k := req.Contact()
	if k != nil {
		b.mu.Lock()
		uri := k.Address
		b.kontakt = &uri
		b.registriert = true
		b.mu.Unlock()
		fmt.Printf("📞 SIP: Telefon registriert als %s\n", uri.String())
	}
	antwort := sip.NewResponseFromRequest(req, 200, "OK", nil)
	// Die Registrierung lange genug halten, dass das Telefon nicht staendig
	// neu anklopft, aber kurz genug, dass ein Neustart auffaellt.
	antwort.AppendHeader(sip.NewHeader("Expires", "600"))
	_ = tx.Respond(antwort)
}

func (b *sipBruecke) beiBye(req *sip.Request, tx sip.ServerTransaction) {
	_ = tx.Respond(sip.NewResponseFromRequest(req, 200, "OK", nil))
	b.mu.Lock()
	f := b.beiAuflegen
	b.laeuft = false
	b.mu.Unlock()
	fmt.Println("📞 SIP: Telefon hat aufgelegt")
	if f != nil {
		f()
	}
}

// klingeln schickt ein INVITE ans Telefon. Ab hier uebernimmt Harmattans
// eigene Anrufansicht -- samt Sperrbildschirm.
func (b *sipBruecke) klingeln(name, nummer string, beiAnnahme, beiAuflegen func()) error {
	b.mu.Lock()
	kontakt := b.kontakt
	b.beiAuflegen = beiAuflegen
	b.beiAnnahme = beiAnnahme
	b.mu.Unlock()
	if kontakt == nil {
		return fmt.Errorf("kein Telefon registriert")
	}

	// Der Anzeigename landet in der Anrufansicht, die Nummer im Verlauf.
	kopf := []sip.Header{
		sip.NewHeader("Content-Type", "application/sdp"),
	}
	if name != "" {
		kopf = append(kopf, sip.NewHeader("X-Caller-Name", name))
	}
	sitzung, err := b.dc.Invite(context.Background(), *kontakt, []byte(b.sdp()), kopf...)
	if err != nil {
		return err
	}
	// Eigener Kontext fuers Warten auf die Antwort: wird er abgebrochen,
	// schickt sipgo ein CANCEL. Genau das braucht es, wenn der Anruf auf
	// einem anderen verknuepften Geraet angenommen wird -- WhatsApp laesst
	// ja alle klingeln, und ohne CANCEL klingelte dieses hier weiter.
	warteCtx, abbrechen := context.WithCancel(context.Background())
	b.mu.Lock()
	b.sitzung = sitzung
	b.abbruch = abbrechen
	b.angenommen = false
	b.mu.Unlock()

	go func() {
		err := sitzung.WaitAnswer(warteCtx, sipgo.AnswerOptions{
			OnResponse: func(res *sip.Response) error {
				if res.StatusCode == 200 {
					b.gegenstelleAusSDP(string(res.Body()))
				}
				return nil
			},
		})
		if err != nil {
			fmt.Println("📞 SIP: nicht angenommen:", err)
			b.mu.Lock()
			f := b.beiAuflegen
			b.mu.Unlock()
			if f != nil {
				f()
			}
			return
		}
		if err = sitzung.Ack(context.Background()); err != nil {
			fmt.Println("📞 SIP: ACK:", err)
			return
		}
		b.mu.Lock()
		b.laeuft = true
		b.angenommen = true
		annahme := b.beiAnnahme
		b.mu.Unlock()
		fmt.Println("📞 SIP: angenommen, Ton laeuft")
		// Jetzt erst darf der WhatsApp-Anruf angenommen werden: bis hierhin
		// hat nur das Telefon geklingelt. Ohne diesen Rueckruf klingelte es
		// zwar, aber das Abheben blieb folgenlos -- der Anruf lief auf der
		// WhatsApp-Seite weiter, bis ihn ein anderes Geraet annahm.
		if annahme != nil {
			annahme()
		}

		// Auf das BYE des Telefons warten.
		<-sitzung.Context().Done()
		b.mu.Lock()
		f := b.beiAuflegen
		b.laeuft = false
		b.mu.Unlock()
		fmt.Println("📞 SIP: Telefon hat aufgelegt")
		if f != nil {
			f()
		}
	}()
	return nil
}

// auflegen beendet die SIP-Seite, wenn der Anruf auf der WhatsApp-Seite endet.
func (b *sipBruecke) auflegen() {
	b.mu.Lock()
	s := b.sitzung
	abbrechen := b.abbruch
	angenommen := b.angenommen
	b.laeuft = false
	b.angenommen = false
	b.beiAuflegen = nil
	b.beiAnnahme = nil
	b.sitzung = nil
	b.abbruch = nil
	b.mu.Unlock()
	if s == nil {
		return
	}
	if !angenommen {
		// Noch kein 200 OK: BYE wuerde hier scheitern ("can not send as no
		// invite response present") und das Telefon klingelte weiter. Der
		// Abbruch des Wartekontexts loest stattdessen ein CANCEL aus.
		if abbrechen != nil {
			abbrechen()
		}
		fmt.Println("📞 SIP: klingeln abgebrochen")
		return
	}
	_ = s.Bye(context.Background())
}

func (b *sipBruecke) sdp() string {
	return strings.Join([]string{
		"v=0",
		fmt.Sprintf("o=- %d %d IN IP4 %s", time.Now().Unix(), time.Now().Unix(), sipHost),
		"s=WhatsApp",
		"c=IN IP4 " + sipHost,
		"t=0 0",
		fmt.Sprintf("m=audio %d RTP/AVP %d", rtpPort, nutzlastPCMU),
		fmt.Sprintf("a=rtpmap:%d PCMU/%d", nutzlastPCMU, sipRate),
		"a=ptime:20",
		"a=sendrecv",
		"",
	}, "\r\n")
}

// gegenstelleAusSDP liest, wohin die RTP-Pakete sollen.
func (b *sipBruecke) gegenstelleAusSDP(sdp string) {
	host, port := sipHost, 0
	for _, zeile := range strings.Split(sdp, "\n") {
		zeile = strings.TrimSpace(zeile)
		if strings.HasPrefix(zeile, "c=IN IP4 ") {
			host = strings.TrimSpace(strings.TrimPrefix(zeile, "c=IN IP4 "))
		}
		if strings.HasPrefix(zeile, "m=audio ") {
			teile := strings.Fields(zeile)
			if len(teile) > 1 {
				port, _ = strconv.Atoi(teile[1])
			}
		}
	}
	if port == 0 {
		return
	}
	b.mu.Lock()
	b.gegen = &net.UDPAddr{IP: net.ParseIP(host), Port: port}
	b.mu.Unlock()
}

// rtpLesen nimmt die Pakete des Telefons entgegen: G.711 auspacken, auf
// 16 kHz bringen, in den Puffer fuer whatsmeow legen.
func (b *sipBruecke) rtpLesen() {
	puffer := make([]byte, 1500)
	for {
		n, von, err := b.rtp.ReadFromUDP(puffer)
		if err != nil {
			return
		}
		b.mu.Lock()
		if b.gegen == nil {
			b.gegen = von // erste Quelle gewinnt, symmetrisches RTP
		}
		laeuft := b.laeuft
		b.mu.Unlock()
		if !laeuft {
			continue
		}
		var p rtp.Packet
		if err = p.Unmarshal(puffer[:n]); err != nil {
			continue
		}
		roh := make([]float32, len(p.Payload))
		for i, c := range p.Payload {
			roh[i] = float32(muLawDekodieren(c)) / 32768
		}
		b.vonTelefon.schreiben(hoch(roh))
	}
}

// ------------------------------------------------- die beiden Enden fuer meowcaller

type sipQuelle struct{ b *sipBruecke }

// ReadFrame liefert, was das Telefonmikrofon aufgenommen hat.
func (q sipQuelle) ReadFrame() ([]float32, error) {
	rahmen := make([]float32, 960) // meowcaller.FrameSamples
	q.b.vonTelefon.lesen(rahmen)
	// 60 ms takten -- sonst laeuft die Schleife so schnell, wie die CPU mag.
	time.Sleep(60 * time.Millisecond)
	return rahmen, nil
}

func (q sipQuelle) Close() error { return nil }

type sipSenke struct{ b *sipBruecke }

// WriteFrame schickt den Ton von WhatsApp als RTP ans Telefon.
func (s sipSenke) WriteFrame(rahmen []float32) error {
	b := s.b
	b.mu.Lock()
	gegen, laeuft := b.gegen, b.laeuft
	b.mu.Unlock()
	if !laeuft || gegen == nil {
		return nil
	}
	acht := runter(rahmen) // 960 -> 480 Samples
	for pos := 0; pos+rtpFrame <= len(acht); pos += rtpFrame {
		nutz := make([]byte, rtpFrame)
		for i := 0; i < rtpFrame; i++ {
			w := acht[pos+i] * 32767
			if w > 32767 {
				w = 32767
			} else if w < -32768 {
				w = -32768
			}
			nutz[i] = muLawKodieren(int16(w))
		}
		b.mu.Lock()
		b.sequenz++
		b.zeitmarke += rtpFrame
		p := &rtp.Packet{
			Header: rtp.Header{
				Version:        2,
				PayloadType:    nutzlastPCMU,
				SequenceNumber: b.sequenz,
				Timestamp:      b.zeitmarke,
				SSRC:           b.ssrc,
			},
			Payload: nutz,
		}
		b.mu.Unlock()
		roh, err := p.Marshal()
		if err != nil {
			continue
		}
		_, _ = b.rtp.WriteToUDP(roh, gegen)
	}
	return nil
}

func (s sipSenke) Close() error { return nil }

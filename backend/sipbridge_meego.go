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
	sipHost  = "127.0.0.1"
	sipPort  = 5060
	rtpPort  = 40100
	sipRate  = 8000  // G.711 ist immer 8 kHz
	waRate   = 16000 // meowcaller.SampleRate
	rtpFrame = 160   // 20 ms bei 8 kHz
	// Ein meowcaller-Rahmen: 60 ms bei 16 kHz (meowcaller.FrameSamples).
	rahmenSamples = 960
	nutzlastPCMU  = 0
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
// takten: RTP kommt alle 20 ms in 160-Sample-Haeppchen, meowcaller holt alle
// 60 ms einen Rahmen von 960. Im Mittel passt das genau; im Einzelnen liegen
// die beiden Takte aber beliebig zueinander, und auf einem ausgelasteten
// Geraet verschiebt sich das staendig.
//
// Deshalb ist der Puffer ein kleiner Verzoegerungsspeicher und kein blosses
// Rohr: er gibt nur *volle* Rahmen heraus. Frueher lieferte er, was gerade
// da war, und fuellte den Rest mit Stille -- das zerschnitt jedes zweite
// Wort mitten im Klang, und beim Angerufenen kamen nur noch Wortfetzen an.
// Ein angefangener Rahmen bleibt jetzt liegen, bis er voll ist.
type tonPuffer struct {
	mu  sync.Mutex
	dat []float32
	max int
	// Wie viel sich ansammeln muss, bevor die Ausgabe (wieder) anlaeuft.
	vorlauf int
	bereit  bool
}

func neuerTonPuffer(max, vorlauf int) *tonPuffer {
	return &tonPuffer{max: max, vorlauf: vorlauf}
}

// schreiben legt Samples ab und sagt, wie viele dabei verloren gingen.
func (p *tonPuffer) schreiben(s []float32) int {
	p.mu.Lock()
	p.dat = append(p.dat, s...)
	// Laeuft er ueber, faellt das Aelteste weg. Bei Ton ist eine Luecke
	// besser als wachsende Verzoegerung, die nie wieder aufholt.
	weg := 0
	if len(p.dat) > p.max {
		weg = len(p.dat) - p.max
		p.dat = p.dat[weg:]
	}
	p.mu.Unlock()
	return weg
}

// zuruecksetzen leert den Speicher fuer ein neues Gespraech -- sonst ginge
// der Rest des vorigen als Vorlauf in das naechste ein.
func (p *tonPuffer) zuruecksetzen() {
	p.mu.Lock()
	p.dat = p.dat[:0]
	p.bereit = false
	p.mu.Unlock()
}

// lesen fuellt ziel mit einem vollen Rahmen und sagt, ob das gelang. Reicht
// der Vorrat nicht, kommt Stille -- und der angefangene Rahmen bleibt
// liegen, statt zerschnitten zu werden.
func (p *tonPuffer) lesen(ziel []float32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range ziel {
		ziel[i] = 0
	}
	if !p.bereit {
		// Nach einer Luecke erst wieder etwas ansammeln lassen, sonst
		// haengt die Ausgabe von da an am Rand des Leerlaufs und jeder
		// zweite Rahmen faellt aus.
		if len(p.dat) < p.vorlauf {
			return false
		}
		p.bereit = true
	}
	if len(p.dat) < len(ziel) {
		p.bereit = false
		return false
	}
	copy(ziel, p.dat[:len(ziel)])
	p.dat = p.dat[len(ziel):]
	return true
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
	abbruch    context.CancelFunc
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

	// Nur zur Fehlersuche: was vom Telefon hereinkam, seit das Gespraech
	// steht. Ohne diese Zahlen liess sich "die Gegenseite hoert mich
	// nicht" nicht auseinanderhalten -- schickt das Telefon nichts, oder
	// schicken wir das Empfangene nicht weiter?
	pakete   uint64
	spitze   float32
	gemeldet time.Time
	// Wie oft der Puffer einen Rahmen schuldig blieb. Wortfetzen beim
	// Angerufenen liest man hier ab, nicht am Paketzaehler.
	luecken        uint64
	lueckeGemeldet time.Time
	// Was der Puffer beim Ueberlaufen wegwerfen musste, und wie viele
	// Rahmen die Sendeschleife geholt hat. Beides gehoert zusammen: holt
	// sie weniger als 16,7 je Sekunde, kommt sie nicht nach, und der
	// Ueberlauf frisst genau die Luecke, die man dann hoert -- ohne dass
	// eine einzige Leerstelle im Puffer entstuende.
	verworfen uint64
	rahmen    uint64
	// Und die Gegenrichtung: Pakete an das Telefon. Ohne sie liess sich
	// "ich hoere den Anrufer nicht" nicht von "es geht nichts hinaus"
	// unterscheiden.
	hinaus uint64

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
		// Hoechstens sechs Rahmen (360 ms) im Speicher, angelaufen wird
		// mit zweien (120 ms). Frueher stand hier eine ganze Sekunde, und
		// weil genau so schnell gelesen wird, wie geschrieben wird, blieb
		// jeder einmal entstandene Rueckstand fuer immer als Verzoegerung
		// stehen -- aufholen kann die Leseseite ja nicht.
		vonTelefon: neuerTonPuffer(rahmenSamples*6, rahmenSamples*2),
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
	// Die Zaehler gehoeren zum Gespraech, nicht zur Laufzeit.
	b.pakete, b.spitze, b.gemeldet = 0, 0, time.Time{}
	b.luecken, b.lueckeGemeldet = 0, time.Time{}
	b.verworfen, b.rahmen, b.hinaus = 0, 0, 0
	b.vonTelefon.zuruecksetzen()
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
					// Die Antwort des Telefons einmal ins Protokoll: an
					// ihr haengt, wohin wir senden -- und ob das Telefon
					// ueberhaupt senden will (sendrecv/recvonly).
					fmt.Printf("📞 SIP: Antwort des Telefons: %s\n",
						strings.Join(strings.Fields(string(res.Body())), " "))
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
		b.zielAktualisieren(von)
		b.mu.Lock()
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
		var spitze float32
		for i, c := range p.Payload {
			w := float32(muLawDekodieren(c)) / 32768
			roh[i] = w
			if w < 0 {
				w = -w
			}
			if w > spitze {
				spitze = w
			}
		}
		weg := b.vonTelefon.schreiben(hoch(roh))
		b.zaehlen(von, p.PayloadType, len(p.Payload), spitze, weg)
	}
}

// zaehlen meldet einmal je Sekunde, was vom Telefon hereinkommt. Das erste
// Paket bekommt eine eigene Zeile, samt Nutzlastart und Absender: kommt
// gar nichts, steht im Protokoll nichts -- und genau das ist die Antwort
// auf "die Gegenseite hoert mich nicht".
func (b *sipBruecke) zaehlen(von *net.UDPAddr, art uint8, laenge int, spitze float32, weg int) {
	b.mu.Lock()
	b.pakete++
	b.verworfen += uint64(weg)
	if spitze > b.spitze {
		b.spitze = spitze
	}
	erstes := b.pakete == 1
	jetzt := time.Now()
	faellig := jetzt.Sub(b.gemeldet) >= time.Second
	anzahl, hoechste := b.pakete, b.spitze
	verworfen, rahmen, hinaus := b.verworfen, b.rahmen, b.hinaus
	if erstes || faellig {
		b.gemeldet = jetzt
		b.spitze = 0
		b.verworfen = 0
		b.rahmen = 0
		b.hinaus = 0
	}
	b.mu.Unlock()
	if erstes {
		fmt.Printf("%s 📞 SIP: erstes RTP vom Telefon: %s, Nutzlast %d, %d Bytes\n",
			jetzt.Format("15:04:05.000"), von, art, laenge)
		return
	}
	if faellig {
		fmt.Printf("%s 📞 SIP: Telefonmikrofon %d Pakete, Spitze %.3f, %d Rahmen abgeholt, %d Samples verworfen, %d Pakete ans Telefon\n",
			jetzt.Format("15:04:05.000"), anzahl, hoechste, rahmen, verworfen, hinaus)
	}
}

// luecke meldet, wenn der Puffer einen Rahmen schuldig bleiben musste.
// Hoechstens eine Zeile je Sekunde: wichtig ist, ob es Luecken gibt und wie
// viele, nicht jede einzelne.
func (b *sipBruecke) luecke() {
	b.mu.Lock()
	b.luecken++
	anzahl := b.luecken
	jetzt := time.Now()
	faellig := jetzt.Sub(b.lueckeGemeldet) >= time.Second
	if faellig {
		b.lueckeGemeldet = jetzt
	}
	b.mu.Unlock()
	if faellig {
		fmt.Printf("%s 📞 SIP: %d Luecken im Ton zum Gespraech\n",
			jetzt.Format("15:04:05.000"), anzahl)
	}
}

// zielAktualisieren merkt sich, wohin der Ton ans Telefon geht: dorthin,
// wo dessen eigene Pakete herkommen (symmetrisches RTP).
//
// Nicht dorthin, wohin die SDP-Antwort zeigt. Das Telefon nannte darin
// "c=IN IP4 100.64.120.110", die Adresse seiner Mobilfunkverbindung aus
// dem Carrier-NAT. Jedes Paket dorthin ging ueber den Router hinaus ins
// Netz (ip route get: via 192.168.1.1 dev wlan0) und kam bei ihm nie an
// -- der Anrufer war nicht zu hoeren, waehrend seine eigenen Pakete die
// ganze Zeit ordentlich von 127.0.0.1 kamen. Welche Adresse ein Endpunkt
// in die SDP schreibt, ist eine Behauptung; woher seine Pakete kommen,
// ist eine Tatsache.
func (b *sipBruecke) zielAktualisieren(von *net.UDPAddr) {
	if von == nil {
		return
	}
	b.mu.Lock()
	alt := b.gegen
	if alt != nil && alt.IP.Equal(von.IP) && alt.Port == von.Port {
		b.mu.Unlock()
		return
	}
	b.gegen = von
	b.mu.Unlock()
	if alt == nil {
		fmt.Printf("📞 SIP: Ton geht an %s (aus den Paketen des Telefons)\n", von)
		return
	}
	fmt.Printf("📞 SIP: Ton geht jetzt an %s statt an %s\n", von, alt)
}

// ------------------------------------------------- die beiden Enden fuer meowcaller

type sipQuelle struct{ b *sipBruecke }

// ReadFrame liefert, was das Telefonmikrofon aufgenommen hat -- sofort,
// ohne eigene Taktung.
//
// Hier stand einmal ein Schlaf von 60 ms, "damit die Schleife nicht so
// schnell laeuft, wie die CPU mag". Die Schleife gehoert aber gar nicht
// uns: meowcaller holt sich den Rahmen aus seiner *getakteten*
// Sendeschleife, einen alle 60 ms, und schickt ihn gleich im selben
// Durchgang weg. Der Schlaf hielt damit genau diese Schleife an. Ein
// Durchgang dauerte danach 60 ms Schlaf + Kodieren + Verschluesseln +
// Senden, also laenger als die 60 ms, die er dauern darf. Der Strom lief
// damit langsamer als die Zeit: die Zeitmarken der Pakete blieben
// zurueck, der Puffer lief staendig ueber, und beim Angerufenen kam
// nichts Brauchbares an -- "ich hoere dich nicht", waehrend die
// Gegenrichtung einwandfrei lief.
//
// Die PulseAudio-Quelle macht es richtig vor (micSource.ReadFrame):
// nehmen, was da ist, Stille auffuellen, sofort zurueckkommen.
func (q sipQuelle) ReadFrame() ([]float32, error) {
	rahmen := make([]float32, rahmenSamples)
	q.b.mu.Lock()
	q.b.rahmen++
	q.b.mu.Unlock()
	if !q.b.vonTelefon.lesen(rahmen) {
		q.b.luecke()
	}
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
		if _, err := b.rtp.WriteToUDP(roh, gegen); err == nil {
			b.mu.Lock()
			b.hinaus++
			b.mu.Unlock()
		}
	}
	return nil
}

func (s sipSenke) Close() error { return nil }

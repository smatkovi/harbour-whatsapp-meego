#!/opt/wunderw/bin/python3.11
# -*- coding: utf-8 -*-
"""Adapter zwischen pybridge und dem WhatsApp-Backend.

pybridge ist der Telepathy-Verbindungsmanager, der auf diesem Geraet schon
Telegram und Matrix in die Nachrichten-App traegt. Er spricht mit jedem
Dienst ueber einen Unix-Socket und Zeilen-JSON:

    Manager -> Daemon:  {"cmd": "get_dialogs", "args": {"limit": 100}}\\n
    Daemon  -> Manager: {"dialogs": [...]}\\n                 (Antwort)
                        {"event": "new_message", "data": {...}}\\n  (Ereignis)

Eine Zeile mit "event" gilt als Ereignis, jede andere als Antwort auf die
zuletzt gestellte Frage -- es gibt keine Vorgangsnummern. Antworten muessen
deshalb in der Reihenfolge der Fragen kommen und duerfen nie ausbleiben:
bleibt eine aus, haengt der Manager 30 Sekunden im Leerlauf.

Dieser Daemon uebersetzt das auf die HTTP-Schnittstelle des Go-Backends.
Er spricht bewusst die Datenform des Telegram-Daemons, denn der Manager
verzweigt nach Protokoll -- und was er fuer Telegram tut, passt fuer
WhatsApp: Chat-Kennung, Absender, Text, Zeitstempel. So bleibt der Eingriff
in fremden Code auf ein paar Zeilen beschraenkt.

Zwei Dinge macht dieser Daemon ausdruecklich NICHT:

  * Er startet das Backend nie selbst. Zwei Backends nebeneinander haben
    hier schon einmal den Nachrichtenspeicher ueberschrieben. Laeuft keines,
    bittet er den Sitzungs-D-Bus um die Aktivierung und wartet.
  * Er liefert beim ersten Lauf nichts aus. Das Backend haelt den ganzen
    zwischengespeicherten Verlauf vor; ohne diese Sperre kippte er beim
    ersten Verbinden vollstaendig in die Nachrichten-App.
"""

import json
import os
import socket
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request

DATA_DIR = os.path.expanduser('~/.pywhatsapp')
SOCK_PATH = os.path.join(DATA_DIR, 'daemon.sock')
STATE_PATH = os.path.join(DATA_DIR, 'state.json')
LOG_PATH = os.path.join(DATA_DIR, 'daemon.log')

# Die Ports, die sich das Backend nimmt (siehe backend/main.go).
PORTS = (8085, 8086, 8087, 8088, 8089)
DBUS_NAME = 'org.smatkovi.WhatsApp'

# So viele Nachrichten je Chat werden beim Abgleich hoechstens betrachtet.
# Mehr braucht es nicht: was aelter ist, war beim letzten Durchlauf schon da.
MAX_NEU = 40
# Obergrenze fuer die Liste bereits ausgelieferter Kennungen.
MAX_GESEHEN = 2000


def log(text):
    zeile = '%s %s\n' % (time.strftime('%H:%M:%S'), text)
    try:
        with open(LOG_PATH, 'a') as f:
            f.write(zeile)
    except Exception:
        pass
    sys.stderr.write(zeile)
    sys.stderr.flush()


# --------------------------------------------------------------------------
# Das Go-Backend
# --------------------------------------------------------------------------

class Backend(object):
    """HTTP-Seite: findet das Backend und fragt es ab."""

    def __init__(self):
        self.port = 0

    def _url(self, pfad, **args):
        if args:
            pfad += '?' + urllib.parse.urlencode(
                {k: v for k, v in args.items() if v is not None})
        return 'http://127.0.0.1:%d%s' % (self.port, pfad)

    def _hole(self, pfad, zeit=20, roh=False, **args):
        antwort = urllib.request.urlopen(self._url(pfad, **args), timeout=zeit)
        inhalt = antwort.read().decode('utf-8', 'replace')
        return inhalt if roh else json.loads(inhalt)

    def finde(self, wecken=True):
        """Sucht das laufende Backend. Gibt True zurueck, wenn es antwortet."""
        for p in ((self.port,) if self.port else ()) + PORTS:
            if not p:
                continue
            self.port = p
            try:
                st = self._hole('/status', zeit=3)
                if isinstance(st, dict):
                    return True
            except Exception:
                continue
        self.port = 0
        if wecken:
            self.wecke()
        return False

    def wecke(self):
        """Bittet den Sitzungs-D-Bus, das Backend zu starten.

        Nicht selbst starten -- die Aktivierung ueber den Bus ist der einzige
        Weg, der garantiert genau eine Instanz ergibt.
        """
        try:
            subprocess.call(
                ['dbus-send', '--session', '--print-reply',
                 '--dest=' + DBUS_NAME, '/',
                 'org.freedesktop.DBus.Peer.Ping'],
                stdout=open(os.devnull, 'w'), stderr=subprocess.STDOUT,
                timeout=20)
        except Exception as e:
            log('wecken fehlgeschlagen: %s' % e)

    # -- Abfragen ----------------------------------------------------------

    def status(self):
        return self._hole('/status', zeit=5)

    def chats(self):
        d = self._hole('/chats', zeit=20)
        return d if isinstance(d, list) else []

    def kontakte(self):
        d = self._hole('/contacts', zeit=20)
        return d if isinstance(d, dict) else {}

    def nachrichten(self, jid):
        d = self._hole('/messages', zeit=20, jid=jid)
        return d if isinstance(d, list) else []

    def warte_auf_ereignis(self, seq):
        """Lange Abfrage: kehrt zurueck, sobald sich im Backend etwas tut."""
        d = self._hole('/events', zeit=40, since=seq)
        return int(d.get('seq', seq)) if isinstance(d, dict) else seq

    def senden(self, jid, text):
        antwort = self._hole('/send', zeit=45, roh=True, to=jid, text=text)
        return antwort.strip() == 'ok'


# --------------------------------------------------------------------------
# Der Daemon
# --------------------------------------------------------------------------

class Daemon(object):

    def __init__(self):
        self.backend = Backend()
        self.klienten = []
        self.klienten_lock = threading.Lock()
        self.zustand_lock = threading.Lock()
        self.titel = {}        # jid -> Anzeigename
        self.namen = {}        # Telefonnummer -> Name (aus /contacts)
        self.eigene = ''       # eigene Nummer
        self.zustand = self._zustand_laden()

    # -- Zustand -----------------------------------------------------------

    def _zustand_laden(self):
        try:
            with open(STATE_PATH) as f:
                z = json.load(f)
            z.setdefault('stand', {})
            z.setdefault('gesehen', [])
            z.setdefault('eingerichtet', False)
            return z
        except Exception:
            return {'stand': {}, 'gesehen': [], 'eingerichtet': False}

    def _zustand_sichern(self):
        try:
            with self.zustand_lock:
                daten = json.dumps(self.zustand)
            # Erst daneben schreiben, dann umbenennen: ein Abbruch mitten im
            # Schreiben darf nicht die Marke zerstoeren, an der haengt,
            # welche Nachrichten schon ausgeliefert sind.
            tmp = STATE_PATH + '.neu'
            with open(tmp, 'w') as f:
                f.write(daten)
            os.rename(tmp, STATE_PATH)
        except Exception as e:
            log('Zustand nicht sicherbar: %s' % e)

    # -- Socket ------------------------------------------------------------

    def sende(self, klient, obj):
        try:
            klient.sendall((json.dumps(obj) + '\n').encode('utf-8'))
        except Exception:
            pass

    def melde(self, name, daten):
        """Schickt ein Ereignis an alle verbundenen Manager."""
        obj = {'event': name, 'data': daten}
        with self.klienten_lock:
            ziele = list(self.klienten)
        for k in ziele:
            self.sende(k, obj)

    def bediene(self, klient):
        with self.klienten_lock:
            self.klienten.append(klient)
        puffer = b''
        try:
            while True:
                teil = klient.recv(8192)
                if not teil:
                    break
                puffer += teil
                while b'\n' in puffer:
                    zeile, puffer = puffer.split(b'\n', 1)
                    if not zeile.strip():
                        continue
                    try:
                        anfrage = json.loads(zeile.decode('utf-8'))
                    except Exception:
                        continue
                    self.sende(klient, self.beantworte(anfrage))
        except Exception as e:
            log('Verbindungsfehler: %s' % e)
        finally:
            with self.klienten_lock:
                if klient in self.klienten:
                    self.klienten.remove(klient)
            try:
                klient.close()
            except Exception:
                pass

    # -- Befehle -----------------------------------------------------------

    def beantworte(self, anfrage):
        befehl = anfrage.get('cmd', '')
        args = anfrage.get('args') or {}
        log('Befehl %s %s' % (befehl, json.dumps(args)[:120]))
        try:
            if befehl == 'get_me':
                return self.cmd_get_me()
            if befehl == 'get_auth_state':
                return self.cmd_auth()
            if befehl == 'get_dialogs':
                return self.cmd_dialoge(int(args.get('limit', 200)))
            if befehl == 'get_messages':
                # Der Abgleich laeuft vollstaendig hier im Daemon; der
                # Nachholmechanismus des Managers wuerde nur ein zweites Mal
                # dasselbe ausliefern.
                return {'messages': []}
            if befehl == 'send_message':
                return self.cmd_senden(args)
            return {'error': 'unbekannter Befehl: %s' % befehl}
        except Exception as e:
            log('Befehl %s gescheitert: %s' % (befehl, e))
            return {'error': str(e)}

    def _sicher_verbunden(self):
        if not self.backend.port and not self.backend.finde():
            raise IOError('Backend nicht erreichbar')

    def cmd_get_me(self):
        self._sicher_verbunden()
        st = self.backend.status()
        self.eigene = str(st.get('phone', '') or '')
        if not self.eigene:
            return {'error': 'nicht verknuepft'}
        return {'id': self.eigene, 'phone': self.eigene}

    def cmd_auth(self):
        try:
            self._sicher_verbunden()
            st = self.backend.status()
        except Exception as e:
            return {'state': 'disconnected', 'error': str(e)}
        if st.get('paired') and st.get('connected'):
            return {'state': 'ready', 'user_id': str(st.get('phone', ''))}
        if st.get('paired'):
            return {'state': 'connecting'}
        return {'state': 'unauthorized'}

    def cmd_dialoge(self, limit):
        """Die Chatliste.

        Der Verbindungsmanager fragt sie GENAU EINMAL beim Verbinden ab
        und baut daraus seine Namenstabelle. Eine leere Antwort ist
        deshalb nicht bloss eine leere Antwort -- sie vergiftet die ganze
        Sitzung: ohne Namen heisst eine Unterhaltung in der
        Nachrichten-App "436781311768" statt "Franz Kainz", und eine neue
        Nachricht darin ist kaum als solche zu erkennen.

        Genau das war zu sehen, nachdem ein Upgrade das Backend neu
        gestartet hatte: es war noch beim Aufbau und antwortete mit null
        Chats, und dabei blieb es.

        Deshalb wird hier gewartet, statt eine leere Liste zurueckzugeben.
        Lieber laesst man den Manager ein paar Sekunden stehen, als ihm
        etwas Unbrauchbares zu geben, das er nie wieder nachfragt.
        """
        self._sicher_verbunden()
        self._namen_auffrischen()
        chats = self.backend.chats()
        versuche = 0
        while not chats and versuche < 12:
            versuche += 1
            time.sleep(2)
            try:
                chats = self.backend.chats()
            except Exception:
                pass
        if versuche:
            log('Chatliste kam erst nach %d Sekunden' % (versuche * 2))
        chats.sort(key=lambda c: c.get('lastTime', 0), reverse=True)
        dialoge = []
        for c in chats[:limit]:
            jid = str(c.get('jid', ''))
            if not jid:
                continue
            titel = c.get('name') or self.namen.get(jid) or jid
            self.titel[jid] = titel
            dialoge.append({
                'id': jid,
                'title': titel,
                'muted': False,
                # Nachholen macht der Daemon selbst -- siehe get_messages.
                'unread': 0,
            })
        log('%d Dialoge geliefert' % len(dialoge))
        return {'dialogs': dialoge}

    def cmd_senden(self, args):
        self._sicher_verbunden()
        jid = str(args.get('chat_id', '') or '')
        text = args.get('text', '') or ''
        if not jid:
            return {'error': 'chat_id fehlt'}
        # Der Manager kennt Chats ueber den Anzeigenamen; kommt einer statt
        # einer Kennung herein, wird er zurueckuebersetzt.
        #
        # Namen sind nicht eindeutig -- es kann eine Gruppe und einen
        # Kontakt gleichen Namens geben. Den ersten Treffer zu nehmen hiesse,
        # eine private Nachricht an eine Gruppe schicken zu koennen. Bei
        # Mehrdeutigkeit gewinnt deshalb der Einzelchat, und es wird
        # vermerkt: lieber an die falsche Person als an alle.
        if not jid.replace('-', '').isdigit():
            treffer = [k for k, v in self.titel.items() if v == jid]
            if len(treffer) > 1:
                log('Name "%s" passt auf %d Chats: %s' % (jid, len(treffer), treffer))
                einzel = [k for k in treffer if '-' not in k and len(k) <= 15]
                treffer = einzel or sorted(treffer)
            if treffer:
                jid = treffer[0]
            else:
                return {'error': 'kein Chat namens %s' % jid}
        if not self.backend.senden(jid, text):
            return {'error': 'Backend hat die Nachricht nicht angenommen'}
        # Die eigene Nachricht gilt sofort als gesehen, sonst kaeme sie beim
        # naechsten Abgleich als eingehend zurueck.
        return {'ok': True}

    # -- Abgleich ----------------------------------------------------------

    def _namen_auffrischen(self):
        try:
            self.namen = {str(k): v for k, v in self.backend.kontakte().items()}
        except Exception:
            pass

    def _absendername(self, nummer):
        if not nummer:
            return ''
        return self.namen.get(str(nummer)) or str(nummer)

    def abgleichen(self):
        """Vergleicht den Backend-Stand mit dem zuletzt ausgelieferten."""
        chats = self.backend.chats()
        with self.zustand_lock:
            stand = dict(self.zustand['stand'])
            gesehen = set(self.zustand['gesehen'])
            erstlauf = not self.zustand['eingerichtet']

        if erstlauf:
            # Nur Marken setzen, nichts ausliefern.
            for c in chats:
                jid = str(c.get('jid', ''))
                if jid:
                    stand[jid] = int(c.get('lastTime', 0) or 0)
            with self.zustand_lock:
                self.zustand['stand'] = stand
                self.zustand['eingerichtet'] = True
            self._zustand_sichern()
            log('Erstlauf: %d Chats als gelesen vermerkt, nichts geliefert'
                % len(stand))
            return

        neue = []
        for c in chats:
            jid = str(c.get('jid', ''))
            if not jid:
                continue
            letzte = int(c.get('lastTime', 0) or 0)
            marke = int(stand.get(jid, 0))
            if letzte <= marke:
                continue
            titel = c.get('name') or self.namen.get(jid) or jid
            self.titel[jid] = titel
            try:
                verlauf = self.backend.nachrichten(jid)
            except Exception as e:
                log('Verlauf %s nicht ladbar: %s' % (jid, e))
                continue
            for m in verlauf[-MAX_NEU:]:
                ts = int(m.get('timestamp', 0) or 0)
                if ts <= marke or m.get('fromMe'):
                    continue
                kennung = str(m.get('id', '')) or '%s:%d' % (jid, ts)
                if kennung in gesehen:
                    continue
                gesehen.add(kennung)
                neue.append((ts, jid, titel, m, kennung))
            stand[jid] = letzte

        neue.sort(key=lambda n: n[0])
        for ts, jid, titel, m, _ in neue:
            absender = str(m.get('sender', '') or '')
            ist_gruppe = '-' in jid or len(jid) > 15
            if ist_gruppe and absender:
                # Unterschiedliche Kennungen: der Manager stellt dann den
                # Absendernamen voran -- genau richtig fuer Gruppen.
                sender_id = absender
            else:
                # Einzelchat: gleiche Kennung, sonst stuende vor jeder
                # Nachricht der eigene Gespraechspartner noch einmal.
                sender_id = jid
            self.melde('new_message', {
                'chat_id': jid,
                'chat_name': titel,
                'sender_id': sender_id,
                'sender_name': self._absendername(absender) or titel,
                'out': False,
                'text': m.get('text', '') or '',
                'date': ts,
                'media_type': '' if m.get('text') else 'Anhang',
            })

        if neue:
            log('%d neue Nachrichten ausgeliefert' % len(neue))
        with self.zustand_lock:
            self.zustand['stand'] = stand
            # Die Liste beschraenken, sonst waechst sie ohne Ende.
            self.zustand['gesehen'] = list(gesehen)[-MAX_GESEHEN:]
        if neue:
            self._zustand_sichern()

    def beobachten(self):
        """Haengt an der langen Abfrage des Backends."""
        seq = 0
        fehler = 0
        while True:
            try:
                if not self.backend.port and not self.backend.finde():
                    # Kein Backend: in Ruhe warten statt zu haemmern. Die
                    # Verbindung zum Manager bleibt dabei bestehen -- ein
                    # geschlossener Socket zaehlt ihm als Absturz und loest
                    # einen Wiederverbindungslauf aus.
                    time.sleep(15)
                    continue
                neu = self.backend.warte_auf_ereignis(seq)
                if neu != seq:
                    seq = neu
                    self.abgleichen()
                fehler = 0
            except Exception as e:
                fehler += 1
                if fehler in (1, 5, 20):
                    log('Beobachter: %s' % e)
                self.backend.port = 0
                time.sleep(min(5 * fehler, 30))

    # -- Start -------------------------------------------------------------

    def laufen(self):
        if not os.path.isdir(DATA_DIR):
            os.makedirs(DATA_DIR)
        if os.path.exists(SOCK_PATH):
            os.unlink(SOCK_PATH)
        server = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        server.bind(SOCK_PATH)
        os.chmod(SOCK_PATH, 0o600)
        server.listen(4)
        log('Daemon laeuft auf %s' % SOCK_PATH)

        threading.Thread(target=self.beobachten, daemon=True).start()

        while True:
            klient, _ = server.accept()
            threading.Thread(target=self.bediene, args=(klient,),
                             daemon=True).start()


if __name__ == '__main__':
    try:
        Daemon().laufen()
    except KeyboardInterrupt:
        pass

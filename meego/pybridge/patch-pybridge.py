#!/usr/bin/python
# -*- coding: utf-8 -*-
"""Traegt WhatsApp in den vorhandenen pybridge-Verbindungsmanager ein.

pybridge gehoert einem anderen Paket. Statt es zu ersetzen, wird es hier an
wenigen Stellen erweitert -- der Manager verzweigt naemlich nach Protokoll,
und unser Daemon spricht bewusst die Datenform des Telegram-Daemons. Aus
jedem "== 'telegram'" wird deshalb ein "in ('telegram', 'whatsapp')", und
mehr ist nicht noetig.

Warum ein pruefendes Skript und kein sed: eine Ersetzung, die ins Leere
laeuft, faellt bei sed nicht auf -- sie hinterlaesst eine Datei, die sich
unauffaellig falsch verhaelt. Hier muss jede erwartete Stelle genau einmal
vorkommen, sonst bricht das Skript ab und ruehrt nichts an. Damit laesst es
sich nach einem pybridge-Update gefahrlos erneut anwenden: entweder es
passt, oder es sagt deutlich, dass sich der fremde Code geaendert hat.

Laeuft unter Python 2.6 (das Bordmittel dieses Geraets) wie unter 3.
"""

import io
import os
import shutil
import sys

CM = '/opt/pybridge/pybridge_cm.py'
MANAGER = '/usr/share/telepathy/managers/pybridge.manager'

MARKE = "'whatsapp': {"


# --- Verbindungsmanager ----------------------------------------------------

ALT_ZUSTELLUNG = '        ch = self._get_or_create_channel(chat_handle)\n        log("DELIVERING to ch=%s h=%s" % (ch._path, sender_handle))\n        ch.receive_message(sender_handle, text, timestamp)'

NEU_ZUSTELLUNG = '        ch = self._get_or_create_channel(chat_handle)\n        log("DELIVERING to ch=%s h=%s" % (ch._path, sender_handle))\n        ch.receive_message(sender_handle, text, timestamp)\n        # Auf einem BESTEHENDEN Kanal erfaehrt CommHistory von einer neuen\n        # Nachricht nur, solange es ihn noch beobachtet. Schliesst man die\n        # Unterhaltung in der Nachrichten-App, hoert das auf -- und danach\n        # stapeln sich die Nachrichten unsichtbar als "pending". Im Feld\n        # sah man dann "pending_before=1" und in der Nachrichten-App\n        # nichts, waehrend die App des Dienstes die Nachricht zeigte.\n        #\n        # Gemeldet wird nur beobachtend (observe_only): die Unterhaltung\n        # soll im Verlauf auftauchen, sich aber nicht von selbst oeffnen.\n        # Hoechstens alle fuenf Sekunden -- jede Meldung startet einen\n        # eigenen Python-Prozess, und der kostet auf diesem Geraet.\n        _jetzt = time.time()\n        if _jetzt - getattr(ch, "_zuletzt_gemeldet", 0) > 5:\n            ch._zuletzt_gemeldet = _jetzt\n            gobject.idle_add(\n                lambda p=ch._path, c=ch: self._dispatch_channel(p, c, True) or False)'

ERSETZUNGEN = [

    # 1) Der Dienst selbst.
    ("""    'matrix': {
        'daemon_script': '/opt/pymatrix/matrix_daemon.py',
        'socket_path': os.path.expanduser('~/.pymatrix/daemon.sock'),
        'data_dir': os.path.expanduser('~/.pymatrix'),
    }
}""",
     """    'matrix': {
        'daemon_script': '/opt/pymatrix/matrix_daemon.py',
        'socket_path': os.path.expanduser('~/.pymatrix/daemon.sock'),
        'data_dir': os.path.expanduser('~/.pymatrix'),
    },
    'whatsapp': {
        'daemon_script': '/opt/pywhatsapp/whatsapp_daemon.py',
        'socket_path': os.path.expanduser('~/.pywhatsapp/daemon.sock'),
        'data_dir': os.path.expanduser('~/.pywhatsapp'),
    }
}"""),

    # 2) Der Rueckfallpfad zum Konto. Ohne eigenen Zweig landeten
    #    WhatsApp-Nachrichten im Verlauf unter dem Matrix-Konto, sobald die
    #    Abfrage beim Kontoverwalter einmal scheitert.
    ("""        if self._protocol == 'telegram':
            return '/org/freedesktop/Telepathy/Account/pybridge/telegram/Telegram0'
        return""",
     """        if self._protocol == 'telegram':
            return '/org/freedesktop/Telepathy/Account/pybridge/telegram/Telegram0'
        if self._protocol == 'whatsapp':
            return '/org/freedesktop/Telepathy/Account/pybridge/whatsapp/WhatsApp0'
        return"""),

    # 3) Senden aus einem offenen Kanal. Die Stelle ist an ihrem "return"
    #    zu erkennen -- der Block beim Nachreichen (5) sieht sonst gleich aus.
    ("""                if protocol == 'telegram':
                    resp = backend.send_request('send_message', {'chat_id': chat_id, 'text': text})
                elif protocol == 'matrix':
                    resp = backend.send_request('send_message', {'room_id': str(chat_id), 'body': text})
                else:
                    return""",
     """                if protocol in ('telegram', 'whatsapp'):
                    resp = backend.send_request('send_message', {'chat_id': chat_id, 'text': text})
                elif protocol == 'matrix':
                    resp = backend.send_request('send_message', {'room_id': str(chat_id), 'body': text})
                else:
                    return"""),

    # 4) Eigene Kennung beim Verbinden.
    ("""            if self._protocol == 'telegram':
                resp = backend.send_request('get_me')""",
     """            if self._protocol in ('telegram', 'whatsapp'):
                resp = backend.send_request('get_me')"""),

    # 5) Nachreichen zwischengespeicherter Nachrichten -- am "continue".
    ("""                if protocol == 'telegram':
                    resp = backend.send_request('send_message', {'chat_id': chat_id, 'text': text})
                elif protocol == 'matrix':
                    resp = backend.send_request('send_message', {'room_id': str(chat_id), 'body': text})
                else:
                    continue""",
     """                if protocol in ('telegram', 'whatsapp'):
                    resp = backend.send_request('send_message', {'chat_id': chat_id, 'text': text})
                elif protocol == 'matrix':
                    resp = backend.send_request('send_message', {'room_id': str(chat_id), 'body': text})
                else:
                    continue"""),

    # 6) Chatliste beim Verbinden.
    ("""            if self._protocol == 'telegram':
                try:
                    resp = backend.send_request('get_dialogs', {'limit': 100})""",
     """            if self._protocol in ('telegram', 'whatsapp'):
                try:
                    resp = backend.send_request('get_dialogs', {'limit': 100})"""),

    # 7) und 8) Dasselbe nach einem Verbindungsabriss.
    ("""                                    if self._protocol=="telegram":
                                        rsp=self._backend.send_request("get_dialogs", {"limit": 100})""",
     """                                    if self._protocol in ("telegram", "whatsapp"):
                                        rsp=self._backend.send_request("get_dialogs", {"limit": 100})"""),

    ("""                                    if self._protocol == "telegram" and rsp and "dialogs" in rsp:""",
     """                                    if self._protocol in ("telegram", "whatsapp") and rsp and "dialogs" in rsp:"""),

    # 9) Die Parameter, nach denen die Kontoverwaltung fragt.
    ("""        if protocol == 'telegram':
            return [('account', dbus.UInt32(4), 's', '')]""",
     """        if protocol in ('telegram', 'whatsapp'):
            return [('account', dbus.UInt32(4), 's', '')]"""),
]


PROTOKOLL_EINTRAG = """
[Protocol whatsapp]
param-account=s required
ConnectionInterfaces=org.freedesktop.Telepathy.Connection.Interface.Requests;org.freedesktop.Telepathy.Connection.Interface.SimplePresence;org.freedesktop.Telepathy.Connection.Interface.Contacts;
RequestableChannelClasses=org.freedesktop.Telepathy.Channel.Type.Text
VCardField=x-whatsapp
EnglishName=WhatsApp
Icon=icon-m-service-whatsapp
"""



def zustellung_ergaenzen(text):
    """Meldet einen bestehenden Kanal erneut, wenn eine Nachricht kommt.

    Diese Ergaenzung gilt fuer ALLE Protokolle -- Telegram, WhatsApp und
    Signal teilen sich die Stelle. Sie wird deshalb auch dann angewandt,
    wenn das eigene Protokoll schon eingetragen ist; sonst kaeme sie nie
    zur Anwendung, weil das Skript vorher abbricht.

    Gibt (Text, ob geaendert) zurueck.
    """
    if "_zuletzt_gemeldet" in text:
        return text, False
    if text.count(ALT_ZUSTELLUNG) != 1:
        sys.stderr.write(
            "pybridge: Zustellstelle nicht eindeutig gefunden - "
            "bleibt unveraendert\n")
        return text, False
    return text.replace(ALT_ZUSTELLUNG, NEU_ZUSTELLUNG, 1), True


def lies(pfad):
    f = io.open(pfad, encoding='utf-8')
    try:
        return f.read()
    finally:
        f.close()


def schreib(pfad, text):
    # Erst daneben, dann umbenennen: ein halb geschriebener
    # Verbindungsmanager legt auch Telegram und Matrix lahm.
    tmp = pfad + '.neu'
    f = io.open(tmp, 'w', encoding='utf-8')
    try:
        f.write(text)
    finally:
        f.close()
    os.rename(tmp, pfad)


def sichern(pfad):
    ab = pfad + '.vor-whatsapp'
    if not os.path.exists(ab):
        shutil.copy2(pfad, ab)


def patche_cm():
    if not os.path.exists(CM):
        sys.stderr.write('pybridge nicht gefunden: %s\n' % CM)
        return False
    text = lies(CM)

    # Zuerst die Ergaenzung, die allen Protokollen gilt. Sie muss auch
    # laufen, wenn das eigene Protokoll schon eingetragen ist.
    text, zustellung = zustellung_ergaenzen(text)
    if zustellung:
        sichern(CM)
        schreib(CM, text)
        print('pybridge: Kanaele werden bei neuen Nachrichten erneut gemeldet')

    if MARKE in text:
        print('pybridge: WhatsApp bereits eingetragen')
        return True

    for alt, neu in ERSETZUNGEN:
        anzahl = text.count(alt)
        if anzahl != 1:
            sys.stderr.write(
                'pybridge hat sich geaendert: Stelle kommt %d-mal vor '
                '(erwartet: genau einmal)\n---\n%s\n---\n'
                % (anzahl, alt[:160]))
            return False

    sichern(CM)
    for alt, neu in ERSETZUNGEN:
        text = text.replace(alt, neu, 1)
    schreib(CM, text)
    print('pybridge: WhatsApp eingetragen (%d Stellen)' % len(ERSETZUNGEN))
    return True


def patche_manager():
    if not os.path.exists(MANAGER):
        sys.stderr.write('Manager-Datei fehlt: %s\n' % MANAGER)
        return False
    text = lies(MANAGER)
    if '[Protocol whatsapp]' in text:
        print('pybridge.manager: bereits eingetragen')
        return True
    sichern(MANAGER)
    # Nicht blind anhaengen: die Datei auf dem Geraet endet mit einer
    # verirrten Heredoc-Marke ("MEOF"). Was dahinter steht, sieht ein
    # Parser, der an dieser Zeile abbricht, nie. Der neue Abschnitt kommt
    # deshalb hinter die letzte verwertbare Zeile, nicht hinter das Ende.
    zeilen = text.split('\n')
    letzte = -1
    for i, z in enumerate(zeilen):
        k = z.strip()
        if k.startswith('[') or ('=' in k and not k.startswith('#')):
            letzte = i
    if letzte < 0:
        sys.stderr.write('Manager-Datei unverstaendlich: %s\n' % MANAGER)
        return False
    kopf = zeilen[:letzte + 1]
    rest = zeilen[letzte + 1:]
    neu = kopf + PROTOKOLL_EINTRAG.rstrip('\n').split('\n') + rest
    schreib(MANAGER, '\n'.join(neu))
    print('pybridge.manager: WhatsApp eingetragen')
    return True


def main():
    ok = patche_cm()
    ok = patche_manager() and ok
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(main())

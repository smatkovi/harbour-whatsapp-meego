#include "Backend.h"
#include "Json.h"

#include <QDateTime>
#include <QDesktopServices>
#include <QFileInfo>
#include <QFileInfoList>
#include <QDir>
#include <QFile>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QProcess>
#include <QStringList>
#include <QTimer>
#include <QUrl>

namespace {

// Das Backend sucht sich selbst einen freien Port zwischen 8085 und 8089 und
// schreibt ihn neben die Datenbank. Es blind auf 8085 festzunageln waere die
// Sorte Annahme, die genau dann bricht, wenn noch eine alte Instanz haengt.
const int PORT_VON = 8085;
const int PORT_BIS = 8089;

QString datenVerzeichnis()
{
    return QDir::homePath() + QLatin1String("/.local/share/harbour/harbour-whatsapp");
}

}

Backend::Backend(const QString &binary, QObject *parent)
    : QObject(parent)
    , m_netz(new QNetworkAccessManager(this))
    , m_ereignis(0)
    , m_dienst(0)
    , m_takt(new QTimer(this))
    , m_binary(binary)
    , m_port(0)
    , m_seq(0)
    , m_gekoppelt(false)
    , m_verbunden(false)
    , m_anrufTakt(new QTimer(this))
    , m_anrufAbfrage(0)
    , m_anrufAktiv(false)
    , m_anrufAus(false)
    , m_anrufSek(0)
    , m_anrufStumm(false)
    , m_laedtAeltere(false)
    , m_laedtGruppe(false)
{
    m_zustand = QLatin1String("starting");
    // Der Takt ist nur das Sicherheitsnetz; die eigentliche Aktualisierung
    // kommt aus /events. Ohne Netz bliebe die Oberflaeche stehen, wenn ein
    // Long-Poll einmal im Nichts endet.
    m_takt->setInterval(15000);
    connect(m_takt, SIGNAL(timeout()), this, SLOT(abfragen()));

    // Anrufe brauchen einen engeren Takt als der Rest: zwei Sekunden, damit
    // ein eingehender Anruf nicht erst nach einer Viertelminute auffaellt.
    // Wie beim Ereignis-Poll bleibt hoechstens eine Abfrage offen -- Qt 4.7
    // laesst nur sechs Verbindungen je Host zu.
    m_anrufTakt->setInterval(2000);
    connect(m_anrufTakt, SIGNAL(timeout()), this, SLOT(anrufAbfragen()));
}

Backend::~Backend()
{
    // Der Dienst laeuft absichtlich weiter: er haelt die Verbindung zu
    // WhatsApp und nimmt Nachrichten entgegen, auch wenn die Oberflaeche zu
    // ist. Beendet wird er ueber /quit oder mit dem Geraet.
}

int Backend::port()
{
    if (m_port)
        return m_port;
    QFile f(datenVerzeichnis() + QLatin1String("/backend.port"));
    if (f.open(QIODevice::ReadOnly)) {
        const int p = QString::fromLatin1(f.readAll()).trimmed().toInt();
        if (p >= PORT_VON && p <= PORT_BIS)
            m_port = p;
    }
    if (!m_port)
        m_port = PORT_VON;
    return m_port;
}

QNetworkReply *Backend::hole(const QString &pfad)
{
    QNetworkRequest r(QUrl(QString::fromLatin1("http://127.0.0.1:%1%2")
                           .arg(port()).arg(pfad)));
    return m_netz->get(r);
}

QUrl Backend::adresse(const QString &pfad)
{
    // Parameter NICHT selbst prozentkodieren und in die Zeichenkette haengen:
    // QUrl kodiert danach ein zweites Mal, aus "," wird ueber "%2C" dann
    // "%252C", das Backend dekodiert einmal -- und im verschickten Text steht
    // "%2C" statt dem Komma. Genau so sind Kommas und Fragezeichen in
    // Nachrichten gelandet. addQueryItem macht es richtig, einmal.
    QUrl u;
    u.setScheme(QLatin1String("http"));
    u.setHost(QLatin1String("127.0.0.1"));
    u.setPort(port());
    u.setPath(pfad);
    return u;
}

void Backend::setzeFehler(const QString &text)
{
    if (m_fehler == text)
        return;
    m_fehler = text;
    emit fehlerChanged();
}

// kontenEinrichten legt die beiden Konten an, die WhatsApp hier braucht --
// eines fuer die Nachrichten-App (pybridge), eines fuer Anrufe (SIP).
//
// Warum aus der App und nicht aus dem postinst, wo es hingehoerte: dort
// laeuft alles als root, und mc-tool wie ag-tool schreiben in die
// Kontoverzeichnisse der Sitzung. Die Kennung zu wechseln verweigert Aegis
// gleich zweifach -- "su: can't set groups: Operation not permitted" und
// "start-stop-daemon: unable to set gid to 29999". Die App dagegen laeuft
// ohnehin als "user" und hat den Sitzungsbus; hier gelingt es.
//
// Die Marke haelt fest, dass es getan ist. Sie wird mit dem Skript
// verglichen: ist das Skript neuer, lief zwischendurch ein Upgrade, und es
// wird noch einmal ausgefuehrt. Das Skript selbst ist ohnehin so gebaut,
// dass ein zweiter Lauf nichts anrichtet.
static void kontenEinrichten()
{
    const QString skript = QLatin1String("/opt/pywhatsapp/whatsapp-setup");
    const QFileInfo si(skript);
    if (!si.exists() || !si.isExecutable())
        return;

    QDir d(QDir::homePath() + QLatin1String("/.local/share/harbour/harbour-whatsapp"));
    if (!d.exists())
        d.mkpath(QLatin1String("."));
    const QString markePfad = d.absoluteFilePath(QLatin1String("konten-eingerichtet"));
    const QFileInfo mi(markePfad);
    if (mi.exists() && mi.lastModified() >= si.lastModified())
        return;

    if (!QProcess::startDetached(skript, QStringList() << QLatin1String("add")))
        return;
    QFile marke(markePfad);
    if (marke.open(QIODevice::WriteOnly))
        marke.close();
}

void Backend::starten()
{
    kontenEinrichten();

    // Laeuft schon einer? Dann nur anklopfen. Das Backend bringt seinen
    // eigenen Mechanismus mit, um doppelte Instanzen zu vermeiden, aber ein
    // zweiter Start kostet auf diesem Geraet mehrere Sekunden.
    QNetworkReply *r = hole(QLatin1String("/status"));
    connect(r, SIGNAL(finished()), this, SLOT(statusFertig()));

    if (!m_dienst && QFile::exists(m_binary)) {
        m_dienst = new QProcess(this);
        // Ohne das faengt QProcess die Ausgabe des Backends ab und niemand
        // sieht sie je -- das hat die Suche nach den fehlenden Kontakten
        // unnoetig lange gekostet.
        m_dienst->setProcessChannelMode(QProcess::ForwardedChannels);
        m_dienst->setWorkingDirectory(QDir::homePath());
        QStringList umgebung = QProcess::systemEnvironment();
        m_dienst->setEnvironment(umgebung);
        m_dienst->start(m_binary, QStringList());
    }
    m_takt->start();
    m_anrufTakt->start();
    QTimer::singleShot(1500, this, SLOT(abfragen()));
}

void Backend::abfragen()
{
    QNetworkReply *r = hole(QLatin1String("/status"));
    connect(r, SIGNAL(finished()), this, SLOT(statusFertig()));
}

void Backend::statusFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError) {
        // Waehrend das Backend hochfaehrt ist das der Normalfall, kein Grund
        // den Nutzer zu behelligen.
        m_zustand = QLatin1String("starting");
        emit statusChanged();
        return;
    }
    const QVariantMap s = Json::parse(QString::fromUtf8(r->readAll())).toMap();
    if (s.isEmpty())
        return;

    const QString zustandNeu = s.value(QLatin1String("state")).toString();
    const bool gekoppeltNeu = s.value(QLatin1String("paired")).toBool();
    const bool verbundenNeu = s.value(QLatin1String("connected")).toBool();
    const QString nummerNeu = s.value(QLatin1String("phone")).toString();
    const QString codeNeu = s.value(QLatin1String("pairCode")).toString();

    const bool frischGekoppelt = gekoppeltNeu && !m_gekoppelt;
    if (zustandNeu != m_zustand || gekoppeltNeu != m_gekoppelt
            || verbundenNeu != m_verbunden || nummerNeu != m_nummer
            || codeNeu != m_code) {
        m_zustand = zustandNeu;
        m_gekoppelt = gekoppeltNeu;
        m_verbunden = verbundenNeu;
        m_nummer = nummerNeu;
        m_code = codeNeu;
        emit statusChanged();
    }
    setzeFehler(s.value(QLatin1String("lastError")).toString());

    if (gekoppeltNeu) {
        if (frischGekoppelt || m_chats.isEmpty())
            neuLaden();
        ereignisPoll();
    }
}

void Backend::ereignisFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    if (r == m_ereignis)
        m_ereignis = 0;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError)
        return;
    const QVariantMap e = Json::parse(QString::fromUtf8(r->readAll())).toMap();
    const qint64 seq = e.value(QLatin1String("seq")).toLongLong();
    if (seq != m_seq) {
        m_seq = seq;
        neuLaden();
    }
    ereignisPoll();
}

void Backend::ereignisPoll()
{
    // Hoechstens EIN offener Long-Poll. Vorher setzte jeder Statusabruf einen
    // neuen auf, und der Takt laeuft alle 15 Sekunden -- bei bis zu 25
    // Sekunden Haltezeit haeuften sie sich. Qt 4.7 erlaubt sechs gleichzeitige
    // Verbindungen je Host; ab da standen alle anderen Anfragen in der
    // Warteschlange, auch /messages. Der Verlauf blieb leer, ohne jede
    // Fehlermeldung, und es wurde mit der Laufzeit schlimmer statt besser.
    if (m_ereignis)
        return;
    m_ereignis = hole(QString::fromLatin1("/events?since=%1").arg(m_seq));
    connect(m_ereignis, SIGNAL(finished()), this, SLOT(ereignisFertig()));
}

void Backend::neuLaden()
{
    QNetworkReply *r = hole(QLatin1String("/chats"));
    connect(r, SIGNAL(finished()), this, SLOT(chatsFertig()));
    if (!m_offenerChat.isEmpty())
        chatOeffnen(m_offenerChat);
}

void Backend::chatsFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError)
        return;
    const QVariant v = Json::parse(QString::fromUtf8(r->readAll()));
    if (!v.isValid())
        return;
    m_chats = v.toList();
    emit chatsChanged();
}

void Backend::chatOeffnen(const QString &jid)
{
    m_offenerChat = jid;
    QUrl um = adresse(QLatin1String("/messages"));
    um.addQueryItem(QLatin1String("jid"), jid);
    QNetworkReply *r = m_netz->get(QNetworkRequest(um));
    connect(r, SIGNAL(finished()), this, SLOT(nachrichtenFertig()));
    // Gelesen melden, damit der ungelesen-Zaehler auf beiden Geraeten stimmt.
    QUrl ug = adresse(QLatin1String("/chat/opened"));
    ug.addQueryItem(QLatin1String("jid"), jid);
    QNetworkReply *g = m_netz->get(QNetworkRequest(ug));
    connect(g, SIGNAL(finished()), g, SLOT(deleteLater()));
}

void Backend::chatSchliessen()
{
    m_offenerChat.clear();
    m_nachrichten.clear();
    emit nachrichtenChanged();
}

void Backend::nachrichtenFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError)
        return;
    const QVariant v = Json::parse(QString::fromUtf8(r->readAll()));
    if (!v.isValid())
        return;
    m_nachrichten = v.toList();
    emit nachrichtenChanged();
}

void Backend::gruppeLaden(const QString &jid)
{
    if (m_laedtGruppe)
        return;
    m_laedtGruppe = true;
    m_mitglieder.clear();
    emit gruppeChanged();
    QUrl u = adresse(QLatin1String("/group/info"));
    u.addQueryItem(QLatin1String("chat"), jid);
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(gruppeFertig()));
}

void Backend::gruppeFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    m_laedtGruppe = false;
    if (r->error() != QNetworkReply::NoError) {
        setzeFehler(r->errorString());
        emit gruppeChanged();
        return;
    }
    const QVariantMap info =
            Json::parse(QString::fromUtf8(r->readAll())).toMap();
    const QVariantList roh = info.value(QLatin1String("participants")).toList();

    // Die Avatare liegen als Dateien neben den Chatbildern -- das Backend
    // laedt sie nach Telefonnummer. Hier nur nachsehen, ob eine da ist:
    // fehlt sie, zeigt die Liste den Ersatzkreis, und nichts haengt.
    const QString avatarOrdner = QDir::homePath()
            + QLatin1String("/MyDocs/Pictures/WhatsApp/avatars/");

    QVariantList aus;
    for (int i = 0; i < roh.size(); ++i) {
        QVariantMap m = roh.at(i).toMap();
        const QString nummer = m.value(QLatin1String("number")).toString();
        const QString bild = avatarOrdner + nummer + QLatin1String(".jpg");
        m.insert(QLatin1String("avatar"),
                 QFile::exists(bild) ? bild : QString());
        // Ohne Namen die Nummer zeigen, nicht eine leere Zeile.
        if (m.value(QLatin1String("name")).toString().isEmpty())
            m.insert(QLatin1String("name"), nummer);
        aus.append(m);
    }
    m_mitglieder = aus;
    emit gruppeChanged();
}

void Backend::aeltereLaden()
{
    if (m_laedtAeltere || m_offenerChat.isEmpty())
        return;
    m_laedtAeltere = true;
    m_aeltereHinweis.clear();
    emit aeltereChanged();
    QUrl u = adresse(QLatin1String("/history/request"));
    u.addQueryItem(QLatin1String("chat"), m_offenerChat);
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(aeltereFertig()));
}

void Backend::aeltereFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    m_laedtAeltere = false;
    const QString antwort = QString::fromUtf8(r->readAll()).trimmed();
    if (r->error() != QNetworkReply::NoError) {
        m_aeltereHinweis = antwort.isEmpty() ? r->errorString() : antwort;
        emit aeltereChanged();
        setzeFehler(m_aeltereHinweis);
        return;
    }
    // /history/request antwortet im Klartext, nicht in JSON -- und es
    // meldet nur, dass die Anfrage beim Telefon ist. Die Nachrichten
    // kommen von dort und treffen als Ereignis ein.
    m_aeltereHinweis = QString::fromUtf8(
        "Beim Telefon angefragt \xe2\x80\x93 die Nachrichten treffen "
        "gleich ein. Das Haupttelefon muss online sein.");
    emit aeltereChanged();
}

void Backend::senden(const QString &jid, const QString &text)
{
    if (text.trimmed().isEmpty())
        return;
    QUrl u = adresse(QLatin1String("/send"));
    u.addQueryItem(QLatin1String("to"), jid);
    u.addQueryItem(QLatin1String("text"), text);
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(sendenFertig()));
}

void Backend::sendenFertig()
{
    // /send antwortet mit dem blanken Wort "ok", nicht mit JSON. Die Antwort
    // an nachrichtenFertig zu haengen war falsch: dort scheiterte das Parsen
    // still, der Verlauf wurde nie nachgeladen -- und ein Fehler beim Senden
    // waere ueberhaupt nicht aufgefallen. Die Nachricht verschwand einfach.
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    const QString antwort = QString::fromUtf8(r->readAll()).trimmed();
    if (r->error() != QNetworkReply::NoError) {
        setzeFehler(antwort.isEmpty() ? r->errorString() : antwort);
        return;
    }
    if (antwort != QLatin1String("ok")) {
        setzeFehler(antwort);
        return;
    }
    setzeFehler(QString());
    // Das Backend traegt die eigene Nachricht erst nach dem Versand ein.
    if (!m_offenerChat.isEmpty())
        chatOeffnen(m_offenerChat);
}

void Backend::koppeln(const QString &telefonnummer)
{
    // Nur Ziffern: das Backend erwartet die internationale Form ohne Plus,
    // und ein Leerzeichen aus der Tastatur darf daran nicht scheitern.
    QString sauber;
    for (int i = 0; i < telefonnummer.length(); ++i) {
        if (telefonnummer.at(i).isDigit())
            sauber.append(telefonnummer.at(i));
    }
    if (sauber.isEmpty()) {
        setzeFehler(QString::fromUtf8("Bitte die Nummer mit Landesvorwahl eingeben."));
        return;
    }
    QNetworkReply *r = hole(QLatin1String("/pair?phone=") + sauber);
    connect(r, SIGNAL(finished()), this, SLOT(kopplungFertig()));
}

void Backend::kopplungFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    const QByteArray roh = r->readAll();
    if (r->error() != QNetworkReply::NoError) {
        setzeFehler(QString::fromUtf8(roh).trimmed());
        return;
    }
    const QVariantMap m = Json::parse(QString::fromUtf8(roh)).toMap();
    const QString code = m.value(QLatin1String("code")).toString();
    if (code.isEmpty()) {
        setzeFehler(QString::fromUtf8("Das Backend hat keinen Code geliefert."));
        return;
    }
    m_code = code;
    setzeFehler(QString());
    emit statusChanged();
}

QString Backend::zeit(const QVariant &wert) const
{
    if (!wert.isValid() || wert.isNull())
        return QString();
    QDateTime t;
    // Das Backend liefert Zeitstempel als Sekunden seit 1970 (int64). Ueber
    // QString gefuehrt wuerde QML daraus schnell "1.75e+09" machen, deshalb
    // wird die Zahl hier direkt genommen und die Zeichenkette nur als
    // Rueckfall behandelt.
    bool ok = false;
    const qint64 zahl = wert.toLongLong(&ok);
    if (ok && zahl > 0) {
        t = QDateTime::fromMSecsSinceEpoch(zahl > Q_INT64_C(100000000000)
                                           ? zahl : zahl * 1000);
    } else {
        t = QDateTime::fromString(wert.toString(), Qt::ISODate);
    }
    if (!t.isValid())
        return QString();
    t = t.toLocalTime();
    if (t.date() == QDate::currentDate())
        return t.toString(QLatin1String("HH:mm"));
    return t.toString(QLatin1String("d.M. HH:mm"));
}

void Backend::medienLaden(const QString &nachrichtenId)
{
    if (nachrichtenId.isEmpty())
        return;
    QUrl u = adresse(QLatin1String("/download"));
    u.addQueryItem(QLatin1String("id"), nachrichtenId);
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(medienFertig()));
}

void Backend::medienFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    const QByteArray roh = r->readAll();
    if (r->error() != QNetworkReply::NoError) {
        setzeFehler(QString::fromUtf8(roh).trimmed());
        return;
    }
    // Die Antwort ist {"path": "..."}. Den Pfad selbst braucht hier niemand:
    // er steht nach dem Abruf auch in der Nachricht, und ein Nachladen des
    // Verlaufs bringt ihn in die Oberflaeche.
    setzeFehler(QString());
    if (!m_offenerChat.isEmpty())
        chatOeffnen(m_offenerChat);
}

void Backend::oeffnen(const QString &pfad)
{
    if (pfad.isEmpty())
        return;
    const QString ziel = nachMyDocs(pfad);
    if (ziel.isEmpty())
        return;

    // Sprachnachrichten kommen als Opus in OGG. Harmattan ist von 2011 und
    // kennt Opus nicht -- der Betrachter oeffnet die Datei zwar, bleibt aber
    // stumm. Das mitgelieferte ffmpeg kann Opus dekodieren, also wird einmal
    // nach WAV gewandelt und das abgespielt.
    if (ziel.endsWith(QLatin1String(".ogg"), Qt::CaseInsensitive)
            || ziel.endsWith(QLatin1String(".opus"), Qt::CaseInsensitive)) {
        const QString wav = ziel.left(ziel.lastIndexOf(QLatin1Char('.'))) + QLatin1String(".wav");
        if (QFile::exists(wav)) {
            starteBetrachter(wav);
            return;
        }
        const QString ffmpeg = QLatin1String("/opt/ffmpeg/bin/ffmpeg");
        if (!QFile::exists(ffmpeg)) {
            setzeFehler(QString::fromUtf8("Sprachnachrichten brauchen ffmpeg "
                                          "(Paket ffmpeg-n9)."));
            return;
        }
        // Nebenlaeufig: auf diesem Geraet dauert das eine Sekunde oder zwei,
        // und die Oberflaeche soll solange nicht stehen.
        QProcess *wandler = new QProcess(this);
        wandler->setProperty("wav", wav);
        connect(wandler, SIGNAL(finished(int, QProcess::ExitStatus)),
                this, SLOT(umwandlungFertig(int)));
        setzeFehler(QString::fromUtf8("Sprachnachricht wird umgewandelt …"));
        wandler->start(ffmpeg, QStringList()
                       << QLatin1String("-loglevel") << QLatin1String("error")
                       << QLatin1String("-y")
                       << QLatin1String("-i") << ziel
                       << QLatin1String("-ar") << QLatin1String("44100")
                       << wav);
        return;
    }

    starteBetrachter(ziel);
}

void Backend::starteBetrachter(const QString &pfad)
{
    // Nicht QDesktopServices: dessen Qt-4.7-Fassung kennt Harmattans
    // Zuordnung nicht und meldet trotzdem Erfolg. xdg-open gibt es auf dem
    // Geraet und trifft den richtigen Betrachter.
    if (!QProcess::startDetached(QLatin1String("/usr/bin/xdg-open"),
                                 QStringList() << pfad)) {
        setzeFehler(QString::fromUtf8("Die Datei laesst sich nicht oeffnen: ") + pfad);
    }
}

void Backend::umwandlungFertig(int code)
{
    QProcess *p = qobject_cast<QProcess *>(sender());
    if (!p)
        return;
    const QString wav = p->property("wav").toString();
    const QByteArray meldung = p->readAllStandardError();
    p->deleteLater();
    if (code != 0 || !QFile::exists(wav)) {
        setzeFehler(QString::fromUtf8("Umwandeln fehlgeschlagen: ")
                    + QString::fromUtf8(meldung).trimmed().left(120));
        return;
    }
    setzeFehler(QString());
    starteBetrachter(wav);
}

QString Backend::nachMyDocs(const QString &pfad)
{
    QFileInfo quelle(pfad);
    if (!quelle.exists()) {
        setzeFehler(QString::fromUtf8("Die Datei ist nicht mehr da."));
        return QString();
    }

    // Seit das Backend seine Medienwurzel plattformabhaengig waehlt, landen
    // Anhaenge schon unter MyDocs -- dort, wo der Tracker sie findet und die
    // Dokumente-App sie zeigt. Dann ist nichts zu tun.
    const QString myDocs = QDir(QDir::homePath() + QLatin1String("/MyDocs")).absolutePath();
    if (quelle.absoluteFilePath().startsWith(myDocs + QLatin1Char('/')))
        return pfad;

    // Rueckfall fuer Dateien, die eine aeltere Fassung noch nach ~/Documents
    // geladen hat: eine Kopie nach MyDocs, sonst bleiben sie unsichtbar.
    const QString ordner = QDir::homePath() + QLatin1String("/MyDocs/Downloads");
    if (!QDir(ordner).exists() && !QDir().mkpath(ordner))
        return pfad;                       // dann eben von dort, wo es liegt
    if (quelle.absolutePath() == QDir(ordner).absolutePath())
        return pfad;                       // schon dort

    const QString ziel = ordner + QLatin1Char('/') + quelle.fileName();
    if (QFile::exists(ziel)) {
        if (QFileInfo(ziel).size() == quelle.size())
            return ziel;                   // dieselbe Datei, nichts zu tun
        QFile::remove(ziel);
    }
    if (!QFile::copy(pfad, ziel)) {
        setzeFehler(QString::fromUtf8("Kopieren nach MyDocs/Downloads ging nicht."));
        return pfad;
    }
    return ziel;
}

QString Backend::groesse(const QVariant &bytes) const
{
    const qint64 n = bytes.toLongLong();
    if (n <= 0)
        return QString();
    if (n < 1024)
        return QString::number(n) + QLatin1String(" B");
    if (n < 1024 * 1024)
        return QString::number(n / 1024) + QLatin1String(" kB");
    return QString::number(n / (1024.0 * 1024.0), 'f', 1) + QLatin1String(" MB");
}

QString Backend::startVerzeichnis() const
{
    // MyDocs ist die Partition, die auch der Dateimanager und der Rechner am
    // USB-Kabel sehen -- dort liegt, was der Nutzer verschicken will.
    const QString myDocs = QDir::homePath() + QLatin1String("/MyDocs");
    return QDir(myDocs).exists() ? myDocs : QDir::homePath();
}

QVariantList Backend::verzeichnis(const QString &pfad) const
{
    QVariantList aus;
    QDir d(pfad);
    if (!d.exists())
        return aus;

    // Eine Stufe zurueck, ausser im Startverzeichnis.
    if (QDir::cleanPath(pfad) != QDir::cleanPath(startVerzeichnis())) {
        QVariantMap hoch;
        hoch.insert(QLatin1String("name"), QString::fromUtf8(".."));
        hoch.insert(QLatin1String("pfad"), QFileInfo(pfad).absolutePath());
        hoch.insert(QLatin1String("istOrdner"), true);
        hoch.insert(QLatin1String("bytes"), 0);
        aus.append(hoch);
    }

    d.setFilter(QDir::Dirs | QDir::Files | QDir::NoDotAndDotDot);
    d.setSorting(QDir::DirsFirst | QDir::Name | QDir::IgnoreCase);

    // Begrenzt, und zwar aus einem handfesten Grund: MyDocs ist VFAT und
    // kann Tausende Dateien enthalten. Jeder Eintrag kostet hier ein stat
    // fuer Groesse und Typ, und das laeuft im Faden der Oberflaeche -- bei
    // einem grossen Ordner steht das Geraet dann so lange, dass es sich
    // aufgehaengt anfuehlt. Lieber die ersten paar hundert zeigen und es
    // sagen, als die App einfrieren zu lassen.
    const int grenze = 400;
    const QFileInfoList eintraege = d.entryInfoList();
    int gezeigt = 0;
    for (int i = 0; i < eintraege.size() && gezeigt < grenze; ++i) {
        const QFileInfo &f = eintraege.at(i);
        if (f.fileName().startsWith(QLatin1Char('.')))
            continue;                       // .thumbnails und Konsorten
        QVariantMap m;
        m.insert(QLatin1String("name"), f.fileName());
        m.insert(QLatin1String("pfad"), f.absoluteFilePath());
        m.insert(QLatin1String("istOrdner"), f.isDir());
        m.insert(QLatin1String("bytes"), f.isDir() ? 0 : (qint64)f.size());
        aus.append(m);
        ++gezeigt;
    }
    if (eintraege.size() > gezeigt) {
        QVariantMap rest;
        rest.insert(QLatin1String("name"),
                    QString::fromUtf8("… %1 weitere, hier nicht gezeigt")
                        .arg(eintraege.size() - gezeigt));
        rest.insert(QLatin1String("pfad"), QString());
        rest.insert(QLatin1String("istOrdner"), false);
        rest.insert(QLatin1String("bytes"), 0);
        aus.append(rest);
    }
    return aus;
}

void Backend::anhangSenden(const QString &jid, const QString &pfad,
                           const QString &beschriftung)
{
    if (jid.isEmpty() || pfad.isEmpty())
        return;
    // Die einfache Form von /sendmedia nimmt einen lokalen Pfad. Multipart
    // waere hier unsinnig: die Datei liegt schon auf demselben Geraet.
    QUrl u = adresse(QLatin1String("/sendmedia"));
    u.addQueryItem(QLatin1String("to"), jid);
    u.addQueryItem(QLatin1String("file"), pfad);
    if (!beschriftung.isEmpty())
        u.addQueryItem(QLatin1String("caption"), beschriftung);
    QNetworkReply *rep = m_netz->post(QNetworkRequest(u), QByteArray());
    connect(rep, SIGNAL(finished()), this, SLOT(anhangFertig()));
}

void Backend::anhangFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    const QString antwort = QString::fromUtf8(r->readAll()).trimmed();
    if (r->error() != QNetworkReply::NoError || antwort != QLatin1String("ok")) {
        setzeFehler(antwort.isEmpty() ? r->errorString() : antwort);
        return;
    }
    setzeFehler(QString());
    if (!m_offenerChat.isEmpty())
        chatOeffnen(m_offenerChat);
}

// ------------------------------------------------------------------ Anrufe

void Backend::anrufAbfragen()
{
    if (m_anrufAbfrage) {
        // Hoechstens eine Abfrage offen -- aber nicht endlos. Qt 4.7 kennt
        // keine Zeitgrenze fuer Netzanfragen: bleibt eine haengen, kommt
        // finished() nie, m_anrufAbfrage bleibt belegt, und die
        // Anrufansicht steht fuer den Rest des Gespraechs auf dem zuletzt
        // gesehenen Zustand. Im Feld sah das so aus, dass "verbindet ..."
        // stehenblieb, obwohl das Backend laengst auf "active" war und die
        // Gegenseite zu hoeren war.
        if (m_anrufAbfrageSeit.isValid() && m_anrufAbfrageSeit.elapsed() < 8000)
            return;
        QNetworkReply *alt = m_anrufAbfrage;
        m_anrufAbfrage = 0;
        alt->abort();   // finished() raeumt auf, der Vergleich dort faellt aus
    }
    m_anrufAbfrage = hole(QLatin1String("/call/state"));
    m_anrufAbfrageSeit.start();
    connect(m_anrufAbfrage, SIGNAL(finished()), this, SLOT(anrufZustandFertig()));
}

void Backend::anrufZustandFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    if (r == m_anrufAbfrage)
        m_anrufAbfrage = 0;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError)
        return;
    const QVariantMap s = Json::parse(QString::fromUtf8(r->readAll())).toMap();
    if (s.isEmpty())
        return;

    const bool aktiv = s.value(QLatin1String("active")).toBool();
    const QString name = s.value(QLatin1String("name")).toString();
    const QString phase = s.value(QLatin1String("phase")).toString();
    const bool aus = s.value(QLatin1String("outgoing")).toBool();
    const int sek = s.value(QLatin1String("seconds")).toInt();
    const bool stumm = s.value(QLatin1String("muted")).toBool();

    if (aktiv != m_anrufAktiv || name != m_anrufName || phase != m_anrufPhase
            || aus != m_anrufAus || sek != m_anrufSek || stumm != m_anrufStumm) {
        m_anrufAktiv = aktiv;
        m_anrufName = name;
        m_anrufPhase = phase;
        m_anrufAus = aus;
        m_anrufSek = sek;
        m_anrufStumm = stumm;
        emit anrufChanged();
    }
    const QString af = s.value(QLatin1String("audioError")).toString();
    if (!af.isEmpty())
        setzeFehler(af);
}

void Backend::anrufen(const QString &jid)
{
    if (jid.isEmpty())
        return;
    QUrl u = adresse(QLatin1String("/call/start"));
    u.addQueryItem(QLatin1String("jid"), jid);
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(anrufBefehlFertig()));
}

void Backend::anrufAnnehmen()  { connect(hole(QLatin1String("/call/accept")), SIGNAL(finished()), this, SLOT(anrufBefehlFertig())); }
void Backend::anrufAblehnen()  { connect(hole(QLatin1String("/call/reject")), SIGNAL(finished()), this, SLOT(anrufBefehlFertig())); }
void Backend::anrufAuflegen()  { connect(hole(QLatin1String("/call/hangup")), SIGNAL(finished()), this, SLOT(anrufBefehlFertig())); }

void Backend::anrufStummSchalten(bool an)
{
    QUrl u = adresse(QLatin1String("/call/mute"));
    u.addQueryItem(QLatin1String("on"), an ? QLatin1String("1") : QLatin1String("0"));
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(anrufBefehlFertig()));
}

void Backend::anrufLautsprecher(bool an)
{
    QUrl u = adresse(QLatin1String("/call/speaker"));
    u.addQueryItem(QLatin1String("on"), an ? QLatin1String("1") : QLatin1String("0"));
    QNetworkReply *r = m_netz->get(QNetworkRequest(u));
    connect(r, SIGNAL(finished()), this, SLOT(anrufBefehlFertig()));
}

// Alle Anrufbefehle antworten mit dem Anrufzustand -- die Antwort gleich
// auswerten spart eine Abfrage und laesst die Oberflaeche sofort reagieren.
void Backend::anrufBefehlFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    const QByteArray roh = r->readAll();
    if (r->error() != QNetworkReply::NoError) {
        setzeFehler(QString::fromUtf8(roh).trimmed());
        return;
    }
    const QVariantMap s = Json::parse(QString::fromUtf8(roh)).toMap();
    const QString fehler = s.value(QLatin1String("error")).toString();
    if (!fehler.isEmpty()) {
        setzeFehler(fehler);
        return;
    }
    setzeFehler(QString());
    anrufAbfragen();
}

void Backend::tonVorbereiten(const QString &pfad)
{
    if (pfad.isEmpty())
        return;
    // Die Musik-App spielt nur, was der Tracker indiziert hat -- eine frisch
    // heruntergeladene Sprachnachricht steht dort nicht und landete als
    // "nicht in der Wiedergabeliste". Und ein Sprachmemo gehoert ohnehin
    // nicht in die Musikbibliothek. Die App spielt es deshalb selbst.
    const bool opus = pfad.endsWith(QLatin1String(".ogg"), Qt::CaseInsensitive)
                   || pfad.endsWith(QLatin1String(".opus"), Qt::CaseInsensitive);
    if (!opus) {
        emit tonBereit(pfad);
        return;
    }
    const QString wav = pfad.left(pfad.lastIndexOf(QLatin1Char('.'))) + QLatin1String(".wav");
    if (QFile::exists(wav) && QFileInfo(wav).size() > 0) {
        emit tonBereit(wav);
        return;
    }
    const QString ffmpeg = QLatin1String("/opt/ffmpeg/bin/ffmpeg");
    if (!QFile::exists(ffmpeg)) {
        emit tonFehler(QString::fromUtf8("Sprachnachrichten brauchen ffmpeg (Paket ffmpeg-n9)."));
        return;
    }
    QProcess *wandler = new QProcess(this);
    wandler->setProperty("wav", wav);
    connect(wandler, SIGNAL(finished(int, QProcess::ExitStatus)),
            this, SLOT(tonUmgewandelt(int)));
    wandler->start(ffmpeg, QStringList()
                   << QLatin1String("-loglevel") << QLatin1String("error")
                   << QLatin1String("-y")
                   << QLatin1String("-i") << pfad
                   << QLatin1String("-ac") << QLatin1String("1")
                   << QLatin1String("-ar") << QLatin1String("44100")
                   << wav);
}

void Backend::tonUmgewandelt(int code)
{
    QProcess *p = qobject_cast<QProcess *>(sender());
    if (!p)
        return;
    const QString wav = p->property("wav").toString();
    const QByteArray meldung = p->readAllStandardError();
    p->deleteLater();
    if (code != 0 || !QFile::exists(wav)) {
        emit tonFehler(QString::fromUtf8("Umwandeln fehlgeschlagen: ")
                       + QString::fromUtf8(meldung).trimmed().left(120));
        return;
    }
    emit tonBereit(wav);
}

void Backend::abmelden()
{
    QNetworkReply *r = hole(QLatin1String("/logout"));
    connect(r, SIGNAL(finished()), this, SLOT(abmeldenFertig()));
}

void Backend::abmeldenFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError) {
        setzeFehler(QString::fromUtf8(r->readAll()).trimmed());
        return;
    }
    // Der lokale Stand ist weg; die Oberflaeche darf nicht auf alten Listen
    // sitzenbleiben, sonst zeigt sie Chats eines Kontos, das nicht mehr
    // verknuepft ist.
    m_chats.clear();
    m_nachrichten.clear();
    m_offenerChat.clear();
    m_gekoppelt = false;
    m_verbunden = false;
    m_code.clear();
    m_zustand = QLatin1String("waiting_for_pair");
    setzeFehler(QString());
    emit chatsChanged();
    emit nachrichtenChanged();
    emit statusChanged();
    abfragen();
}

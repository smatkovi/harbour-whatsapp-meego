#include "Backend.h"
#include "Json.h"

#include <QDateTime>
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
    , m_dienst(0)
    , m_takt(new QTimer(this))
    , m_binary(binary)
    , m_port(0)
    , m_seq(0)
    , m_gekoppelt(false)
    , m_verbunden(false)
{
    m_zustand = QLatin1String("starting");
    // Der Takt ist nur das Sicherheitsnetz; die eigentliche Aktualisierung
    // kommt aus /events. Ohne Netz bliebe die Oberflaeche stehen, wenn ein
    // Long-Poll einmal im Nichts endet.
    m_takt->setInterval(15000);
    connect(m_takt, SIGNAL(timeout()), this, SLOT(abfragen()));
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

void Backend::setzeFehler(const QString &text)
{
    if (m_fehler == text)
        return;
    m_fehler = text;
    emit fehlerChanged();
}

void Backend::starten()
{
    // Laeuft schon einer? Dann nur anklopfen. Das Backend bringt seinen
    // eigenen Mechanismus mit, um doppelte Instanzen zu vermeiden, aber ein
    // zweiter Start kostet auf diesem Geraet mehrere Sekunden.
    QNetworkReply *r = hole(QLatin1String("/status"));
    connect(r, SIGNAL(finished()), this, SLOT(statusFertig()));

    if (!m_dienst && QFile::exists(m_binary)) {
        m_dienst = new QProcess(this);
        m_dienst->setWorkingDirectory(QDir::homePath());
        QStringList umgebung = QProcess::systemEnvironment();
        m_dienst->setEnvironment(umgebung);
        m_dienst->start(m_binary, QStringList());
    }
    m_takt->start();
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
        // Long-Poll aufsetzen: haengt bis sich etwas tut.
        QNetworkReply *ev = hole(QString::fromLatin1("/events?since=%1").arg(m_seq));
        connect(ev, SIGNAL(finished()), this, SLOT(ereignisFertig()));
    }
}

void Backend::ereignisFertig()
{
    QNetworkReply *r = qobject_cast<QNetworkReply *>(sender());
    if (!r)
        return;
    r->deleteLater();
    if (r->error() != QNetworkReply::NoError)
        return;
    const QVariantMap e = Json::parse(QString::fromUtf8(r->readAll())).toMap();
    const qint64 seq = e.value(QLatin1String("seq")).toLongLong();
    if (seq != m_seq) {
        m_seq = seq;
        neuLaden();
    }
    // Sofort wieder aufsetzen -- so bleibt immer genau ein Poll offen.
    QNetworkReply *ev = hole(QString::fromLatin1("/events?since=%1").arg(m_seq));
    connect(ev, SIGNAL(finished()), this, SLOT(ereignisFertig()));
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
    QNetworkReply *r = hole(QLatin1String("/messages?jid=") + QString::fromLatin1(QUrl::toPercentEncoding(jid)));
    connect(r, SIGNAL(finished()), this, SLOT(nachrichtenFertig()));
    // Gelesen melden, damit der ungelesen-Zaehler auf beiden Geraeten stimmt.
    QNetworkReply *g = hole(QLatin1String("/chat/opened?jid=") + QString::fromLatin1(QUrl::toPercentEncoding(jid)));
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

void Backend::senden(const QString &jid, const QString &text)
{
    if (text.trimmed().isEmpty())
        return;
    const QString pfad = QLatin1String("/send?to=")
            + QString::fromLatin1(QUrl::toPercentEncoding(jid))
            + QLatin1String("&text=")
            + QString::fromLatin1(QUrl::toPercentEncoding(text));
    QNetworkReply *r = hole(pfad);
    connect(r, SIGNAL(finished()), this, SLOT(nachrichtenFertig()));
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

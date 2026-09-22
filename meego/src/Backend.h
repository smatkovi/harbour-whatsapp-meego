#ifndef BACKEND_H
#define BACKEND_H

// Die Verbindung der Oberflaeche zum Go-Backend.
//
// Das Backend ist derselbe Prozess wie auf Sailfish -- es spricht HTTP auf
// 127.0.0.1 und kennt /status, /chats, /messages, /send, /pair, /events.
// Diese Klasse startet es, findet seinen Port, fragt ab und reicht die
// Ergebnisse als QVariant an QML weiter.
//
// Neuigkeiten kommen ueber /events: ein Long-Poll, der bis zu 25 Sekunden
// offen bleibt und eine laufende Nummer zurueckgibt. Aendert sie sich, wird
// neu geladen. Das ist sparsamer als Pollen im Sekundentakt -- auf einem
// Geraet, das oft im 2G haengt, ist das kein Detail.

#include <QObject>
#include <QString>
#include <QStringList>
#include <QVariantList>
#include <QUrl>
#include <QVariantMap>

class QNetworkAccessManager;
class QNetworkReply;
class QProcess;
class QTimer;

class Backend : public QObject
{
    Q_OBJECT
    Q_PROPERTY(QString zustand READ zustand NOTIFY statusChanged)
    Q_PROPERTY(bool gekoppelt READ gekoppelt NOTIFY statusChanged)
    Q_PROPERTY(bool verbunden READ verbunden NOTIFY statusChanged)
    Q_PROPERTY(QString nummer READ nummer NOTIFY statusChanged)
    Q_PROPERTY(QString kopplungscode READ kopplungscode NOTIFY statusChanged)
    Q_PROPERTY(QString fehler READ fehler NOTIFY fehlerChanged)
    Q_PROPERTY(QVariantList chats READ chats NOTIFY chatsChanged)
    Q_PROPERTY(QVariantList nachrichten READ nachrichten NOTIFY nachrichtenChanged)
    Q_PROPERTY(QString offenerChat READ offenerChat NOTIFY nachrichtenChanged)
    Q_PROPERTY(bool anrufAktiv READ anrufAktiv NOTIFY anrufChanged)
    Q_PROPERTY(QString anrufName READ anrufName NOTIFY anrufChanged)
    Q_PROPERTY(QString anrufPhase READ anrufPhase NOTIFY anrufChanged)
    Q_PROPERTY(bool anrufAusgehend READ anrufAusgehend NOTIFY anrufChanged)
    Q_PROPERTY(int anrufSekunden READ anrufSekunden NOTIFY anrufChanged)
    Q_PROPERTY(bool anrufStumm READ anrufStumm NOTIFY anrufChanged)

public:
    explicit Backend(const QString &binary, QObject *parent = 0);
    ~Backend();

    QString zustand() const { return m_zustand; }
    bool gekoppelt() const { return m_gekoppelt; }
    bool verbunden() const { return m_verbunden; }
    QString nummer() const { return m_nummer; }
    QString kopplungscode() const { return m_code; }
    QString fehler() const { return m_fehler; }
    QVariantList chats() const { return m_chats; }
    QVariantList nachrichten() const { return m_nachrichten; }
    QString offenerChat() const { return m_offenerChat; }
    bool anrufAktiv() const { return m_anrufAktiv; }
    QString anrufName() const { return m_anrufName; }
    QString anrufPhase() const { return m_anrufPhase; }
    bool anrufAusgehend() const { return m_anrufAus; }
    int anrufSekunden() const { return m_anrufSek; }
    bool anrufStumm() const { return m_anrufStumm; }

    // Startet den Dienst, falls er nicht schon laeuft, und beginnt abzufragen.
    Q_INVOKABLE void starten();
    Q_INVOKABLE void koppeln(const QString &telefonnummer);
    Q_INVOKABLE void chatOeffnen(const QString &jid);
    Q_INVOKABLE void chatSchliessen();
    Q_INVOKABLE void senden(const QString &jid, const QString &text);
    Q_INVOKABLE void neuLaden();
    Q_INVOKABLE QString zeit(const QVariant &wert) const;
    // Holt den Anhang einer Nachricht aufs Geraet. Bilder und Dokumente
    // liegen erst nach dem Abruf lokal -- auf 2G will man das nicht
    // automatisch fuer jeden Verlauf tun.
    Q_INVOKABLE void medienLaden(const QString &nachrichtenId);
    Q_INVOKABLE void oeffnen(const QString &pfad);
    // Liefert ueber tonBereit() einen Pfad, den QtMultimediaKit
    // abspielen kann -- Opus wird vorher nach WAV gewandelt.
    Q_INVOKABLE void tonVorbereiten(const QString &pfad);
    Q_INVOKABLE QString groesse(const QVariant &bytes) const;
    // Trennt das Geraet von WhatsApp und raeumt den lokalen Stand.
    Q_INVOKABLE void abmelden();
    Q_INVOKABLE void anrufen(const QString &jid);
    Q_INVOKABLE void anrufAnnehmen();
    Q_INVOKABLE void anrufAblehnen();
    Q_INVOKABLE void anrufAuflegen();
    Q_INVOKABLE void anrufStummSchalten(bool an);
    Q_INVOKABLE void anrufLautsprecher(bool an);
    // Dateiwaehler: Harmattan bringt keinen mit, den eine fremde App
    // aufrufen koennte, also listet die App selbst.
    Q_INVOKABLE QString startVerzeichnis() const;
    Q_INVOKABLE QVariantList verzeichnis(const QString &pfad) const;
    Q_INVOKABLE void anhangSenden(const QString &jid, const QString &pfad,
                                  const QString &beschriftung);

signals:
    void statusChanged();
    void chatsChanged();
    void nachrichtenChanged();
    void fehlerChanged();
    void anrufChanged();
    void tonBereit(const QString &pfad);
    void tonFehler(const QString &text);

private slots:
    void statusFertig();
    void chatsFertig();
    void nachrichtenFertig();
    void sendenFertig();
    void medienFertig();
    void anrufZustandFertig();
    void anrufBefehlFertig();
    void abmeldenFertig();
    void umwandlungFertig(int code);
    void tonUmgewandelt(int code);
    void anhangFertig();
    void ereignisFertig();
    void kopplungFertig();
    void abfragen();

private:
    QNetworkReply *hole(const QString &pfad);
    // Basisadresse; Parameter gehoeren per addQueryItem daran,
    // nicht von Hand kodiert in die Zeichenkette.
    QUrl adresse(const QString &pfad);
    void ereignisPoll();
    void anrufAbfragen();
    // Kopiert einen Anhang nach MyDocs/Downloads und gibt den
    // dortigen Pfad zurueck -- nur dort findet ihn das Geraet.
    QString nachMyDocs(const QString &pfad);
    void starteBetrachter(const QString &pfad);
    void setzeFehler(const QString &text);
    int port();

    QNetworkAccessManager *m_netz;
    // Genau ein Long-Poll darf offen sein, siehe ereignisPoll().
    QNetworkReply *m_ereignis;
    QProcess *m_dienst;
    QTimer *m_takt;
    QString m_binary;
    int m_port;
    qint64 m_seq;

    QString m_zustand;
    bool m_gekoppelt;
    bool m_verbunden;
    QString m_nummer;
    QString m_code;
    QString m_fehler;
    QVariantList m_chats;
    QVariantList m_nachrichten;
    QString m_offenerChat;

    QTimer *m_anrufTakt;
    QNetworkReply *m_anrufAbfrage;   // hoechstens eine offen
    bool m_anrufAktiv;
    QString m_anrufName;
    QString m_anrufPhase;
    bool m_anrufAus;
    int m_anrufSek;
    bool m_anrufStumm;
};

#endif

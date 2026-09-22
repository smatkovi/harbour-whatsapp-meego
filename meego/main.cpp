// WhatsApp fuer MeeGo Harmattan (Nokia N9/N950).
//
// Die Oberflaeche ist neu geschrieben und nicht aus dem Sailfish-Zweig
// portiert: dort sind es 9654 Zeilen Silica-QML, und Silica gibt es auf
// Harmattan nicht. Was beide teilen, ist das Go-Backend -- es spricht HTTP
// auf 127.0.0.1, und genau daran haengt diese App.

#include <QApplication>
#include <QDeclarativeContext>
#include <QDeclarativeEngine>
#include <QDeclarativeView>
#include <QDir>
#include <QFile>
#include <QInputContext>
#include <QInputContextFactory>
#include <QFileInfo>

#include "src/Backend.h"

// Harmattan schreibt die Adresse des Sitzungsbusses hierhin. Ein per ssh
// oder aus einem Dienst gestarteter Prozess erbt sie nicht -- und ohne sie
// scheitert alles, was ueber den Bus laeuft. Sichtbar wurde das beim
// Abspielen einer Sprachnachricht: xdg-open reicht die Datei ueber
// libcontentaction an die Musik-App weiter und meldete nur
// "Not connected to D-Bus server", ohne dass etwas passierte.
static void sitzungsBusSetzen()
{
    if (!qgetenv("DBUS_SESSION_BUS_ADDRESS").isEmpty())
        return;
    QFile f(QLatin1String("/tmp/session_bus_address.user"));
    if (!f.open(QIODevice::ReadOnly))
        return;
    const QStringList zeilen = QString::fromLatin1(f.readAll()).split(QLatin1Char('\n'));
    for (int i = 0; i < zeilen.size(); ++i) {
        const QString z = zeilen.at(i);
        const int p = z.indexOf(QLatin1String("DBUS_SESSION_BUS_ADDRESS="));
        if (p < 0)
            continue;
        QString wert = z.mid(p + 25).trimmed();
        if (wert.endsWith(QLatin1Char(';')))
            wert.chop(1);
        if (wert.length() > 1 && (wert.at(0) == QLatin1Char('"') || wert.at(0) == QLatin1Char('\''))
                && wert.at(wert.length() - 1) == wert.at(0)) {
            wert = wert.mid(1, wert.length() - 2);
        }
        if (!wert.isEmpty())
            qputenv("DBUS_SESSION_BUS_ADDRESS", wert.toLatin1());
        return;
    }
}

int main(int argc, char *argv[])
{
    sitzungsBusSetzen();
    QApplication app(argc, argv);

    // Ohne das bleibt die virtuelle Tastatur weg, sobald die ausziehbare
    // eingeklappt ist: Qt waehlt dann gar keinen Eingabekontext, und ein
    // TextField bekommt zwar den Fokus, aber nichts erscheint. Harmattans
    // Tastatur haengt an MInputContext, und die Standardapps setzen ihn
    // ueber ihre Bibliotheken -- eine nackte QApplication tut das nicht.
    if (QInputContext *ic = QInputContextFactory::create(
            QLatin1String("MInputContext"), &app)) {
        app.setInputContext(ic);
    }

    app.setApplicationName(QLatin1String("harbour-whatsapp"));
    app.setOrganizationName(QLatin1String("harbour-whatsapp"));

    // Alles liegt relativ zum Programm, damit sich das Paket verschieben
    // laesst, ohne dass Pfade nachgezogen werden muessen.
    const QString wurzel = QFileInfo(QCoreApplication::applicationFilePath())
            .absolutePath() + QLatin1String("/..");

    Backend backend(QDir(wurzel).absoluteFilePath(QLatin1String("bin/wa-backend")));

    QDeclarativeView view;
    view.engine()->rootContext()->setContextProperty(QLatin1String("Dienst"), &backend);
    view.setResizeMode(QDeclarativeView::SizeRootObjectToView);
    view.setSource(QUrl::fromLocalFile(
        QDir(wurzel).absoluteFilePath(QLatin1String("qml/main.qml"))));
    view.showFullScreen();

    backend.starten();
    return app.exec();
}

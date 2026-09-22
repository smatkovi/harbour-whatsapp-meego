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
#include <QFileInfo>

#include "src/Backend.h"

int main(int argc, char *argv[])
{
    QApplication app(argc, argv);
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

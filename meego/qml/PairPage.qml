import QtQuick 1.1
import com.nokia.meego 1.0

// Anmeldung per Telefonnummer. Den QR-Weg gibt es hier nicht: das N9 hat
// keine Kamera-Anbindung dafuer, und der Code laesst sich ohnehin bequemer
// tippen als ein Bildschirm abfotografieren.
Page {
    id: seite

    // Durchgaengig schwarz wie die uebrigen Seiten. Ohne das zeichnet
    // PageStackWindow seinen Themenhintergrund, und die Kopplungsseite
    // stach als einzige hell heraus.
    Rectangle { anchors.fill: parent; color: "#000000" }

    Flickable {
        anchors.fill: parent
        anchors.margins: 16
        contentHeight: inhalt.height

        Column {
            id: inhalt
            width: parent.width
            spacing: 20

            Item { width: 1; height: 8 }

            Label {
                width: parent.width
                text: "WhatsApp verknüpfen"
                font.pixelSize: 32
                font.bold: true
            }

            Label {
                width: parent.width
                wrapMode: Text.WordWrap
                color: "#b0b0b0"
                text: Dienst.verbunden
                      ? "Nummer mit Landesvorwahl eingeben, ohne Plus — also etwa 43 statt +43."
                      : "Warte auf die Verbindung zu WhatsApp …"
            }

            TextField {
                id: nummernFeld
                width: parent.width
                placeholderText: "43…"
                inputMethodHints: Qt.ImhDialableCharactersOnly
                // Tippen muss immer gehen. Frueher haing das Feld an
                // Dienst.verbunden -- nach einem Trennen ist die Verbindung
                // aber weg, und dann liess sich die Nummer nicht mehr
                // eingeben, mit der man sich gerade neu verknuepfen will.
                // Auf die Verbindung wartet nur der Knopf.
                enabled: Dienst.kopplungscode === ""
            }

            Button {
                width: parent.width
                text: "Code anfordern"
                // Nicht an Dienst.verbunden haengen: nach einem Trennen ist
                // die Verbindung weg, und der Knopf blieb tot. Das Backend
                // wartet beim /pair ohnehin bis zu 15 Sekunden auf sie und
                // meldet sonst einen Fehler -- besser eine Meldung als ein
                // Knopf, den man nicht druecken kann.
                enabled: nummernFeld.text.length > 5
                         && Dienst.kopplungscode === ""
                onClicked: Dienst.koppeln(nummernFeld.text)
            }

            // Der Code ist das Herzstueck dieser Seite -- entsprechend gross.
            Rectangle {
                width: parent.width
                height: codeSpalte.height + 32
                visible: Dienst.kopplungscode !== ""
                color: "#1a3d1a"
                radius: 8

                Column {
                    id: codeSpalte
                    anchors.centerIn: parent
                    width: parent.width - 32
                    spacing: 10

                    Label {
                        width: parent.width
                        horizontalAlignment: Text.AlignHCenter
                        text: Dienst.kopplungscode
                        font.pixelSize: 44
                        font.bold: true
                        font.family: "Nokia Pure Text Light"
                    }
                    Label {
                        width: parent.width
                        horizontalAlignment: Text.AlignHCenter
                        wrapMode: Text.WordWrap
                        color: "#c0e0c0"
                        font.pixelSize: 20
                        text: "Am Haupttelefon: Einstellungen → Verknüpfte Geräte → "
                              + "Gerät hinzufügen → „Stattdessen mit Telefonnummer "
                              + "verknüpfen“, dann diesen Code eingeben."
                    }
                }
            }

            Label {
                width: parent.width
                wrapMode: Text.WordWrap
                color: "#ff6060"
                visible: Dienst.fehler !== ""
                text: Dienst.fehler
            }

            Label {
                width: parent.width
                color: "#808080"
                font.pixelSize: 18
                text: "Zustand: " + Dienst.zustand
            }
        }
    }
}

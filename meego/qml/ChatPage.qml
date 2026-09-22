import QtQuick 1.1
import com.nokia.meego 1.0

Page {
    id: seite
    property string titel: ""
    property string jid: ""

    tools: ToolBarLayout {
        ToolIcon {
            platformIconId: "toolbar-back"
            onClicked: { Dienst.chatSchliessen(); pageStack.pop() }
        }
        Label {
            text: seite.titel
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pixelSize: 24
            width: parent.width - 140
            anchors.verticalCenter: parent.verticalCenter
        }
    }

    Rectangle { anchors.fill: parent; color: "#000000" }

    ListView {
        id: verlauf
        anchors.top: parent.top
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: eingabe.top
        anchors.margins: 8
        clip: true
        model: Dienst.nachrichten
        spacing: 6
        // Der Verlauf kommt in zeitlicher Reihenfolge; die neueste Nachricht
        // gehoert unten und sichtbar.
        onCountChanged: positionViewAtEnd()
        Component.onCompleted: positionViewAtEnd()

        // Breiteste zulaessige Blase. Einmal hier statt in jedem Eintrag.
        property real maxBlase: width * 0.82

        delegate: Item {
            width: verlauf.width
            height: blase.height + 4

            Rectangle {
                id: blase
                anchors.right: modelData.fromMe ? parent.right : undefined
                anchors.left: modelData.fromMe ? undefined : parent.left
                width: spalte.width + 24
                height: spalte.height + 16
                radius: 10
                color: modelData.fromMe ? "#1f4d2e" : "#1c1c1c"

                // Der Messtext haengt an nichts und wird von nichts gelesen
                // ausser seiner eigenen Breite. Frueher bestimmte die Blase
                // ihre Breite aus paintedWidth des umbrechenden Textes -- und
                // dessen Breite kam von der Blase. Diese Schleife liess die
                // Blasen ohne brauchbare Groesse, der Verlauf blieb leer.
                Text {
                    id: messer
                    visible: false
                    text: inhalt.text
                    font.pixelSize: inhalt.font.pixelSize
                }

                Column {
                    id: spalte
                    x: 12
                    y: 8
                    width: Math.max(60, Math.min(verlauf.maxBlase - 24,
                                                 messer.paintedWidth))
                    spacing: 2

                    // In Gruppen ist ohne Absender nicht zu erkennen, wer
                    // spricht; im Einzelchat waere es nur Laerm.
                    Label {
                        width: parent.width
                        visible: !modelData.fromMe && modelData.sender !== undefined
                                 && modelData.sender !== ""
                        height: visible ? implicitHeight : 0
                        text: modelData.sender || ""
                        color: "#6aa6d6"
                        font.pixelSize: 17
                        font.bold: true
                        elide: Text.ElideRight
                        maximumLineCount: 1
                    }
                    Label {
                        id: inhalt
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: modelData.text || ""
                        font.pixelSize: 22
                    }
                    Label {
                        width: parent.width
                        horizontalAlignment: Text.AlignRight
                        color: "#808080"
                        font.pixelSize: 15
                        text: Dienst.zeit(modelData.timestamp)
                    }
                }
            }
        }

        Label {
            anchors.centerIn: parent
            visible: verlauf.count === 0
            color: "#707070"
            text: "Keine Nachrichten"
        }
    }

    ScrollDecorator { flickableItem: verlauf }

    // Ein Fehler beim Senden darf nicht wieder unsichtbar bleiben.
    Rectangle {
        anchors.bottom: eingabe.top
        anchors.left: parent.left
        anchors.right: parent.right
        height: meldung.paintedHeight + 12
        visible: Dienst.fehler !== ""
        color: "#5a1a1a"
        Label {
            id: meldung
            anchors.centerIn: parent
            width: parent.width - 16
            wrapMode: Text.Wrap
            font.pixelSize: 17
            text: Dienst.fehler
        }
    }

    Item {
        id: eingabe
        anchors.bottom: parent.bottom
        anchors.left: parent.left
        anchors.right: parent.right
        height: 72

        Rectangle { anchors.fill: parent; color: "#101010" }

        TextField {
            id: feld
            anchors.left: parent.left
            anchors.leftMargin: 8
            anchors.right: sendeKnopf.left
            anchors.rightMargin: 8
            anchors.verticalCenter: parent.verticalCenter
            placeholderText: "Nachricht"
            onAccepted: sendeKnopf.abschicken()
        }

        Button {
            id: sendeKnopf
            anchors.right: parent.right
            anchors.rightMargin: 8
            anchors.verticalCenter: parent.verticalCenter
            width: 100
            text: "Senden"
            enabled: feld.text.length > 0

            function abschicken() {
                if (feld.text.length === 0)
                    return
                Dienst.senden(seite.jid, feld.text)
                feld.text = ""
            }
            onClicked: abschicken()
        }
    }
}

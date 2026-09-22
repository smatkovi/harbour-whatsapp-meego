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
            font.pixelSize: 24
            width: parent.width - 140
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

        delegate: Item {
            width: verlauf.width
            height: blase.height + 4

            Rectangle {
                id: blase
                width: Math.min(verlauf.width * 0.82, textteil.paintedWidth + 24)
                height: spalte.height + 16
                radius: 10
                anchors.right: modelData.fromMe ? parent.right : undefined
                anchors.left: modelData.fromMe ? undefined : parent.left
                color: modelData.fromMe ? "#1f4d2e" : "#1c1c1c"

                Column {
                    id: spalte
                    anchors.centerIn: parent
                    width: parent.width - 24
                    spacing: 2

                    // In Gruppen ist ohne Absender nicht zu erkennen, wer
                    // spricht; im Einzelchat waere es nur Laerm.
                    Label {
                        width: parent.width
                        visible: !modelData.fromMe && modelData.sender !== undefined
                                 && modelData.sender !== ""
                        text: modelData.sender || ""
                        color: "#6aa6d6"
                        font.pixelSize: 17
                        font.bold: true
                    }
                    Label {
                        id: textteil
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: modelData.text || ""
                        font.pixelSize: 22
                    }
                    Label {
                        anchors.right: parent.right
                        color: "#808080"
                        font.pixelSize: 15
                        text: Dienst.zeit(modelData.timestamp)
                    }
                }
            }
        }
    }

    ScrollDecorator { flickableItem: verlauf }

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

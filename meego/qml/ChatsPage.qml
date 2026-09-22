import QtQuick 1.1
import com.nokia.meego 1.0

Page {
    id: seite

    tools: ToolBarLayout {
        ToolIcon {
            platformIconId: "toolbar-refresh"
            onClicked: Dienst.neuLaden()
        }
        ToolIcon {
            platformIconId: "toolbar-view-menu"
            onClicked: menue.open()
        }
    }

    Menu {
        id: menue
        MenuLayout {
            MenuItem {
                text: "Zustand: " + Dienst.zustand
                      + (Dienst.nummer !== "" ? " · " + Dienst.nummer : "")
            }
            MenuItem {
                text: "Gerät trennen"
                onClicked: trennenFrage.open()
            }
        }
    }

    QueryDialog {
        id: trennenFrage
        titleText: "Gerät trennen"
        message: "Die Verknüpfung mit WhatsApp wird aufgehoben und der "
                 + "lokale Verlauf gelöscht. Deine Nachrichten auf dem "
                 + "Haupttelefon bleiben unberührt. Zum Weiterbenutzen "
                 + "musst du danach neu verknüpfen."
        acceptButtonText: "Trennen"
        rejectButtonText: "Abbrechen"
        onAccepted: Dienst.abmelden()
    }

    Rectangle {
        anchors.fill: parent
        color: "#000000"
    }

    ListView {
        id: liste
        anchors.fill: parent
        model: Dienst.chats
        clip: true

        header: Item {
            width: liste.width
            height: kopf.height + 16
            Column {
                id: kopf
                width: parent.width - 32
                x: 16
                y: 12
                spacing: 2
                Label {
                    text: "Chats"
                    font.pixelSize: 30
                    font.bold: true
                }
                Label {
                    color: Dienst.verbunden ? "#60c060" : "#c0a060"
                    font.pixelSize: 18
                    text: Dienst.verbunden ? "verbunden" : Dienst.zustand
                }
            }
        }

        delegate: Item {
            width: liste.width
            height: 88
            clip: true

            Rectangle {
                anchors.fill: parent
                color: mausbereich.pressed ? "#202020" : "transparent"
            }

            // Avatar oder Ersatzkreis mit dem Anfangsbuchstaben. Das Backend
            // legt geladene Bilder als Datei ab; fehlt eines, ist der Kreis
            // besser als ein Loch.
            Rectangle {
                id: bildchen
                x: 12
                anchors.verticalCenter: parent.verticalCenter
                width: 64; height: 64; radius: 32
                color: "#2a4d6a"
                Image {
                    anchors.fill: parent
                    source: modelData.avatar ? "file://" + modelData.avatar : ""
                    visible: status === Image.Ready
                    fillMode: Image.PreserveAspectCrop
                    smooth: true
                }
                Label {
                    anchors.centerIn: parent
                    visible: !modelData.avatar
                    text: (modelData.name || "?").substring(0, 1).toUpperCase()
                    font.pixelSize: 28
                }
            }

            Column {
                anchors.left: bildchen.right
                anchors.leftMargin: 12
                anchors.right: rechts.left
                anchors.rightMargin: 8
                anchors.verticalCenter: parent.verticalCenter
                spacing: 2

                Label {
                    width: parent.width
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    text: modelData.name || modelData.jid
                    font.pixelSize: 24
                    font.bold: modelData.unread > 0
                }
                Label {
                    width: parent.width
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    color: "#909090"
                    font.pixelSize: 19
                    text: (modelData.fromMe ? "Du: " : "")
                          + (modelData.lastMessage || "").replace(/\s+/g, " ")
                }
            }

            Column {
                id: rechts
                anchors.right: parent.right
                anchors.rightMargin: 12
                anchors.verticalCenter: parent.verticalCenter
                spacing: 4

                Label {
                    anchors.right: parent.right
                    color: "#808080"
                    font.pixelSize: 17
                    text: Dienst.zeit(modelData.lastTime)
                }
                Rectangle {
                    anchors.right: parent.right
                    visible: modelData.unread > 0
                    width: Math.max(24, zaehler.width + 12); height: 24
                    radius: 12
                    color: "#25d366"
                    Label {
                        id: zaehler
                        anchors.centerIn: parent
                        text: modelData.unread ? modelData.unread : ""
                        color: "#000000"
                        font.pixelSize: 16
                        font.bold: true
                    }
                }
            }

            Rectangle {
                anchors.bottom: parent.bottom
                width: parent.width; height: 1
                color: "#1c1c1c"
            }

            MouseArea {
                id: mausbereich
                anchors.fill: parent
                onClicked: {
                    Dienst.chatOeffnen(modelData.jid)
                    pageStack.push(Qt.resolvedUrl("ChatPage.qml"),
                                   { titel: modelData.name || modelData.jid,
                                     jid: modelData.jid })
                }
            }
        }

        Label {
            anchors.centerIn: parent
            visible: liste.count === 0
            color: "#707070"
            text: Dienst.gekoppelt ? "Noch keine Chats geladen" : "Nicht verknüpft"
        }
    }

    ScrollDecorator { flickableItem: liste }
}

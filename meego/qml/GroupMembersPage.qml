import QtQuick 1.1
import com.nokia.meego 1.0

// Die Mitglieder einer Gruppe, mit dem Weg in den Einzelchat.
//
// Antippen oeffnet den Chat mit der Person -- auch mit jemandem, der noch in
// keiner Chatliste steht. Damit ist das zugleich der bequemste Weg, jemanden
// aus einer Gruppe zum ersten Mal anzuschreiben.
Page {
    id: seite
    property string titel: ""
    property string jid: ""

    tools: ToolBarLayout {
        ToolIcon {
            platformIconId: "toolbar-back"
            onClicked: pageStack.pop()
        }
        Label {
            text: Dienst.laedtGruppe
                  ? "Mitglieder …"
                  : Dienst.mitglieder.length + " Mitglieder"
            font.pixelSize: 24
            width: parent.width - 140
            elide: Text.ElideRight
            anchors.verticalCenter: parent.verticalCenter
        }
    }

    Rectangle { anchors.fill: parent; color: "#000000" }

    ListView {
        id: liste
        anchors.fill: parent
        anchors.margins: 4
        clip: true
        model: Dienst.mitglieder
        cacheBuffer: 400

        delegate: Item {
            width: liste.width
            height: 76

            Rectangle {
                id: bildchen
                x: 10
                anchors.verticalCenter: parent.verticalCenter
                width: 56; height: 56; radius: 28
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
                    font.pixelSize: 24
                }
            }

            Column {
                anchors.left: bildchen.right
                anchors.leftMargin: 12
                anchors.right: abzeichen.left
                anchors.rightMargin: 8
                anchors.verticalCenter: parent.verticalCenter
                spacing: 2

                Label {
                    width: parent.width
                    text: modelData.name
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pixelSize: 24
                }
                Label {
                    width: parent.width
                    // Die Nummer nur dann, wenn sie nicht schon oben steht.
                    visible: modelData.name !== modelData.number
                    text: "+" + modelData.number
                    color: "#808080"
                    font.pixelSize: 18
                }
            }

            Label {
                id: abzeichen
                anchors.right: parent.right
                anchors.rightMargin: 10
                anchors.verticalCenter: parent.verticalCenter
                visible: modelData.isAdmin === true
                text: "Admin"
                color: "#5fa85f"
                font.pixelSize: 18
            }

            Rectangle {
                anchors.bottom: parent.bottom
                anchors.left: parent.left
                anchors.right: parent.right
                height: 1
                color: "#1a1a1a"
            }

            MouseArea {
                anchors.fill: parent
                onClicked: {
                    Dienst.chatOeffnen(modelData.number)
                    pageStack.push(Qt.resolvedUrl("ChatPage.qml"),
                                   { titel: modelData.name,
                                     jid: modelData.number })
                }
            }
        }
    }

    Label {
        anchors.centerIn: parent
        color: "#707070"
        visible: !Dienst.laedtGruppe && Dienst.mitglieder.length === 0
        text: "Keine Mitglieder geladen"
    }

    BusyIndicator {
        anchors.centerIn: parent
        running: Dienst.laedtGruppe
        visible: Dienst.laedtGruppe
        platformStyle: BusyIndicatorStyle { size: "large" }
    }

    Component.onCompleted: Dienst.gruppeLaden(seite.jid)
}

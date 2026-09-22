import QtQuick 1.1
import com.nokia.meego 1.0

// Dateiwaehler. Harmattan bringt keinen mit, den eine fremde App aufrufen
// koennte, also listet die App selbst -- ausgehend von MyDocs, der Partition,
// die auch der Dateimanager und der Rechner am USB-Kabel sehen.
Page {
    id: seite
    property string jid: ""
    property string ordner: Dienst.startVerzeichnis()

    tools: ToolBarLayout {
        ToolIcon {
            platformIconId: "toolbar-back"
            onClicked: pageStack.pop()
        }
        Label {
            text: seite.ordner.split("/").pop() || "MyDocs"
            elide: Text.ElideLeft
            maximumLineCount: 1
            font.pixelSize: 22
            width: parent.width - 140
            anchors.verticalCenter: parent.verticalCenter
        }
    }

    Rectangle { anchors.fill: parent; color: "#000000" }

    ListView {
        id: liste
        anchors.fill: parent
        clip: true
        model: Dienst.verzeichnis(seite.ordner)

        delegate: Item {
            width: liste.width
            height: 72
            clip: true

            Rectangle {
                anchors.fill: parent
                color: bereich.pressed ? "#202020" : "transparent"
            }

            Rectangle {
                id: marke
                x: 12
                anchors.verticalCenter: parent.verticalCenter
                width: 44; height: 44; radius: 4
                color: modelData.istOrdner ? "#4a4a2a" : "#3a5a7a"
                Label {
                    anchors.centerIn: parent
                    font.pixelSize: 14
                    font.bold: true
                    text: {
                        if (modelData.istOrdner)
                            return modelData.name === ".." ? "↑" : "DIR"
                        var n = modelData.name
                        var i = n.lastIndexOf(".")
                        return (i > 0 && n.length - i <= 5)
                                ? n.substring(i + 1).toUpperCase() : "?"
                    }
                }
            }

            Column {
                anchors.left: marke.right
                anchors.leftMargin: 12
                anchors.right: parent.right
                anchors.rightMargin: 12
                anchors.verticalCenter: parent.verticalCenter
                spacing: 2
                Label {
                    width: parent.width
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    text: modelData.name
                    font.pixelSize: 21
                }
                Label {
                    visible: !modelData.istOrdner
                    height: visible ? implicitHeight : 0
                    color: "#909090"
                    font.pixelSize: 16
                    text: Dienst.groesse(modelData.bytes)
                }
            }

            Rectangle {
                anchors.bottom: parent.bottom
                width: parent.width; height: 1
                color: "#1c1c1c"
            }

            MouseArea {
                id: bereich
                anchors.fill: parent
                onClicked: {
                    if (modelData.istOrdner) {
                        seite.ordner = modelData.pfad
                        liste.model = Dienst.verzeichnis(seite.ordner)
                        liste.positionViewAtBeginning()
                    } else {
                        Dienst.anhangSenden(seite.jid, modelData.pfad, "")
                        pageStack.pop()
                    }
                }
            }
        }

        Label {
            anchors.centerIn: parent
            visible: liste.count === 0
            color: "#707070"
            text: "Ordner ist leer"
        }
    }

    ScrollDecorator { flickableItem: liste }
}

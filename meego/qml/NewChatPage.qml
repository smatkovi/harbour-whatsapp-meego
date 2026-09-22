import QtQuick 1.1
import com.nokia.meego 1.0

// Drei Wege zu einem neuen Gespraech, auf einer Seite:
// jemand aus dem Adressbuch, eine Nummer, die noch niemand hier kennt,
// und der Einladungslink einer Gruppe.
//
// Eine Seite statt dreier, weil es dieselbe Absicht ist -- und weil ein
// Geraet mit diesem Bildschirm jede Navigationsebene spuert.
Page {
    id: seite

    tools: ToolBarLayout {
        ToolIcon {
            platformIconId: "toolbar-back"
            onClicked: pageStack.pop()
        }
        Label {
            text: "Neuer Chat"
            font.pixelSize: 24
            anchors.verticalCenter: parent.verticalCenter
        }
    }

    Rectangle { anchors.fill: parent; color: "#000000" }

    function chatOeffnen(jid, name) {
        Dienst.chatOeffnen(jid)
        pageStack.pop()
        pageStack.push(Qt.resolvedUrl("ChatPage.qml"),
                       { titel: name || jid, jid: jid })
    }

    Connections {
        target: Dienst
        onBeigetreten: seite.chatOeffnen(jid, "")
    }

    ListView {
        id: liste
        anchors.fill: parent
        anchors.margins: 6
        clip: true
        model: Dienst.kontakte
        cacheBuffer: 400

        header: Column {
            width: liste.width
            spacing: 10

            Item { width: 1; height: 6 }

            // --- Nummer ---------------------------------------------
            Label {
                text: "Neue Nummer"
                color: "#909090"
                font.pixelSize: 20
            }
            Row {
                width: parent.width
                spacing: 8
                TextField {
                    id: nummernFeld
                    width: parent.width - schreibKnopf.width - 8
                    placeholderText: "43664…  (mit Vorwahl, ohne +)"
                    inputMethodHints: Qt.ImhDialableCharactersOnly
                }
                Button {
                    id: schreibKnopf
                    width: 140
                    text: "Schreiben"
                    enabled: Dienst.nummerNormalisieren(nummernFeld.text).length > 7
                    onClicked: {
                        var n = Dienst.nummerNormalisieren(nummernFeld.text)
                        seite.chatOeffnen(n, "+" + n)
                    }
                }
            }
            Label {
                width: parent.width
                wrapMode: Text.WordWrap
                color: "#707070"
                font.pixelSize: 17
                text: "Ob die Nummer bei WhatsApp ist, zeigt sich erst beim "
                      + "Senden — vorher lässt sich das nicht sagen."
            }

            Item { width: 1; height: 4 }

            // --- Gruppenlink ----------------------------------------
            Label {
                text: "Gruppe per Einladungslink"
                color: "#909090"
                font.pixelSize: 20
            }
            Row {
                width: parent.width
                spacing: 8
                TextField {
                    id: linkFeld
                    width: parent.width - beitrittKnopf.width - 8
                    placeholderText: "chat.whatsapp.com/…"
                }
                Button {
                    id: beitrittKnopf
                    width: 140
                    text: Dienst.tritteBei ? "…" : "Beitreten"
                    enabled: !Dienst.tritteBei && linkFeld.text.length > 8
                    onClicked: Dienst.gruppeBeitreten(linkFeld.text)
                }
            }
            Label {
                width: parent.width
                visible: Dienst.beitrittHinweis !== ""
                wrapMode: Text.WordWrap
                font.pixelSize: 18
                color: Dienst.beitrittHinweis === "Beigetreten"
                       ? "#5fa85f" : "#ff6060"
                text: Dienst.beitrittHinweis
            }

            Item { width: 1; height: 6 }

            // --- Adressbuch -----------------------------------------
            Label {
                text: "Kontakte (" + Dienst.kontakte.length + ")"
                color: "#909090"
                font.pixelSize: 20
            }
            TextField {
                id: suchFeld
                width: parent.width
                placeholderText: "Suchen"
            }
            Item { width: 1; height: 4 }
        }

        delegate: Item {
            width: liste.width
            // Die Suche blendet Eintraege aus, statt das Modell neu zu
            // bauen: bei ein paar hundert Kontakten ist das auf diesem
            // Geraet spuerbar schneller und flackert nicht.
            property bool passt: suchFeld.text === ""
                || modelData.name.toLowerCase().indexOf(
                       suchFeld.text.toLowerCase()) >= 0
                || modelData.jid.indexOf(suchFeld.text) >= 0
            height: passt ? 72 : 0
            visible: passt
            clip: true

            Rectangle {
                id: bildchen
                x: 8
                anchors.verticalCenter: parent.verticalCenter
                width: 54; height: 54; radius: 27
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
                anchors.right: parent.right
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
                    text: "+" + modelData.jid
                    color: "#808080"
                    font.pixelSize: 17
                }
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
                onClicked: seite.chatOeffnen(modelData.jid, modelData.name)
            }
        }
    }

    Component.onCompleted: Dienst.kontakteLaden()
}

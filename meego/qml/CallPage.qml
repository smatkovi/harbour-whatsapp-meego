import QtQuick 1.1
import com.nokia.meego 1.0

// Anrufseite fuer ausgehende Anrufe.
//
// Eingehende laufen nicht hierher: die klingeln ueber die SIP-Bruecke in
// Harmattans eigener Anrufansicht, samt Sperrbildschirm. Diese Seite ist
// fuer den Fall, dass man selbst waehlt -- und als Rueckfall, wenn kein
// SIP-Konto eingerichtet ist.
Page {
    id: seite
    property string titel: ""

    tools: ToolBarLayout {
        ToolIcon {
            platformIconId: "toolbar-back"
            onClicked: pageStack.pop()
        }
    }

    Rectangle { anchors.fill: parent; color: "#0a0a0a" }

    Column {
        anchors.centerIn: parent
        width: parent.width - 60
        spacing: 16

        Label {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            elide: Text.ElideRight
            maximumLineCount: 1
            text: Dienst.anrufName !== "" ? Dienst.anrufName : seite.titel
            font.pixelSize: 32
        }

        Label {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            color: "#909090"
            font.pixelSize: 22
            text: {
                if (!Dienst.anrufAktiv)
                    return "beendet"
                switch (Dienst.anrufPhase) {
                case "calling":    return "wählt …"
                case "ringing":    return "klingelt …"
                case "connecting": return "verbindet …"
                case "active":     return dauer(Dienst.anrufSekunden)
                default:           return Dienst.anrufPhase
                }
            }
        }
    }

    function dauer(s) {
        var m = Math.floor(s / 60)
        var r = s % 60
        return m + ":" + (r < 10 ? "0" + r : r)
    }

    Row {
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 40
        spacing: 16

        Button {
            width: 130
            text: Dienst.anrufStumm ? "Laut" : "Stumm"
            enabled: Dienst.anrufAktiv
            onClicked: Dienst.anrufStummSchalten(!Dienst.anrufStumm)
        }
        Button {
            width: 130
            text: "Auflegen"
            onClicked: { Dienst.anrufAuflegen(); pageStack.pop() }
        }
    }

    // Legt die Gegenseite auf, schliesst sich die Seite von selbst -- sonst
    // bliebe eine Anrufansicht ohne Anruf stehen.
    Connections {
        target: Dienst
        onAnrufChanged: {
            if (!Dienst.anrufAktiv && pageStack.currentPage === seite)
                pageStack.pop()
        }
    }
}

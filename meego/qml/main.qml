import QtQuick 1.1
import com.nokia.meego 1.0

PageStackWindow {
    id: app
    showStatusBar: true
    showToolBar: true

    // Harmattans Standardthema ist hell: ein Label ohne eigene Farbe zeichnet
    // dunkel. Auf dem schwarzen Hintergrund dieser App waren damit
    // ausgerechnet die Namen fast unsichtbar, waehrend die grau gesetzte
    // Vorschau darunter heller wirkte als sie. Das dunkle Thema stellt die
    // Standardfarben richtig -- besser als in jedem Label einzeln
    // nachzufaerben.
    Component.onCompleted: theme.inverted = true

    // Solange nicht gekoppelt ist, hat die Chatliste nichts zu zeigen. Die
    // Entscheidung faellt hier einmal und nicht in jeder Seite neu.
    initialPage: Dienst.gekoppelt ? chatsSeite : kopplungsSeite

    ChatsPage { id: chatsSeite }
    PairPage  { id: kopplungsSeite }

    Connections {
        target: Dienst
        onStatusChanged: {
            if (Dienst.gekoppelt && pageStack.currentPage === kopplungsSeite)
                pageStack.replace(chatsSeite)
        }
    }
}

import QtQuick 1.1
import com.nokia.meego 1.0

PageStackWindow {
    id: app
    showStatusBar: true
    showToolBar: true

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

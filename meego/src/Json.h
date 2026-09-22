#ifndef JSON_H
#define JSON_H

#include <QString>
#include <QVariant>

// Qt 4.7 has no JSON of its own -- QJsonDocument arrived with Qt 5. QtScript
// is on the device though, and its engine is a real ECMAScript engine with
// JSON built in, so parsing and writing both go through that rather than
// through a hand-written parser nobody would want to debug.
namespace Json {

QVariant parse(const QString &text, QString *error = nullptr);
QVariant parseFile(const QString &path, QString *error = nullptr);
QString stringify(const QVariant &value, bool pretty = false);
bool writeFile(const QString &path, const QVariant &value);

}

#endif

#include "Json.h"

#include <QFile>
#include <QScriptEngine>
#include <QScriptValue>
#include <QScriptValueIterator>
#include <QTextStream>

namespace {

// One engine for the whole process. Building a QScriptEngine costs real
// time on this hardware, and the course file is parsed at start-up when
// the learner is already waiting.
QScriptEngine *engine()
{
    static QScriptEngine *shared = nullptr;
    if (!shared)
        shared = new QScriptEngine();
    return shared;
}

}

namespace Json {

QVariant parse(const QString &text, QString *error)
{
    QScriptEngine *js = engine();
    QScriptValue json = js->globalObject().property(QLatin1String("JSON"));
    QScriptValue parseFn = json.property(QLatin1String("parse"));
    QScriptValue result = parseFn.call(json, QScriptValueList() << text);
    if (js->hasUncaughtException()) {
        if (error)
            *error = js->uncaughtException().toString();
        js->clearExceptions();
        return QVariant();
    }
    return result.toVariant();
}

QVariant parseFile(const QString &path, QString *error)
{
    QFile file(path);
    if (!file.open(QIODevice::ReadOnly)) {
        if (error)
            *error = QString::fromUtf8("%1 lässt sich nicht öffnen").arg(path);
        return QVariant();
    }
    QTextStream stream(&file);
    stream.setCodec("UTF-8");
    return parse(stream.readAll(), error);
}

QString stringify(const QVariant &value, bool pretty)
{
    QScriptEngine *js = engine();
    QScriptValue json = js->globalObject().property(QLatin1String("JSON"));
    QScriptValue fn = json.property(QLatin1String("stringify"));
    QScriptValueList args;
    args << js->toScriptValue(value);
    if (pretty)
        args << QScriptValue() << QScriptValue(1);
    QScriptValue result = fn.call(json, args);
    if (js->hasUncaughtException()) {
        js->clearExceptions();
        return QString();
    }
    return result.toString();
}

bool writeFile(const QString &path, const QVariant &value)
{
    // Written beside the target and renamed: a half-written progress file
    // would lose everything the learner has done, and phones do lose power.
    const QString temporary = path + QLatin1String(".neu");
    QFile file(temporary);
    if (!file.open(QIODevice::WriteOnly | QIODevice::Truncate))
        return false;
    QTextStream stream(&file);
    stream.setCodec("UTF-8");
    stream << stringify(value, true);
    stream.flush();
    file.close();
    QFile::remove(path);
    return QFile::rename(temporary, path);
}

}

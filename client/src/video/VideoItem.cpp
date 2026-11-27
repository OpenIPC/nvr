#include "video/VideoItem.h"

#include <QPainter>
#include <QPointF>
#include <QSizeF>

VideoItem::VideoItem(QQuickItem *parent)
    : QQuickPaintedItem(parent)
{
    // Кэш в изображении: кадры приходят часто, и держать их в текстуре
    // напрямую дешевле, чем перерисовывать сцену целиком.
    setRenderTarget(QQuickPaintedItem::Image);
    setPerformanceHint(QQuickPaintedItem::FastFBOResizing, true);
    // Сглаживание при уменьшении размывает мелкие детали — а на записи
    // номеров и лиц они и нужны. Кадр и так растягивается по размеру ячейки.
    setAntialiasing(false);
}

void VideoItem::setFrame(const QImage &image)
{
    bool sizeChanged = false;
    {
        QMutexLocker locker(&m_mutex);
        // Размер кадра меняется при переключении потока (main/sub) и при
        // смене разрешения на камере — наложению нужно об этом узнать.
        sizeChanged = m_image.size() != image.size();
        m_image = image;
    }
    if (sizeChanged) {
        emit frameSizeChanged();
    }
    // Кадры приходят из потока GStreamer: он ничего не знает о состоянии
    // отрисовки, поэтому обновление запрашиваем через очередь событий.
    QMetaObject::invokeMethod(this, "update", Qt::QueuedConnection);
}

void VideoItem::clearFrame()
{
    bool hadFrame = false;
    {
        QMutexLocker locker(&m_mutex);
        hadFrame = !m_image.isNull();
        m_image = QImage();
    }
    if (hadFrame) {
        emit frameSizeChanged();
    }
    QMetaObject::invokeMethod(this, "update", Qt::QueuedConnection);
}

int VideoItem::frameWidth() const
{
    QMutexLocker locker(&m_mutex);
    return m_image.width();
}

int VideoItem::frameHeight() const
{
    QMutexLocker locker(&m_mutex);
    return m_image.height();
}

QRectF VideoItem::normalizedRect(qreal x, qreal y, qreal w, qreal h) const
{
    QImage frame;
    {
        QMutexLocker locker(&m_mutex);
        frame = m_image;
    }
    if (frame.isNull() || size().isEmpty()) {
        return QRectF();
    }

    // Те же расчёты, что и в paint: иначе рамка разойдётся с картинкой.
    const QSizeF target = size();
    const QSizeF scaled = QSizeF(frame.size()).scaled(target, Qt::KeepAspectRatio);
    const QRectF destination(QPointF((target.width() - scaled.width()) / 2.0,
                                     (target.height() - scaled.height()) / 2.0),
                             scaled);
    return QRectF(destination.x() + x * destination.width(),
                  destination.y() + y * destination.height(),
                  w * destination.width(), h * destination.height());
}

void VideoItem::paint(QPainter *painter)
{
    QImage frame;
    {
        QMutexLocker locker(&m_mutex);
        frame = m_image;
    }

    if (frame.isNull()) {
        return;
    }

    // Вписываем кадр в ячейку с сохранением пропорций: камеры парка отдают
    // и 16:9, и 4:3 — растягивание исказило бы расстояния на картинке.
    const QSizeF target = size();
    if (target.isEmpty()) {
        return;
    }

    const QSizeF scaled = QSizeF(frame.size()).scaled(target, Qt::KeepAspectRatio);
    const QRectF destination(QPointF((target.width() - scaled.width()) / 2.0,
                                     (target.height() - scaled.height()) / 2.0),
                             scaled);

    painter->drawImage(destination, frame);
}

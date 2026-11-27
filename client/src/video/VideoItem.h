#pragma once

#include <QQuickPaintedItem>
#include <QImage>
#include <QMutex>
#include <QRectF>

/**
 * Элемент QML, который рисует кадр с камеры.
 *
 * Рисуем через QQuickPaintedItem, а не через собственный узел сцены.
 * Прежний вариант использовал QSGSimpleTextureNode и ронял приложение с
 * нарушением доступа внутри Qt6Quick.dll: окно закрывалось через пару
 * секунд после открытия. Ручная работа с узлами сцены требует аккуратности
 * в мелочах (порядок установки текстуры и фильтрации, пустой кадр, владение
 * текстурой), а ошибка там приводит не к сообщению, а к исчезновению
 * процесса — искать её приходится по журналу и коду исключения.
 *
 * QQuickPaintedItem сам управляет текстурой и очередью отрисовки, поэтому
 * на первом этапе выбран он: важнее не падать. Быстрый путь (свои узлы
 * сцены, кадры без лишних копирований) вернём отдельной задачей, когда
 * сетка заработает на стенде и появятся замеры.
 */
class VideoItem : public QQuickPaintedItem
{
    Q_OBJECT

    /**
     * Размер последнего принятого кадра.
     *
     * Нужен наложению: рамки событий заданы в пикселях кадра камеры, а
     * элемент рисует кадр с сохранением пропорций и полями. Без размера
     * кадра координаты не перевести в координаты ячейки.
     */
    Q_PROPERTY(int frameWidth READ frameWidth NOTIFY frameSizeChanged)
    Q_PROPERTY(int frameHeight READ frameHeight NOTIFY frameSizeChanged)

public:
    explicit VideoItem(QQuickItem *parent = nullptr);

    void paint(QPainter *painter) override;

    int frameWidth() const;
    int frameHeight() const;

    /**
     * Переводит нормализованную рамку (доли кадра 0..1) в координаты
     * элемента с учётом полей от «вписать с сохранением пропорций».
     *
     * Пустой прямоугольник — кадра ещё нет.
     */
    Q_INVOKABLE QRectF normalizedRect(qreal x, qreal y, qreal w, qreal h) const;

public slots:
    /** Новый кадр. Можно вызывать из любого потока. */
    void setFrame(const QImage &image);
    /** Убрать кадр: ячейка показывает снимок или надпись. */
    void clearFrame();

signals:
    /** Сменился размер кадра: наложение должно пересчитать координаты. */
    void frameSizeChanged();

private:
    mutable QMutex m_mutex;
    QImage m_image;
};

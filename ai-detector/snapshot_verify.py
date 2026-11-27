"""Проверка, что на снимке события ещё виден объект, из-за которого оно записано.

Зачем модуль отдельный. Снимок события берётся из основного потока отдельным
запросом к публикатору и приходит через секунды после кадра детекции: пока
идёт рукопожатие RTSP и пока снимок ждёт свободный слот, машина успевает
уехать. В событие попадал кадр, на котором объекта уже нет, хотя событие
записано именно из-за него.

Проверить это можно только повторной детекцией по самому снимку. Здесь —
только арифметика над рамками: ни numpy, ни моделей, поэтому правила
проверки тестируются без загрузки весов (см. test_snapshot_verify.py).
"""

# По каким классам искать на снимке объект события.
#
# Лицо и номер сами по себе моделью объектов не детектируются: на снимке
# ищем «родителя» — человека для лица и машину для номера. Сравнивать рамку
# лица с рамкой человека напрямую бессмысленно (площади разные), поэтому
# ниже дополнительно работает проверка «центр попал внутрь».
_PARENT_CLASSES = {
    "face": ("person",),
    "plate": ("car", "truck", "bus", "motorcycle", "bicycle"),
}


def parent_classes(object_class: str) -> tuple[str, ...]:
    """Классы, по которым искать объект события на снимке.

    Для обычных объектов это он сам: `car` ищем как `car`. Для лиц и
    номеров — класс-родитель, потому что собственных классов у них в модели
    объектов нет.
    """
    return _PARENT_CLASSES.get(object_class, (object_class,))


def to_fraction(bbox: dict | None, frame_size: tuple[int, int] | None) -> dict | None:
    """Переводит рамку из пикселей кадра детекции в доли кадра (0..1).

    Доли нужны потому, что снимок события — другого разрешения (основной
    поток, 1920 px), чем кадр детекции (субпоток). Сравнивать пиксели
    напрямую нельзя, а доли сохраняются при любом масштабе.

    None — рамки нет или она вырождена: сравнивать нечего.
    """
    if not bbox or not frame_size:
        return None
    try:
        frame_w, frame_h = float(frame_size[0]), float(frame_size[1])
        x = float(bbox.get("x") or 0)
        y = float(bbox.get("y") or 0)
        w = float(bbox.get("w") or 0)
        h = float(bbox.get("h") or 0)
    except (TypeError, ValueError):
        return None
    if frame_w <= 0 or frame_h <= 0 or w <= 0 or h <= 0:
        return None
    return {"x": x / frame_w, "y": y / frame_h,
            "w": w / frame_w, "h": h / frame_h}


def bbox_iou(a: dict, b: dict) -> float:
    """Доля пересечения двух рамок (Intersection over Union), 0..1."""
    ax2, ay2 = a["x"] + a["w"], a["y"] + a["h"]
    bx2, by2 = b["x"] + b["w"], b["y"] + b["h"]
    inter_w = min(ax2, bx2) - max(a["x"], b["x"])
    inter_h = min(ay2, by2) - max(a["y"], b["y"])
    if inter_w <= 0 or inter_h <= 0:
        return 0.0
    inter = inter_w * inter_h
    union = a["w"] * a["h"] + b["w"] * b["h"] - inter
    return inter / union if union > 0 else 0.0


def bbox_center_inside(outer: dict, inner: dict) -> bool:
    """Лежит ли центр `inner` внутри `outer`.

    Нужно для лиц и номеров: их рамка значительно меньше рамки человека или
    машины, и по IoU они совпадут только при почти полном наложении. Зато
    центр лица внутри человека — надёжный признак, что человек на снимке
    тот самый.
    """
    cx = inner["x"] + inner["w"] / 2
    cy = inner["y"] + inner["h"] / 2
    return (outer["x"] <= cx <= outer["x"] + outer["w"]
            and outer["y"] <= cy <= outer["y"] + outer["h"])


def keeps_object(expected: list[dict], found: list[dict],
                 min_iou: float = 0.1) -> bool:
    """Остался ли на снимке хотя бы один объект события.

    expected — рамки объектов события в долях кадра;
    found    — всё, что нашлось на снимке, в тех же долях.

    Порог IoU низкий намеренно. Между кадром детекции и снимком проходит
    несколько секунд, объект успевает сместиться, и требовать высокого
    совпадения значило бы отбрасывать правильные снимки. Задача проверки
    обратная: отсечь кадр, где объекта нет ВООБЩЕ.
    """
    for exp in expected:
        wanted = parent_classes(str(exp.get("object_class") or ""))
        for box in found:
            if box.get("object_class") not in wanted:
                continue
            if bbox_iou(exp["bbox"], box["bbox"]) >= min_iou:
                return True
            if bbox_center_inside(box["bbox"], exp["bbox"]):
                return True
    return False

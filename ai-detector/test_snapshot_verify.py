"""Юнит-тест: проверка снимка события на наличие объекта.

Запуск:
  python3 test_snapshot_verify.py

Импортируется только snapshot_verify — в нём нет numpy, OpenCV и моделей,
поэтому тест можно гонять без загрузки детектора.
"""
import sys

sys.path.insert(0, "/app")

from snapshot_verify import (
    bbox_center_inside,
    bbox_iou,
    keeps_object,
    parent_classes,
    to_fraction,
)

failures = []


def check(name, cond, extra=""):
    if cond:
        print(f"  OK   {name} {extra}")
    else:
        print(f"  FAIL {name} {extra}")
        failures.append(name)


def box(x, y, w, h):
    return {"x": x, "y": y, "w": w, "h": h}


# --- Классы-родители -------------------------------------------------------
check("объект ищется по своему классу",
      parent_classes("car") == ("car",))
check("лицо ищется как человек",
      parent_classes("face") == ("person",))
check("номер ищется как машина",
      "car" in parent_classes("plate") and "truck" in parent_classes("plate"))

# --- Пиксели → доли кадра --------------------------------------------------
check("рамка переводится в доли кадра",
      to_fraction(box(960, 540, 480, 270), (1920, 1080))
      == box(0.5, 0.5, 0.25, 0.25))
check("нулевая рамка отбрасывается",
      to_fraction(box(10, 10, 0, 20), (1920, 1080)) is None)
check("без размера кадра переводить нечего",
      to_fraction(box(10, 10, 20, 20), None) is None)
check("пустая рамка не ломает проверку",
      to_fraction(None, (1920, 1080)) is None)

# --- IoU и «центр внутри» --------------------------------------------------
check("совпадающие рамки дают IoU 1",
      round(bbox_iou(box(0.1, 0.1, 0.2, 0.2), box(0.1, 0.1, 0.2, 0.2)), 6) == 1.0)
check("непересекающиеся рамки дают IoU 0",
      bbox_iou(box(0.0, 0.0, 0.1, 0.1), box(0.5, 0.5, 0.1, 0.1)) == 0.0)
check("центр малой рамки внутри большой",
      bbox_center_inside(box(0.2, 0.2, 0.4, 0.4), box(0.3, 0.3, 0.05, 0.05)))
check("центр снаружи — не внутри",
      not bbox_center_inside(box(0.2, 0.2, 0.1, 0.1), box(0.6, 0.6, 0.05, 0.05)))

# --- Решение «объект остался на снимке» ------------------------------------
# Смещение за пару секунд: объект сдвинулся на треть своей ширины, IoU
# остаётся выше порога — снимок годный.
expected_car = [{"object_class": "car", "bbox": box(0.40, 0.40, 0.20, 0.20)}]
check("сдвинувшаяся машина остаётся на снимке",
      keeps_object(expected_car,
                   [{"object_class": "car", "bbox": box(0.47, 0.42, 0.20, 0.20)}]))
check("пустой снимок отбрасывается",
      not keeps_object(expected_car, []))
check("другой класс не считается объектом события",
      not keeps_object(expected_car,
                       [{"object_class": "person", "bbox": box(0.40, 0.40, 0.20, 0.20)}]))
check("машина вне кадра — снимок отбрасывается",
      not keeps_object(expected_car,
                       [{"object_class": "car", "bbox": box(0.0, 0.0, 0.05, 0.05)}]))

# Лицо и номер проверяются по родителю: на снимке есть человек/машина, а
# рамка лица/номера лежит внутри неё.
expected_face = [{"object_class": "face", "bbox": box(0.30, 0.30, 0.03, 0.03)}]
check("лицо подтверждается человеком на снимке",
      keeps_object(expected_face,
                   [{"object_class": "person", "bbox": box(0.25, 0.20, 0.25, 0.50)}]))
check("лицо не подтверждается машиной на снимке",
      not keeps_object(expected_face,
                       [{"object_class": "car", "bbox": box(0.25, 0.20, 0.25, 0.50)}]))

expected_plate = [{"object_class": "plate", "bbox": box(0.42, 0.60, 0.06, 0.03)}]
check("номер подтверждается машиной на снимке",
      keeps_object(expected_plate,
                   [{"object_class": "car", "bbox": box(0.30, 0.40, 0.40, 0.40)}]))
check("номер не подтверждается человеком на снимке",
      not keeps_object(expected_plate,
                       [{"object_class": "person", "bbox": box(0.30, 0.40, 0.40, 0.40)}]))

print()
if failures:
    print(f"ПРОВАЛЕНО: {len(failures)} — {failures}")
    raise SystemExit(1)
print("Все тесты пройдены")

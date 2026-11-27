import React, { useEffect, useRef, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';

import type { ApiClient } from '../net/api';

/**
 * OSD поверх живого видео в мобильном приложении: имя камеры, время
 * сервера и рамки детекции.
 *
 * Почему в клиенте, а не в потоке: перекодировать видео ради надписи
 * дорого, а телефон и так получает события для журнала. Рамки берутся
 * опросом того же журнала: на экране живого просмотра открыта одна камера,
 * поэтому один запрос в секунду — это немного. Для стены из десятков
 * камер такой путь не годится, там рамки идут потоком событий.
 */

/** Сколько секунд событие считается свежим и рисуется на кадре. */
const FRESH_SECONDS = 2.5;
/** Как часто перечитывать события, миллисекунды. */
const POLL_MS = 1000;

/** Цвета рамок: человек и транспорт различаются с одного взгляда. */
const CLASS_COLORS: Record<string, string> = {
  person: '#34c759',
  bicycle: '#30d158',
  motorcycle: '#30d158',
  car: '#0a84ff',
  bus: '#0a84ff',
  truck: '#0a84ff',
  dog: '#ff9f0a',
  cat: '#ff9f0a',
};

interface Box {
  key: string;
  left: number;
  top: number;
  width: number;
  height: number;
  color: string;
  label: string;
}

function pad(value: number): string {
  return value < 10 ? `0${value}` : String(value);
}

/**
 * Время в поясе сервера.
 *
 * now — текущий момент по локальным часам, skewMs — поправка часов сервера,
 * tzOffsetSec — смещение пояса сервера от UTC. Сумма даёт «сейчас» на
 * сервере в его местном времени; форматируем её UTC-геттерами, чтобы
 * телефон не подставил свой пояс.
 */
function formatClock(now: Date, skewMs: number, tzOffsetSec: number): string {
  const d = new Date(now.getTime() + skewMs + tzOffsetSec * 1000);
  return `${pad(d.getUTCDate())}.${pad(d.getUTCMonth() + 1)}.${d.getUTCFullYear()} ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
}

export default function LiveOsd({
  client,
  cameraId,
  cameraName,
  width,
  height,
  active = true,
}: {
  client: ApiClient | null;
  cameraId: string;
  cameraName?: string;
  /** Размер контейнера с видео — по нему считаются координаты рамок. */
  width: number;
  height: number;
  /** Идёт ли видео. Пока картинки нет, рамки рисовать не над чем. */
  active?: boolean;
}) {
  const [boxes, setBoxes] = useState<Box[]>([]);
  const [now, setNow] = useState(() => new Date());
  const [skewMs, setSkewMs] = useState(0);
  // До ответа сервера показываем время в поясе телефона, а не UTC:
  // смещение 0 дало бы на несколько часов неверную метку.
  const [tzOffsetSec, setTzOffsetSec] = useState(
    () => -new Date().getTimezoneOffset() * 60
  );
  // Поправка нужна и в опросе событий: возраст считается по серверным
  // часам, иначе сбитые часы телефона показывали бы чужие рамки.
  const skewRef = useRef(0);

  // Часы: смещение спрашиваем один раз, дальше тикаем локально.
  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .getServerTime()
      .then((time) => {
        if (cancelled) return;
        const skew = time.unix * 1000 - Date.now();
        skewRef.current = skew;
        setSkewMs(skew);
        setTzOffsetSec(time.offset_seconds);
      })
      .catch(() => {
        // Сервер не ответил — показываем локальное время без поправки.
      });

    const timer = setInterval(() => setNow(new Date()), 1000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [client]);

  // Рамки: события журнала, свежие не дольше FRESH_SECONDS.
  useEffect(() => {
    if (!client || !active || !width || !height) {
      setBoxes([]);
      return;
    }
    let cancelled = false;
    let lastPoll = 0;

    const poll = async () => {
      const started = Date.now();
      if (started - lastPoll < POLL_MS) return;
      lastPoll = started;
      try {
        const page = await client.listEvents({ cameraId, pageSize: 10 });
        if (cancelled) return;
        const serverNow = Date.now() + skewRef.current;
        const next: Box[] = [];

        // Кадр вписан в контейнер целиком (contain): у 4:3-камер по бокам
        // остаются поля, и без этой поправки рамка уехала бы в сторону.
        for (const ev of page.events || []) {
          const ts = new Date(ev.timestamp).getTime();
          if (Number.isNaN(ts)) continue;
          const age = (serverNow - ts) / 1000;
          if (age < -0.5 || age > FRESH_SECONDS) continue;

          const meta = (ev.metadata || {}) as Record<string, unknown>;
          const fw = Number(meta.frame_w);
          const fh = Number(meta.frame_h);
          const bbox = ev.bbox;
          if (!bbox || !fw || !fh) continue;

          const frameAspect = fw / fh;
          const viewAspect = width / height;
          let viewW = width;
          let viewH = height;
          let offsetX = 0;
          let offsetY = 0;
          if (viewAspect > frameAspect) {
            viewW = height * frameAspect;
            offsetX = (width - viewW) / 2;
          } else {
            viewH = width / frameAspect;
            offsetY = (height - viewH) / 2;
          }

          next.push({
            key: ev.id,
            left: offsetX + (bbox.x / fw) * viewW,
            top: offsetY + (bbox.y / fh) * viewH,
            width: (bbox.w / fw) * viewW,
            height: (bbox.h / fh) * viewH,
            color: CLASS_COLORS[ev.object_class] || '#0a84ff',
            label: ev.object_class,
          });
        }

        setBoxes(next);
      } catch {
        // Сеть пропала — оставляем прошлые рамки до следующего опроса,
        // чтобы картинка не мигала пустым слоем.
      }
    };

    poll();
    const timer = setInterval(poll, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [client, cameraId, width, height, active]);

  return (
    <View pointerEvents="none" style={styles.root}>
      <View style={styles.osd}>
        {cameraName ? <Text style={styles.name}>{cameraName}</Text> : null}
        <Text style={styles.time}>{formatClock(now, skewMs, tzOffsetSec)}</Text>
      </View>

      {boxes.map((box) => (
        <View
          key={box.key}
          style={[
            styles.box,
            {
              left: box.left,
              top: box.top,
              width: box.width,
              height: box.height,
              borderColor: box.color,
            },
          ]}
        >
          <Text style={[styles.label, { backgroundColor: box.color }]}>
            {box.label}
          </Text>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    ...StyleSheet.absoluteFillObject,
  },
  osd: {
    position: 'absolute',
    top: 6,
    left: 6,
    backgroundColor: 'rgba(0,0,0,0.5)',
    borderRadius: 4,
    paddingHorizontal: 6,
    paddingVertical: 2,
  },
  name: {
    color: '#fff',
    fontSize: 11,
    fontWeight: '700',
  },
  time: {
    color: '#fff',
    fontSize: 11,
    fontVariant: ['tabular-nums'],
  },
  box: {
    position: 'absolute',
    borderWidth: 2,
    borderRadius: 2,
  },
  label: {
    position: 'absolute',
    top: -14,
    left: -2,
    color: '#fff',
    fontSize: 9,
    fontWeight: '700',
    paddingHorizontal: 3,
    borderRadius: 2,
    overflow: 'hidden',
  },
});

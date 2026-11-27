import { useEffect, useState } from 'react'
import { clockAPI } from '../api/clock'

/**
 * Часы сервера для OSD.
 *
 * Смещение часов запрашивается ОДИН раз и общее на все компоненты: на
 * странице сетки одновременно открыты десятки ячеек, и отдельный запрос
 * на каждую означал бы десятки обращений в секунду к одному и тому же
 * эндпоинту. Дальше время тикает локально по таймеру, а сервер не
 * дёргается вовсе.
 */

let skewMs = 0
let timezoneName = ''
let timezoneOffsetSec = 0
let started = false
const listeners = new Set<() => void>()
let timerId: ReturnType<typeof setInterval> | null = null

function notify() {
  listeners.forEach((listener) => listener())
}

function startClock() {
  if (started) return
  started = true

  clockAPI
    .now()
    .then(({ data }) => {
      timezoneName = data.timezone
      timezoneOffsetSec = data.offset_seconds
      // Смещение = серверное «сейчас» минус локальное «сейчас». Формула
      // учитывает и разницу поясов, и сбитые часы рабочего места.
      skewMs = data.unix * 1000 - Date.now()
      notify()
    })
    .catch(() => {
      // Сервер не ответил — показываем локальное время. Молчаливое
      // отсутствие метки хуже неточной: оператор хотя бы видит время.
    })

  timerId = setInterval(notify, 1000)
}

/** Текущее серверное время и его пояс. */
export function useServerClock(): {
  now: Date
  timezone: string
  offsetSeconds: number
} {
  const [now, setNow] = useState(() => new Date(Date.now() + skewMs))

  useEffect(() => {
    startClock()
    const update = () => setNow(new Date(Date.now() + skewMs))
    listeners.add(update)
    update()
    return () => {
      listeners.delete(update)
      // Общий таймер не останавливаем: следующий смонтированный компонент
      // подхватит его без повторного запроса времени.
      if (listeners.size === 0 && timerId !== null) {
        clearInterval(timerId)
        timerId = null
        started = false
      }
    }
  }, [])

  return {
    now,
    timezone: timezoneName,
    offsetSeconds: timezoneOffsetSec,
  }
}

// Форматтеры кэшируются: Intl.DateTimeFormat стоит заметно дороже, чем
// кажется, а на сетке из 36 ячеек он вызывался бы 36 раз в секунду.
const formatters = new Map<string, Intl.DateTimeFormat>()

function pad(value: number): string {
  return value < 10 ? `0${value}` : String(value)
}

/**
 * Время в часовом поясе сервера.
 *
 * timezone — имя пояса (Europe/Moscow). Go может вернуть «Local», если
 * пояс не задан явно; Intl такое имя не принимает. Тогда, как и при пустом
 * имени, считаем по offset_seconds: сдвигаем момент на смещение пояса и
 * печатаем UTC-поля.
 *
 * withDate — показывать дату вместе со временем (крупный просмотр), иначе
 * только часы: в ячейке сетки места мало.
 */
export function formatServerClock(
  date: Date,
  timezone: string,
  offsetSeconds: number,
  withDate: boolean
): string {
  if (timezone && timezone !== 'Local') {
    const key = `${timezone}|${withDate ? 'full' : 'time'}`
    let formatter = formatters.get(key)
    if (!formatter) {
      try {
        formatter = new Intl.DateTimeFormat('ru-RU', {
          hour: '2-digit',
          minute: '2-digit',
          second: '2-digit',
          hour12: false,
          ...(withDate
            ? { day: '2-digit', month: '2-digit', year: 'numeric' }
            : {}),
          timeZone: timezone,
        })
        formatters.set(key, formatter)
      } catch {
        // Неизвестное браузеру имя пояса — ниже печатаем по смещению.
        formatter = undefined
      }
    }
    if (formatter) return formatter.format(date)
  }

  const shifted = new Date(date.getTime() + offsetSeconds * 1000)
  const time = `${pad(shifted.getUTCHours())}:${pad(shifted.getUTCMinutes())}:${pad(shifted.getUTCSeconds())}`
  if (!withDate) return time
  const day = `${pad(shifted.getUTCDate())}.${pad(shifted.getUTCMonth() + 1)}.${shifted.getUTCFullYear()}`
  return `${day} ${time}`
}

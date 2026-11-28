import type { CSSProperties } from 'react'
import type { CameraOSD } from '../api/client'
import { formatServerClock, useServerClock } from '../hooks/useServerClock'

/**
 * OSD поверх живого видео: имя камеры и время сервера.
 *
 * Рисуется в браузере, а не вжигается в поток: перекодировать 24 потока
 * ради надписи недопустимо дорого, а картинка при клиентском наложении
 * остаётся чёткой. Рамки детекции рисует DetectionOverlay — они тоже
 * клиентские, по тем же причинам.
 *
 * Время берётся у сервера (см. useServerClock): часы рабочего места могут
 * отставать, и тогда метка на видео не совпала бы с архивом.
 */
export default function LiveOsd({
  name,
  osd,
  compact = false,
  position = 'tl',
  showName,
}: {
  /** Имя камеры. Не передано — показываем только время. */
  name?: string
  /** Настройки наложения этой камеры (карточка камеры). */
  osd?: CameraOSD
  /** Мелкий вариант для ячейки сетки. */
  compact?: boolean
  /** Угол по умолчанию, если настройки у камеры ещё нет. */
  position?: 'tl' | 'bl' | 'tr' | 'br'
  /** Принудительно скрыть имя: в сетке оно есть отдельной полосой. */
  showName?: boolean
}) {
  const { now, timezone, offsetSeconds } = useServerClock()

  // Значения по умолчанию повторяют прежний вид: имя, время, левый верхний
  // угол. Камера без сохранённой настройки выглядит как раньше.
  const cfg: CameraOSD = {
    enabled: true,
    show_name: true,
    show_time: true,
    show_boxes: true,
    position,
    x: 0.02,
    y: 0.02,
    size: 'normal',
    ...osd,
  }
  if (!cfg.enabled) return null

  const showNameFinal = showName !== false && cfg.show_name && Boolean(name)
  if (!showNameFinal && !cfg.show_time) return null

  const time = formatServerClock(now, timezone, offsetSeconds, !compact)
  const fontSize = cfg.size === 'small'
    ? (compact ? 9 : 11)
    : cfg.size === 'large'
      ? (compact ? 12 : 16)
      : (compact ? 10 : 13)

  const margin = compact ? 4 : 8
  const corner: CSSProperties = (() => {
    if (cfg.position === 'custom') {
      return { left: `${cfg.x * 100}%`, top: `${cfg.y * 100}%` }
    }
    switch (cfg.position) {
      case 'tr': return { top: margin, right: margin }
      case 'bl': return { bottom: margin, left: margin }
      case 'br': return { bottom: margin, right: margin }
      default: return { top: margin, left: margin }
    }
  })()

  return (
    <div
      style={{
        position: 'absolute',
        ...corner,
        zIndex: 4,
        display: 'flex',
        flexDirection: 'column',
        gap: 1,
        padding: compact ? '2px 5px' : '4px 8px',
        borderRadius: 3,
        background: 'rgba(0,0,0,0.45)',
        color: '#fff',
        fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
        fontSize,
        fontWeight: 600,
        lineHeight: 1.25,
        textShadow: '0 1px 2px rgba(0,0,0,0.9)',
        // Надпись информационная: щелчки должны проходить к видео и кнопкам.
        pointerEvents: 'none',
        whiteSpace: 'nowrap',
      }}
    >
      {showNameFinal ? <span>{name}</span> : null}
      {cfg.show_time ? <span>{time}</span> : null}
    </div>
  )
}

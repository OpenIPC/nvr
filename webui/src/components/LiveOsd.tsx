import type { CSSProperties } from 'react'
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
  compact = false,
  position = 'tl',
}: {
  /** Имя камеры. Не передано — показываем только время. */
  name?: string
  /** Мелкий вариант для ячейки сетки. */
  compact?: boolean
  /** Угол, в котором стоит надпись. */
  position?: 'tl' | 'bl' | 'tr' | 'br'
}) {
  const { now, timezone, offsetSeconds } = useServerClock()
  const time = formatServerClock(now, timezone, offsetSeconds, !compact)

  const corner: CSSProperties =
    position === 'tl'
      ? { top: compact ? 4 : 8, left: compact ? 4 : 8 }
      : position === 'tr'
        ? { top: compact ? 4 : 8, right: compact ? 4 : 8 }
        : position === 'bl'
          ? { bottom: compact ? 4 : 8, left: compact ? 4 : 8 }
          : { bottom: compact ? 4 : 8, right: compact ? 4 : 8 }

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
        fontSize: compact ? 10 : 13,
        fontWeight: 600,
        lineHeight: 1.25,
        textShadow: '0 1px 2px rgba(0,0,0,0.9)',
        // Надпись информационная: щелчки должны проходить к видео и кнопкам.
        pointerEvents: 'none',
        whiteSpace: 'nowrap',
      }}
    >
      {name ? <span>{name}</span> : null}
      <span>{time}</span>
    </div>
  )
}

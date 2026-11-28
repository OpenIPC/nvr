import { useEffect, useState } from 'react'
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, Loader2, MonitorPlay, Save } from 'lucide-react'

import {
  camerasAPI, type Camera, type CameraOSD, type CameraOSDPosition, type CameraOSDSize,
} from '../api/client'
import { useToast } from '../context/ToastContext'

/**
 * Настройки наложения на видео для одной камеры.
 *
 * Рисуется клиентом, а не вжигается в поток: перекодировать 24 потока ради
 * подписи недопустимо дорого. Поэтому настройка общая для всех клиентов
 * (браузер, телефон, стена) и хранится на сервере в карточке камеры.
 */
const DEFAULT_OSD: CameraOSD = {
  enabled: true,
  show_name: true,
  show_time: true,
  show_boxes: true,
  position: 'tl',
  x: 0.02,
  y: 0.02,
  size: 'normal',
}

function normalize(incoming?: CameraOSD): CameraOSD {
  if (!incoming) return { ...DEFAULT_OSD }
  return { ...DEFAULT_OSD, ...incoming }
}

export default function CameraOsdPanel({
  camera,
  onSaved,
}: {
  camera: Camera
  /** Перечитать камеру: после сохранения меняется поле osd. */
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const { success, error } = useToast()
  const [form, setForm] = useState<CameraOSD>(() => normalize(camera.osd))
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)

  // Камера могла обновиться после сохранения — берём серверное значение,
  // но не затираем несохранённую правку.
  useEffect(() => {
    if (!dirty) setForm(normalize(camera.osd))
  }, [camera.osd, dirty])

  const patch = <K extends keyof CameraOSD>(key: K, value: CameraOSD[K]) => {
    setForm((current) => ({ ...current, [key]: value }))
    setDirty(true)
  }

  const save = async () => {
    setSaving(true)
    try {
      await camerasAPI.update(camera.id, { osd: form })
      setDirty(false)
      success(t('osdPanel.saved'))
      onSaved()
    } catch (e: any) {
      error(e.response?.data?.error || t('osdPanel.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  const fontSize = form.size === 'small' ? 10 : form.size === 'large' ? 16 : 13
  const previewText = t('osdPanel.previewTime')
  // Положение в предпросмотре повторяет то, что делают клиенты: углы — по
  // сторонам, «вручную» — по долям кадра.
  const previewStyle: CSSProperties = (() => {
    const base: CSSProperties = {
      position: 'absolute', color: '#fff', fontWeight: 600,
      fontFamily: 'ui-monospace, monospace', fontSize, lineHeight: 1.25,
      background: 'rgba(0,0,0,0.45)', padding: '2px 5px', borderRadius: 3,
      whiteSpace: 'nowrap',
    }
    switch (form.position) {
      case 'tl': return { ...base, top: 6, left: 6 }
      case 'tr': return { ...base, top: 6, right: 6 }
      case 'bl': return { ...base, bottom: 6, left: 6 }
      case 'br': return { ...base, bottom: 6, right: 6 }
      default: return { ...base, left: `${form.x * 100}%`, top: `${form.y * 100}%` }
    }
  })()

  return (
    <div style={{ marginTop: 24, borderTop: '1px solid var(--border)', paddingTop: 20 }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12, gap: 8, flexWrap: 'wrap' }}>
        <h3 style={{ fontSize: 15, margin: 0, display: 'flex', alignItems: 'center', gap: 8 }}>
          <MonitorPlay size={18} style={{ color: 'var(--accent)' }} />
          {t('osdPanel.title')}
        </h3>
        <button className="btn btn-primary btn-sm" onClick={save} disabled={saving || !dirty}>
          {saving ? <Loader2 size={14} className="spin" /> : <Save size={14} />}
          {dirty ? t('common.save') : t('osdPanel.saved')}
        </button>
      </div>

      <p style={{ fontSize: 12, color: 'var(--text-secondary)', margin: '0 0 12px' }}>
        {t('osdPanel.hint')}
      </p>

      <label style={{ display: 'flex', alignItems: 'center', gap: 10, cursor: 'pointer', marginBottom: 12 }}>
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(e) => patch('enabled', e.target.checked)}
          style={{ width: 16, height: 16, accentColor: 'var(--accent)' }}
        />
        <span style={{ fontSize: 13, fontWeight: 500 }}>{t('osdPanel.enabled')}</span>
      </label>

      <div style={{
        display: 'grid', gap: 8, marginBottom: 14,
        gridTemplateColumns: 'repeat(auto-fit, minmax(170px, 1fr))',
      }}>
        {([
          ['show_name', 'osdPanel.showName'],
          ['show_time', 'osdPanel.showTime'],
          ['show_boxes', 'osdPanel.showBoxes'],
        ] as const).map(([key, label]) => (
          <label key={key} style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, opacity: form.enabled ? 1 : 0.5 }}>
            <input
              type="checkbox"
              checked={form[key]}
              disabled={!form.enabled}
              onChange={(e) => patch(key, e.target.checked)}
              style={{ accentColor: 'var(--accent)' }}
            />
            {t(label)}
          </label>
        ))}
      </div>

      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', marginBottom: 12, opacity: form.enabled ? 1 : 0.5 }}>
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
          {t('osdPanel.position')}
          <select
            className="input"
            value={form.position}
            disabled={!form.enabled}
            onChange={(e) => patch('position', e.target.value as CameraOSDPosition)}
            style={{ width: 'auto', fontSize: 12, padding: '3px 8px' }}
          >
            <option value="tl">{t('osdPanel.posTl')}</option>
            <option value="tr">{t('osdPanel.posTr')}</option>
            <option value="bl">{t('osdPanel.posBl')}</option>
            <option value="br">{t('osdPanel.posBr')}</option>
            <option value="custom">{t('osdPanel.posCustom')}</option>
          </select>
        </label>

        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
          {t('osdPanel.size')}
          <select
            className="input"
            value={form.size}
            disabled={!form.enabled}
            onChange={(e) => patch('size', e.target.value as CameraOSDSize)}
            style={{ width: 'auto', fontSize: 12, padding: '3px 8px' }}
          >
            <option value="small">{t('osdPanel.sizeSmall')}</option>
            <option value="normal">{t('osdPanel.sizeNormal')}</option>
            <option value="large">{t('osdPanel.sizeLarge')}</option>
          </select>
        </label>
      </div>

      {form.position === 'custom' && (
        <div style={{ display: 'grid', gap: 8, marginBottom: 12, opacity: form.enabled ? 1 : 0.5 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
            <span style={{ width: 90 }}>{t('osdPanel.offsetX', { value: Math.round(form.x * 100) })}</span>
            <input
              type="range" min={0} max={1} step={0.01}
              value={form.x}
              disabled={!form.enabled}
              onChange={(e) => patch('x', parseFloat(e.target.value))}
              style={{ flex: 1, accentColor: 'var(--accent)' }}
            />
          </label>
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)' }}>
            <span style={{ width: 90 }}>{t('osdPanel.offsetY', { value: Math.round(form.y * 100) })}</span>
            <input
              type="range" min={0} max={1} step={0.01}
              value={form.y}
              disabled={!form.enabled}
              onChange={(e) => patch('y', parseFloat(e.target.value))}
              style={{ flex: 1, accentColor: 'var(--accent)' }}
            />
          </label>
        </div>
      )}

      {/* Предпросмотр: сразу видно, что и где окажется на картинке. */}
      <div style={{
        position: 'relative', width: '100%', aspectRatio: '16 / 9',
        borderRadius: 8, overflow: 'hidden', border: '1px solid var(--border)',
        background: 'linear-gradient(135deg, #1a2230, #0d1117)',
      }}>
        <div style={{
          position: 'absolute', inset: 0, display: 'flex',
          alignItems: 'center', justifyContent: 'center',
          color: '#3a4759', fontSize: 12,
        }}>
          {t('osdPanel.preview')}
        </div>
        {form.enabled && (form.show_name || form.show_time) && (
          <div style={previewStyle}>
            {form.show_name ? (
              <div style={{ display: 'block' }}>{camera.name}</div>
            ) : null}
            {form.show_time ? <div style={{ display: 'block' }}>{previewText}</div> : null}
          </div>
        )}
        {form.enabled && form.show_boxes && (
          <div style={{
            position: 'absolute', left: '38%', top: '42%', width: '24%', height: '30%',
            border: '2px solid #0a84ff', borderRadius: 2,
          }}>
            <span style={{
              position: 'absolute', left: -2, top: -16, background: '#0a84ff',
              color: '#fff', fontSize: 9, fontWeight: 700, padding: '0 4px',
              borderRadius: 2,
            }}>
              {t('osdPanel.previewBox')}
            </span>
          </div>
        )}
        {!form.enabled && (
          <div style={{
            position: 'absolute', inset: 0, display: 'flex',
            alignItems: 'center', justifyContent: 'center',
            color: 'var(--text-secondary)', fontSize: 12,
          }}>
            {t('osdPanel.disabledPreview')}
          </div>
        )}
      </div>

      {!dirty && (
        <p style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--success)', margin: '10px 0 0' }}>
          <CheckCircle2 size={13} />
          {t('osdPanel.saved')}
        </p>
      )}
    </div>
  )
}

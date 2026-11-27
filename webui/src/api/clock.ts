import api from './client'

/**
 * Время сервера.
 *
 * Нужно клиентам для метки на видео: часы рабочего места могут отставать
 * или идти в другом поясе, и тогда надпись на картинке разойдётся с
 * метками событий и архива.
 */
export interface ServerClock {
  /** Время сервера в формате RFC3339 с его поясом. */
  time: string
  unix: number
  /** Имя пояса сервера, например Europe/Moscow. */
  timezone: string
  /** Смещение пояса сервера от UTC в секундах на этот момент. */
  offset_seconds: number
}

export const clockAPI = {
  now: () => api.get<ServerClock>('/time'),
}

-- Канал MAX переезжает из общей строки notifications в свой ключ.
--
-- Причина: пока Telegram и MAX лежали в одной строке, сохранение настроек
-- Telegram записывало туда пустые поля MAX, и настройки мессенджера
-- пропадали из интерфейса. Теперь каждый канал пишется своим ключом, а эту
-- миграцию нужно один раз перенести сохранённые значения, чтобы уже
-- настроенные установки не потеряли токен и chat_id.

INSERT INTO server_settings (key, value, updated_at)
SELECT 'notifications_max', value -> 'max', now()
FROM server_settings
WHERE key = 'notifications'
  AND value ? 'max'
  -- Пустую заготовку не переносим: отдельный ключ уже может быть заполнен,
  -- и затирать его пустым значением нельзя.
  AND COALESCE(value -> 'max' ->> 'bot_token', '') <> ''
ON CONFLICT (key) DO UPDATE
    SET value = EXCLUDED.value, updated_at = now()
    WHERE COALESCE(server_settings.value ->> 'bot_token', '') = '';

-- Из общей строки поле max убираем: иначе при чтении оно снова конкурирует
-- с отдельным ключом, и исход зависит от порядка строк из базы.
UPDATE server_settings
SET value = value - 'max'
WHERE key = 'notifications'
  AND value ? 'max';

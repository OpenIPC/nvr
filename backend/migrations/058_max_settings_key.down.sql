-- Откат: возвращаем MAX в общую строку notifications и убираем отдельный
-- ключ. Нужен только при откате на версию, которая знает один ключ.
UPDATE server_settings AS n
SET value = jsonb_set(n.value, '{max}', m.value)
FROM server_settings AS m
WHERE n.key = 'notifications'
  AND m.key = 'notifications_max';

DELETE FROM server_settings WHERE key = 'notifications_max';

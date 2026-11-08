# YouTube Music

Медиаскилл `musicyoutube`: колонка по SSH, интенты без своей БД лайков.

Общие таблицы ядра в `data/yascore.db`:

- `music_tracks` — карточка title+artist (`liked_at`, `play_count`, …)
- `music_plays` — очередь previous/next с `current`
- `music_history` — журнал каждого старта (`started_at`, `listened_ms`, `skill`); дубли ок
- `music_playback` — пауза пользователя

Лайк keyed by title+artist. Каждый реальный старт (search/play/mix/next/previous) пишет карточку и `music_history`; очередь `music_plays` растёт на новых хвостах и при next/previous колонки, а сдвиг внутри сохранённой очереди только двигает `current`.

Конфиг: `skills.musicyoutube` (host, bin, database path, …).

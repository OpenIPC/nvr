package handlers

import (
	"net/http"
	"time"
)

// ClockHandler отдаёт текущее время сервера.
//
// Зачем отдельный эндпоинт. Клиенты рисуют на видео метку времени, и она
// должна совпадать с метками событий и записей архива. Часы рабочего места
// оператора могут отставать или идти в другом поясе, поэтому время берётся
// у сервера: клиент один раз узнаёт смещение своих часов и дальше тикает
// сам, не спрашивая сервер каждую секунду.
type ClockHandler struct{}

// NewClockHandler создаёт обработчик времени.
func NewClockHandler() *ClockHandler {
	return &ClockHandler{}
}

// Now отдаёт время в поясе сервера.
//
// Пояс — тот же, что у группировки архива по дням (см. timezone.go): иначе
// метка на видео и дата записи в календаре разошлись бы у установок, где
// пояс выбран не UTC.
func (h *ClockHandler) Now(w http.ResponseWriter, r *http.Request) {
	now := time.Now().In(localZone())
	_, offset := now.Zone()
	// «Local» — не настоящее имя пояса: Go так называет зону, загруженную
	// из /etc/localtime, а Intl в браузере такого имени не знает. Пустая
	// строка означает «считать по offset_seconds».
	zoneName := now.Location().String()
	if zoneName == "Local" {
		zoneName = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"time":     now.Format(time.RFC3339),
		"unix":     now.Unix(),
		"timezone": zoneName,
		// offset_seconds — смещение пояса сервера от UTC в этот момент.
		// Нужно клиентам, которые показывают время по поясу сервера, а не
		// по своему: на одном рабочем месте пояс может быть другим.
		"offset_seconds": offset,
	})
}

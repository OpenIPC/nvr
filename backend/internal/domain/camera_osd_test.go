package domain

import "testing"

// Наложение на видео: значения по умолчанию, нормализация и проверка.
//
// Ошибка здесь тихая: неверное положение или размер просто не дадут
// нарисовать надпись, а событие и поток останутся рабочими — поэтому
// правила проверяются отдельно от клиентов.
func TestCameraOSDDefaults(t *testing.T) {
	got := CameraOSDFromSettings(nil)
	want := DefaultCameraOSD()
	if got != want {
		t.Fatalf("без настроек ожидали %+v, получили %+v", want, got)
	}
	if !got.Enabled || !got.ShowName || !got.ShowTime || !got.ShowBoxes {
		t.Fatalf("по умолчанию наложение должно быть включено: %+v", got)
	}
}

func TestCameraOSDFromSettingsPartial(t *testing.T) {
	// В сохранённой записи может не быть части полей: недостающие должны
	// остаться включёнными, а не превратиться в false.
	got := CameraOSDFromSettings(map[string]any{
		"osd": map[string]any{"show_time": false, "position": "br"},
	})
	if got.ShowTime {
		t.Fatal("show_time=false из настроек не применился")
	}
	if !got.ShowName || !got.ShowBoxes {
		t.Fatalf("недостающие поля должны остаться включёнными: %+v", got)
	}
	if got.Position != "br" {
		t.Fatalf("position = %q, ожидали br", got.Position)
	}
}

func TestCameraOSDNormalize(t *testing.T) {
	got := NormalizeCameraOSD(CameraOSD{Position: "где-то", Size: "огромный", X: -1, Y: 2})
	if got.Position != "tl" || got.Size != "normal" {
		t.Fatalf("неизвестные значения не заменены: %+v", got)
	}
	if got.X != 0 || got.Y != 1 {
		t.Fatalf("координаты не ограничены 0..1: %+v", got)
	}
}

func TestCameraOSDValidate(t *testing.T) {
	if err := ValidateCameraOSD(DefaultCameraOSD()); err != nil {
		t.Fatalf("значения по умолчанию не прошли проверку: %v", err)
	}
	if err := ValidateCameraOSD(CameraOSD{Position: "custom", Size: "normal", X: 1.5, Y: 0.2}); err == nil {
		t.Fatal("ожидали отказ для x > 1")
	}
	if err := ValidateCameraOSD(CameraOSD{Position: "center", Size: "normal"}); err == nil {
		t.Fatal("ожидали отказ для неизвестного положения")
	}
	if err := ValidateCameraOSD(CameraOSD{Position: "tl", Size: "huge"}); err == nil {
		t.Fatal("ожидали отказ для неизвестного размера")
	}
}

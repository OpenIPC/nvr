package nats

import (
	"encoding/json"
	"testing"
)

// frameSize достаёт размер кадра из метаданных события детекции.
//
// Проверяются все встречающиеся представления: после разбора JSON числа
// приходят float64, при ручной сборке события — int, а json.Number может
// появиться при использовании декодера с UseNumber. Ошибка здесь тихая:
// размер станет нулём, и рамка на видео просто не нарисуется, хотя
// событие сохранится.
func TestFrameSize(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		wantW int
		wantH int
	}{
		{
			name:  "float64 из JSON",
			meta:  map[string]any{"frame_w": float64(704), "frame_h": float64(576)},
			wantW: 704,
			wantH: 576,
		},
		{
			name:  "int при ручной сборке",
			meta:  map[string]any{"frame_w": 1920, "frame_h": 1080},
			wantW: 1920,
			wantH: 1080,
		},
		{
			name:  "json.Number",
			meta:  map[string]any{"frame_w": json.Number("960"), "frame_h": json.Number("540")},
			wantW: 960,
			wantH: 540,
		},
		{
			name:  "метаданных нет",
			meta:  nil,
			wantW: 0,
			wantH: 0,
		},
		{
			name:  "неизвестный тип",
			meta:  map[string]any{"frame_w": "704", "frame_h": true},
			wantW: 0,
			wantH: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := frameSize(tc.meta)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Fatalf("frameSize() = (%d, %d), ожидали (%d, %d)",
					gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

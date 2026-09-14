package httpapi

import (
	"os"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/vad"
)

// TestVADRealPCM — реальный PCM (Silero TTS 24k→16k ресемплинг) + тишина 1.5 с.
func TestVADRealPCM(t *testing.T) {
	data, err := os.ReadFile("/tmp/vadpcm.bin")
	if err != nil {
		t.Skip("нет /tmp/vadpcm.bin")
	}
	d := vad.New(vad.DefaultConfig())
	var got int
	for off := 0; off < len(data); off += 8000 {
		end := off + 8000
		if end > len(data) {
			end = len(data)
		}
		u, done := d.Feed(data[off:end])
		if done {
			got = len(u)
			t.Logf("реплика завершена: %d байт (%.1f с)", got, float64(got)/2/16000)
			break
		}
	}
	if got == 0 {
		t.Fatal("VAD не завершил реплику на реальном PCM")
	}
}

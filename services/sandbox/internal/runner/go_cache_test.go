package runner

// Общий кэш сборки go (SANDBOX_GO_CACHE): без него каждый прогон пересобирает
// стандартную библиотеку (~40 с), а gate генерации делает до 7 прогонов.
import (
	"reflect"
	"testing"
)

func TestGoCacheRefs(t *testing.T) {
	tests := []struct {
		name        string
		cfg         string
		stack       string
		wantMounts  []string
		wantCacheIn string
	}{
		{"без конфига — per-run", "", "go", nil, "/work/.gocache"},
		{"том для go", "grade_gocache", "go", []string{"-v", "grade_gocache:/cache"}, "/cache/go-build"},
		{"python кэш не трогает", "grade_gocache", "python", nil, "/work/.gocache"},
		{"shell кэш не трогает", "grade_gocache", "shell", nil, "/work/.gocache"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := New(Config{Mode: "docker", GoCache: tc.cfg})
			mounts, dir := r.goCacheRefs(tc.stack)
			if !reflect.DeepEqual(mounts, tc.wantMounts) {
				t.Errorf("монтирование = %v, хотим %v", mounts, tc.wantMounts)
			}
			if dir != tc.wantCacheIn {
				t.Errorf("GOCACHE = %s, хотим %s", dir, tc.wantCacheIn)
			}
		})
	}
}

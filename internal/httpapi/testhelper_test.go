package httpapi

import (
	"testing"
	"time"

	"github.com/rf-us/zhihai-radar/internal/store"
)

// closeTestStore 关闭 SQLite 后等一拍：Windows 上 WAL/SHM 落盘有短暂延迟，
// t.TempDir 的清理会立刻删目录，偶发 "directory is not empty"。
func closeTestStore(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.Close(); err != nil {
		t.Logf("close store: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
}

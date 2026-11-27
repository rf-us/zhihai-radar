package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopDataDirUsesLocalAppData(t *testing.T) {
	t.Setenv("RADAR_DATA_DIR", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "LocalAppData"))
	got := desktopDataDir()
	want := filepath.Join(os.Getenv("LOCALAPPDATA"), "zhihai-radar")
	if got != want {
		t.Fatalf("data dir = %q, want %q", got, want)
	}
}

func TestDesktopDataDirRespectsOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("RADAR_DATA_DIR", dir)
	if got := desktopDataDir(); got != dir {
		t.Fatalf("override 未生效：%q", got)
	}
}

func TestStartupDegradesGracefullyWithoutReefBinary(t *testing.T) {
	// 无 REEF_BIN、无 REEF_MANAGED 时应跳过托管，仅起本地服务，不 panic。
	t.Setenv("REEF_BIN", "")
	t.Setenv("REEF_MANAGED", "0")
	t.Setenv("RADAR_DATA_DIR", t.TempDir())
	t.Setenv("RADAR_SOURCE", "fixture")
	a := NewApp("test")
	got := a.Startup()
	if got.Message != "" {
		t.Fatalf("离线模式启动不应报错：%s", got.Message)
	}
	if !got.Ready || got.BaseURL == "" || !strings.Contains(got.Version, "test") {
		t.Fatalf("启动状态不正确：%+v", got)
	}
	a.Quit()
}

func TestVersion(t *testing.T) {
	if got := NewApp("1.2.3").Version(); got != "v1.2.3" {
		t.Fatalf("Version = %q", got)
	}
}

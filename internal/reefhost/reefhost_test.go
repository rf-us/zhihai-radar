package reefhost

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- 指纹解析与脱敏（纯单元，不需要起进程） ----

func TestFingerprintScannerCRLFAndSpaces(t *testing.T) {
	s := newFingerprintScanner()
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	// 分两次写，且带 CRLF 与前后空格：日志是字节流，半行到达是常态。
	_, _ = s.Write([]byte("ready: ok\r\n  lan: 主机设备指纹 " + id[:32]))
	select {
	case <-s.found:
		t.Fatal("半条指纹不应触发")
	default:
	}
	_, _ = s.Write([]byte(id[32:] + "  \r\n"))
	select {
	case got := <-s.found:
		if got != id {
			t.Fatalf("指纹解析错误：got %q want %q", got, id)
		}
	case <-time.After(time.Second):
		t.Fatal("未解析到指纹")
	}
	// 第二条不同指纹不应再触发（once）。
	_, _ = s.Write([]byte("lan: 主机设备指纹 " + strings.Repeat("f", 64) + "\n"))
	select {
	case <-s.found:
		t.Fatal("第二条指纹不应再通知")
	default:
	}
}

func TestScrubWriterNeverLeaksFullFingerprint(t *testing.T) {
	id := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	var mu sync.Mutex
	var lines []string
	w := &scrubWriter{logf: func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}}
	_, _ = w.Write([]byte("lan: 主机设备指纹 " + id + "\r\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(lines) == 0 {
		t.Fatal("没有写出日志")
	}
	for _, line := range lines {
		if strings.Contains(line, id) {
			t.Fatalf("完整指纹泄漏到日志：%q", line)
		}
		if !strings.Contains(line, id[:8]) {
			t.Fatalf("脱敏后应保留前 8 位：%q", line)
		}
	}
}

func TestMaskedDeviceID(t *testing.T) {
	m := &Managed{deviceID: "0123456789abcdef"}
	if got := m.MaskedDeviceID(); got != "01234567" {
		t.Fatalf("MaskedDeviceID = %q，想要前 8 位", got)
	}
	m.deviceID = "abc"
	if got := m.MaskedDeviceID(); got != "abc" {
		t.Fatalf("短指纹应原样返回，got %q", got)
	}
}

// ---- 端口顺延（纯单元） ----

func TestPickPortUsesPreferred(t *testing.T) {
	p, err := pickPort(0)
	if err != nil {
		t.Fatal(err)
	}
	if p != defaultPort {
		t.Fatalf("默认端口 = %d，想要 %d", p, defaultPort)
	}
}

func TestPickPortSkipsOccupied(t *testing.T) {
	// 占用默认端口，pickPort 应顺延到下一个。
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	if err != nil {
		t.Skipf("端口 %d 已被占用，跳过：%v", defaultPort, err)
	}
	defer ln.Close()
	p, err := pickPort(0)
	if err != nil {
		t.Fatal(err)
	}
	if p <= defaultPort || p > maxPort {
		t.Fatalf("顺延端口 %d 不在 (%d, %d] 内", p, defaultPort, maxPort)
	}
}

func TestPickPortExhausted(t *testing.T) {
	// 占满 8080–8084。
	var listeners []interface{ Close() error }
	for p := defaultPort; p <= maxPort; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			// 环境里已被占用也算占上了，但无法确认是谁，跳过以免误判。
			for _, l := range listeners {
				_ = l.Close()
			}
			t.Skipf("端口 %d 意外不可用：%v", p, err)
		}
		listeners = append(listeners, ln)
	}
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	_, err := pickPort(0)
	if err != ErrPortsExhausted {
		t.Fatalf("端口占满时应返回 ErrPortsExhausted，got %v", err)
	}
}

// ---- 生命周期（helper 进程） ----

// helperSource 是一个最小"假运行环境"：启动时打印主机指纹，并在请求端口上
// 提供 /healthz。用 go run 在当前目录编译出的临时二进制跑，避免平台差异。
const helperSource = `package main
import (
 "fmt"
 "net/http"
 "os"
 "strings"
)
func main() {
 addr := "127.0.0.1:8080"
 for i, a := range os.Args {
  if a == "-addr" && i+1 < len(os.Args) { addr = os.Args[i+1] }
 }
 fmt.Println("lan: 主机设备指纹 " + strings.Repeat("a", 64))
 http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
 _ = http.ListenAndServe(addr, nil)
}
`

func buildHelper(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" && os.Getenv("CGO_ENABLED") == "1" {
		// 纯 Go helper 不需要 CGO，关掉更快更稳。
		_ = os.Setenv("CGO_ENABLED", "0")
		defer os.Unsetenv("CGO_ENABLED")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(helperSource), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-reef")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译 helper 失败：%v\n%s", err, out)
	}
	return bin
}

func TestStartFingerprintReadyAndClose(t *testing.T) {
	bin := buildHelper(t)
	m, err := Start(Options{Bin: bin, Port: defaultPort})
	if err != nil {
		t.Fatalf("Start 失败：%v", err)
	}
	defer m.Close()

	if got := m.DeviceID(); strings.Count(got, "a") != 64 || len(got) != 64 {
		t.Fatalf("设备指纹不正确：%q", got)
	}
	if m.Port() != defaultPort {
		t.Fatalf("端口 = %d，想要 %d", m.Port(), defaultPort)
	}
	st := m.GetStatus()
	if st.State != "running" || st.DeviceID != m.DeviceID()[:8] {
		t.Fatalf("状态不正确：%+v", st)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close 失败：%v", err)
	}
	if got := m.GetStatus().State; got != "down" {
		t.Fatalf("Close 后状态应为 down，got %s", got)
	}
}

func TestStartFailsWithoutBinary(t *testing.T) {
	_, err := Start(Options{})
	if err == nil || !strings.Contains(err.Error(), "REEF_BIN") {
		t.Fatalf("缺少二进制应报 REEF_BIN，got %v", err)
	}
}

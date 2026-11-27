// Package reefhost 负责在托管模式下拉起并监管运行环境（RFERP）子进程。
//
// 为什么需要它：运行环境未导入授权时，只有"主机"设备指纹能登录；这个指纹
// 在启动日志里打印，过去靠人工从数据库查出来填环境变量——换台机器演示就挂。
// 托管模式下 radar 自己拉起运行环境、从日志流里抓指纹、探活，再把 base 地址
// 与指纹注入配置，评委只需装一个包、双击 radar。
//
// 三条边界（与 issue #2 一致）：
//   - 完整指纹只存在本包实例的内存里，日志与 API 一律只露出前 8 位；
//   - 托管模式与外部模式共用同一个 reef.Client，只在构造时决定 base 与指纹；
//   - radar 退出时必须带走子进程，不留孤儿。
package reefhost

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort = 8080
	maxPort     = 8084 // 8080 被占用依次试到 8084，全占用报错退出

	fingerprintWait = 15 * time.Second // 抓指纹超时
	readyWait       = 20 * time.Second // /healthz 就绪超时
	readyInterval   = 500 * time.Millisecond
)

var (
	// 容忍 CRLF 与前后空格：日志行是字节流，Windows 下换行是 \r\n。
	fingerprintRe = regexp.MustCompile(`lan:\s*主机设备指纹\s*([0-9a-f]{64})`)
	// 脱敏用：任何 64 位十六进制串都只留前 8 位，避免指纹借别的日志行外泄。
	scrubRe = regexp.MustCompile(`[0-9a-f]{64}`)
)

// ErrPortsExhausted 表示 8080–8084 全被占用。
var ErrPortsExhausted = errors.New("端口 8080–8084 全部被占用")

// Options 是托管器的构造参数。
type Options struct {
	Bin  string // 运行环境可执行文件路径
	Port int    // 期望端口，0 表示 8080

	// Logf 接收子进程的脱敏输出；nil 时用标准 log。
	Logf func(format string, args ...any)

	// Cmd 可选：替换进程构造方式，仅供测试注入 helper 进程。
	// 返回 *exec.Cmd，参数由调用方设置（-addr）。
	Cmd func(addr string) *exec.Cmd
}

// Managed 是一个已启动的托管实例。
type Managed struct {
	base     string
	deviceID string
	port     int

	cmd  *exec.Cmd
	done chan struct{}

	mu       sync.Mutex
	exited   bool
	exitCode int
}

// Base 返回运行环境的可访问地址（http://127.0.0.1:<实际端口>）。
func (m *Managed) Base() string { return m.base }

// DeviceID 返回完整设备指纹。调用方只应把它交给 reef.Client，不得写入日志或响应。
func (m *Managed) DeviceID() string { return m.deviceID }

// Port 返回实际使用的端口。
func (m *Managed) Port() int { return m.port }

// MaskedDeviceID 返回可安全展示的指纹前缀（仅前 8 位）。
func (m *Managed) MaskedDeviceID() string {
	id := m.deviceID
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// Status 是对外可展示的运行状态。
type Status struct {
	Managed  bool   `json:"managed"`
	State    string `json:"state"` // running | down
	Port     int    `json:"port"`
	DeviceID string `json:"deviceId,omitempty"` // 仅前 8 位
	ExitCode int    `json:"exitCode,omitempty"`
}

// GetStatus 返回当前进程状态，不阻塞。
func (m *Managed) GetStatus() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Status{Managed: true, Port: m.port, DeviceID: m.MaskedDeviceID()}
	if m.exited {
		out.State = "down"
		out.ExitCode = m.exitCode
	} else {
		out.State = "running"
	}
	return out
}

// Start 拉起运行环境：选端口 → 起子进程 → 并行等指纹与 /healthz。
//
// 指纹与就绪并行等：首次启动可能要先做数据库迁移（5s 以上属正常），串行等
// 会把 15s + 20s 叠成 35s。任何一项超时都视为启动失败，并带走已起的子进程。
func Start(opts Options) (*Managed, error) {
	if strings.TrimSpace(opts.Bin) == "" && opts.Cmd == nil {
		return nil, errors.New("托管模式需要 REEF_BIN 指定运行环境可执行文件")
	}
	logf := opts.Logf
	if logf == nil {
		logf = log.Printf
	}

	port, err := pickPort(opts.Port)
	if err != nil {
		return nil, err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	base := "http://" + addr

	var cmd *exec.Cmd
	if opts.Cmd != nil {
		cmd = opts.Cmd(addr)
	}
	if cmd == nil {
		cmd = exec.Command(opts.Bin, "-addr", addr)
	}
	scanner := newFingerprintScanner()
	mw := io.MultiWriter(&scrubWriter{logf: logf}, scanner)
	cmd.Stdout = mw
	cmd.Stderr = mw
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动运行环境失败：%w", err)
	}

	m := &Managed{base: base, port: port, cmd: cmd, done: make(chan struct{})}
	// Wait 只能调一次；统一在这里收，Close 通过 <-done 等它完成。
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.exited = true
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				m.exitCode = ee.ExitCode()
			} else {
				m.exitCode = -1
			}
			logf("运行环境子进程退出：%v", err)
		}
		m.mu.Unlock()
		close(m.done)
	}()

	// 指纹与就绪各自一个结果通道，各收一次：两个 goroutine 都会在超时前
	// 把自己的结果写进缓冲通道，主 goroutine 顺序收即可，不会互相抢读。
	type fpResult struct{ id string }
	type readyResult struct{ err error }
	fpCh := make(chan fpResult, 1)
	readyCh := make(chan readyResult, 1)
	go func() {
		select {
		case id := <-scanner.found:
			fpCh <- fpResult{id: id}
		case <-time.After(fingerprintWait):
			fpCh <- fpResult{}
		}
	}()
	go func() { readyCh <- readyResult{err: waitReady(base)} }()

	fingerprint := (<-fpCh).id
	readyErr := (<-readyCh).err

	if fingerprint == "" {
		_ = m.Close()
		return nil, fmt.Errorf("启动运行环境超时：%s 内未抓到主机设备指纹（可手工设置 REEF_DEVICE_ID 覆盖）", fingerprintWait)
	}
	if readyErr != nil {
		_ = m.Close()
		return nil, fmt.Errorf("运行环境未就绪：%w", readyErr)
	}

	m.deviceID = fingerprint
	logf("reefhost: 运行环境就绪 base=%s 指纹=%s（完整指纹仅存内存）", base, m.MaskedDeviceID())
	return m, nil
}

// Close 终止子进程并等到 Wait 完成，确保不留孤儿。
func (m *Managed) Close() error {
	m.mu.Lock()
	exited := m.exited
	cmd := m.cmd
	m.mu.Unlock()
	if exited || cmd == nil || cmd.Process == nil {
		return nil
	}
	err := cmd.Process.Kill()
	<-m.done // 让 Wait 回收进程，避免僵尸
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("终止运行环境失败：%w", err)
	}
	return nil
}

// pickPort 找第一个可绑定的端口；先监听再释放，交给子进程真正绑定。
func pickPort(preferred int) (int, error) {
	start := preferred
	if start == 0 {
		start = defaultPort
	}
	for p := start; p <= maxPort; p++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err == nil {
			_ = ln.Close()
			return p, nil
		}
	}
	return 0, ErrPortsExhausted
}

// waitReady 每 500ms 探一次 /healthz，最长 20s。
func waitReady(base string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(readyWait)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(base + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("healthz 返回 %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(readyInterval)
	}
	if lastErr == nil {
		lastErr = errors.New("未收到响应")
	}
	return fmt.Errorf("%s 内未就绪（最后错误：%v）", readyWait, lastErr)
}

// fingerprintScanner 扫描字节流里的指纹行；找到第一条就通过 found 通知一次。
// 日志是持续输出的字节流，按行切分才能正确处理 CRLF 与半行到达。
type fingerprintScanner struct {
	mu    sync.Mutex
	buf   []byte
	found chan string
	once  sync.Once
}

func newFingerprintScanner() *fingerprintScanner {
	return &fingerprintScanner{found: make(chan string, 1)}
}

func (s *fingerprintScanner) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf = append(s.buf, p...)
	for {
		i := bytes.IndexByte(s.buf, '\n')
		if i < 0 {
			break
		}
		line := string(bytes.TrimSpace(s.buf[:i]))
		s.buf = s.buf[i+1:]
		m := fingerprintRe.FindStringSubmatch(line)
		if len(m) == 2 {
			id := m[1]
			s.once.Do(func() { s.found <- id })
		}
	}
	return len(p), nil
}

// scrubWriter 把写出的日志里的 64 位十六进制串替换为前 8 位 + "…"，
// 保证完整指纹不进任何日志文件。
type scrubWriter struct {
	logf func(string, ...any)
}

func (w *scrubWriter) Write(p []byte) (int, error) {
	text := string(p)
	text = scrubRe.ReplaceAllStringFunc(text, func(s string) string { return s[:8] + "…" })
	for _, line := range strings.Split(strings.TrimRight(text, "\r\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			w.logf("reef: %s", line)
		}
	}
	return len(p), nil
}

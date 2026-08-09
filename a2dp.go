package main

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

//go:embed a2dp-bridge/a2dp_bridge.dll
var embeddedDLL embed.FS

// 返回码与 a2dp_bridge.h 中的 A2DP_RESULT_* 宏保持一致
const (
	a2dpSuccess          = 0
	a2dpRequestTimedOut  = 1
	a2dpDeniedBySystem   = 2
	a2dpUnknownFailure   = 3
	a2dpNotInitialized   = -1
	a2dpNotSupported     = -2
	a2dpInvalidArgument  = -3
	a2dpCreateFailed     = -4
	a2dpAlreadyConnected = -5
	a2dpException        = -6
)

type device struct {
	ID   string
	Name string
}

type a2dpBridge struct {
	dll              *syscall.DLL
	init             *syscall.Proc
	shutdown         *syscall.Proc
	getDeviceCount   *syscall.Proc
	getDeviceName    *syscall.Proc
	getDeviceID      *syscall.Proc
	connect          *syscall.Proc
	disconnect       *syscall.Proc
	disconnectAll    *syscall.Proc
	getConnectionCnt *syscall.Proc
	getLastError     *syscall.Proc
}

// 将内嵌的 DLL 释放到临时目录（仅内容变化时才重写），返回释放路径
func extractDLL() (string, error) {
	data, err := embeddedDLL.ReadFile("a2dp-bridge/a2dp_bridge.dll")
	if err != nil {
		return "", fmt.Errorf("读取内嵌 DLL 失败: %w", err)
	}

	dir := filepath.Join(os.TempDir(), "AudioBridge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "a2dp_bridge.dll")

	// 已存在且内容一致则直接使用，避免重复写盘
	if existing, err := os.ReadFile(path); err == nil && len(existing) == len(data) {
		return path, nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("释放 DLL 失败: %w", err)
	}
	return path, nil
}

func loadBridge() (*a2dpBridge, error) {
	path, err := extractDLL()
	if err != nil {
		return nil, err
	}
	dll, err := syscall.LoadDLL(path)
	if err != nil {
		return nil, fmt.Errorf("加载 a2dp_bridge.dll 失败: %w", err)
	}

	b := &a2dpBridge{dll: dll}
	procs := []struct {
		name string
		dst  **syscall.Proc
	}{
		{"A2DP_Init", &b.init},
		{"A2DP_Shutdown", &b.shutdown},
		{"A2DP_GetDeviceCount", &b.getDeviceCount},
		{"A2DP_GetDeviceName", &b.getDeviceName},
		{"A2DP_GetDeviceId", &b.getDeviceID},
		{"A2DP_Connect", &b.connect},
		{"A2DP_Disconnect", &b.disconnect},
		{"A2DP_DisconnectAll", &b.disconnectAll},
		{"A2DP_GetConnectionCount", &b.getConnectionCnt},
		{"A2DP_GetLastError", &b.getLastError},
	}
	for _, p := range procs {
		proc, err := dll.FindProc(p.name)
		if err != nil {
			return nil, fmt.Errorf("DLL 缺少导出函数 %s: %w", p.name, err)
		}
		*p.dst = proc
	}
	return b, nil
}

func (b *a2dpBridge) Init() error {
	ret, _, err := b.init.Call()
	if int32(ret) != a2dpSuccess {
		return fmt.Errorf("A2DP_Init 返回 %d: %v", int32(ret), err)
	}
	return nil
}

func (b *a2dpBridge) Shutdown() {
	b.shutdown.Call()
}

// 枚举已配对的蓝牙音频设备
func (b *a2dpBridge) Devices() ([]device, error) {
	ret, _, _ := b.getDeviceCount.Call()
	count := int32(ret)
	if count < 0 {
		return nil, b.resultError(int(count), "枚举设备失败")
	}
	devices := make([]device, 0, count)
	for i := 0; i < int(count); i++ {
		name, err := b.getWString(b.getDeviceName, i)
		if err != nil {
			return nil, err
		}
		id, err := b.getWString(b.getDeviceID, i)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device{ID: id, Name: name})
	}
	return devices, nil
}

func (b *a2dpBridge) Connect(deviceID string) error {
	ptr, err := windows.UTF16PtrFromString(deviceID)
	if err != nil {
		return err
	}
	ret, _, _ := b.connect.Call(uintptr(unsafe.Pointer(ptr)))
	if code := int(int32(ret)); code != a2dpSuccess {
		return b.resultError(code, "连接失败")
	}
	return nil
}

func (b *a2dpBridge) Disconnect(deviceID string) error {
	ptr, err := windows.UTF16PtrFromString(deviceID)
	if err != nil {
		return err
	}
	ret, _, _ := b.disconnect.Call(uintptr(unsafe.Pointer(ptr)))
	if code := int(int32(ret)); code != a2dpSuccess {
		return b.resultError(code, "断开失败")
	}
	return nil
}

func (b *a2dpBridge) DisconnectAll() {
	b.disconnectAll.Call()
}

func (b *a2dpBridge) ConnectionCount() int {
	ret, _, _ := b.getConnectionCnt.Call()
	return int(int32(ret))
}

func (b *a2dpBridge) getWString(proc *syscall.Proc, index int) (string, error) {
	buf := make([]uint16, 256)
	ret, _, _ := proc.Call(uintptr(int32(index)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if code := int(int32(ret)); code < 0 {
		return "", b.resultError(code, "读取设备信息失败")
	}
	return windows.UTF16ToString(buf), nil
}

func (b *a2dpBridge) lastError() string {
	buf := make([]uint16, 512)
	ret, _, _ := b.getLastError.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if int32(ret) <= 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// 将 A2DP_RESULT_* 返回码转换为可读错误
func (b *a2dpBridge) resultError(code int, prefix string) error {
	var msg string
	switch code {
	case a2dpRequestTimedOut:
		msg = "请求超时（请确认手机蓝牙已开启并在附近）"
	case a2dpDeniedBySystem:
		msg = "被系统拒绝（请确认手机已与电脑配对）"
	case a2dpUnknownFailure:
		msg = "未知错误"
	case a2dpNotInitialized:
		msg = "桥接库未初始化"
	case a2dpNotSupported:
		msg = "当前系统不支持（需要 Windows 11 22000+）"
	case a2dpInvalidArgument:
		msg = "参数无效"
	case a2dpCreateFailed:
		msg = "无法创建连接（设备可能未配对或不支持）"
	case a2dpAlreadyConnected:
		msg = "设备已连接"
	case a2dpException:
		msg = "内部异常"
	default:
		msg = fmt.Sprintf("错误码 %d", code)
	}
	if detail := b.lastError(); detail != "" {
		msg += ": " + detail
	}
	return errors.New(prefix + ": " + msg)
}

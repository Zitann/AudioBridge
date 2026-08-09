package main

import (
	"os/exec"
	"syscall"
	"unsafe"

	"fyne.io/systray"
	"golang.org/x/sys/windows"
)

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	bridge, err := loadBridge()
	if err != nil {
		messageBox("AudioBridge 启动失败", err.Error())
		systray.Quit()
		return
	}
	if err := bridge.Init(); err != nil {
		messageBox("AudioBridge 启动失败",
			"初始化音频桥接失败（需要 Windows 11 22000 及以上版本）\n\n"+err.Error())
		systray.Quit()
		return
	}

	a := newApp(bridge)
	a.setup()
}

func onExit() {
	// 退出前断开所有连接并释放桥接库资源
	//（bridge 在 loadBridge 成功后才存在，这里做一次兜底检查）
	if b, err := loadBridgeQuiet(); err == nil {
		b.DisconnectAll()
		b.Shutdown()
	}
}

// onExit 阶段 DLL 已释放到磁盘，直接重新加载同一实例做清理
func loadBridgeQuiet() (*a2dpBridge, error) {
	return loadBridge()
}

// 打开 Windows 蓝牙设置页
func openBluetoothSettings() {
	exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", "ms-settings:bluetooth").Start()
}

// 弹出 Win32 消息框提示错误
func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	s, _ := windows.UTF16PtrFromString(text)
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	const mbIconError = 0x10
	proc.Call(0, uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(t)), mbIconError)
}

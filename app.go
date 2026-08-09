package main

import (
	"fmt"
	"sync"
	"time"

	"fyne.io/systray"
)

type app struct {
	bridge *a2dpBridge

	mu         sync.Mutex
	connected  map[string]bool   // deviceID -> 是否已连接
	connecting map[string]bool   // deviceID -> 正在连接中
	devNames   map[string]string // deviceID -> 设备名称（用于状态显示）

	statusItem *systray.MenuItem // 第一行：连接状态
	hintItem   *systray.MenuItem // 第二行：提示信息
}

func newApp(bridge *a2dpBridge) *app {
	return &app{
		bridge:     bridge,
		connected:  make(map[string]bool),
		connecting: make(map[string]bool),
		devNames:   make(map[string]string),
	}
}

func (a *app) setup() {
	ico, err := generateIconICO()
	if err == nil {
		systray.SetIcon(ico)
	}
	systray.SetTitle("AudioBridge")
	systray.SetTooltip("AudioBridge - 手机音频桥接")

	a.rebuildMenu()
	go a.watchConnections()
}

// 整体重建菜单（顺序：状态 → 设备 → 刷新 → 蓝牙设置 → 退出）
// systray 只支持往菜单末尾追加，因此用 ResetMenu 保证设备项位置正确
func (a *app) rebuildMenu() {
	systray.ResetMenu()

	// 状态显示：两行，不可点击
	a.statusItem = systray.AddMenuItem("未连接", "当前连接状态")
	a.statusItem.Disable()
	a.hintItem = systray.AddMenuItem("", "提示信息")
	a.hintItem.Disable()
	systray.AddSeparator()

	// 设备列表
	devices, err := a.bridge.Devices()
	switch {
	case err != nil:
		item := systray.AddMenuItem("（枚举设备失败）", err.Error())
		item.Disable()
	case len(devices) == 0:
		item := systray.AddMenuItem("（无已配对设备）", "请先在蓝牙设置中配对手机")
		item.Disable()
	default:
		for _, dev := range devices {
			item := systray.AddMenuItem(dev.Name, "点击连接/断开: "+dev.Name)
			a.mu.Lock()
			a.devNames[dev.ID] = dev.Name
			on := a.connected[dev.ID]
			a.mu.Unlock()
			if on {
				item.Check()
			}
			go a.deviceLoop(item, dev)
		}
	}

	refreshItem := systray.AddMenuItem("刷新设备列表", "重新枚举已配对的蓝牙音频设备")
	systray.AddSeparator()

	btItem := systray.AddMenuItem("蓝牙设置", "打开 Windows 蓝牙设置")
	quitItem := systray.AddMenuItem("退出", "断开所有连接并退出")

	go func() {
		for range refreshItem.ClickedCh {
			a.rebuildMenu()
		}
	}()
	go func() {
		for range btItem.ClickedCh {
			openBluetoothSettings()
		}
	}()
	go func() {
		for range quitItem.ClickedCh {
			systray.Quit()
		}
	}()

	a.updateStatus()
}

// 定时校对连接状态：手机端主动断开时，DLL 侧连接会自动关闭，
// 这里用实际连接数同步本地状态并重建菜单勾选
func (a *app) watchConnections() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		actual := a.bridge.ConnectionCount()
		a.mu.Lock()
		drift := actual != len(a.connected)
		if actual < len(a.connected) {
			// 实际连接变少，清空后等用户重新连接
			a.connected = make(map[string]bool)
		}
		a.mu.Unlock()
		if drift {
			a.rebuildMenu()
		}
	}
}

// 单个设备菜单项的点击处理：已连接则断开，未连接则连接
func (a *app) deviceLoop(item *systray.MenuItem, dev device) {
	for range item.ClickedCh {
		a.mu.Lock()
		isConnected := a.connected[dev.ID]
		isConnecting := a.connecting[dev.ID]
		a.mu.Unlock()

		if isConnecting {
			continue // 连接过程中忽略重复点击
		}

		if isConnected {
			if err := a.bridge.Disconnect(dev.ID); err != nil {
				messageBox("断开失败", err.Error())
				continue
			}
			a.mu.Lock()
			delete(a.connected, dev.ID)
			a.mu.Unlock()
		} else {
			a.mu.Lock()
			a.connecting[dev.ID] = true
			a.mu.Unlock()

			// 连接失败自动重试 2 次，间隔 1 秒（蓝牙偶发超时很常见）
			var err error
			for attempt := 1; attempt <= 3; attempt++ {
				if attempt == 1 {
					a.setStatus("连接中…", dev.Name)
				} else {
					a.setStatus(fmt.Sprintf("重试 %d/2…", attempt-1), dev.Name)
					time.Sleep(time.Second)
				}
				err = a.bridge.Connect(dev.ID)
				if err == nil {
					break
				}
			}

			a.mu.Lock()
			delete(a.connecting, dev.ID)
			a.mu.Unlock()

			if err != nil {
				messageBox("连接失败", err.Error())
				a.updateStatus()
				continue
			}
			a.mu.Lock()
			a.connected[dev.ID] = true
			a.devNames[dev.ID] = dev.Name
			a.mu.Unlock()
		}
		a.rebuildMenu()
	}
}

func (a *app) updateStatus() {
	a.mu.Lock()
	names := make([]string, 0, len(a.connected))
	for id := range a.connected {
		if name := a.devNames[id]; name != "" {
			names = append(names, name)
		}
	}
	a.mu.Unlock()

	switch len(names) {
	case 0:
		a.setStatus("未连接", "点击设备名称连接手机")
	case 1:
		a.setStatus("已连接 "+names[0], "正在播放手机音频")
	default:
		a.setStatus(fmt.Sprintf("已连接 %d 台", len(names)), "正在播放手机音频")
	}
}

// setStatus 双行显示：第一行状态，第二行提示
func (a *app) setStatus(status, hint string) {
	a.statusItem.SetTitle(status)
	a.hintItem.SetTitle(hint)
	systray.SetTooltip("AudioBridge - " + status + " " + hint)
}

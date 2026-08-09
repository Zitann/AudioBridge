package main

import (
	"fmt"
	"sync"

	"fyne.io/systray"
)

type app struct {
	bridge *a2dpBridge

	mu        sync.Mutex
	connected map[string]bool // deviceID -> 是否已连接

	deviceMenu *systray.MenuItem // “设备”子菜单
	deviceItem map[*systray.MenuItem]device
	statusItem *systray.MenuItem // 第一行：连接状态
	hintItem   *systray.MenuItem // 第二行：提示信息
}

func newApp(bridge *a2dpBridge) *app {
	return &app{
		bridge:     bridge,
		connected:  make(map[string]bool),
		deviceItem: make(map[*systray.MenuItem]device),
	}
}

func (a *app) setup() {
	ico, err := generateIconICO()
	if err == nil {
		systray.SetIcon(ico)
	}
	systray.SetTitle("AudioBridge")
	systray.SetTooltip("AudioBridge - 手机音频桥接")

	// 状态显示：两行，不可点击
	a.statusItem = systray.AddMenuItem("未连接", "当前连接状态")
	a.statusItem.Disable()
	a.hintItem = systray.AddMenuItem("", "提示信息")
	a.hintItem.Disable()
	systray.AddSeparator()

	// 设备子菜单
	a.deviceMenu = systray.AddMenuItem("设备", "已配对的蓝牙音频设备")
	refreshItem := a.deviceMenu.AddSubMenuItem("刷新", "重新枚举设备")

	systray.AddSeparator()
	btItem := systray.AddMenuItem("蓝牙设置", "打开 Windows 蓝牙设置")
	quitItem := systray.AddMenuItem("退出", "断开所有连接并退出")

	// 各菜单项的事件循环
	go a.refreshLoop(refreshItem)
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

	a.refreshDevices()
}

func (a *app) refreshLoop(item *systray.MenuItem) {
	for range item.ClickedCh {
		a.refreshDevices()
	}
}

// 重新枚举设备并重建子菜单
func (a *app) refreshDevices() {
	devices, err := a.bridge.Devices()
	if err != nil {
		a.setStatus("枚举失败", err.Error())
		return
	}

	// 移除旧的设备菜单项（保留最后的“刷新”项）
	for item := range a.deviceItem {
		item.Remove()
	}
	a.deviceItem = make(map[*systray.MenuItem]device, len(devices))

	if len(devices) == 0 {
		empty := a.deviceMenu.AddSubMenuItem("（无已配对设备）", "请先在蓝牙设置中配对手机")
		empty.Disable()
		a.deviceItem[empty] = device{}
		a.setStatus("未发现设备", "请在蓝牙设置中配对手机")
		return
	}

	for _, dev := range devices {
		item := a.deviceMenu.AddSubMenuItem(dev.Name, "点击连接/断开: "+dev.Name)
		if a.connected[dev.ID] {
			item.Check()
		}
		a.deviceItem[item] = dev
		go a.deviceLoop(item, dev)
	}
	a.updateStatus()
}

// 单个设备菜单项的点击处理：已连接则断开，未连接则连接
func (a *app) deviceLoop(item *systray.MenuItem, dev device) {
	for range item.ClickedCh {
		a.mu.Lock()
		isConnected := a.connected[dev.ID]
		a.mu.Unlock()

		if isConnected {
			if err := a.bridge.Disconnect(dev.ID); err != nil {
				messageBox("断开失败", err.Error())
				continue
			}
			a.mu.Lock()
			delete(a.connected, dev.ID)
			a.mu.Unlock()
			item.Uncheck()
		} else {
			a.setStatus("连接中…", dev.Name)
			if err := a.bridge.Connect(dev.ID); err != nil {
				messageBox("连接失败", err.Error())
				a.updateStatus()
				continue
			}
			a.mu.Lock()
			a.connected[dev.ID] = true
			a.mu.Unlock()
			item.Check()
		}
		a.updateStatus()
	}
}

func (a *app) updateStatus() {
	a.mu.Lock()
	n := len(a.connected)
	a.mu.Unlock()
	if n == 0 {
		a.setStatus("未连接", "点击“设备”连接手机")
	} else {
		a.setStatus(fmt.Sprintf("已连接 %d 台", n), "正在播放手机音频")
	}
}

// setStatus 双行显示：第一行状态，第二行提示
func (a *app) setStatus(status, hint string) {
	a.statusItem.SetTitle(status)
	a.hintItem.SetTitle(hint)
	systray.SetTooltip("AudioBridge - " + status + " " + hint)
}

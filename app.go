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
	systray.SetTitle(T(keyAppName))
	systray.SetTooltip(T(keyTooltipPrefix) + T(keyTooltipDesc))

	a.rebuildMenu()
	go a.watchConnections()
}

// 整体重建菜单（顺序：状态 → 设备 → 刷新 → 蓝牙设置 → 语言 → 退出）
// systray 只支持往菜单末尾追加，因此用 ResetMenu 保证设备项位置正确
func (a *app) rebuildMenu() {
	systray.ResetMenu()

	// 状态显示：两行，不可点击
	a.statusItem = systray.AddMenuItem(T(keyStatusNotConn), T(keyStatusTip))
	a.statusItem.Disable()
	a.hintItem = systray.AddMenuItem("", T(keyHintTip))
	a.hintItem.Disable()
	systray.AddSeparator()

	// 设备列表
	devices, err := a.bridge.Devices()
	switch {
	case err != nil:
		item := systray.AddMenuItem(T(keyEnumFailed), err.Error())
		item.Disable()
	case len(devices) == 0:
		item := systray.AddMenuItem(T(keyNoPaired), T(keyHintPairFirst))
		item.Disable()
	default:
		for _, dev := range devices {
			item := systray.AddMenuItem(dev.Name, T(keyClickToggle)+dev.Name)
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

	refreshItem := systray.AddMenuItem(T(keyMenuRefresh), T(keyReEnumDevices))
	systray.AddSeparator()

	btItem := systray.AddMenuItem(T(keyMenuBluetooth), T(keyOpenBTSettings))
	langMenu := systray.AddMenuItem(T(keyMenuLanguage), "")
	quitItem := systray.AddMenuItem(T(keyMenuExit), T(keyExitTip))

	// 语言子菜单
	for _, li := range langMenuItems {
		sub := langMenu.AddSubMenuItemCheckbox(li.native, "", getLang() == li.code)
		go a.langLoop(sub, li.code)
	}

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

// 语言切换
func (a *app) langLoop(item *systray.MenuItem, code langCode) {
	for range item.ClickedCh {
		if getLang() == code {
			continue // 已经是当前语言
		}
		setLang(code)
		a.rebuildMenu()
	}
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
				messageBox(T(keyErrDisconnect), err.Error())
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
					a.setStatus(T(keyStatusConnecting), dev.Name)
				} else {
					a.setStatus(fmt.Sprintf(T(keyStatusRetry), attempt-1), dev.Name)
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
				messageBox(T(keyErrConnect), err.Error())
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
		a.setStatus(T(keyStatusNotConn), T(keyHintClickConn))
	case 1:
		a.setStatus(T(keyStatusConn)+names[0], T(keyHintPlaying))
	default:
		a.setStatus(fmt.Sprintf(T(keyStatusConnN), len(names)), T(keyHintPlaying))
	}
}

// setStatus 双行显示：第一行状态，第二行提示
func (a *app) setStatus(status, hint string) {
	a.statusItem.SetTitle(status)
	a.hintItem.SetTitle(hint)
	systray.SetTooltip(T(keyTooltipPrefix) + status + " " + hint)
}

package main

// 多语言支持：通过 i18n.go 中的字典管理所有界面文本
// 添加新语言只需：1) 在 i18n.go 添加新的 map  2) 在 langMenuItems 添加菜单项

import "sync"

// 语言代码
type langCode string

const (
	langZhCN langCode = "zh-CN"
	langZhTW langCode = "zh-TW"
	langEnUS langCode = "en-US"
	langJaJP langCode = "ja-JP"
	langKoKR langCode = "ko-KR"
	langDeDE langCode = "de-DE"
	langFrFR langCode = "fr-FR"
	langEsES langCode = "es-ES"
	langRuRU langCode = "ru-RU"
)

// 当前语言（线程安全）
var (
	langMu  sync.RWMutex
	curLang = langZhCN
)

func getLang() langCode {
	langMu.RLock()
	defer langMu.RUnlock()
	return curLang
}

func setLang(l langCode) {
	langMu.Lock()
	defer langMu.Unlock()
	curLang = l
}

// 所有界面文本的 key
const (
	keyAppName          = "app_name"
	keyStatusNotConn    = "status_not_connected"
	keyStatusConn       = "status_connected"   // 单设备：已连接 {device}
	keyStatusConnN      = "status_connected_n" // 多设备：已连接 {n} 台
	keyStatusConnecting = "status_connecting"  // 连接中…
	keyStatusRetry      = "status_retry"       // 重试 {n}/2…
	keyHintClickConn    = "hint_click_connect" // 点击设备名称连接手机
	keyHintPlaying      = "hint_playing"       // 正在播放手机音频
	keyHintPairFirst    = "hint_pair_first"    // 请先在蓝牙设置中配对手机
	keyMenuRefresh      = "menu_refresh"       // 刷新设备列表
	keyMenuBluetooth    = "menu_bluetooth"     // 蓝牙设置
	keyMenuExit         = "menu_exit"          // 退出
	keyMenuLanguage     = "menu_language"      // 语言 / Language
	keyNoPaired         = "no_paired_devices"  // （无已配对设备）
	keyEnumFailed       = "enum_failed"        // （枚举设备失败）
	keyErrConnect       = "err_connect"        // 连接失败
	keyErrDisconnect    = "err_disconnect"     // 断开失败
	keyTooltipPrefix    = "tooltip_prefix"     // AudioBridge -
	keyTooltipDesc      = "tooltip_desc"       // 手机音频桥接
	keyClickToggle      = "click_toggle"       // 点击连接/断开:
	keyReEnumDevices    = "re_enum_devices"    // 重新枚举已配对的蓝牙音频设备
	keyOpenBTSettings   = "open_bt_settings"   // 打开 Windows 蓝牙设置
	keyExitTip          = "exit_tip"           // 断开所有连接并退出
	keyStatusTip        = "status_tip"         // 当前连接状态
	keyHintTip          = "hint_tip"           // 提示信息
	keyInitFailed       = "init_failed"        // 初始化音频桥接失败…
	keyStartFailed      = "start_failed"       // AudioBridge 启动失败
)

// 获取当前语言的文本
func T(key string) string {
	langMu.RLock()
	l := curLang
	langMu.RUnlock()

	if dict, ok := i18n[l]; ok {
		if text, ok := dict[key]; ok {
			return text
		}
	}
	// fallback 到英文
	if text, ok := i18n[langEnUS][key]; ok {
		return text
	}
	return key
}

// 语言菜单项配置（名称、母语显示、对应代码）
type langItem struct {
	code   langCode
	native string // 该语言的母语名称（如 "中文" / "English"）
}

var langMenuItems = []langItem{
	{langZhCN, "中文（简体）"},
	{langZhTW, "中文（繁體）"},
	{langEnUS, "English"},
	{langJaJP, "日本語"},
	{langKoKR, "한국어"},
	{langDeDE, "Deutsch"},
	{langFrFR, "Français"},
	{langEsES, "Español"},
	{langRuRU, "Русский"},
}

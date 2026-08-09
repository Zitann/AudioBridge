# AudioBridge

Windows 托盘程序：将手机通过蓝牙连接到电脑，把手机音频流传输到电脑扬声器播放。

基于 Windows 11 的 `AudioPlaybackConnection` WinRT API 实现。

## 使用场景

打《无畏契约》《CSGO》等竞技游戏时，死亡等待复活的几秒间隙：

- **不用摘耳机** —— 手机声音直接从电脑扬声器/耳机输出
- **不用切换蓝牙** —— 手机同时保持与电脑音频桥接，游戏语音和手机音乐互不干扰
- **死亡间隙刷抖音** —— 复活提示音一响，立刻切回游戏，不错过任何时机

## 系统要求

- Windows 11 22000 及以上
- 手机已在 Windows 蓝牙设置中完成配对

## 使用方法

1. 双击 `AudioBridge.exe` 运行，托盘出现蓝色扬声器图标
2. 右键图标 → **蓝牙设置**，确认手机已配对
3. 右键图标 → **设备** → 点击手机名称，开始连接
4. 连接成功后，手机播放的音乐/视频声音将从电脑扬声器输出
5. 再次点击设备名称断开连接；点击 **退出** 断开所有连接并退出程序

## 项目结构

```
AudioBridge/
├── main.go          # 程序入口、systray 生命周期、消息框
├── app.go           # 托盘菜单逻辑：设备列表、连接/断开、状态显示
├── a2dp.go          # a2dp_bridge.dll 的 Go 封装（go:embed 嵌入 + syscall 调用）
├── icon.go          # 程序生成托盘图标（蓝色扬声器，PNG→ICO）
├── icon.ico         # 生成的图标文件（供 rsrc 嵌入 exe）
├── app.manifest     # DPI 感知 + Win11 兼容声明
├── rsrc.syso        # rsrc 生成的资源文件，go build 自动链接
├── go.mod
├── go.sum
├── a2dp-bridge/     # C++/WinRT DLL 源码及头文件
│   ├── a2dp_bridge.cpp
│   ├── a2dp_bridge.h
│   └── a2dp_bridge.dll
└── .github/workflows/build.yml   # GitHub Actions 自动编译 DLL
```

## 技术架构

```
┌───────────────┐      ┌──────────────────┐      ┌─────────────────────────┐
│  AudioBridge  │────▶│ a2dp_bridge.dll  │────▶│ AudioPlaybackConnection │
│  (Go 托盘程序)│      │ (C ABI 封装)     │      │ (Windows 11 WinRT API)  │
└───────────────┘      └──────────────────┘      └─────────────────────────┘
```

- Go 程序通过 `syscall.LoadDLL` 调用 DLL 导出的 C 函数
- DLL 由 `go:embed` 嵌入 exe，运行时释放到 `%TEMP%\AudioBridge\` 后加载
- 单文件分发，无需额外 DLL

## 开发指南

### 环境要求

- Go 1.21+（纯 Go 编译，无需 CGO）
- Windows 11（开发/测试）
- GitHub Actions（自动编译 DLL，无需本地安装 MSVC）

### 编译步骤

```bash
# 1. 获取依赖
go mod tidy

# 2. 编译（rsrc.syso 已提供，go build 会自动链接）
go build -ldflags="-H windowsgui -s -w" -o AudioBridge.exe .
```

### 重新生成图标和 rsrc.syso

```bash
# 安装 rsrc 工具（一次性）
go install github.com/akavel/rsrc@latest

# 生成图标
go run genicon.go

# 打包资源
rsrc -manifest app.manifest -ico icon.ico -o rsrc.syso
```

### 重新编译 a2dp_bridge.dll

DLL 源码在 `a2dp-bridge/` 目录，推送到 GitHub 后 Actions 会自动编译：

```bash
git add a2dp-bridge/a2dp_bridge.cpp
git commit -m "feat: 更新 A2DP 桥接逻辑"
git push
# 到 GitHub Actions 页面下载编译好的 a2dp_bridge.dll
```

本地编译（需 Visual Studio 2022 + Windows SDK）：

```cmd
cl.exe /std:c++20 /EHsc /O2 /LD a2dp-bridge/a2dp_bridge.cpp /link /OUT:a2dp-bridge/a2dp_bridge.dll
```

## 已知限制

- 仅支持 Windows 11 22000+（`AudioPlaybackConnection` API 要求）
- 仅支持已配对的蓝牙音频设备
- 音频延迟取决于蓝牙编码和系统缓冲

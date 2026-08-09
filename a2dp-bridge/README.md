# A2DP Bridge

把 Windows 11 的 `AudioPlaybackConnection`（WinRT）封装成普通 C 接口的 DLL，
供 Go 等语言调用，用于从电脑向蓝牙音箱/耳机建立音频回放连接。

核心逻辑移植自 [ysc3839/AudioPlaybackConnector](https://github.com/ysc3839/AudioPlaybackConnector)。

## 文件

| 文件 | 说明 |
|------|------|
| `a2dp_bridge.h` | C 接口声明（返回值、函数说明） |
| `a2dp_bridge.cpp` | DLL 实现（C++/WinRT） |
| `.github/workflows/build.yml` | GitHub Actions 手动构建流程 |

## 用 GitHub Actions 编译

1. 新建一个 GitHub 仓库（例如 `a2dp-bridge`），把本目录**内容**上传到仓库根目录
2. 仓库 → Actions → `Build A2DP Bridge DLL` → **Run workflow**
3. 等 2~3 分钟，在 workflow 运行页的 **Artifacts** 里下载 `a2dp_bridge_dll`

编译产物：`a2dp_bridge.dll`（x64，Release）+ `a2dp_bridge.h`。
运行环境要求 Windows 11（`AudioPlaybackConnection` 需要 22000+，推荐 22H2+）。

本地编译（可选）：打开 VS 的 Developer Command Prompt，运行
`cl.exe /std:c++20 /EHsc /O2 /LD a2dp_bridge.cpp /link /OUT:a2dp_bridge.dll`。

## API 速览

| 函数 | 说明 |
|------|------|
| `int A2DP_Init()` | 初始化 WinRT 环境并检查系统支持（可重复调用，线程安全） |
| `int A2DP_GetDeviceCount()` | 枚举蓝牙音频设备并缓存，返回数量（负数=错误） |
| `int A2DP_GetDeviceName(index, buf, size)` | 取设备名称（UTF-16，先调 GetDeviceCount） |
| `int A2DP_GetDeviceId(index, buf, size)` | 取设备 ID（用于 Connect） |
| `int A2DP_Connect(deviceId)` | 建立连接，0=成功；1=超时；2=系统拒绝；3=未知失败；负数=本地错误 |
| `int A2DP_Disconnect(deviceId)` | 断开指定连接 |
| `int A2DP_DisconnectAll()` | 断开全部连接 |
| `int A2DP_GetConnectionCount()` | 当前活跃连接数 |
| `int A2DP_GetLastError(buf, size)` | 取最近一次错误文本 |
| `int A2DP_Shutdown()` | 清理并释放资源 |

## Go 调用示例

```go
package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	dll          = syscall.NewLazyDLL("a2dp_bridge.dll")
	init_        = dll.NewProc("A2DP_Init")
	getCount     = dll.NewProc("A2DP_GetDeviceCount")
	getName      = dll.NewProc("A2DP_GetDeviceName")
	getID        = dll.NewProc("A2DP_GetDeviceId")
	connect      = dll.NewProc("A2DP_Connect")
	disconnect   = dll.NewProc("A2DP_Disconnect")
	shutdown     = dll.NewProc("A2DP_Shutdown")
)

func getString(proc *syscall.LazyProc, index int) string {
	buf := make([]uint16, 256)
	proc.Call(uintptr(index), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func main() {
	if ret, _, _ := init_.Call(); ret != 0 {
		panic("A2DP_Init failed")
	}
	defer shutdown.Call()

	n, _, _ := getCount.Call()
	fmt.Printf("devices: %d\n", int(n))
	for i := 0; i < int(n); i++ {
		fmt.Printf("  %d: %s\n", i, getString(getName, i))
	}

	deviceID := getString(getID, 0)
	ret, _, _ := connect.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(deviceID))))
	fmt.Printf("connect: %d (%s)\n", int(ret), deviceID)
	_ = disconnect
}
```

## 注意事项

- 设备需先与 Windows 配对（设置 → 蓝牙），列表只显示支持音频回放连接的已配对设备
- 如果 `A2DP_Connect` 返回 2（`DeniedBySystem`），说明系统拒绝了请求：
  确认设备可用、驱动正常，或尝试以管理员身份运行你的程序
- 首次使用建议在真机上测试 `OpenAsync` 的行为（不同设备表现可能不同）

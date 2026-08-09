// a2dp_bridge.cpp
//
// Wraps Windows.Media.Audio.AudioPlaybackConnection (WinRT, Windows 11) into
// a synchronous C API DLL callable from Go and other languages.
//
// The core logic is ported from AudioPlaybackConnector (ysc3839):
//   winrt::init_apartment()
//   -> enumerate devices (GetDeviceSelector + FindAllAsync)
//   -> AudioPlaybackConnection::TryCreateFromId
//   -> StartAsync -> OpenAsync
//
// WinRT APIs used by the reference implementation (this file only uses the
// audio-related ones):
//   - winrt::init_apartment() / uninit_apartment()   COM apartment (MTA)
//   - Windows::Foundation::Metadata::ApiInformation::IsTypePresent
//   - Windows::Devices::Enumeration::DeviceInformation
//       FindAllAsync / CreateFromIdAsync / Id() / Name()
//   - Windows::Media::Audio::AudioPlaybackConnection
//       GetDeviceSelector / TryCreateFromId / StartAsync / OpenAsync / Close
//       StateChanged / State() / DeviceId()
//   - Windows::Media::Audio::AudioPlaybackConnectionOpenResult
//       Status() / ExtendedError()
//   - Enums: AudioPlaybackConnectionState,
//   AudioPlaybackConnectionOpenResultStatus
// The reference app also uses UI APIs (DevicePicker, DesktopWindowXamlSource,
// XAML controls, ...) which a plain DLL does not need.

#define A2DP_EXPORTS
#include "a2dp_bridge.h"

#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>

#include <algorithm>
#include <cstring>
#include <cwchar>
#include <iterator>
#include <mutex>
#include <string>
#include <string_view>
#include <thread>
#include <unordered_map>
#include <utility>
#include <vector>

#include <winrt/Windows.Devices.Enumeration.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <winrt/Windows.Foundation.Metadata.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Media.Audio.h>
#include <winrt/base.h>

#pragma comment(lib, "windowsapp.lib")

using namespace winrt;
using namespace winrt::Windows::Devices::Enumeration;
using namespace winrt::Windows::Foundation;
using namespace winrt::Windows::Media::Audio;

namespace {
// State is protected by one mutex; the error text uses its own mutex so
// that error reporting never nests inside the state mutex.
std::mutex g_mutex;
std::mutex g_errorMutex;
std::wstring g_lastError;
bool g_inited = false;
std::thread::id g_initThreadId{};

struct CachedDevice {
  std::wstring id;
  std::wstring name;
};
std::vector<CachedDevice> g_devices;

// device id -> active AudioPlaybackConnection
std::unordered_map<std::wstring, AudioPlaybackConnection> g_connections;

void SetLastError(std::wstring message) {
  std::lock_guard<std::mutex> lock(g_errorMutex);
  g_lastError = std::move(message);
}

void SetLastError(winrt::hresult_error const &ex) {
  wchar_t buffer[512];
  swprintf_s(buffer, std::size(buffer), L"%s (0x%08X)", ex.message().c_str(),
             static_cast<uint32_t>(ex.code()));
  SetLastError(buffer);
}

void SetLastError(std::wstring_view message, uint32_t code) {
  wchar_t buffer[512];
  swprintf_s(buffer, std::size(buffer), L"%.*ls (0x%08X)",
             static_cast<int>(message.size()), message.data(), code);
  SetLastError(buffer);
}

bool CopyString(std::wstring const &source, wchar_t *buffer, int bufferSize) {
  if (buffer == nullptr || bufferSize <= 0) {
    return false;
  }
  size_t count =
      std::min<size_t>(source.size(), static_cast<size_t>(bufferSize) - 1);
  memcpy(buffer, source.data(), count * sizeof(wchar_t));
  buffer[count] = L'\0';
  return true;
}

// Ensure the calling thread has a COM apartment. Go may call the exported
// functions from any of its threads, and each thread needs RoInitialize.
bool EnsureApartment() {
  int32_t hr = winrt::init_apartment(winrt::apartment_type::multi_threaded);
  return !FAILED(hr);
}

int GetCachedDeviceField(int index, wchar_t *buffer, int bufferSize,
                         std::wstring CachedDevice::*field) {
  std::lock_guard<std::mutex> lock(g_mutex);
  if (!g_inited) {
    SetLastError(L"A2DP_Init must be called first.");
    return A2DP_RESULT_NOT_INITIALIZED;
  }
  if (index < 0 || static_cast<size_t>(index) >= g_devices.size()) {
    SetLastError(L"Invalid device index, call A2DP_GetDeviceCount first.");
    return A2DP_RESULT_INVALID_ARGUMENT;
  }
  std::wstring const &value = g_devices[static_cast<size_t>(index)].*field;
  if (!CopyString(value, buffer, bufferSize)) {
    SetLastError(L"Invalid buffer.");
    return A2DP_RESULT_INVALID_ARGUMENT;
  }
  return A2DP_RESULT_SUCCESS;
}
} // namespace

extern "C" A2DP_API int A2DP_Init() {
  std::lock_guard<std::mutex> lock(g_mutex);
  if (g_inited) {
    return A2DP_RESULT_SUCCESS;
  }

  // Initialize COM/WinRT (multithreaded apartment: callable from any thread).
  int32_t hr = winrt::init_apartment(winrt::apartment_type::multi_threaded);
  if (FAILED(hr)) {
    wchar_t buffer[512];
    swprintf_s(buffer, std::size(buffer),
               L"winrt::init_apartment failed (0x%08X)",
               static_cast<uint32_t>(hr));
    SetLastError(buffer);
    return A2DP_RESULT_EXCEPTION;
  }

  // Check whether the system supports AudioPlaybackConnection (Win11 22000+).
  try {
    using namespace winrt::Windows::Foundation::Metadata;
    if (!ApiInformation::IsTypePresent(
            winrt::name_of<AudioPlaybackConnection>())) {
      SetLastError(L"AudioPlaybackConnection is not supported on this Windows "
                   L"version (Windows 11 required).");
      return A2DP_RESULT_NOT_SUPPORTED;
    }
  } catch (winrt::hresult_error const &ex) {
    SetLastError(ex);
    return A2DP_RESULT_EXCEPTION;
  }

  g_initThreadId = std::this_thread::get_id();
  g_inited = true;
  return A2DP_RESULT_SUCCESS;
}

extern "C" A2DP_API int A2DP_Shutdown() {
  std::vector<AudioPlaybackConnection> toClose;
  bool shouldUninit = false;
  {
    std::lock_guard<std::mutex> lock(g_mutex);
    for (auto const &entry : g_connections) {
      toClose.push_back(entry.second);
    }
    g_connections.clear();
    g_devices.clear();
    if (g_inited) {
      g_inited = false;
      // RoUninitialize may only be called on the thread that initialized.
      shouldUninit = (std::this_thread::get_id() == g_initThreadId);
    }
  }
  for (auto const &connection : toClose) {
    connection.Close();
  }
  if (shouldUninit) {
    winrt::uninit_apartment();
  }
  return A2DP_RESULT_SUCCESS;
}

extern "C" A2DP_API int A2DP_GetDeviceCount() {
  {
    std::lock_guard<std::mutex> lock(g_mutex);
    if (!g_inited) {
      SetLastError(L"A2DP_Init must be called first.");
      return A2DP_RESULT_NOT_INITIALIZED;
    }
  }
  if (!EnsureApartment()) {
    SetLastError(L"Failed to initialize the COM apartment on this thread.");
    return A2DP_RESULT_EXCEPTION;
  }

  try {
    auto selector = AudioPlaybackConnection::GetDeviceSelector();
    auto devices = DeviceInformation::FindAllAsync(selector).get();

    std::vector<CachedDevice> list;
    uint32_t size = devices.Size();
    list.reserve(size);
    for (uint32_t i = 0; i < size; ++i) {
      auto device = devices.GetAt(i);
      list.push_back({std::wstring(device.Id()), std::wstring(device.Name())});
    }

    int count;
    {
      std::lock_guard<std::mutex> lock(g_mutex);
      g_devices = std::move(list);
      count = static_cast<int>(g_devices.size());
    }
    return count;
  } catch (winrt::hresult_error const &ex) {
    SetLastError(ex);
    return A2DP_RESULT_EXCEPTION;
  }
}

extern "C" A2DP_API int A2DP_GetDeviceName(int index, wchar_t *buffer,
                                           int bufferSize) {
  return GetCachedDeviceField(index, buffer, bufferSize, &CachedDevice::name);
}

extern "C" A2DP_API int A2DP_GetDeviceId(int index, wchar_t *buffer,
                                         int bufferSize) {
  return GetCachedDeviceField(index, buffer, bufferSize, &CachedDevice::id);
}

extern "C" A2DP_API int A2DP_Connect(const wchar_t *deviceId) {
  if (deviceId == nullptr || deviceId[0] == L'\0') {
    SetLastError(L"deviceId must not be null or empty.");
    return A2DP_RESULT_INVALID_ARGUMENT;
  }
  if (!EnsureApartment()) {
    SetLastError(L"Failed to initialize the COM apartment on this thread.");
    return A2DP_RESULT_EXCEPTION;
  }

  std::wstring id(deviceId);
  {
    std::lock_guard<std::mutex> lock(g_mutex);
    if (!g_inited) {
      SetLastError(L"A2DP_Init must be called first.");
      return A2DP_RESULT_NOT_INITIALIZED;
    }
    if (g_connections.find(id) != g_connections.end()) {
      SetLastError(L"Already connected to this device.");
      return A2DP_RESULT_ALREADY_CONNECTED;
    }
  }

  try {
    auto connection = AudioPlaybackConnection::TryCreateFromId(id);
    if (!connection) {
      SetLastError(L"TryCreateFromId failed: device not found or unsupported.");
      return A2DP_RESULT_CREATE_FAILED;
    }

    // Remove the connection from the active map when the system closes it.
    connection.StateChanged(
        [](AudioPlaybackConnection const &sender, IInspectable const &) {
          if (sender.State() == AudioPlaybackConnectionState::Closed) {
            std::lock_guard<std::mutex> lock(g_mutex);
            g_connections.erase(std::wstring(sender.DeviceId()));
          }
        });

    // Start first, then open (same order as the reference implementation).
    connection.StartAsync().get();
    auto result = connection.OpenAsync().get();

    auto status = result.Status();
    if (status == AudioPlaybackConnectionOpenResultStatus::Success) {
      std::lock_guard<std::mutex> lock(g_mutex);
      auto [it, inserted] = g_connections.emplace(std::move(id), connection);
      if (!inserted) {
        // Another thread connected the same device first.
        SetLastError(L"Already connected to this device.");
        connection.Close();
        return A2DP_RESULT_ALREADY_CONNECTED;
      }
      return A2DP_RESULT_SUCCESS;
    }

    // Connection failed: record the reason and close.
    switch (status) {
    case AudioPlaybackConnectionOpenResultStatus::RequestTimedOut:
      SetLastError(L"The connection request timed out.");
      break;
    case AudioPlaybackConnectionOpenResultStatus::DeniedBySystem:
      SetLastError(L"The operation was denied by the system.");
      break;
    default:
      SetLastError(L"Unknown failure",
                   static_cast<uint32_t>(result.ExtendedError()));
      break;
    }
    connection.Close();
    return static_cast<int>(status);
  } catch (winrt::hresult_error const &ex) {
    SetLastError(ex);
    return A2DP_RESULT_EXCEPTION;
  }
}

extern "C" A2DP_API int A2DP_Disconnect(const wchar_t *deviceId) {
  if (deviceId == nullptr || deviceId[0] == L'\0') {
    SetLastError(L"deviceId must not be null or empty.");
    return A2DP_RESULT_INVALID_ARGUMENT;
  }

  AudioPlaybackConnection connection = nullptr;
  {
    std::lock_guard<std::mutex> lock(g_mutex);
    if (!g_inited) {
      SetLastError(L"A2DP_Init must be called first.");
      return A2DP_RESULT_NOT_INITIALIZED;
    }
    auto it = g_connections.find(std::wstring(deviceId));
    if (it == g_connections.end()) {
      // Not connected is not an error.
      return A2DP_RESULT_SUCCESS;
    }
    connection = it->second;
    g_connections.erase(it);
  }
  connection.Close();
  return A2DP_RESULT_SUCCESS;
}

extern "C" A2DP_API int A2DP_DisconnectAll() {
  std::vector<AudioPlaybackConnection> toClose;
  {
    std::lock_guard<std::mutex> lock(g_mutex);
    if (!g_inited) {
      SetLastError(L"A2DP_Init must be called first.");
      return A2DP_RESULT_NOT_INITIALIZED;
    }
    for (auto const &entry : g_connections) {
      toClose.push_back(entry.second);
    }
    g_connections.clear();
  }
  for (auto const &connection : toClose) {
    connection.Close();
  }
  return A2DP_RESULT_SUCCESS;
}

extern "C" A2DP_API int A2DP_GetConnectionCount() {
  std::lock_guard<std::mutex> lock(g_mutex);
  if (!g_inited) {
    SetLastError(L"A2DP_Init must be called first.");
    return A2DP_RESULT_NOT_INITIALIZED;
  }
  return static_cast<int>(g_connections.size());
}

extern "C" A2DP_API int A2DP_GetLastError(wchar_t *buffer, int bufferSize) {
  if (buffer == nullptr || bufferSize <= 0) {
    return A2DP_RESULT_INVALID_ARGUMENT;
  }
  std::lock_guard<std::mutex> lock(g_errorMutex);
  if (!CopyString(g_lastError, buffer, bufferSize)) {
    return A2DP_RESULT_INVALID_ARGUMENT;
  }
  return A2DP_RESULT_SUCCESS;
}

#pragma once

/*
 * a2dp_bridge.h -- C interface of the A2DP bridge DLL.
 *
 * Wraps Windows.Media.Audio.AudioPlaybackConnection (WinRT, Windows 11)
 * into a plain synchronous C API so other languages (e.g. Go) can call it.
 *
 * Build (x64, MSVC):
 *   cl.exe /std:c++20 /EHsc /O2 /LD a2dp_bridge.cpp /link /OUT:a2dp_bridge.dll
 *
 * Return value convention (A2DP_Connect and friends):
 *   >= 0  success (0), or the raw AudioPlaybackConnectionOpenResultStatus:
 *           1 = RequestTimedOut, 2 = DeniedBySystem, 3 = UnknownFailure
 *   <  0  local error, see the A2DP_RESULT_* macros below.
 *
 * Usage:
 *   1. A2DP_Init()
 *   2. A2DP_GetDeviceCount() -> A2DP_GetDeviceName(i) / A2DP_GetDeviceId(i)
 *   3. A2DP_Connect(deviceId)
 *   4. A2DP_DisconnectAll() / A2DP_Shutdown() when done
 *
 * Notes:
 *   - Requires Windows 11 (AudioPlaybackConnection needs build 22000+).
 *   - Only Bluetooth audio devices already paired with Windows are listed.
 *   - The device name/id getters return UTF-16 (wchar_t) strings.
 */

#ifdef __cplusplus
extern "C" {
#endif

#if defined(_WIN32) && defined(A2DP_EXPORTS)
#define A2DP_API __declspec(dllexport)
#elif defined(_WIN32)
#define A2DP_API __declspec(dllimport)
#else
#define A2DP_API
#endif

/* ---- Return values ---- */
#define A2DP_RESULT_SUCCESS 0 /* success */
/* Raw AudioPlaybackConnectionOpenResultStatus values */
#define A2DP_RESULT_REQUEST_TIMED_OUT 1
#define A2DP_RESULT_DENIED_BY_SYSTEM 2
#define A2DP_RESULT_UNKNOWN_FAILURE 3
/* Local errors */
#define A2DP_RESULT_NOT_INITIALIZED -1 /* A2DP_Init not called yet */
#define A2DP_RESULT_NOT_SUPPORTED                                              \
  -2 /* no AudioPlaybackConnection on this system (needs Win11) */
#define A2DP_RESULT_INVALID_ARGUMENT -3 /* bad parameter */
#define A2DP_RESULT_CREATE_FAILED -4    /* TryCreateFromId returned null */
#define A2DP_RESULT_ALREADY_CONNECTED -5
#define A2DP_RESULT_EXCEPTION                                                  \
  -6 /* unexpected exception, use A2DP_GetLastError for details */

/* Initialize the WinRT apartment and check AudioPlaybackConnection support.
   After success, later calls may come from any thread (MTA). Thread-safe and
   idempotent. */
A2DP_API int A2DP_Init(void);

/* Close all connections and release resources. Call before process exit. */
A2DP_API int A2DP_Shutdown(void);

/* Re-enumerate Bluetooth audio playback devices and cache them.
   Returns the device count, or a negative error code. */
A2DP_API int A2DP_GetDeviceCount(void);

/* Get the name / ID of the index-th device (call A2DP_GetDeviceCount first).
   buffer is a caller-provided wchar_t buffer, bufferSize its capacity
   (including the trailing NUL). */
A2DP_API int A2DP_GetDeviceName(int index, wchar_t *buffer, int bufferSize);
A2DP_API int A2DP_GetDeviceId(int index, wchar_t *buffer, int bufferSize);

/* Open a connection to the given device (deviceId from A2DP_GetDeviceId).
   Returns an A2DP_RESULT_* value; on failure A2DP_GetLastError gives details.
 */
A2DP_API int A2DP_Connect(const wchar_t *deviceId);

/* Close a specific connection / all connections. */
A2DP_API int A2DP_Disconnect(const wchar_t *deviceId);
A2DP_API int A2DP_DisconnectAll(void);

/* Number of currently active connections. */
A2DP_API int A2DP_GetConnectionCount(void);

/* Copy the last error text (UTF-16) into buffer. */
A2DP_API int A2DP_GetLastError(wchar_t *buffer, int bufferSize);

#ifdef __cplusplus
}
#endif

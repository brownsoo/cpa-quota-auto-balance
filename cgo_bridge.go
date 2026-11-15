package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
    void* ptr;
    size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void* host_ctx, const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response);
typedef void (*cliproxy_host_free_fn)(void* ptr, size_t len);

typedef struct {
    uint32_t abi_version;
    void* host_ctx;
    cliproxy_host_call_fn call;
    cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char* method, uint8_t* request, size_t request_len, cliproxy_buffer* response);
typedef void (*cliproxy_plugin_free_fn)(void* ptr, size_t len);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
    uint32_t abi_version;
    cliproxy_plugin_call_fn call;
    cliproxy_plugin_free_fn free_buffer;
    cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

static int invoke_host_call(cliproxy_host_api host, const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
    if (host.call == NULL) {
        return -1;
    }
    return host.call(host.host_ctx, method, request, request_len, response);
}

static void invoke_host_free(cliproxy_host_api host, void* ptr, size_t len) {
    if (host.free_buffer != NULL && ptr != NULL) {
        host.free_buffer(ptr, len);
    }
}

extern int cliproxyPluginCall(char* method, uint8_t* request, size_t request_len, cliproxy_buffer* response);
extern void cliproxyPluginFree(void* ptr, size_t len);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"
)

var (
	hostMu     sync.RWMutex
	cachedHost C.cliproxy_host_api

	globalEngine = NewBalanceEngine(HostClient{
		AuthList: hostAuthList,
		AuthGet:  hostAuthGet,
		AuthSave: hostAuthSave,
		HTTPDo:   hostHTTPDo,
	})
)

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if host == nil || plugin == nil {
		return -1
	}
	if uint32(host.abi_version) != ABIVersion {
		return -2
	}

	hostMu.Lock()
	cachedHost = *host
	hostMu.Unlock()

	plugin.abi_version = C.uint32_t(ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if method == nil || response == nil {
		return -1
	}

	methodName := C.GoString(method)
	var reqBytes []byte
	if request != nil && requestLen > 0 {
		reqBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}

	result, err := dispatchPluginMethod(methodName, reqBytes)
	var payload []byte
	if err != nil {
		payload, _ = json.Marshal(Envelope{
			OK: false,
			Error: &ErrorInfo{
				Code:    "plugin_error",
				Message: err.Error(),
			},
		})
	} else {
		rawResult, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			payload, _ = json.Marshal(Envelope{
				OK: false,
				Error: &ErrorInfo{
					Code:    "marshal_error",
					Message: marshalErr.Error(),
				},
			})
		} else {
			payload, _ = json.Marshal(Envelope{
				OK:     true,
				Result: rawResult,
			})
		}
	}

	writeCBuffer(response, payload)
	if err != nil {
		return 1
	}
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, _ C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	globalEngine.Shutdown()
}

func writeCBuffer(buf *C.cliproxy_buffer, data []byte) {
	if len(data) == 0 {
		buf.ptr = nil
		buf.len = 0
		return
	}
	buf.ptr = C.CBytes(data)
	buf.len = C.size_t(len(data))
}

func dispatchPluginMethod(method string, reqBytes []byte) (any, error) {
	switch method {
	case MethodPluginRegister:
		var req RegistrationRequest
		if len(reqBytes) > 0 {
			_ = json.Unmarshal(reqBytes, &req)
		}
		cfg := ParseConfig(req.Config, req.ConfigYAML)
		globalEngine.Configure(req.PluginDir, cfg, false)

		return RegistrationResponse{
			Metadata: Metadata{
				ID:           PluginID,
				Name:         PluginName,
				Version:      PluginVersion,
				Description:  "Automatically balances credential priority and weight based on remaining quota windows, preserving manual settings.",
				Capabilities: []string{CapabilityManagementAPI},
				ConfigFields: PluginConfigFields(),
			},
		}, nil

	case MethodPluginReconfigure:
		var req ReconfigureRequest
		if len(reqBytes) > 0 {
			_ = json.Unmarshal(reqBytes, &req)
		}
		cfg := ParseConfig(req.Config, req.ConfigYAML)
		globalEngine.Configure(req.PluginDir, cfg, true)
		return ReconfigureResponse{Applied: true}, nil

	case MethodManagementRegister:
		return BuildManagementRegistration(), nil

	case MethodManagementHandle:
		var req ManagementRequest
		if len(reqBytes) > 0 {
			if err := json.Unmarshal(reqBytes, &req); err != nil {
				return nil, fmt.Errorf("invalid management request: %w", err)
			}
		}
		return globalEngine.HandleManagementRequest(req), nil

	default:
		return nil, fmt.Errorf("unsupported plugin method: %s", method)
	}
}

func callHost(method string, reqPayload any, respOut any) error {
	hostMu.RLock()
	h := cachedHost
	hostMu.RUnlock()

	if h.call == nil {
		return fmt.Errorf("host API is not initialized")
	}

	var reqBytes []byte
	if reqPayload != nil {
		var err error
		reqBytes, err = json.Marshal(reqPayload)
		if err != nil {
			return fmt.Errorf("marshal host request: %w", err)
		}
	}

	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var reqPtr *C.uint8_t
	if len(reqBytes) > 0 {
		cReq := C.CBytes(reqBytes)
		defer C.free(cReq)
		reqPtr = (*C.uint8_t)(cReq)
	}

	var respBuf C.cliproxy_buffer
	rc := C.invoke_host_call(h, cMethod, reqPtr, C.size_t(len(reqBytes)), &respBuf)
	defer C.invoke_host_free(h, respBuf.ptr, respBuf.len)

	if respBuf.ptr == nil || respBuf.len == 0 {
		if rc != 0 {
			return fmt.Errorf("host call %s failed with code %d", method, int(rc))
		}
		return nil
	}

	rawBytes := C.GoBytes(respBuf.ptr, C.int(respBuf.len))
	var env Envelope
	if err := json.Unmarshal(rawBytes, &env); err != nil {
		return fmt.Errorf("decode host envelope from %s: %w", method, err)
	}
	if !env.OK {
		if env.Error != nil {
			return fmt.Errorf("%s: %s", method, env.Error.Message)
		}
		return fmt.Errorf("host call %s returned ok=false", method)
	}
	if respOut != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, respOut); err != nil {
			return fmt.Errorf("decode host result from %s: %w", method, err)
		}
	}
	return nil
}

func hostAuthList() ([]HostAuthFileEntry, error) {
	var resp HostAuthListResponse
	if err := callHost(HostMethodAuthList, map[string]any{}, &resp); err != nil {
		return nil, err
	}
	return resp.Files, nil
}

func hostAuthGet(name string) (*HostAuthGetResponse, error) {
	var resp HostAuthGetResponse
	if err := callHost(HostMethodAuthGet, HostAuthGetRequest{Name: name}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func hostAuthSave(req HostAuthSaveRequest) (*HostAuthSaveResponse, error) {
	var resp HostAuthSaveResponse
	if err := callHost(HostMethodAuthSave, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func hostHTTPDo(req HostHTTPRequest) (*HostHTTPResponse, error) {
	var resp HostHTTPResponse
	if err := callHost(HostMethodHTTPDo, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func main() {}

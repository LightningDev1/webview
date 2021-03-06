package webview

/*
#cgo linux openbsd freebsd CXXFLAGS: -DWEBVIEW_GTK -std=c++11
#cgo linux openbsd freebsd pkg-config: gtk+-3.0 webkit2gtk-4.0

#cgo darwin CXXFLAGS: -DWEBVIEW_COCOA -std=c++11
#cgo darwin LDFLAGS: -framework WebKit

#cgo windows CXXFLAGS: -std=c++11
#cgo windows,amd64 LDFLAGS: -L./dll/x64 -lwebview -lWebView2Loader
#cgo windows,386 LDFLAGS: -L./dll/x86 -lwebview -lWebView2Loader

#define WEBVIEW_HEADER
#include "webview.h"

#include <stdlib.h>
#include <stdint.h>

extern void _webviewDispatchGoCallback(void *);
static inline void _webview_dispatch_cb(webview_t w, void *arg) {
	_webviewDispatchGoCallback(arg);
}
static inline void CgoWebViewDispatch(webview_t w, uintptr_t arg) {
	webview_dispatch(w, _webview_dispatch_cb, (void *)arg);
}

struct binding_context {
	webview_t w;
	uintptr_t index;
};
extern void _webviewBindingGoCallback(webview_t, char *, char *, uintptr_t);
static inline void _webview_binding_cb(const char *id, const char *req, void *arg) {
	struct binding_context *ctx = (struct binding_context *) arg;
	_webviewBindingGoCallback(ctx->w, (char *)id, (char *)req, ctx->index);
}
static inline void CgoWebViewBind(webview_t w, const char *name, uintptr_t index) {
	struct binding_context *ctx = calloc(1, sizeof(struct binding_context));
	ctx->w = w;
	ctx->index = index;
	webview_bind(w, name, _webview_binding_cb, (void *)ctx);
}
*/
import "C"
import (
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"unsafe"
)

func init() {
	// Ensure that main.main is called from the main thread
	runtime.LockOSThread()
}

// Hints are used to configure window sizing and resizing
type Hint int

const (
	// Width and height are default size
	HintNone = C.WEBVIEW_HINT_NONE

	// Window size can not be changed by a user
	HintFixed = C.WEBVIEW_HINT_FIXED

	// Width and height are minimum bounds
	HintMin = C.WEBVIEW_HINT_MIN

	// Width and height are maximum bounds
	HintMax = C.WEBVIEW_HINT_MAX
)

type WebView interface {

	// Run runs the main loop until it's terminated. After this function exits -
	// you must destroy the webview.
	Run()

	// Terminate stops the main loop. It is safe to call this function from
	// a background thread.
	Terminate()

	// Dispatch posts a function to be executed on the main thread. You normally
	// do not need to call this function, unless you want to tweak the native
	// window.
	Dispatch(f func())

	// Destroy destroys a webview and closes the native window.
	Destroy()

	// Window returns a native window handle pointer. When using GTK backend the
	// pointer is GtkWindow pointer, when using Cocoa backend the pointer is
	// NSWindow pointer, when using Win32 backend the pointer is HWND pointer.
	Window() unsafe.Pointer

	// Show shows the window when it's hidden
	Show()

	// Hide hides the webview window
	Hide()

	// Minimize the window
	Minimize()

	// Maximize the window
	Maximize()

	// HideToSystemTray hides the window to the system tray. The window will be shown
	// again when the icon in the system tray is clicked. You must call SetIcon or
	// SetIconFromFile before using this as it won't work without an icon.
	// This only works on Windows.
	HideToSystemTray()

	// SetTitle updates the title of the native window. Must be called from the UI
	// thread.
	SetTitle(title string)

	// SetSize updates native window size. See Hint constants.
	SetSize(w int, h int, hint Hint)

	// SetIcon sets the window icon
	SetIcon(iconBytes []byte)

	// SetIcon sets the window icon
	SetIconFromFile(iconFile string)

	// Navigate navigates webview to the given URL. URL may be a data URI, i.e.
	// "data:text/text,<html>...</html>". It is often ok not to url-encode it
	// properly, webview will re-encode it for you.
	Navigate(url string)

	// Init injects JavaScript code at the initialization of the new page. Every
	// time the webview will open a the new page - this initialization code will
	// be executed. It is guaranteed that code is executed before window.onload.
	Init(js string)

	// Eval evaluates arbitrary JavaScript code. Evaluation happens asynchronously,
	// also the result of the expression is ignored. Use RPC bindings if you want
	// to receive notifications about the results of the evaluation.
	Eval(js string)

	// Bind binds a callback function so that it will appear under the given name
	// as a global JavaScript function. Internally it uses webview_init().
	// Callback receives a request string and a user-provided argument pointer.
	// Request string is a JSON array of all the arguments passed to the
	// JavaScript function.
	//
	// f must be a function
	// f must return either value and error or just error
	Bind(name string, f interface{}) error

	// Create the JS SendEvent function that will callback to Go with eventName and eventData (JSON) so
	// we can use non-web stuff like creating or reading files. A FILE_READ example event is supplied,
	// you can add events like how it's shown there.
	// Usage in JavaScript:
	// window.SendEvent("FILE_READ", JSON.stringify({
	// 	file: "some_file.txt",
	// 	base64: false
	// })).then(data => {
	// 	const JsonData = JSON.parse(data);
	// 	if (JsonData.error) {
	// 		return console.error("An error has occurred: " + JsonData.message);
	// 	}
	// 	console.log("File Contents: " + JsonData["fileContents"]);
	// });
	InitMessageHandler()

	// Add a message handler. Must first call InitMessageHandler().
	AddMessageHandler(eventName string, f func(message string, messageData string) string)

	// Send a message event to javascript. You can use it with this code in JS:
	// window.addEventListener("webview_message", (eventName, eventData) => {
	//     console.log(eventName);
	//     console.log(eventData);
	// });
	SendMessage(eventName string, eventData string)
}

type webview struct {
	w C.webview_t
}

var (
	m                   sync.Mutex
	index               uintptr
	dispatch            = map[uintptr]func(){}
	bindings            = map[uintptr]func(id, req string) (interface{}, error){}
	messageHandlerMutex sync.Mutex
	messageHandlers     = map[string]func(message string, messageData string) string{}
)

func boolToInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// New calls NewWindow to create a new window and a new webview instance. If debug
// is non-zero - developer tools will be enabled (if the platform supports them).
func New(width int, height int, title string, debug bool) WebView {
	return NewWindow(width, height, title, debug, nil)
}

// NewWindow creates a new webview instance. If debug is non-zero - developer
// tools will be enabled (if the platform supports them). Window parameter can be
// a pointer to the native window handle. If it's non-null - then child WebView is
// embedded into the given parent window. Otherwise a new window is created.
// Depending on the platform, a GtkWindow, NSWindow or HWND pointer can be passed
// here.
func NewWindow(width int, height int, title string, debug bool, window unsafe.Pointer) WebView {
	s := C.CString(title)
	defer C.free(unsafe.Pointer(s))
	w := &webview{}
	w.w = C.webview_create(C.int(width), C.int(height), s, boolToInt(debug), window)
	return w
}

func (w *webview) Destroy() {
	C.webview_destroy(w.w)
}

func (w *webview) Run() {
	C.webview_run(w.w)
}

func (w *webview) Terminate() {
	C.webview_terminate(w.w)
}

func (w *webview) Window() unsafe.Pointer {
	return C.webview_get_window(w.w)
}

func (w *webview) Show() {
	C.webview_show(w.w)
}

func (w *webview) Hide() {
	C.webview_hide(w.w)
}

func (w *webview) Minimize() {
	C.webview_minimize(w.w)
}

func (w *webview) Maximize() {
	C.webview_maximize(w.w)
}

func (w *webview) HideToSystemTray() {
	C.webview_hide_to_system_tray(w.w)
}

func (w *webview) Navigate(url string) {
	s := C.CString(url)
	defer C.free(unsafe.Pointer(s))
	C.webview_navigate(w.w, s)
}

func (w *webview) SetTitle(title string) {
	s := C.CString(title)
	defer C.free(unsafe.Pointer(s))
	C.webview_set_title(w.w, s)
}

func (w *webview) SetSize(width int, height int, hint Hint) {
	C.webview_set_size(w.w, C.int(width), C.int(height), C.int(hint))
}

func (w *webview) SetIcon(iconBytes []byte) {
	C.webview_set_icon(w.w, unsafe.Pointer(&iconBytes[0]), C.int(len(iconBytes)))
}

func (w *webview) SetIconFromFile(iconFile string) {
	s := C.CString(iconFile)
	defer C.free(unsafe.Pointer(s))
	C.webview_set_icon_from_file(w.w, s)
}

func (w *webview) Init(js string) {
	s := C.CString(js)
	defer C.free(unsafe.Pointer(s))
	C.webview_init(w.w, s)
}

func (w *webview) Eval(js string) {
	s := C.CString(js)
	defer C.free(unsafe.Pointer(s))
	C.webview_eval(w.w, s)
}

func (w *webview) Dispatch(f func()) {
	m.Lock()
	for ; dispatch[index] != nil; index++ {
	}
	dispatch[index] = f
	m.Unlock()
	C.CgoWebViewDispatch(w.w, C.uintptr_t(index))
}

//export _webviewDispatchGoCallback
func _webviewDispatchGoCallback(index unsafe.Pointer) {
	m.Lock()
	f := dispatch[uintptr(index)]
	delete(dispatch, uintptr(index))
	m.Unlock()
	f()
}

//export _webviewBindingGoCallback
func _webviewBindingGoCallback(w C.webview_t, id *C.char, req *C.char, index uintptr) {
	m.Lock()
	f := bindings[uintptr(index)]
	m.Unlock()
	jsString := func(v interface{}) string { b, _ := json.Marshal(v); return string(b) }
	status, result := 0, ""
	if res, err := f(C.GoString(id), C.GoString(req)); err != nil {
		status = -1
		result = jsString(err.Error())
	} else if b, err := json.Marshal(res); err != nil {
		status = -1
		result = jsString(err.Error())
	} else {
		status = 0
		result = string(b)
	}
	s := C.CString(result)
	defer C.free(unsafe.Pointer(s))
	C.webview_return(w, id, C.int(status), s)
}

func (w *webview) Bind(name string, f interface{}) error {
	v := reflect.ValueOf(f)
	// f must be a function
	if v.Kind() != reflect.Func {
		return errors.New("only functions can be bound")
	}
	// f must return either value and error or just error
	if n := v.Type().NumOut(); n > 2 {
		return errors.New("function may only return a value or a value+error")
	}

	binding := func(id, req string) (interface{}, error) {
		raw := []json.RawMessage{}
		if err := json.Unmarshal([]byte(req), &raw); err != nil {
			return nil, err
		}

		isVariadic := v.Type().IsVariadic()
		numIn := v.Type().NumIn()
		if (isVariadic && len(raw) < numIn-1) || (!isVariadic && len(raw) != numIn) {
			return nil, errors.New("function arguments mismatch")
		}
		args := []reflect.Value{}
		for i := range raw {
			var arg reflect.Value
			if isVariadic && i >= numIn-1 {
				arg = reflect.New(v.Type().In(numIn - 1).Elem())
			} else {
				arg = reflect.New(v.Type().In(i))
			}
			if err := json.Unmarshal(raw[i], arg.Interface()); err != nil {
				return nil, err
			}
			args = append(args, arg.Elem())
		}
		errorType := reflect.TypeOf((*error)(nil)).Elem()
		res := v.Call(args)
		switch len(res) {
		case 0:
			// No results from the function, just return nil
			return nil, nil
		case 1:
			// One result may be a value, or an error
			if res[0].Type().Implements(errorType) {
				if res[0].Interface() != nil {
					return nil, res[0].Interface().(error)
				}
				return nil, nil
			}
			return res[0].Interface(), nil
		case 2:
			// Two results: first one is value, second is error
			if !res[1].Type().Implements(errorType) {
				return nil, errors.New("second return value must be an error")
			}
			if res[1].Interface() == nil {
				return res[0].Interface(), nil
			}
			return res[0].Interface(), res[1].Interface().(error)
		default:
			return nil, errors.New("unexpected number of return values")
		}
	}

	m.Lock()
	for ; bindings[index] != nil; index++ {
	}
	bindings[index] = binding
	m.Unlock()
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	C.CgoWebViewBind(w.w, cname, C.uintptr_t(index))
	return nil
}

func (w *webview) InitMessageHandler() {
	w.Bind("_SendEvent", func(eventName string, eventData string) string {
		messageHandlerMutex.Lock()
		f := messageHandlers[eventName]
		messageHandlerMutex.Unlock()
		if f != nil {
			return f(eventName, eventData)
		}
		return ""
	})

	// Move the SendEvent function to window and "hook" window.addEventListener and
	// window.removeEventListener so we can use them for sending events from Go to JS
	w.Init(`
	window.SendEvent = _SendEvent;
	delete _SendEvent;

	window.oldAddEventListener = window.addEventListener;
	window.oldRemoveEventListener = window.removeEventListener;
	window.webViewEventListeners = [];
	window.addEventListener = (event, func, ...args) => {
		if (event === "webview_message") {
			window.webViewEventListeners.push(func);
		}
		else {
			return window.oldAddEventListener(event, func, ...args)
		}
	}
	window.removeEventListener = (event, func, ...args) => {
		if (event === "webview_message") {
			window.webViewEventListeners.filter((_func) => _func !== func);
		}
		else {
			return window.oldRemoveEventListener(event, func, ...args)
		}
	}
	`)
}

func (w *webview) AddMessageHandler(eventName string, f func(message string, messageData string) string) {
	messageHandlerMutex.Lock()
	messageHandlers[eventName] = f
	messageHandlerMutex.Unlock()
}

func (w *webview) SendMessage(eventName string, eventData string) {
	// json.Marshal will escape the strings so you can use stuff like quotes
	// without breaking out of the string in JS
	jsonEventName, _ := json.Marshal(eventName)
	jsonEventData, _ := json.Marshal(eventData)
	w.Eval(`
	window.webViewEventListeners.forEach(eventListener => {
		eventListener(` + string(jsonEventName) + `, ` + string(jsonEventData) + `);
	})
	`)
}

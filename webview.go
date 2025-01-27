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
	"strings"
	"sync"
	"unsafe"
	// App
	"net"
	"net/http"
	"io/fs"
	"strconv"
	"embed"
	"fmt"
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

	// SetTitle updates the title of the native window. Must be called from the UI
	// thread.
	SetTitle(title string)

	// SetSize updates native window size. See Hint constants.
	SetSize(w int, h int, hint Hint)

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

	// Topmost forces a window to float above all other windows
	Topmost(makeTopmost ...bool)

	// SetPosition updates the position of the native window
	SetPosition(x int, y int)

	// Center centers the window relative to the primary monitor
	Center()

	// NoCtx, removes the default right click context menu in the webview
	NoCtx()
}

type webview struct {
	w C.webview_t
}

var (
	m        sync.Mutex
	index    uintptr
	dispatch = map[uintptr]func(){}
	bindings = map[uintptr]func(id, req string) (interface{}, error){}
)

func boolToInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// New calls NewWindow to create a new window and a new webview instance. If debug
// is non-zero - developer tools will be enabled (if the platform supports them).
func New(debug ...bool) WebView {
	d := false
	if len(debug) > 0 {
		d = debug[0]
	}
	return NewWindow(d, nil)
}

// NewWindow creates a new webview instance. If debug is non-zero - developer
// tools will be enabled (if the platform supports them). Window parameter can be
// a pointer to the native window handle. If it's non-null - then child WebView is
// embedded into the given parent window. Otherwise a new window is created.
// Depending on the platform, a GtkWindow, NSWindow or HWND pointer can be passed
// here.
func NewWindow(debug bool, window unsafe.Pointer) WebView {
	w := &webview{}
	w.w = C.webview_create(boolToInt(debug), window)
	return w
}

// EscapeJs is a helper function that escapes characters in html/js code that would otherwise be removed by url_decode, causing errors
// if your url is as data:text/html, with a script tag containing the characters + or %, use this function and pass its output to Navigate
func EscapeJs(js string) string {
	length := len(js)
	var output strings.Builder
	for i := 0; i < length; i++ {
		if js[i] == '+' {
			output.WriteString("%2b")
		} else if js[i] == '%' {
			output.WriteString("%25")
		} else {
			output.WriteByte(js[i])
		}
	}

	return output.String()
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

func (w *webview) Topmost(makeTopmost ...bool) {
	var setting C.int
    if len(makeTopmost) > 0 {
        if t := makeTopmost[0]; t {
            setting = 1
        } else {
            setting = 0
        }
    }

	C.webview_topmost(w.w, setting)
}

func (w *webview) SetPosition(x int, y int) {
	C.webview_set_position(w.w, C.int(x), C.int(y))
}

func (w *webview) Center() {
	C.webview_center(w.w)
}
func (w *webview) NoCtx() {
	C.webview_no_ctx(w.w)
}

type RouteFunc = func(http.ResponseWriter, *http.Request)

type App struct {
	Width int
	Height int
	Title string
	Content embed.FS
	ContentRoot string
	Init func(WebView)
	ServerInit func()
	Routes map[string] RouteFunc
	Topmost bool
	Debug bool
	server *http.ServeMux
}

func (app App) Run() {
	portChannel := make(chan string)
	go serve(&app, portChannel)

	runtime.LockOSThread()
	w := New(app.Debug)
	defer w.Destroy()
	if app.Width == 0 {
		app.Width = 500
	}
	if app.Height == 0 {
		app.Height = 500
	}
	w.SetSize(app.Width, app.Height, HintFixed)
	w.Center()
	if !app.Debug {
		w.NoCtx()
	}
	w.SetTitle(app.Title)
	if app.Topmost {
		w.Topmost(true)
	}
	if (app.Init != nil) {
		app.Init(w)
	}
	port := <- portChannel
	w.Navigate("http://localhost:"+port)

	w.Bind("_webview_log", func(args []string){
		joined := strings.Join(args, " ")
		println(joined)
	})

	w.Init(fmt.Sprintf(`
	const _webview_width=%d;
	const _webview_height=%d;
	const _webview={
		width:_webview_width,
		height:_webview_height,
		title:'%s',
		address:'http://localhost:%s/',
		log(){
			const args = Array.from(arguments).map(a=>typeof a == 'object' ? JSON.stringify(a) : a.toString());
			_webview_log(args);
		}
	};
	Object.freeze(_webview);

	window.addEventListener("load", ()=>{
		document.body.style.cssText += 'margin:0px;overflow:hidden;'
	})
	`, app.Width, app.Height, app.Title, port))
	w.Run()
}

func serve(app *App, portChannel chan<- string) {
	if app.server == nil {
		app.server = http.NewServeMux()
	}

	content, _ := fs.Sub(app.Content, app.ContentRoot)

	app.server.Handle("/", http.FileServer(http.FS(content)))

	if app.ServerInit != nil {
		app.ServerInit()
	}
	// handle routes
	if app.Routes != nil {
		for route, handler := range app.Routes {
			wrapper := func(w http.ResponseWriter, r *http.Request) {
				// set headers to allow fetch locally
				w.Header().Set("Access-Control-Allow-Origin", "*")
				w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRFToken, Authorization")
				// call the actual handler function
				handler(w,r)
			}
			app.server.HandleFunc(route, wrapper)
		}
	}

	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}

	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	portChannel <- port
	close(portChannel)
	http.Serve(listener, app.server)
}

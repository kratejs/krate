//go:build linux

package linux

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

// api is the function table for one GTK/WebKit ABI. This file fills it for
// GTK3 + WebKitGTK 4.1; a GTK4 + WebKitGTK 6.0 table can be added next to
// loadAPI41 with the same field set.
type api struct {
	name string // e.g. "webkit2gtk-4.1"

	// glib, gobject, gio
	gIdleAddFull        func(prio int32, fn, data, notify uintptr) uint32
	gSignalConnectData  func(inst uintptr, sig string, cb, data, notify uintptr, flags int32) uint64
	gObjectRef          func(p uintptr) uintptr
	gObjectUnref        func(p uintptr)
	gFree               func(p uintptr)
	gQuarkFromString    func(s string) uint32
	gErrorNewLiteral    func(domain uint32, code int32, msg string) uintptr
	gErrorFree          func(e uintptr)
	gSetPrgname         func(s string)
	gSetApplicationName func(s string)
	gInputStreamRead    func(stream uintptr, buf unsafe.Pointer, count uint64, cancel uintptr, gerr *uintptr) int64
	gInputStreamClose   func(stream, cancel uintptr, gerr *uintptr) int32
	gUnixInputStreamNew func(fd int32, closeFd int32) uintptr

	// libsoup
	soupHeadersNew     func(kind int32) uintptr
	soupHeadersAppend  func(h uintptr, name, value string)
	soupHeadersForeach func(h uintptr, cb, data uintptr)

	// gtk
	gtkInitCheck        func(argc, argv uintptr) int32
	gtkMain             func()
	gtkMainQuit         func()
	gtkWindowNew        func(kind int32) uintptr
	gtkWindowSetTitle   func(w uintptr, title string)
	gtkWindowGetTitle   func(w uintptr) uintptr
	gtkWindowSetDefSize func(w uintptr, width, height int32)
	gtkWindowResize     func(w uintptr, width, height int32)
	gtkWindowGetSize    func(w uintptr, width, height *int32)
	gtkWindowSetResize  func(w uintptr, resizable int32)
	gtkContainerAdd     func(c, child uintptr)
	gtkWidgetShowAll    func(w uintptr)
	gtkWidgetDestroy    func(w uintptr)
	gtkWidgetSetSizeReq func(w uintptr, width, height int32)

	// webkit
	wkMajor, wkMinor, wkMicro func() uint32

	wkDataManagerNew       func(k1, v1, k2, v2 string, end uintptr) uintptr
	wkContextNewWithMgr    func(mgr uintptr) uintptr
	wkContextRegisterSchem func(ctx uintptr, scheme string, cb, data, notify uintptr)
	wkContextSecurityMgr   func(ctx uintptr) uintptr
	wkSecSecure            func(sm uintptr, scheme string)
	wkSecCORS              func(sm uintptr, scheme string)
	wkSecLocal             func(sm uintptr, scheme string)

	wkReqURI         func(r uintptr) uintptr
	wkReqMethod      func(r uintptr) uintptr
	wkReqHeaders     func(r uintptr) uintptr
	wkReqBody        func(r uintptr) uintptr
	wkReqFinish      func(r, resp uintptr)
	wkReqFinishError func(r, gerr uintptr)

	wkRespNew        func(stream uintptr, length int64) uintptr
	wkRespSetStatus  func(resp uintptr, status uint32, reason uintptr)
	wkRespSetCType   func(resp uintptr, ctype string)
	wkRespSetHeaders func(resp, headers uintptr)

	wkViewNewWithContext func(ctx uintptr) uintptr
	wkViewLoadURI        func(v uintptr, uri string)
	wkViewGetSettings    func(v uintptr) uintptr
	wkViewEval           func(v uintptr, script string, length int64, world, srcURI, cancel, cb, data uintptr)
	wkSettingsDevExtras  func(s uintptr, on int32)
	wkViewGetInspector   func(v uintptr) uintptr
	wkInspectorShow      func(i uintptr)

	wkNavDecisionAction func(d uintptr) uintptr
	wkNavActionRequest  func(a uintptr) uintptr
	wkURIRequestURI     func(r uintptr) uintptr
	wkPolicyIgnore      func(d uintptr)
}

const (
	rtldNow    = 0x2
	rtldGlobal = 0x100

	minMajor, minMinor = 2, 40
)

// MissingLibError reports shared libraries that could not be loaded.
type MissingLibError struct {
	Lib     string
	Package string
	Err     error
}

func (e *MissingLibError) Error() string {
	return fmt.Sprintf("krate: cannot load %s (%v); on Debian/Ubuntu install the %s package", e.Lib, e.Err, e.Package)
}

func (e *MissingLibError) Unwrap() error { return e.Err }

func openLib(name, pkg string) (uintptr, error) {
	h, err := purego.Dlopen(name, rtldNow|rtldGlobal)
	if err != nil {
		return 0, &MissingLibError{Lib: name, Package: pkg, Err: err}
	}
	return h, nil
}

type binder struct {
	missing []string
}

func (b *binder) bind(fn any, lib uintptr, sym string) {
	if _, err := purego.Dlsym(lib, sym); err != nil {
		b.missing = append(b.missing, sym)
		return
	}
	purego.RegisterLibFunc(fn, lib, sym)
}

// loadAPI41 loads GTK 3 and WebKitGTK 4.1 (libsoup 3).
func loadAPI41() (*api, error) {
	const wkPkg = "libwebkit2gtk-4.1-0"
	glib, err := openLib("libglib-2.0.so.0", "libglib2.0-0")
	if err != nil {
		return nil, err
	}
	gobj, err := openLib("libgobject-2.0.so.0", "libglib2.0-0")
	if err != nil {
		return nil, err
	}
	gio, err := openLib("libgio-2.0.so.0", "libglib2.0-0")
	if err != nil {
		return nil, err
	}
	soup, err := openLib("libsoup-3.0.so.0", "libsoup-3.0-0")
	if err != nil {
		return nil, err
	}
	gtk, err := openLib("libgtk-3.so.0", "libgtk-3-0")
	if err != nil {
		return nil, err
	}
	wk, err := openLib("libwebkit2gtk-4.1.so.0", wkPkg)
	if err != nil {
		return nil, err
	}

	a := &api{name: "webkit2gtk-4.1"}
	b := &binder{}

	b.bind(&a.gIdleAddFull, glib, "g_idle_add_full")
	b.bind(&a.gFree, glib, "g_free")
	b.bind(&a.gQuarkFromString, glib, "g_quark_from_string")
	b.bind(&a.gErrorNewLiteral, glib, "g_error_new_literal")
	b.bind(&a.gErrorFree, glib, "g_error_free")
	b.bind(&a.gSetPrgname, glib, "g_set_prgname")
	b.bind(&a.gSetApplicationName, glib, "g_set_application_name")
	b.bind(&a.gSignalConnectData, gobj, "g_signal_connect_data")
	b.bind(&a.gObjectRef, gobj, "g_object_ref")
	b.bind(&a.gObjectUnref, gobj, "g_object_unref")
	b.bind(&a.gInputStreamRead, gio, "g_input_stream_read")
	b.bind(&a.gInputStreamClose, gio, "g_input_stream_close")
	// g_unix_input_stream_new lives in libgio.
	b.bind(&a.gUnixInputStreamNew, gio, "g_unix_input_stream_new")

	b.bind(&a.soupHeadersNew, soup, "soup_message_headers_new")
	b.bind(&a.soupHeadersAppend, soup, "soup_message_headers_append")
	b.bind(&a.soupHeadersForeach, soup, "soup_message_headers_foreach")

	b.bind(&a.gtkInitCheck, gtk, "gtk_init_check")
	b.bind(&a.gtkMain, gtk, "gtk_main")
	b.bind(&a.gtkMainQuit, gtk, "gtk_main_quit")
	b.bind(&a.gtkWindowNew, gtk, "gtk_window_new")
	b.bind(&a.gtkWindowSetTitle, gtk, "gtk_window_set_title")
	b.bind(&a.gtkWindowGetTitle, gtk, "gtk_window_get_title")
	b.bind(&a.gtkWindowSetDefSize, gtk, "gtk_window_set_default_size")
	b.bind(&a.gtkWindowResize, gtk, "gtk_window_resize")
	b.bind(&a.gtkWindowGetSize, gtk, "gtk_window_get_size")
	b.bind(&a.gtkWindowSetResize, gtk, "gtk_window_set_resizable")
	b.bind(&a.gtkContainerAdd, gtk, "gtk_container_add")
	b.bind(&a.gtkWidgetShowAll, gtk, "gtk_widget_show_all")
	b.bind(&a.gtkWidgetDestroy, gtk, "gtk_widget_destroy")
	b.bind(&a.gtkWidgetSetSizeReq, gtk, "gtk_widget_set_size_request")

	b.bind(&a.wkMajor, wk, "webkit_get_major_version")
	b.bind(&a.wkMinor, wk, "webkit_get_minor_version")
	b.bind(&a.wkMicro, wk, "webkit_get_micro_version")
	b.bind(&a.wkDataManagerNew, wk, "webkit_website_data_manager_new")
	b.bind(&a.wkContextNewWithMgr, wk, "webkit_web_context_new_with_website_data_manager")
	b.bind(&a.wkContextRegisterSchem, wk, "webkit_web_context_register_uri_scheme")
	b.bind(&a.wkContextSecurityMgr, wk, "webkit_web_context_get_security_manager")
	b.bind(&a.wkSecSecure, wk, "webkit_security_manager_register_uri_scheme_as_secure")
	b.bind(&a.wkSecCORS, wk, "webkit_security_manager_register_uri_scheme_as_cors_enabled")
	b.bind(&a.wkSecLocal, wk, "webkit_security_manager_register_uri_scheme_as_local")
	b.bind(&a.wkReqURI, wk, "webkit_uri_scheme_request_get_uri")
	b.bind(&a.wkReqMethod, wk, "webkit_uri_scheme_request_get_http_method")
	b.bind(&a.wkReqHeaders, wk, "webkit_uri_scheme_request_get_http_headers")
	b.bind(&a.wkReqBody, wk, "webkit_uri_scheme_request_get_http_body")
	b.bind(&a.wkReqFinish, wk, "webkit_uri_scheme_request_finish_with_response")
	b.bind(&a.wkReqFinishError, wk, "webkit_uri_scheme_request_finish_error")
	b.bind(&a.wkRespNew, wk, "webkit_uri_scheme_response_new")
	b.bind(&a.wkRespSetStatus, wk, "webkit_uri_scheme_response_set_status")
	b.bind(&a.wkRespSetCType, wk, "webkit_uri_scheme_response_set_content_type")
	b.bind(&a.wkRespSetHeaders, wk, "webkit_uri_scheme_response_set_http_headers")
	b.bind(&a.wkViewNewWithContext, wk, "webkit_web_view_new_with_context")
	b.bind(&a.wkViewLoadURI, wk, "webkit_web_view_load_uri")
	b.bind(&a.wkViewGetSettings, wk, "webkit_web_view_get_settings")
	b.bind(&a.wkViewEval, wk, "webkit_web_view_evaluate_javascript")
	b.bind(&a.wkSettingsDevExtras, wk, "webkit_settings_set_enable_developer_extras")
	b.bind(&a.wkViewGetInspector, wk, "webkit_web_view_get_inspector")
	b.bind(&a.wkInspectorShow, wk, "webkit_web_inspector_show")
	b.bind(&a.wkNavDecisionAction, wk, "webkit_navigation_policy_decision_get_navigation_action")
	b.bind(&a.wkNavActionRequest, wk, "webkit_navigation_action_get_request")
	b.bind(&a.wkURIRequestURI, wk, "webkit_uri_request_get_uri")
	b.bind(&a.wkPolicyIgnore, wk, "webkit_policy_decision_ignore")

	if len(b.missing) > 0 {
		// A WebKitGTK older than 2.40 lacks evaluate_javascript and
		// get_http_body; report the version if we can read it.
		return nil, fmt.Errorf("krate: %s is too old or incomplete (missing symbols: %s); WebKitGTK %d.%d or newer is required (package %s)",
			a.name, strings.Join(b.missing, ", "), minMajor, minMinor, wkPkg)
	}
	if v := a.version(); !versionOK(a.wkMajor(), a.wkMinor()) {
		return nil, fmt.Errorf("krate: WebKitGTK %s is too old; %d.%d or newer is required (package %s)", v, minMajor, minMinor, wkPkg)
	}
	return a, nil
}

func versionOK(major, minor uint32) bool {
	return major > minMajor || (major == minMajor && minor >= minMinor)
}

func (a *api) version() string {
	return fmt.Sprintf("%d.%d.%d", a.wkMajor(), a.wkMinor(), a.wkMicro())
}

// goString copies a NUL-terminated C string.
func goString(p uintptr) string {
	if p == 0 {
		return ""
	}
	base := *(*unsafe.Pointer)(unsafe.Pointer(&p))
	n := 0
	for *(*byte)(unsafe.Add(base, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(base), n))
}

var errNoDisplay = errors.New("krate: gtk_init failed; no display available (is DISPLAY or WAYLAND_DISPLAY set?)")

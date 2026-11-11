//go:build linux

package linux

import (
	"unsafe"

	"github.com/ebitengine/purego"
)

// ext holds the optional GTK, GDK and X11 bindings behind the capability
// interfaces. A symbol that is missing leaves its field nil; the feature that
// needs it then reports krate.ErrUnsupported instead of the whole platform
// failing to load.
type ext struct {
	// glib / gobject
	gSlistFree        func(l uintptr)
	gStrfreev         func(p uintptr)
	gSignalEmitByName func(inst uintptr, sig string, arg int32)
	gObjectGetData    func(o uintptr, key string) uintptr
	gObjectSetData    func(o uintptr, key string, data uintptr)

	// gtk window
	gtkWindowKeepAbove    func(w uintptr, on int32)
	gtkWindowFullscreen   func(w uintptr)
	gtkWindowUnfullscreen func(w uintptr)
	gtkWindowDecorated    func(w uintptr, on int32)
	gtkWindowGetDecorated func(w uintptr) int32
	gtkWindowMove         func(w uintptr, x, y int32)
	gtkWindowGetPosition  func(w uintptr, x, y *int32)
	gtkWindowIconify      func(w uintptr)
	gtkWindowDeiconify    func(w uintptr)
	gtkWindowMaximize     func(w uintptr)
	gtkWindowUnmaximize   func(w uintptr)
	gtkWindowPresent      func(w uintptr)
	gtkWindowIsActive     func(w uintptr) int32
	gtkWindowAddAccel     func(w, group uintptr)
	gtkWindowRemoveAccel  func(w, group uintptr)
	gtkWidgetShow         func(w uintptr)
	gtkWidgetHide         func(w uintptr)
	gtkCheckResize        func(c uintptr)
	gtkWidgetGetVisible   func(w uintptr) int32
	gtkWidgetGetWindow    func(w uintptr) uintptr
	gtkWidgetSensitive    func(w uintptr, on int32)
	gtkWidgetAddAccel     func(w uintptr, sig string, group uintptr, key, mods uint32, flags int32)
	gtkWidgetGetParent    func(w uintptr) uintptr
	gdkWindowGetState     func(w uintptr) uint32
	gdkDisplayDefault     func() uintptr
	gdkDisplaySeat        func(d uintptr) uintptr
	gdkSeatPointer        func(s uintptr) uintptr
	gdkDevicePosition     func(dev uintptr, screen *uintptr, x, y *int32)
	gdkWindowGetOrigin    func(w uintptr, x, y *int32) int32

	gtkBoxNew          func(orient, spacing int32) uintptr
	gtkBoxPackStart    func(box, child uintptr, expand, fill int32, pad uint32)
	gtkBoxReorderChild func(box, child uintptr, pos int32)
	gtkContainerRemove func(c, child uintptr)

	// split panes
	gtkPanedNew        func(orient int32) uintptr
	gtkPanedPack1      func(p, child uintptr, resize, shrink int32)
	gtkPanedPack2      func(p, child uintptr, resize, shrink int32)
	gtkPanedSetPos     func(p uintptr, pos int32)
	gtkPanedGetPos     func(p uintptr) int32
	gtkWidgetAllocW    func(w uintptr) int32
	gtkWidgetAllocH    func(w uintptr) int32
	gtkWidgetGrabFocus func(w uintptr)

	// gtk menus
	gtkMenuBarNew       func() uintptr
	gtkMenuNew          func() uintptr
	gtkMenuItemMnemonic func(label string) uintptr
	gtkCheckMenuItem    func(label string) uintptr
	gtkRadioMenuItem    func(group uintptr, label string) uintptr
	gtkRadioGetGroup    func(item uintptr) uintptr
	gtkCheckSetActive   func(item uintptr, on int32)
	gtkCheckGetActive   func(item uintptr) int32
	gtkSeparatorItem    func() uintptr
	gtkMenuItemSubmenu  func(item, sub uintptr)
	gtkMenuItemActivate func(item uintptr)
	gtkMenuShellAppend  func(shell, item uintptr)
	gtkMenuPopupPointer func(menu, ev uintptr)
	gtkMenuPopupRect    func(menu, gdkWin uintptr, rect unsafe.Pointer, rectAnchor, menuAnchor int32, ev uintptr)
	gtkMenuPopdown      func(menu uintptr)
	gtkMenuAttachWidget func(menu, widget uintptr)
	gtkAccelGroupNew    func() uintptr
	gtkAccelParse       func(s string, key *uint32, mods *uint32)

	// file chooser
	gtkFileChooserNative   func(title string, parent uintptr, action int32, accept, cancel string) uintptr
	gtkNativeShow          func(d uintptr)
	gtkNativeHide          func(d uintptr)
	gtkNativeDestroy       func(d uintptr)
	gtkNativeGetTitle      func(d uintptr) uintptr
	gtkNativeModal         func(d uintptr, on int32)
	gtkChooserMultiple     func(c uintptr, on int32)
	gtkChooserGetMultiple  func(c uintptr) int32
	gtkChooserGetAction    func(c uintptr) int32
	gtkChooserSetFolder    func(c uintptr, dir string) int32
	gtkChooserSetName      func(c uintptr, name string)
	gtkChooserOverwrite    func(c uintptr, on int32)
	gtkChooserShowHidden   func(c uintptr, on int32)
	gtkChooserAddFilter    func(c, f uintptr)
	gtkChooserListFilters  func(c uintptr) uintptr
	gtkChooserGetFilenames func(c uintptr) uintptr
	gtkChooserSelectFile   func(c uintptr, name string) int32
	gtkFileFilterNew       func() uintptr
	gtkFileFilterName      func(f uintptr, name string)
	gtkFileFilterGetName   func(f uintptr) uintptr
	gtkFileFilterPattern   func(f uintptr, pat string)

	// message dialog
	gtkMessageDialogNew func(parent uintptr, flags, kind, buttons int32, format, text string) uintptr
	gtkMessageSecondary func(d uintptr, format, text string)
	gtkDialogAddButton  func(d uintptr, label string, id int32) uintptr
	gtkDialogDefault    func(d uintptr, id int32)
	gtkWindowTransient  func(w, parent uintptr)
	gtkWindowSetModal   func(w uintptr, on int32)
	gtkWindowSetTitle   func(w uintptr, title string)

	// clipboard
	gdkAtomIntern        func(name string, onlyExisting int32) uintptr
	gtkClipboardGet      func(sel uintptr) uintptr
	gtkClipboardSetText  func(c uintptr, text string, n int32)
	gtkClipboardWaitText func(c uintptr) uintptr
	gtkClipboardSetImage func(c, pixbuf uintptr)
	gtkClipboardWaitImg  func(c uintptr) uintptr
	gtkClipboardWaitData func(c, target uintptr) uintptr
	gtkClipboardSetData  func(c uintptr, targets unsafe.Pointer, n uint32, get, clear, owner uintptr) int32
	gtkClipboardClear    func(c uintptr)
	gtkSelGetUris        func(sd uintptr) uintptr
	gtkSelFree           func(sd uintptr)
	gtkSelSet            func(sd, typ uintptr, format int32, data unsafe.Pointer, n int32)
	gtkSelGetTarget      func(sd uintptr) uintptr

	// gdk-pixbuf
	pixbufLoaderNew   func(typ string, gerr *uintptr) uintptr
	pixbufLoaderWrite func(l uintptr, buf unsafe.Pointer, n uint64, gerr *uintptr) int32
	pixbufLoaderClose func(l uintptr, gerr *uintptr) int32
	pixbufLoaderGet   func(l uintptr) uintptr
	pixbufSaveBuffer  func(pb uintptr, buf *uintptr, size *uint64, typ string, keys, vals uintptr, gerr *uintptr) int32
}

// The GTK constants below are fixed by the ABI.
const (
	gdkSelectionClipboard = 69 // GDK_SELECTION_CLIPBOARD is an atom with this value

	gtkResponseReject = -2
	gtkResponseAccept = -3
	gtkResponseCancel = -6
	gtkResponseClose  = -7

	chooserOpen   = 0
	chooserSave   = 1
	chooserFolder = 2

	winIconified  = 1 << 1
	winMaximized  = 1 << 2
	winFullscreen = 1 << 4
	winAbove      = 1 << 5
	winFocused    = 1 << 7
)

func loadExt() *ext {
	e := &ext{}
	b := &binder{}
	open := func(name string) uintptr {
		h, err := purego.Dlopen(name, rtldNow|rtldGlobal)
		if err != nil {
			return 0
		}
		return h
	}
	glib := open("libglib-2.0.so.0")
	gobj := open("libgobject-2.0.so.0")
	gtk := open("libgtk-3.so.0")
	gdk := open("libgdk-3.so.0")
	pix := open("libgdk_pixbuf-2.0.so.0")
	bind := func(lib uintptr, fn any, sym string) {
		if lib == 0 {
			return
		}
		b.bind(fn, lib, sym)
	}

	bind(glib, &e.gSlistFree, "g_slist_free")
	bind(glib, &e.gStrfreev, "g_strfreev")
	bind(gobj, &e.gSignalEmitByName, "g_signal_emit_by_name")
	bind(gobj, &e.gObjectGetData, "g_object_get_data")
	bind(gobj, &e.gObjectSetData, "g_object_set_data")

	bind(gtk, &e.gtkWindowKeepAbove, "gtk_window_set_keep_above")
	bind(gtk, &e.gtkWindowFullscreen, "gtk_window_fullscreen")
	bind(gtk, &e.gtkWindowUnfullscreen, "gtk_window_unfullscreen")
	bind(gtk, &e.gtkWindowDecorated, "gtk_window_set_decorated")
	bind(gtk, &e.gtkWindowGetDecorated, "gtk_window_get_decorated")
	bind(gtk, &e.gtkWindowMove, "gtk_window_move")
	bind(gtk, &e.gtkWindowGetPosition, "gtk_window_get_position")
	bind(gtk, &e.gtkWindowIconify, "gtk_window_iconify")
	bind(gtk, &e.gtkWindowDeiconify, "gtk_window_deiconify")
	bind(gtk, &e.gtkWindowMaximize, "gtk_window_maximize")
	bind(gtk, &e.gtkWindowUnmaximize, "gtk_window_unmaximize")
	bind(gtk, &e.gtkWindowPresent, "gtk_window_present")
	bind(gtk, &e.gtkWindowIsActive, "gtk_window_is_active")
	bind(gtk, &e.gtkWindowAddAccel, "gtk_window_add_accel_group")
	bind(gtk, &e.gtkWindowRemoveAccel, "gtk_window_remove_accel_group")
	bind(gtk, &e.gtkWidgetShow, "gtk_widget_show")
	bind(gtk, &e.gtkWidgetHide, "gtk_widget_hide")
	bind(gtk, &e.gtkCheckResize, "gtk_container_check_resize")
	bind(gtk, &e.gtkWidgetGetVisible, "gtk_widget_get_visible")
	bind(gtk, &e.gtkWidgetGetWindow, "gtk_widget_get_window")
	bind(gtk, &e.gtkWidgetSensitive, "gtk_widget_set_sensitive")
	bind(gtk, &e.gtkWidgetAddAccel, "gtk_widget_add_accelerator")
	bind(gtk, &e.gtkWidgetGetParent, "gtk_widget_get_parent")
	bind(gdk, &e.gdkWindowGetState, "gdk_window_get_state")
	bind(gdk, &e.gdkDisplayDefault, "gdk_display_get_default")
	bind(gdk, &e.gdkDisplaySeat, "gdk_display_get_default_seat")
	bind(gdk, &e.gdkSeatPointer, "gdk_seat_get_pointer")
	bind(gdk, &e.gdkDevicePosition, "gdk_device_get_position")
	bind(gdk, &e.gdkWindowGetOrigin, "gdk_window_get_origin")

	bind(gtk, &e.gtkBoxNew, "gtk_box_new")
	bind(gtk, &e.gtkBoxPackStart, "gtk_box_pack_start")
	bind(gtk, &e.gtkBoxReorderChild, "gtk_box_reorder_child")
	bind(gtk, &e.gtkContainerRemove, "gtk_container_remove")
	bind(gtk, &e.gtkPanedNew, "gtk_paned_new")
	bind(gtk, &e.gtkPanedPack1, "gtk_paned_pack1")
	bind(gtk, &e.gtkPanedPack2, "gtk_paned_pack2")
	bind(gtk, &e.gtkPanedSetPos, "gtk_paned_set_position")
	bind(gtk, &e.gtkPanedGetPos, "gtk_paned_get_position")
	bind(gtk, &e.gtkWidgetAllocW, "gtk_widget_get_allocated_width")
	bind(gtk, &e.gtkWidgetAllocH, "gtk_widget_get_allocated_height")
	bind(gtk, &e.gtkWidgetGrabFocus, "gtk_widget_grab_focus")

	bind(gtk, &e.gtkMenuBarNew, "gtk_menu_bar_new")
	bind(gtk, &e.gtkMenuNew, "gtk_menu_new")
	bind(gtk, &e.gtkMenuItemMnemonic, "gtk_menu_item_new_with_mnemonic")
	bind(gtk, &e.gtkCheckMenuItem, "gtk_check_menu_item_new_with_mnemonic")
	bind(gtk, &e.gtkRadioMenuItem, "gtk_radio_menu_item_new_with_mnemonic")
	bind(gtk, &e.gtkRadioGetGroup, "gtk_radio_menu_item_get_group")
	bind(gtk, &e.gtkCheckSetActive, "gtk_check_menu_item_set_active")
	bind(gtk, &e.gtkCheckGetActive, "gtk_check_menu_item_get_active")
	bind(gtk, &e.gtkSeparatorItem, "gtk_separator_menu_item_new")
	bind(gtk, &e.gtkMenuItemSubmenu, "gtk_menu_item_set_submenu")
	bind(gtk, &e.gtkMenuItemActivate, "gtk_menu_item_activate")
	bind(gtk, &e.gtkMenuShellAppend, "gtk_menu_shell_append")
	bind(gtk, &e.gtkMenuPopupPointer, "gtk_menu_popup_at_pointer")
	bind(gtk, &e.gtkMenuPopupRect, "gtk_menu_popup_at_rect")
	bind(gtk, &e.gtkMenuPopdown, "gtk_menu_popdown")
	bind(gtk, &e.gtkMenuAttachWidget, "gtk_menu_attach_to_widget")
	bind(gtk, &e.gtkAccelGroupNew, "gtk_accel_group_new")
	bind(gtk, &e.gtkAccelParse, "gtk_accelerator_parse")

	bind(gtk, &e.gtkFileChooserNative, "gtk_file_chooser_native_new")
	bind(gtk, &e.gtkNativeShow, "gtk_native_dialog_show")
	bind(gtk, &e.gtkNativeHide, "gtk_native_dialog_hide")
	bind(gtk, &e.gtkNativeDestroy, "gtk_native_dialog_destroy")
	bind(gtk, &e.gtkNativeGetTitle, "gtk_native_dialog_get_title")
	bind(gtk, &e.gtkNativeModal, "gtk_native_dialog_set_modal")
	bind(gtk, &e.gtkChooserMultiple, "gtk_file_chooser_set_select_multiple")
	bind(gtk, &e.gtkChooserGetMultiple, "gtk_file_chooser_get_select_multiple")
	bind(gtk, &e.gtkChooserGetAction, "gtk_file_chooser_get_action")
	bind(gtk, &e.gtkChooserSetFolder, "gtk_file_chooser_set_current_folder")
	bind(gtk, &e.gtkChooserSetName, "gtk_file_chooser_set_current_name")
	bind(gtk, &e.gtkChooserOverwrite, "gtk_file_chooser_set_do_overwrite_confirmation")
	bind(gtk, &e.gtkChooserShowHidden, "gtk_file_chooser_set_show_hidden")
	bind(gtk, &e.gtkChooserAddFilter, "gtk_file_chooser_add_filter")
	bind(gtk, &e.gtkChooserListFilters, "gtk_file_chooser_list_filters")
	bind(gtk, &e.gtkChooserGetFilenames, "gtk_file_chooser_get_filenames")
	bind(gtk, &e.gtkChooserSelectFile, "gtk_file_chooser_set_filename")
	bind(gtk, &e.gtkFileFilterNew, "gtk_file_filter_new")
	bind(gtk, &e.gtkFileFilterName, "gtk_file_filter_set_name")
	bind(gtk, &e.gtkFileFilterGetName, "gtk_file_filter_get_name")
	bind(gtk, &e.gtkFileFilterPattern, "gtk_file_filter_add_pattern")

	bind(gtk, &e.gtkMessageDialogNew, "gtk_message_dialog_new")
	bind(gtk, &e.gtkMessageSecondary, "gtk_message_dialog_format_secondary_text")
	bind(gtk, &e.gtkDialogAddButton, "gtk_dialog_add_button")
	bind(gtk, &e.gtkDialogDefault, "gtk_dialog_set_default_response")
	bind(gtk, &e.gtkWindowTransient, "gtk_window_set_transient_for")
	bind(gtk, &e.gtkWindowSetModal, "gtk_window_set_modal")
	bind(gtk, &e.gtkWindowSetTitle, "gtk_window_set_title")

	bind(gdk, &e.gdkAtomIntern, "gdk_atom_intern")
	bind(gtk, &e.gtkClipboardGet, "gtk_clipboard_get")
	bind(gtk, &e.gtkClipboardSetText, "gtk_clipboard_set_text")
	bind(gtk, &e.gtkClipboardWaitText, "gtk_clipboard_wait_for_text")
	bind(gtk, &e.gtkClipboardSetImage, "gtk_clipboard_set_image")
	bind(gtk, &e.gtkClipboardWaitImg, "gtk_clipboard_wait_for_image")
	bind(gtk, &e.gtkClipboardWaitData, "gtk_clipboard_wait_for_contents")
	bind(gtk, &e.gtkClipboardSetData, "gtk_clipboard_set_with_data")
	bind(gtk, &e.gtkClipboardClear, "gtk_clipboard_clear")
	bind(gtk, &e.gtkSelGetUris, "gtk_selection_data_get_uris")
	bind(gtk, &e.gtkSelFree, "gtk_selection_data_free")
	bind(gtk, &e.gtkSelSet, "gtk_selection_data_set")
	bind(gtk, &e.gtkSelGetTarget, "gtk_selection_data_get_target")

	bind(pix, &e.pixbufLoaderNew, "gdk_pixbuf_loader_new_with_type")
	bind(pix, &e.pixbufLoaderWrite, "gdk_pixbuf_loader_write")
	bind(pix, &e.pixbufLoaderClose, "gdk_pixbuf_loader_close")
	bind(pix, &e.pixbufLoaderGet, "gdk_pixbuf_loader_get_pixbuf")
	bind(pix, &e.pixbufSaveBuffer, "gdk_pixbuf_save_to_bufferv")
	return e
}

// gerrMessage reads GError.message (the second field after domain and code).
func gerrMessage(gerr uintptr) string {
	if gerr == 0 {
		return ""
	}
	p := *(*uintptr)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&gerr)), 8))
	return goString(p)
}

// slistStrings collects the strings of a GSList of char*, then frees the list
// and (when freeData) the strings.
func (a *App) slistStrings(list uintptr, freeData bool) []string {
	var out []string
	for n := list; n != 0; {
		node := *(*unsafe.Pointer)(unsafe.Pointer(&n))
		data := *(*uintptr)(node)
		next := *(*uintptr)(unsafe.Add(node, 8))
		out = append(out, goString(data))
		if freeData {
			a.api.gFree(data)
		}
		n = next
	}
	if list != 0 {
		a.x.gSlistFree(list)
	}
	return out
}

// slistPtrs returns the data pointers of a GSList without freeing anything
// but the list itself.
func (a *App) slistPtrs(list uintptr) []uintptr {
	var out []uintptr
	for n := list; n != 0; {
		node := *(*unsafe.Pointer)(unsafe.Pointer(&n))
		out = append(out, *(*uintptr)(node))
		n = *(*uintptr)(unsafe.Add(node, 8))
	}
	if list != 0 {
		a.x.gSlistFree(list)
	}
	return out
}

func cstr(s string) []byte { return append([]byte(s), 0) }

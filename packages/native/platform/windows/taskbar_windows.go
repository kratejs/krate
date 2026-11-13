//go:build windows

package windows

// The Windows taskbar: thumbnail toolbar buttons (ITaskbarList3) and the
// jump list tasks (ICustomDestinationList). Both are COM objects driven
// through vtable slots (vtables_chrome.go). The thumbnail buttons need the
// window's taskbar button to exist, which Explorer announces with the
// TaskbarButtonCreated message; buttons set earlier are applied then.

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"github.com/kratejs/krate/packages/native/platform/caps"
)

var (
	pPropVariantClear = ole32.NewProc("PropVariantClear")
)

const clsctxInprocServer = 0x1

// coCreate creates a COM object of a class and asks it for an interface.
func coCreate(clsid, iid string) (unsafe.Pointer, error) {
	c, i := mustGUID(clsid), mustGUID(iid)
	var out uintptr
	hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&c)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&i)), uintptr(unsafe.Pointer(&out)))
	if failed(hr) || out == 0 {
		return nil, fmt.Errorf("windows: CoCreateInstance(%s): 0x%08x", clsid, uint32(hr))
	}
	return ptr(out), nil
}

// SetThumbButtons replaces the buttons in the window's taskbar thumbnail.
func (w *Window) SetThumbButtons(bs []caps.ThumbButton) (err error) {
	if err := caps.ValidateThumbButtons(bs); err != nil {
		return err
	}
	cp := make([]caps.ThumbButton, len(bs))
	for i, b := range bs {
		cp[i] = b
		cp[i].Icon = append([]byte(nil), b.Icon...)
	}
	w.app.callMain(func() {
		if w.destroyed {
			err = errors.New("windows: window closed")
			return
		}
		w.chrome.thumbs = cp
		err = w.applyThumbs()
	})
	return
}

// applyThumbs pushes the wanted buttons to the shell once the taskbar button
// exists (main thread). Before that it only creates the taskbar object.
func (w *Window) applyThumbs() error {
	c := &w.chrome
	if c.taskbar == nil {
		if len(c.thumbs) == 0 {
			return nil
		}
		tb, err := coCreate(clsidTaskbarList, iidITaskbarList3)
		if err != nil {
			return err
		}
		if hr := vcall(tb, tbHrInit); failed(hr) {
			comRelease(tb)
			return fmt.Errorf("windows: ITaskbarList::HrInit: 0x%08x", uint32(hr))
		}
		c.taskbar = tb
	}
	if !c.taskbarReady {
		return nil
	}
	for _, h := range c.icons {
		_, _, _ = pDestroyIcon.Call(h)
	}
	c.icons = c.icons[:0]
	var arr [thumbSlots]thumbButton
	for slot := 0; slot < thumbSlots; slot++ {
		if slot >= len(c.thumbs) {
			arr[slot] = makeThumb(slot, nil, 0)
			continue
		}
		var icon uintptr
		if len(c.thumbs[slot].Icon) > 0 {
			if h, owned, err := createHIcon(c.thumbs[slot].Icon); err == nil {
				icon = h
				if owned {
					c.icons = append(c.icons, h)
				}
			}
		}
		arr[slot] = makeThumb(slot, &c.thumbs[slot], icon)
	}
	slot := tbThumbBarUpdateButtons
	if !c.thumbsAdded {
		slot = tbThumbBarAddButtons
	}
	hr := vcall(c.taskbar, slot, w.hwnd, thumbSlots, uintptr(unsafe.Pointer(&arr[0])))
	if failed(hr) {
		return fmt.Errorf("windows: ITaskbarList3 thumbnail buttons: 0x%08x", uint32(hr))
	}
	c.thumbsAdded = true
	return nil
}

// thumbClicked reports a click on a thumbnail button.
func (w *Window) thumbClicked(id uint32) {
	slot := thumbSlot(id)
	if slot < 0 || slot >= len(w.chrome.thumbs) {
		return
	}
	emitChrome(w, caps.ChromeThumbClick, w.chrome.thumbs[slot].ID)
}

// releaseTaskbar frees the window's taskbar object and icons.
func (w *Window) releaseTaskbar() {
	c := &w.chrome
	if c.taskbar != nil {
		comRelease(c.taskbar)
		c.taskbar = nil
	}
	for _, h := range c.icons {
		_, _, _ = pDestroyIcon.Call(h)
	}
	c.icons = nil
}

// SetJumpList replaces the user tasks of the app's jump list.
func (a *App) SetJumpList(tasks []caps.JumpTask) (err error) {
	if err := caps.ValidateJumpTasks(tasks); err != nil {
		return err
	}
	if len(tasks) == 0 {
		return a.ClearJumpList()
	}
	a.callMain(func() { err = a.commitJumpList(tasks) })
	return
}

// ClearJumpList removes the custom jump list entries.
func (a *App) ClearJumpList() (err error) {
	a.callMain(func() {
		list, e := coCreate(clsidDestinationList, iidICustomDestinationList)
		if e != nil {
			err = e
			return
		}
		defer comRelease(list)
		if hr := vcall(list, cdlDeleteList, uintptr(unsafe.Pointer(wstr(aumidFor(a.cfg.AppID))))); failed(hr) {
			err = fmt.Errorf("windows: ICustomDestinationList::DeleteList: 0x%08x", uint32(hr))
		}
	})
	return
}

// commitJumpList builds the task collection and commits the list (main
// thread).
func (a *App) commitJumpList(tasks []caps.JumpTask) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	list, err := coCreate(clsidDestinationList, iidICustomDestinationList)
	if err != nil {
		return err
	}
	defer comRelease(list)
	if hr := vcall(list, cdlSetAppID, uintptr(unsafe.Pointer(wstr(aumidFor(a.cfg.AppID))))); failed(hr) {
		return fmt.Errorf("windows: ICustomDestinationList::SetAppID: 0x%08x", uint32(hr))
	}
	var slots uint32
	var removed uintptr
	arrIID := mustGUID(iidIObjectArray)
	if hr := vcall(list, cdlBeginList, uintptr(unsafe.Pointer(&slots)), uintptr(unsafe.Pointer(&arrIID)), uintptr(unsafe.Pointer(&removed))); failed(hr) {
		return fmt.Errorf("windows: ICustomDestinationList::BeginList: 0x%08x", uint32(hr))
	}
	if removed != 0 {
		comRelease(ptr(removed))
	}
	committed := false
	defer func() {
		if !committed {
			vcall(list, cdlAbortList)
		}
	}()
	coll, err := coCreate(clsidEnumerableObjectCollection, iidIObjectCollection)
	if err != nil {
		return err
	}
	defer comRelease(coll)
	for _, t := range tasks {
		link, err := a.taskLink(exe, t)
		if err != nil {
			return err
		}
		hr := vcall(coll, objCollAddObject, uintptr(link))
		comRelease(link)
		if failed(hr) {
			return fmt.Errorf("windows: IObjectCollection::AddObject: 0x%08x", uint32(hr))
		}
	}
	arr := queryInterface(coll, iidIObjectArray)
	if arr == nil {
		return errors.New("windows: the task collection has no IObjectArray")
	}
	defer comRelease(arr)
	if hr := vcall(list, cdlAddUserTasks, uintptr(arr)); failed(hr) {
		return fmt.Errorf("windows: ICustomDestinationList::AddUserTasks: 0x%08x", uint32(hr))
	}
	if hr := vcall(list, cdlCommitList); failed(hr) {
		return fmt.Errorf("windows: ICustomDestinationList::CommitList: 0x%08x", uint32(hr))
	}
	committed = true
	return nil
}

// taskLink makes the IShellLink of one task: it starts this executable with
// the task's arguments, and carries the title as PKEY_Title.
func (a *App) taskLink(exe string, t caps.JumpTask) (unsafe.Pointer, error) {
	link, err := coCreate(clsidShellLink, iidIShellLinkW)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			comRelease(link)
		}
	}()
	if hr := vcall(link, linkSetPath, uintptr(unsafe.Pointer(wstr(exe)))); failed(hr) {
		return nil, fmt.Errorf("windows: IShellLink::SetPath: 0x%08x", uint32(hr))
	}
	if len(t.Args) > 0 {
		vcall(link, linkSetArguments, uintptr(unsafe.Pointer(wstr(caps.QuoteArgs(caps.JumpArgs(t.Args))))))
	}
	if t.Description != "" {
		vcall(link, linkSetDescription, uintptr(unsafe.Pointer(wstr(t.Description))))
	}
	icon, idx := exe, 0
	if t.IconPath != "" {
		icon, idx = t.IconPath, t.IconIndex
	}
	vcall(link, linkSetIconLocation, uintptr(unsafe.Pointer(wstr(icon))), uintptr(idx))

	store := queryInterface(link, iidIPropertyStore)
	if store == nil {
		return nil, errors.New("windows: the shell link has no IPropertyStore")
	}
	defer comRelease(store)
	key := propertyKey{fmtid: mustGUID(fmtidTitle), pid: pidTitle}
	pv, err := newPropVariantString(t.Title)
	if err != nil {
		return nil, err
	}
	defer func() { _, _, _ = pPropVariantClear.Call(uintptr(unsafe.Pointer(&pv))) }()
	if hr := vcall(store, propStoreSetValue, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&pv))); failed(hr) {
		return nil, fmt.Errorf("windows: IPropertyStore::SetValue(title): 0x%08x", uint32(hr))
	}
	if hr := vcall(store, propStoreCommit); failed(hr) {
		return nil, fmt.Errorf("windows: IPropertyStore::Commit: 0x%08x", uint32(hr))
	}
	ok = true
	return link, nil
}

// propertyKey is PROPERTYKEY.
type propertyKey struct {
	fmtid guid
	pid   uint32
}

// propVariant is a PROPVARIANT holding a VT_LPWSTR (24 bytes on 64-bit
// Windows, 16 on 32-bit; the union starts after the 8-byte header).
type propVariant struct {
	vt       uint16
	reserved [3]uint16
	val      uintptr
	pad      [unsafe.Sizeof(uintptr(0))]byte
}

const vtLPWSTR = 31

// newPropVariantString makes a VT_LPWSTR PROPVARIANT whose string is allocated
// with CoTaskMemAlloc, so PropVariantClear frees it.
func newPropVariantString(s string) (propVariant, error) {
	u := wstr(s)
	n := 0
	for p := unsafe.Pointer(u); *(*uint16)(p) != 0; p = unsafe.Add(p, 2) {
		n++
	}
	size := uintptr(n+1) * 2
	mem, _, _ := pCoTaskMemAlloc.Call(size)
	if mem == 0 {
		return propVariant{}, errors.New("windows: CoTaskMemAlloc failed")
	}
	copy(unsafe.Slice((*byte)(ptr(mem)), size), unsafe.Slice((*byte)(unsafe.Pointer(u)), size))
	return propVariant{vt: vtLPWSTR, val: mem}, nil
}

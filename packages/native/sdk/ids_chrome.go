package sdk

// chromeIDs are the ids of the chrome area (sdk/chrome): window chrome,
// platform decorations, taskbar and dock. The TypeScript wrappers are in
// packages/app/src/chrome.ts.
var chromeIDs = []ID{
	{"krate/chrome.state", "window", "[window: string]", "ChromeState", "chrome"},
	{"krate/chrome.setTitleBar", "window", "[window: string, style: TitleBarStyle]", "void", "chrome"},
	{"krate/chrome.setBackdrop", "window", "[window: string, name: Backdrop]", "void", "chrome"},
	{"krate/chrome.setVibrancy", "window", "[window: string, material: Vibrancy | \"\"]", "void", "chrome"},
	{"krate/chrome.setTitleBarOverlay", "window", "[window: string, overlay: TitleBarOverlay | null]", "void", "chrome"},
	{"krate/chrome.setTrafficLightPosition", "window", "[window: string, x: number, y: number]", "void", "chrome"},
	{"krate/chrome.startDrag", "window", "[window: string]", "void", "chrome"},
	{"krate/chrome.toggleMaximize", "window", "[window: string]", "void", "chrome"},
	{"krate/chrome.setDocumentEdited", "window", "[window: string, on: boolean]", "void", "chrome"},
	{"krate/chrome.setRepresentedFile", "window", "[window: string, path: string]", "void", "chrome"},
	{"krate/chrome.attachSheet", "window", "[window: string, child: string]", "void", "chrome"},
	{"krate/chrome.endSheet", "window", "[window: string]", "void", "chrome"},
	{"krate/chrome.setTabbing", "window", "[window: string, identifier: string, mode: TabbingMode]", "void", "chrome"},
	{"krate/chrome.mergeAllWindows", "window", "[window: string]", "void", "chrome"},
	{"krate/chrome.moveTabToNewWindow", "window", "[window: string]", "void", "chrome"},
	{"krate/chrome.tabs", "window", "[window: string]", "string[]", "chrome"},
	{"krate/chrome.setThumbButtons", "window", "[window: string, buttons: ThumbButton[]]", "void", "chrome"},
	{"krate/chrome.share", "share", "[window: string, items: ShareItems]", "void", "chrome"},
	{"krate/chrome.setJumpList", "app", "[tasks: JumpTask[]]", "void", "chrome"},
	{"krate/chrome.clearJumpList", "app", "[]", "void", "chrome"},
	{"krate/chrome.setActivationPolicy", "app", "[policy: ActivationPolicy]", "void", "chrome"},
	{"krate/chrome.setDockVisible", "app", "[on: boolean]", "void", "chrome"},
	{"krate/chrome.events", "window", "[] (stream)", "ChromeEvent", "chrome"},
}

func init() { IDs = append(IDs, chromeIDs...) }

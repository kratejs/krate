package sdk

func init() {
	IDs = append(IDs, []ID{
		{"krate/clipboard.write", "clipboard", "[data: ClipboardData]", "void", "clipboard"},
		{"krate/clipboard.writeFormat", "clipboard", "[name: string, data: Uint8Array]", "void", "clipboard"},
		{"krate/clipboard.readHtml", "clipboard:read", "[]", "string", "clipboard"},
		{"krate/clipboard.readRtf", "clipboard:read", "[]", "string", "clipboard"},
		{"krate/clipboard.formats", "clipboard:read", "[]", "string[]", "clipboard"},
		{"krate/clipboard.readFormat", "clipboard:read", "[name: string]", "Uint8Array | null", "clipboard"},
	}...)
}

func init() {
	IDs = append(IDs, []ID{
		{"krate/menu.setContextMenu", "menu", "[window: string, view: string, options: ContextMenuOptions]", "void", "menu"},
		{"krate/menu.contextEvents", "menu", "[] (stream)", "ContextMenuEvent", "menu"},
	}...)
}

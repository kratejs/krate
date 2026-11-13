//go:build windows

package windows

import "fmt"

// The shell cannot show a web page without the Microsoft Edge WebView2
// Runtime. A GUI program has no console, so the failure would otherwise be
// invisible: the app reports it in a message box that offers the download
// page.

const (
	// webview2DownloadPage is the page with the Evergreen bootstrapper and
	// installer downloads.
	webview2DownloadPage = "https://developer.microsoft.com/microsoft-edge/webview2/"

	hrFileNotFound = 0x80070002 // the public loader's "no runtime installed"
)

// runtimeError is a failure to start the WebView2 runtime.
type runtimeError struct {
	// missing is true when no runtime was found at all, false when one was
	// found but could not be started.
	missing bool
	detail  string
}

func (e *runtimeError) Error() string {
	if e.missing {
		return "the WebView2 Runtime is not installed; install the Evergreen Runtime from " + webview2DownloadPage +
			" (the bootstrapper is https://go.microsoft.com/fwlink/p/?LinkId=2124703), or ship WebView2Loader.dll next to the executable" + e.detail
	}
	return "the WebView2 Runtime could not be started" + e.detail
}

// runtimeMissingHRESULT reports whether an environment-creation HRESULT means
// that no runtime is installed.
func runtimeMissingHRESULT(hr uint32) bool { return hr == hrFileNotFound }

// runtimeDialogText is the body of the message box, and its caption.
func runtimeDialogText(appName string, missing bool, detail string) (text, caption string) {
	if appName == "" {
		appName = "This app"
	}
	caption = appName
	if missing {
		text = fmt.Sprintf("%s needs the Microsoft Edge WebView2 Runtime, which is not installed on this computer.\n\n"+
			"It is free and installs in a minute. Download it from:\n%s\n\nOpen the download page now?", appName, webview2DownloadPage)
		return text, caption
	}
	text = fmt.Sprintf("%s could not start the Microsoft Edge WebView2 Runtime.\n\n"+
		"Reinstalling the runtime usually fixes this. Download it from:\n%s\n\nOpen the download page now?", appName, webview2DownloadPage)
	if detail != "" {
		text += "\n\nDetails: " + detail
	}
	return text, caption
}

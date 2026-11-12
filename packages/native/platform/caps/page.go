package caps

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Per-view web contents controls: print, find, zoom and capture. They are
// methods of the Window value (like WindowViews) and take the view they act
// on, so a window's primary view and every added view are served alike. Every
// method may be called from any goroutine.
//
// Printing and capture need the view to be shown on some platforms; a window
// that was Released returns an error.

// Margins are page margins in inches.
type Margins struct {
	Top, Right, Bottom, Left float64
}

// DefaultMargins is the margin of PDFOptions with no Margins: 0.4 inch.
var DefaultMargins = Margins{0.4, 0.4, 0.4, 0.4}

// PDFOptions configures PageControls.PrintPDF.
type PDFOptions struct {
	Landscape       bool
	PrintBackground bool
	// PageWidth and PageHeight are the paper size in inches (portrait
	// orientation; Landscape swaps them). Both zero means A4.
	PageWidth, PageHeight float64
	// Margins in inches; nil means DefaultMargins.
	Margins *Margins
}

// Paper sizes in inches, portrait.
var paperSizes = map[string][2]float64{
	"a3":      {11.69, 16.54},
	"a4":      {8.27, 11.69},
	"a5":      {5.83, 8.27},
	"letter":  {8.5, 11},
	"legal":   {8.5, 14},
	"tabloid": {11, 17},
}

// PaperSize returns the size in inches of a named paper size ("A4",
// "Letter", "Legal", "A3", "A5", "Tabloid"; case-insensitive).
func PaperSize(name string) (w, h float64, ok bool) {
	s, ok := paperSizes[strings.ToLower(name)]
	return s[0], s[1], ok
}

// Resolve fills in the defaults and validates the options. The returned
// options always have a positive paper size (portrait, Landscape does not
// swap it; see Sheet) and margins that leave room for content.
func (o PDFOptions) Resolve() (PDFOptions, error) {
	if o.PageWidth == 0 && o.PageHeight == 0 {
		o.PageWidth, o.PageHeight = paperSizes["a4"][0], paperSizes["a4"][1]
	}
	for _, v := range []float64{o.PageWidth, o.PageHeight} {
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 200 {
			return o, fmt.Errorf("krate: page size %vx%v inches is not valid", o.PageWidth, o.PageHeight)
		}
	}
	m := DefaultMargins
	if o.Margins != nil {
		m = *o.Margins
	}
	for _, v := range []float64{m.Top, m.Right, m.Bottom, m.Left} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return o, errors.New("krate: margins cannot be negative")
		}
	}
	w, h := o.Sheet()
	if m.Left+m.Right >= w || m.Top+m.Bottom >= h {
		return o, errors.New("krate: margins leave no room for content on the page")
	}
	o.Margins = &m
	return o, nil
}

// Sheet is the paper size as it comes out of the printer: the page size with
// width and height swapped for Landscape.
func (o PDFOptions) Sheet() (w, h float64) {
	if o.Landscape {
		return o.PageHeight, o.PageWidth
	}
	return o.PageWidth, o.PageHeight
}

// FindOptions configures PageControls.Find.
type FindOptions struct {
	// Backward searches towards the start of the page; the zero value goes
	// forward.
	Backward  bool
	MatchCase bool
	// NoWrap stops at the end of the page instead of wrapping around.
	NoWrap bool
	// Continue moves to the next (or previous) match of the search already
	// running for the same text instead of starting over.
	Continue bool
}

// FindEvent reports the outcome of a find. Matches is the total count, Active
// the 1-based index of the highlighted match (0 when unknown or no match).
type FindEvent struct {
	View    ViewRef
	Text    string
	Matches int
	Active  int
	// Final is true when the count will not change any more for this search.
	Final bool
}

// CaptureOptions configures PageControls.Capture.
type CaptureOptions struct{}

// PageControls is implemented by Window values with print, find, zoom and
// capture support for each of their views.
type PageControls interface {
	// PrintDialog opens the system print dialog for the view. It returns
	// when the dialog has been shown (or the print job handed over), not
	// when the user is done.
	PrintDialog(v ViewRef) error
	// PrintPDF renders the view's page to a PDF and returns the bytes.
	PrintPDF(v ViewRef, o PDFOptions) ([]byte, error)
	// Find searches for text in the view, highlights the first match and
	// reports counts through the handler set with PageEvents.OnFind.
	Find(v ViewRef, text string, o FindOptions) error
	// StopFind clears the highlight of the view's search.
	StopFind(v ViewRef) error
	// ZoomFactor returns the page zoom of the view; 1 is 100 percent.
	ZoomFactor(v ViewRef) (float64, error)
	// SetZoomFactor sets the page zoom of the view.
	SetZoomFactor(v ViewRef, factor float64) error
	// Capture returns a PNG of the visible area of the view.
	Capture(v ViewRef, o CaptureOptions) ([]byte, error)
}

// Zoom limits, the same as the browsers use.
const (
	MinZoom = 0.25
	MaxZoom = 5.0
)

// ZoomSteps are the zoom factors Ctrl/Cmd plus and minus step through.
var ZoomSteps = []float64{0.25, 0.33, 0.5, 0.67, 0.75, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2, 2.5, 3, 4, 5}

// ClampZoom limits f to [MinZoom, MaxZoom]; NaN becomes 1.
func ClampZoom(f float64) float64 {
	switch {
	case math.IsNaN(f):
		return 1
	case f < MinZoom:
		return MinZoom
	case f > MaxZoom:
		return MaxZoom
	}
	return f
}

// NextZoom returns the next larger (up) or smaller step from cur.
func NextZoom(cur float64, up bool) float64 {
	const eps = 1e-6
	if up {
		for _, s := range ZoomSteps {
			if s > cur+eps {
				return s
			}
		}
		return MaxZoom
	}
	for i := len(ZoomSteps) - 1; i >= 0; i-- {
		if ZoomSteps[i] < cur-eps {
			return ZoomSteps[i]
		}
	}
	return MinZoom
}

// PageEvents is implemented by Apps that report page events (find results).
type PageEvents interface {
	// OnFind sets the single handler for find results of every view. fn
	// runs on the main thread and must not block.
	OnFind(fn func(FindEvent))
}

// DownloadState is the stage of a download.
type DownloadState string

// Download states.
const (
	DownloadStarted   DownloadState = "started"
	DownloadProgress  DownloadState = "progress"
	DownloadCompleted DownloadState = "completed"
	DownloadCancelled DownloadState = "cancelled"
	DownloadFailed    DownloadState = "failed"
)

// DownloadRequest describes a download the page started, before it has a
// destination.
type DownloadRequest struct {
	// ID is unique within the process.
	ID   uint64
	View ViewRef
	URL  string
	// SuggestedName is the file name the server or the download attribute
	// proposed; it can be empty.
	SuggestedName string
	MIME          string
	// Total is the size in bytes, -1 when unknown.
	Total int64
}

// DownloadEvent reports the progress of a download.
type DownloadEvent struct {
	ID       uint64
	View     ViewRef
	State    DownloadState
	URL      string
	Path     string
	Received int64
	Total    int64
	// Error is set for DownloadFailed.
	Error string
}

// Downloads is implemented by Apps that can take over downloads the page
// starts (a link with the download attribute, a navigation to a file
// response). Without a handler set with OnDownloadStart the platform's own
// default applies.
type Downloads interface {
	// OnDownloadStart sets the handler that chooses the destination of each
	// download. It runs on the main thread and must not block: it calls
	// decide, from any goroutine and exactly once, with the absolute path to
	// write to, or "" to cancel the download.
	OnDownloadStart(fn func(r DownloadRequest, decide func(path string)))
	// OnDownload sets the handler for download events.
	OnDownload(fn func(DownloadEvent))
	// CancelDownload cancels a running download.
	CancelDownload(id uint64) error
}

// SameSite is the SameSite attribute of a cookie.
type SameSite string

// SameSite values; the empty string leaves the attribute unset.
const (
	SameSiteNone   SameSite = "none"
	SameSiteLax    SameSite = "lax"
	SameSiteStrict SameSite = "strict"
)

// Cookie is one HTTP cookie.
type Cookie struct {
	Name, Value string
	Domain      string
	Path        string
	// Expires is the zero Time for a session cookie.
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	SameSite SameSite
}

// ClearSiteDataOptions selects what ClearSiteData removes. All false means
// everything.
type ClearSiteDataOptions struct {
	Cookies bool
	// Cache is the HTTP and memory cache.
	Cache bool
	// Storage is localStorage, sessionStorage, IndexedDB and service workers.
	Storage bool
}

// Everything reports whether o selects every kind of data.
func (o ClearSiteDataOptions) Everything() bool {
	return (o.Cookies && o.Cache && o.Storage) || (!o.Cookies && !o.Cache && !o.Storage)
}

// SiteData is implemented by Apps that give access to cookies and site data.
//
// The engines on Linux and macOS keep no cookies for the custom scheme of the
// app origin (document.cookie is inert there), so on those platforms the
// cookie calls answer krate.ErrUnsupported for it and work for http(s)
// origins, such as the API a page fetches. On Windows the app origin is an
// https origin and has cookies like any other.
type SiteData interface {
	// Cookies lists the cookies that would be sent to origin, a URL such as
	// "https://api.example.com".
	Cookies(origin string) ([]Cookie, error)
	// SetCookie stores c. An empty Domain and Path default to the origin's.
	SetCookie(origin string, c Cookie) error
	// DeleteCookie removes the cookie with this name, domain and path.
	DeleteCookie(origin string, c Cookie) error
	// ClearSiteData removes the selected data of the app origin.
	ClearSiteData(o ClearSiteDataOptions) error
}

// CookieDomainMatches reports whether a cookie with this Domain attribute is
// sent to host: an exact match, or a domain cookie (leading dot) that host is
// inside of.
func CookieDomainMatches(domain, host string) bool {
	domain, host = strings.ToLower(domain), strings.ToLower(host)
	bare := strings.TrimPrefix(domain, ".")
	if bare == "" || host == "" {
		return false
	}
	if bare == host {
		return true
	}
	return strings.HasPrefix(domain, ".") && strings.HasSuffix(host, "."+bare)
}

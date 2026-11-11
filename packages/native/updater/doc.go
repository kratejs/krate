// Package updater checks a feed for a newer version of the running app,
// downloads and verifies the update, replaces the installed files and
// relaunches.
//
// Feed. Config.URL is fetched with GET after {target} (<goos>-<goarch>, for
// example "linux-amd64" or "darwin-arm64") and {version} (the running
// version) are expanded. The answer is JSON:
//
//	{"version":"1.2.0","notes":"...","pub_date":"...","url":"https://.../app-1.2.0-linux-amd64.tar.gz",
//	 "signature":"<base64>","size":123456}
//
// HTTP 204 or 404 means "no update". A version that is not newer than
// Config.Version is also "no update".
//
// Signature. The signature is Ed25519ph (RFC 8032, SHA-512 prehash, empty
// context) over the bytes of the file at url, base64 encoded. Prehashing lets
// the download be verified as a stream instead of held in memory. The public
// key (base64, 32 bytes) comes from native.updater.publicKey in the app
// configuration; `krate native keygen` creates the pair and `krate native
// package` signs update artifacts with it.
//
// Artifacts. An update is a .tar.gz or .zip whose entries mirror the install
// directory (the directory that holds the executable) on Linux and Windows,
// or a .app bundle on macOS, or a single AppImage file. Apply replaces the
// installed files by renaming: a failure part way restores what was moved.
//
// Delta updates are not implemented. A later version can add an optional
// "patches" list to the feed (binary diffs from specific older versions)
// and fall back to the full artifact; clients that do not know the field
// ignore it.
package updater

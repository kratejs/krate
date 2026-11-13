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
// Signature. The signature is Ed25519 over a canonical message that binds the
// file to its metadata (see Sign), base64 encoded:
//
//	krate-update-v1\n<appId>\n<target>\n<version>\n<sha512 of the file, hex>\n<size>
//
// The file digest is computed as a stream, so a download is never held in
// memory. The expected appId, target and version come from Config and the
// feed, so a validly signed artifact cannot be relabeled as a newer version
// or served to another app or target. The public key (base64, 32 bytes) comes
// from native.updater.publicKey in the app configuration; `krate native
// keygen` creates the pair and `krate native package` signs update artifacts
// with it.
//
// Artifacts. An update is a .tar.gz or .zip whose entries mirror the install
// directory (the directory that holds the executable) on Linux and Windows,
// or a .app bundle on macOS, or a single AppImage file. Apply replaces the
// installed files by renaming: a failure part way restores what was moved.
//
// Delta updates. The feed may list "patches": signed binary diffs (bsdiff,
// pure Go) from specific older versions to this one, see GeneratePatch and
// SignPatch. Download takes the patch for Config.Version, checks its signature
// and every file hash while it builds the new tree in a staging directory next
// to the install, and falls back to the full artifact on any failure.
//
// Staged rollouts. The feed may carry "rollout" (percentage, start, ramp),
// "critical" and "min_version". Check offers the update only to installs whose
// stable bucket (from a random per-install id kept in Config.StateDir) is
// inside the effective percentage, unless the update is critical or required.
//
// Rollback. With Config.StateDir set, Apply keeps the replaced files and records
// the update as unconfirmed. Boot, called first thing at start-up, counts the
// starts of the new version; MarkHealthy confirms one and deletes the kept
// files. A start that crashed or did not confirm in time makes the next Boot
// restore the previous version and report a RollbackEvent. Feed fields are
// optional and ignored by older clients; an old feed works unchanged.
package updater

package updater

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultChannel is the channel used when none is configured.
const DefaultChannel = "stable"

var channelRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

// ValidChannel reports whether s is a usable channel name.
func ValidChannel(s string) bool { return channelRe.MatchString(s) }

// Rollout limits an update to a share of the installs.
//
// Each install has a random id, created on first use and kept in the state
// directory. Its bucket for a release is a number in [0, 100) derived from
// the id and the release's Salt (default: the version), so the same install
// keeps its place while the percentage grows, and different releases pick
// different early adopters. An install is offered the update when its bucket
// is below the effective percentage.
type Rollout struct {
	// Percentage is the share of installs, 0 to 100. 0 halts the rollout.
	Percentage float64 `json:"percentage"`
	// Start is an RFC 3339 time; before it nobody is offered the update.
	Start string `json:"start,omitempty"`
	// RampHours grows the share linearly from 0 at Start to Percentage this
	// many hours later. 0 means the full Percentage from Start on.
	RampHours float64 `json:"ramp_hours,omitempty"`
	// Salt replaces the version in the bucket computation.
	Salt string `json:"salt,omitempty"`
}

func (r *Rollout) validate() error {
	if math.IsNaN(r.Percentage) || r.Percentage < 0 || r.Percentage > 100 {
		return fmt.Errorf("updater: rollout percentage %v is not between 0 and 100", r.Percentage)
	}
	if math.IsNaN(r.RampHours) || r.RampHours < 0 {
		return errors.New("updater: rollout ramp_hours is negative")
	}
	if r.Start != "" {
		if _, err := time.Parse(time.RFC3339, r.Start); err != nil {
			return fmt.Errorf("updater: rollout start %q is not an RFC 3339 time", r.Start)
		}
	}
	return nil
}

// EffectivePercent is the share of installs the rollout includes at now.
func (r *Rollout) EffectivePercent(now time.Time) float64 {
	if r == nil {
		return 100
	}
	p := r.Percentage
	if r.Start != "" {
		start, err := time.Parse(time.RFC3339, r.Start)
		if err != nil {
			return 0
		}
		if now.Before(start) {
			return 0
		}
		if r.RampHours > 0 {
			frac := now.Sub(start).Hours() / r.RampHours
			if frac < 1 {
				p *= frac
			}
		}
	}
	return p
}

// Bucket is the install's position in [0, 100) for a release.
func Bucket(installID, salt string) float64 {
	sum := sha256.Sum256([]byte("krate-rollout-v1\n" + installID + "\n" + salt))
	// 53 random bits make an exact float64 fraction.
	v := binary.BigEndian.Uint64(sum[:8]) >> 11
	return float64(v) / float64(uint64(1)<<53) * 100
}

// Includes reports whether the install is inside the rollout at now.
func (r *Rollout) Includes(installID, version string, now time.Time) bool {
	if r == nil {
		return true
	}
	salt := r.Salt
	if salt == "" {
		salt = version
	}
	return Bucket(installID, salt) < r.EffectivePercent(now)
}

var (
	idMu      sync.Mutex
	memoryIDs = map[string]string{}
)

// InstallID returns the install's random id, creating and persisting it in
// dir on first use. When dir is empty or unwritable the id lasts for the
// process.
func InstallID(dir string) string {
	idMu.Lock()
	defer idMu.Unlock()
	if id, ok := memoryIDs[dir]; ok {
		return id
	}
	var id string
	if dir != "" {
		p := filepath.Join(dir, "install-id")
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); len(s) >= 16 && len(s) <= 128 {
				id = s
			}
		}
		if id == "" {
			id = newID()
			if err := os.MkdirAll(dir, 0o700); err == nil {
				// Another process may have won the race; keep its id.
				if f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
					_, werr := f.WriteString(id + "\n")
					f.Close()
					if werr != nil {
						os.Remove(p)
					}
				} else if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) >= 16 {
					id = strings.TrimSpace(string(b))
				}
			}
		}
	}
	if id == "" {
		id = newID()
	}
	memoryIDs[dir] = id
	return id
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("updater: no randomness: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func (c *Config) installID() string {
	if c.InstallID != "" {
		return c.InstallID
	}
	return InstallID(c.StateDir)
}

// channel resolves the channel: the one chosen with SetChannel, else
// Config.Channel, else the default.
func (c *Config) channel() string {
	if c.StateDir != "" {
		if ch := loadState(c.StateDir).Channel; ValidChannel(ch) {
			return ch
		}
	}
	if ValidChannel(c.Channel) {
		return c.Channel
	}
	return DefaultChannel
}

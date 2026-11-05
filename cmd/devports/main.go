// Command devports prints deterministic development ports for a dev slot.
//
// Usage:
//
//	devports <slot>          print "<apiPort> <webPort>"
//	devports <slot> api      print only the API port
//	devports <slot> web      print only the web port
//	devports -path <dir> ... same, but derive the ports from an absolute data dir path
//
// The "main" slot (or an empty name) gets the default ports 8080/5173. Any other
// identifier gets offset = fnv32a(id) mod 99 + 1, i.e. API 8081-8179, web 5174-5272.
package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	baseAPIPort = 8080
	baseWebPort = 5173
	portRange   = 99
)

var slotPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

func main() {
	args := os.Args[1:]
	pathMode := false
	if len(args) > 0 && args[0] == "-path" {
		pathMode = true
		args = args[1:]
	}
	if len(args) < 1 || len(args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: devports [-path] <slot|dir> [api|web]")
		os.Exit(2)
	}
	id := args[0]
	which := "both"
	if len(args) == 2 {
		which = args[1]
	}

	api, web, err := portsFor(id, pathMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "devports:", err)
		os.Exit(2)
	}

	switch which {
	case "api":
		fmt.Println(api)
	case "web":
		fmt.Println(web)
	case "both":
		fmt.Printf("%d %d\n", api, web)
	default:
		fmt.Fprintln(os.Stderr, "devports: invalid port selector:", which)
		os.Exit(2)
	}
}

// portsFor returns the API and web ports for an identifier. In path mode the
// identifier is a data directory path, resolved to an absolute path first so the
// same directory always maps to the same ports regardless of the caller's cwd.
func portsFor(id string, pathMode bool) (int, int, error) {
	if !pathMode {
		if id == "" || id == "main" {
			return baseAPIPort, baseWebPort, nil
		}
		if !slotPattern.MatchString(id) {
			return 0, 0, fmt.Errorf("invalid slot name %q (want ^[a-z0-9][a-z0-9-]{0,31}$)", id)
		}
	} else {
		// Expand a leading ~ like a shell would
		if id == "~" || strings.HasPrefix(id, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				id = filepath.Join(home, strings.TrimPrefix(id, "~/"))
			}
		}
		abs, err := filepath.Abs(id)
		if err != nil {
			return 0, 0, fmt.Errorf("cannot resolve path %q: %w", id, err)
		}
		id = filepath.Clean(abs)
	}

	h := fnv.New32a()
	h.Write([]byte(id))
	offset := int(h.Sum32()%portRange) + 1
	return baseAPIPort + offset, baseWebPort + offset, nil
}

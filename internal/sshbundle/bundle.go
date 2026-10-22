// Package sshbundle packages a deliberately limited, static SSH configuration.
package sshbundle

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zyc945/zKeys/internal/keystore"
)

type Bundle struct {
	Type    string `json:"type"`
	Version int    `json:"version"`
	Hosts   []Host `json:"hosts"`
	Files   []File `json:"files"`
}
type Host struct {
	Name    string   `json:"name"`
	Options []Option `json:"options"`
}
type Option struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}
type File struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Data []byte `json:"data"`
}

var bundleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var hostName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,252}$`)
var fileID = regexp.MustCompile(`^f[0-9]{4,}$`)

var optionNames = map[string]bool{
	"hostname": true, "user": true, "port": true, "identityfile": true,
	"certificatefile": true, "proxyjump": true, "proxycommand": true,
	"identitiesonly": true, "serveraliveinterval": true, "serveralivecountmax": true,
	"hostkeyalias": true, "checkhostip": true, "stricthostkeychecking": true,
	"userknownhostsfile": true, "batchmode": true, "passwordauthentication": true,
	"kbdinteractiveauthentication": true, "pubkeyauthentication": true,
	"preferredauthentications": true, "connecttimeout": true, "connectionattempts": true,
	"addressfamily": true, "compression": true,
}

func invalid() error { return keystore.Error("SSH 包格式或静态配置无效。") }
func CheckName(name string) error {
	if !bundleName.MatchString(name) {
		return keystore.Error("SSH 包名称须为 1–64 个字母、数字或 _ . -，以字母或数字开头。")
	}
	return nil
}

func Encode(b *Bundle) (string, error) {
	if err := Validate(b); err != nil {
		return "", err
	}
	data, err := json.Marshal(b)
	if err != nil {
		return "", invalid()
	}
	return string(data), nil
}

func Decode(value string) (*Bundle, error) {
	if !utf8.ValidString(value) {
		return nil, invalid()
	}
	// The standard struct decoder otherwise silently accepts duplicate fields.
	check := json.NewDecoder(strings.NewReader(value))
	if err := uniqueJSON(check); err != nil {
		return nil, invalid()
	}
	if _, err := check.Token(); err != io.EOF {
		return nil, invalid()
	}
	d := json.NewDecoder(strings.NewReader(value))
	d.DisallowUnknownFields()
	var b Bundle
	if d.Decode(&b) != nil {
		return nil, invalid()
	}
	if err := Validate(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

func uniqueJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return invalid()
			}
			seen[name] = true
			if err := uniqueJSON(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := uniqueJSON(d); err != nil {
				return err
			}
		}
	default:
		return invalid()
	}
	_, err = d.Token()
	return err
}

func cleanArg(s string, raw bool) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == 0 || r == '\r' || r == '\n' || (!raw && unicode.IsControl(r)) {
			return false
		}
	}
	return true
}

func validateOption(o Option) bool {
	if !optionNames[o.Name] || len(o.Args) == 0 {
		return false
	}
	if o.Name != "identityfile" && o.Name != "certificatefile" && o.Name != "userknownhostsfile" && len(o.Args) != 1 {
		return false
	}
	if (o.Name == "identityfile" || o.Name == "certificatefile") && len(o.Args) != 1 {
		return false
	}
	for _, a := range o.Args {
		if !cleanArg(a, o.Name == "proxycommand") {
			return false
		}
	}
	a := o.Args[0]
	switch o.Name {
	case "hostname", "user", "hostkeyalias":
		return !strings.ContainsFunc(a, unicode.IsSpace)
	case "port":
		n, e := strconv.Atoi(a)
		return e == nil && n > 0 && n <= 65535
	case "serveraliveinterval", "serveralivecountmax", "connecttimeout", "connectionattempts":
		_, e := strconv.ParseUint(a, 10, 32)
		return e == nil
	case "identitiesonly", "checkhostip", "batchmode", "passwordauthentication", "kbdinteractiveauthentication", "compression":
		return a == "yes" || a == "no"
	case "pubkeyauthentication":
		return a == "yes" || a == "no" || a == "unbound" || a == "host-bound"
	case "stricthostkeychecking":
		return a == "yes" || a == "no" || a == "ask" || a == "accept-new" || a == "off"
	case "addressfamily":
		return a == "any" || a == "inet" || a == "inet6"
	case "preferredauthentications":
		return !strings.ContainsFunc(a, unicode.IsSpace)
	case "proxyjump":
		if a == "none" {
			return true
		}
		for _, name := range strings.Split(a, ",") {
			if !hostName.MatchString(name) {
				return false
			}
		}
	}
	return true
}

func Validate(b *Bundle) error {
	if b == nil || b.Type != "zkeys-ssh" || b.Version != 1 || len(b.Hosts) == 0 || len(b.Files) == 0 {
		return invalid()
	}
	files := map[string]File{}
	for _, f := range b.Files {
		if !fileID.MatchString(f.ID) || len(f.Data) == 0 {
			return invalid()
		}
		if _, ok := files[f.ID]; ok {
			return invalid()
		}
		switch f.Kind {
		case "private", "public", "certificate", "known_hosts":
		default:
			return invalid()
		}
		files[f.ID] = f
	}
	hosts := map[string]Host{}
	used := map[string]bool{}
	for _, h := range b.Hosts {
		if !hostName.MatchString(h.Name) {
			return invalid()
		}
		if _, ok := hosts[h.Name]; ok {
			return invalid()
		}
		hosts[h.Name] = h
		seen := map[string]bool{}
		realKey := false
		repeated := map[string]bool{}
		for _, o := range h.Options {
			if !validateOption(o) {
				return invalid()
			}
			if seen[o.Name] && o.Name != "identityfile" && o.Name != "certificatefile" {
				return invalid()
			}
			if (o.Name == "proxyjump" && seen["proxycommand"]) || (o.Name == "proxycommand" && seen["proxyjump"]) {
				return invalid()
			}
			seen[o.Name] = true
			for _, a := range o.Args {
				kind := ""
				switch o.Name {
				case "identityfile":
					if a == "none" {
						continue
					}
					kind = "private"
					realKey = true
				case "certificatefile":
					kind = "certificate"
				case "userknownhostsfile":
					if a == "none" || a == "/dev/null" {
						continue
					}
					kind = "known_hosts"
				default:
					continue
				}
				f, ok := files[a]
				if !ok || f.Kind != kind {
					return invalid()
				}
				if repeated[o.Name+"\x00"+a] {
					return invalid()
				}
				repeated[o.Name+"\x00"+a] = true
				used[a] = true
			}
		}
		if !seen["hostname"] || !seen["user"] || !seen["port"] || !realKey {
			return invalid()
		}
	}
	for _, f := range b.Files {
		if !used[f.ID] && f.Kind != "public" {
			return invalid()
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) error
	visit = func(name string) error {
		if visiting[name] {
			return invalid()
		}
		if done[name] {
			return nil
		}
		h, ok := hosts[name]
		if !ok {
			return invalid()
		}
		visiting[name] = true
		for _, o := range h.Options {
			if o.Name == "proxyjump" && o.Args[0] != "none" {
				for _, dep := range strings.Split(o.Args[0], ",") {
					if err := visit(dep); err != nil {
						return err
					}
				}
			}
		}
		delete(visiting, name)
		done[name] = true
		return nil
	}
	for name := range hosts {
		if err := visit(name); err != nil {
			return err
		}
	}
	return nil
}

// Identical key files can be referenced by several source paths.
func sameFile(a File, kind string, data []byte) bool {
	return a.Kind == kind && bytes.Equal(a.Data, data)
}

package schema

import (
	"strconv"
	"strings"
)

// KeySeparator is the separator used to separate the domains, family name, and semantic version in a Key
// (and therefore also the filename of a schema).
const KeySeparator byte = '_'

// KeySeparatorString is the string representation of KeySeparator.
var KeySeparatorString = string(KeySeparator)

// Key is a string uniquely identifying a JSON Schema Manager schema in a schema cache in memory.
// The schema filename in the registry is the key suffixed by ".schema.json".
// Keys are always valid, and are used in contexts where many keys might be managed
// in lists or maps. Contrast with Core, which has explicit validation, and uses separate
// properties for each component.
type Key string

// NewKey creates a new Key from a string that is supposedly a valid key.
// If the string is not a valid key, an error is returned.
func NewKey(s string) (Key, error) {
	// check that the string is a valid key by tring to build a Core from it.
	if _, e := NewCoreFromString(s, KeySeparator); e != nil {
		return "", e
	}
	// We know it's a valid key, so just return it.
	return Key(s), nil
}

// ParseKey creates a Key from a string written with either separator: `domain_family_1_0_0`, or
// `domain/family/1/0/0`, which is easier to type and read. Domains and family names cannot contain
// either separator, so the two forms cannot be confused.
func ParseKey(s string) (Key, error) {
	if !strings.Contains(s, "/") {
		return NewKey(s)
	}
	c, err := NewCoreFromString(s, '/')
	if err != nil {
		return "", err
	}
	return c.Key(), nil
}

// Domain returns the domain components of the key.
func (k Key) Domain() []string {
	parts := strings.Split(string(k), KeySeparatorString)
	return parts[:len(parts)-4]
}

// FamilyName returns the family name component of the key.
func (k Key) FamilyName() string {
	parts := strings.Split(string(k), KeySeparatorString)
	return parts[len(parts)-4]
}

// Major returns the major version component of the key.
func (k Key) Major() uint64 {
	parts := strings.Split(string(k), KeySeparatorString)
	major, _ := strconv.ParseUint(parts[len(parts)-3], 10, 64)
	return major
}

// Minor returns the minor version component of the key.
func (k Key) Minor() uint64 {
	parts := strings.Split(string(k), KeySeparatorString)
	minor, _ := strconv.ParseUint(parts[len(parts)-2], 10, 64)
	return minor
}

// Patch returns the patch version component of the key.
func (k Key) Patch() uint64 {
	parts := strings.Split(string(k), KeySeparatorString)
	patch, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	return patch
}

// Version returns the semantic version of the key.
func (k Key) Version() SemVer {
	return SemVer{k.Major(), k.Minor(), k.Patch()}
}

// InScope returns true if the key falls within the given search scope.
func (k Key) InScope(ss SearchScope) bool {
	if ss == "" {
		return true
	}
	scopeParts := strings.Split(string(ss), "/")
	keyParts := strings.Split(string(k), KeySeparatorString)

	if len(scopeParts) > len(keyParts) {
		return false
	}

	for i := range scopeParts {
		if scopeParts[i] != keyParts[i] {
			return false
		}
	}
	return true
}

package app

import (
	"fmt"
	"slices"
	"strings"
)

// formatValue implements pflag.Value to provide a custom type name in help text
// and validation for output formats.
type formatValue struct {
	value   string
	allowed []string // the formats the command supports, the first being the default
}

// newFormatValue returns a format flag value which accepts the given formats, and defaults to the first.
func newFormatValue(allowed ...string) *formatValue {
	return &formatValue{value: allowed[0], allowed: allowed}
}

func (f *formatValue) String() string {
	return f.value
}

const (
	formatJSON  = "json"
	formatJUnit = "junit"
	formatText  = "text"
)

func (f *formatValue) Set(v string) error {
	if !slices.Contains(f.allowed, v) {
		return fmt.Errorf("must be one of %s", f.usage())
	}
	f.value = v
	return nil
}

// usage lists the allowed formats, for the flag's help text.
func (f *formatValue) usage() string {
	return strings.Join(f.allowed, ", ")
}

func (f *formatValue) Type() string {
	return "<format>"
}

// pathValue implements pflag.Value to provide a custom type name in help text.
type pathValue string

func (p *pathValue) String() string {
	return string(*p)
}

func (p *pathValue) Set(v string) error {
	*p = pathValue(v)
	return nil
}

func (p *pathValue) Type() string {
	return "<path>"
}

package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatValue(t *testing.T) {
	t.Parallel()

	t.Run("defaults to the first format", func(t *testing.T) {
		t.Parallel()
		f := newFormatValue(formatText, formatJSON)
		assert.Equal(t, "text", f.String())
		assert.Equal(t, "<format>", f.Type())
	})

	t.Run("valid values", func(t *testing.T) {
		t.Parallel()
		f := newFormatValue(formatText, formatJSON, formatJUnit)
		for _, v := range []string{"json", "junit", "text"} {
			require.NoError(t, f.Set(v))
			assert.Equal(t, v, f.String())
		}
	})

	t.Run("invalid value", func(t *testing.T) {
		t.Parallel()
		f := newFormatValue(formatText, formatJSON)
		require.EqualError(t, f.Set("junit"), "must be one of text, json")
		assert.Equal(t, "text", f.String())
	})
}

func TestPathValue(t *testing.T) {
	t.Parallel()

	p := pathValue("")
	assert.Empty(t, p.String())
	assert.Equal(t, "<path>", p.Type())

	t.Run("set value", func(t *testing.T) {
		t.Parallel()
		err := p.Set("/some/path")
		require.NoError(t, err)
		assert.Equal(t, "/some/path", p.String())
	})
}

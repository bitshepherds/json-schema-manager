package validator

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailedKeywords(t *testing.T) {
	t.Parallel()

	const id = "https://example.com/s.schema.json"
	c := NewSanthoshCompiler()
	require.NoError(t, c.AddSchema(id, map[string]any{
		"type":     "object",
		"required": []any{"total"},
		"properties": map[string]any{
			"status": map[string]any{"enum": []any{"paid"}},
			"note":   map[string]any{"type": "string", "maxLength": 2.0},
			"never":  false,
		},
	}))
	v, err := c.Compile(id)
	require.NoError(t, err)

	err = v.Validate(map[string]any{"status": "x", "note": "long"})
	assert.Equal(t, []string{"#/properties/note/maxLength", "#/properties/status/enum", "#/required"},
		FailedKeywords(err, id))

	err = v.Validate(map[string]any{"total": 1.0, "never": 1.0})
	assert.Equal(t, []string{"#/properties/never"}, FailedKeywords(err, id), "a false schema has no keyword")

	err = v.Validate("not an object")
	assert.Equal(t, []string{"#/type"}, FailedKeywords(err, id))

	assert.Nil(t, FailedKeywords(errors.New("other"), id))
	assert.Nil(t, FailedKeywords(nil, id))
}

func TestSanthoshCompiler_AssertFormat(t *testing.T) {
	t.Parallel()

	const id = "https://example.com/email.schema.json"
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"format":  "email",
	}
	compile := func(c Compiler) Validator {
		require.NoError(t, c.AddSchema(id, schema))
		v, err := c.Compile(id)
		require.NoError(t, err)
		return v
	}

	c := NewSanthoshCompiler()
	require.NoError(t, compile(c).Validate("not-an-email"), "format is an annotation under 2020-12")

	fa, ok := c.(FormatAsserter)
	require.True(t, ok)
	fa.AssertFormat()
	c.Clear()
	v := compile(c)
	require.Error(t, v.Validate("not-an-email"), "format is asserted, and stays so after Clear")
	require.NoError(t, v.Validate("ann@example.com"))
}

func TestSanthoshCompiler_ExternalRef(t *testing.T) {
	t.Parallel()

	const id = "https://example.com/place.schema.json"
	c := NewSanthoshCompiler()
	require.NoError(t, c.AddSchema(id, map[string]any{"$ref": "https://geojson.org/schema/Point.json"}))
	_, err := c.Compile(id)
	require.ErrorContains(t, err,
		"$ref https://geojson.org/schema/Point.json is external, and external $refs are not supported")
}

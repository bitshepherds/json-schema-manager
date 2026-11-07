package schema

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpec(t *testing.T) {
	t.Parallel()
	s := New("domain_family_1_0_0", nil)
	ti := TestInfo{Path: "test.json"}
	spec := NewSpec(s, ti, TestDocTypePass)

	assert.Equal(t, s, spec.Schema)
	assert.Equal(t, ti, spec.TestInfo)
	assert.Equal(t, TestDocTypePass, spec.TestDocType)
	assert.Equal(t, RuleOwnDocuments, spec.Rule)
	assert.Equal(t, s.Key(), spec.DocumentFrom)
}

func TestSpec_Run(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		testDocType TestDocType
		validateErr error
		wantErrType interface{}
	}{
		{
			name:        "Pass test doc succeeds",
			testDocType: TestDocTypePass,
			validateErr: nil,
			wantErrType: nil,
		},
		{
			name:        "Pass test doc fails",
			testDocType: TestDocTypePass,
			validateErr: errors.New("validation failed"),
			wantErrType: &PassTestFailedError{},
		},
		{
			name:        "Fail test doc succeeds (it fails validation as expected)",
			testDocType: TestDocTypeFail,
			validateErr: errors.New("validation failed"),
			wantErrType: nil,
		},
		{
			name:        "Fail test doc fails (it passes validation unexpectedly)",
			testDocType: TestDocTypeFail,
			validateErr: nil,
			wantErrType: &FailTestPassedError{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := setupTestRegistry(t)
			s := New(Key("domain_family_1_0_0"), r)
			ti := TestInfo{
				Path:         "test.json",
				Unmarshalled: map[string]interface{}{},
			}
			spec := NewSpec(s, ti, tt.testDocType)
			v := &mockValidator{Err: tt.validateErr}

			err := spec.Run(v)

			if tt.wantErrType != nil {
				require.Error(t, err)
				//nolint:testifylint // IsType is appropriate for table-driven tests with interface{}
				assert.IsType(t, tt.wantErrType, err)
				assert.Equal(t, err, spec.Err)
			} else {
				require.NoError(t, err)
				require.NoError(t, spec.Err)
			}
		})
	}
}

func TestSpec_ResultLabel(t *testing.T) {
	t.Parallel()

	across := Spec{
		Schema:       New("shop_order_1_0_0", nil),
		Rule:         RuleNoLoosening,
		DocumentFrom: "shop_order_1_1_0",
		Earlier:      "shop_order_1_0_0",
		Later:        "shop_order_1_1_0",
	}

	tests := []struct {
		name        string
		testDocType TestDocType
		err         error
		across      bool
		want        string
	}{
		{
			name:        "Pass test doc - passed",
			testDocType: TestDocTypePass,
			err:         nil,
			want:        "passed",
		},
		{
			name:        "Pass test doc - failed",
			testDocType: TestDocTypePass,
			err:         errors.New("fail"),
			want:        "failed",
		},
		{
			name:        "Fail test doc - failed as expected",
			testDocType: TestDocTypeFail,
			err:         nil,
			want:        "failed, as expected",
		},
		{
			name:        "Fail test doc - passed unexpectedly",
			testDocType: TestDocTypeFail,
			err:         errors.New("pass"),
			want:        "passed, when expected fail",
		},
		{
			name:        "Pass test doc across versions - rejected",
			testDocType: TestDocTypePass,
			err:         errors.New("fail"),
			across:      true,
			want:        "no-loosening broken: pass document of 1.1.0 rejected by 1.0.0",
		},
		{
			name:        "Pass test doc across versions - accepted",
			testDocType: TestDocTypePass,
			across:      true,
			want:        "no-loosening holds: pass document of 1.1.0 accepted by 1.0.0",
		},
		{
			name:        "Fail test doc across versions - accepted",
			testDocType: TestDocTypeFail,
			err:         errors.New("pass"),
			across:      true,
			want:        "no-loosening broken: fail document of 1.1.0 accepted by 1.0.0",
		},
		{
			name:        "Fail test doc across versions - rejected",
			testDocType: TestDocTypeFail,
			across:      true,
			want:        "no-loosening holds: fail document of 1.1.0 rejected by 1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec := Spec{}
			if tt.across {
				spec = across
			}
			spec.TestDocType = tt.testDocType
			spec.Err = tt.err
			assert.Equal(t, tt.want, spec.ResultLabel())
		})
	}
}

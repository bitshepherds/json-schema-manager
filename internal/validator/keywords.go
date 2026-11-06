package validator

import (
	"errors"
	"slices"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// FailedKeywords returns the locations of the keywords which rejected a document, given the error
// Validate returned. A keyword in the schema with the ID rootID is given as a fragment, such as
// "#/properties/total/type"; one in another schema is given with that schema's URL. It returns
// nil for an error which does not come from validation.
func FailedKeywords(err error, rootID string) []string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	var out []string
	collectFailedKeywords(ve, strings.TrimSuffix(rootID, "#"), &out)
	slices.Sort(out)
	return slices.Compact(out)
}

// collectFailedKeywords adds the locations of the keywords at the leaves of ve to out.
func collectFailedKeywords(ve *jsonschema.ValidationError, rootID string, out *[]string) {
	if len(ve.Causes) > 0 {
		for _, c := range ve.Causes {
			collectFailedKeywords(c, rootID, out)
		}
		return
	}
	loc := strings.TrimPrefix(ve.SchemaURL, rootID)
	if kp := ve.ErrorKind.KeywordPath(); len(kp) > 0 {
		loc += "/" + strings.Join(kp, "/")
	}
	*out = append(*out, loc)
}

// FaultLocations returns the instance location of each independent reason a document was rejected, given
// the error Validate returned. An anyOf or oneOf counts once, at its own location, however its arms
// failed, so it doesn't look like several faults. It returns nil for an error which does not come
// from validation.
func FaultLocations(err error) []string {
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	var out []string
	collectFaultLocations(ve, &out)
	slices.Sort(out)
	return slices.Compact(out)
}

func collectFaultLocations(ve *jsonschema.ValidationError, out *[]string) {
	switch ve.ErrorKind.(type) {
	case *kind.AnyOf, *kind.OneOf:
	default:
		if len(ve.Causes) > 0 {
			for _, c := range ve.Causes {
				collectFaultLocations(c, out)
			}
			return
		}
	}
	*out = append(*out, "/"+strings.Join(ve.InstanceLocation, "/"))
}

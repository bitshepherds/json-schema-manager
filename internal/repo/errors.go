package repo

import (
	"fmt"

	"github.com/bitshepherds/json-schema-manager/internal/config"
)

// ShallowCloneError is returned when no deployment tag can be found in a shallow clone,
// so JSM cannot tell which schemas have been deployed.
type ShallowCloneError struct {
	Env config.Env
}

func (e *ShallowCloneError) Error() string {
	return fmt.Sprintf("cannot find the last deployment to %s: this is a shallow clone and no %s/%s/* tag "+
		"is in its history. Fetch the full history and tags (with actions/checkout, set fetch-depth: 0)",
		e.Env, JSMDeployTagPrefix, e.Env)
}

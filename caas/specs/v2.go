// Copyright 2019 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package specs

import (
	"github.com/juju/errors"
)

// PodSpecV2 defines the data values used to configure
// a pod on the CAAS substrate for version 2.
type PodSpecV2 struct {
	podSpec        `yaml:",inline"`
	ServiceAccount *ServiceAccountSpec `json:"serviceAccount,omitempty"`
}

// Version2 defines the version number for pod spec version 2.
const Version2 Version = 2

// Validate returns an error if the spec is not valid.
func (spec *PodSpecV2) Validate() error {
	if err := spec.podSpec.Validate(Version2); err != nil {
		return errors.Trace(err)
	}
	if spec.ServiceAccount != nil {
		return errors.Trace(spec.ServiceAccount.Validate())
	}
	return nil
}

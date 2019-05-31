// Copyright 2012-2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environ

import (
	"reflect"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"gopkg.in/juju/worker.v1/catacomb"

	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/core/watcher"
	"github.com/juju/juju/environs"
)

var logger = loggo.GetLogger("juju.worker.environ")

// ConfigObserver exposes a model configuration and a watch constructor
// that allows clients to be informed of changes to the configuration.
type ConfigObserver interface {
	environs.EnvironConfigGetter
	WatchForModelConfigChanges() (watcher.NotifyWatcher, error)
	WatchCloudSpecChanges() (watcher.NotifyWatcher, error)
}

// Config describes the dependencies of a Tracker.
//
// It's arguable that it should be called TrackerConfig, because of the heavy
// use of model config in this package.
type Config struct {
	Observer       ConfigObserver
	NewEnvironFunc environs.NewEnvironFunc
}

// Validate returns an error if the config cannot be used to start a Tracker.
func (config Config) Validate() error {
	if config.Observer == nil {
		return errors.NotValidf("nil Observer")
	}
	if config.NewEnvironFunc == nil {
		return errors.NotValidf("nil NewEnvironFunc")
	}
	return nil
}

// Tracker loads an environment, makes it available to clients, and updates
// the environment in response to config changes until it is killed.
type Tracker struct {
	config           Config
	catacomb         catacomb.Catacomb
	environ          environs.Environ
	currentCloudSpec environs.CloudSpec
}

// NewTracker loads an environment from the observer and returns a new Tracker,
// or an error if anything goes wrong. If a tracker is returned, its Environ()
// method is immediately usable.
//
// The caller is responsible for Kill()ing the returned Tracker and Wait()ing
// for any errors it might return.
func NewTracker(config Config) (*Tracker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	environ, spec, err := environs.GetEnvironAndCloud(config.Observer, config.NewEnvironFunc)
	if err != nil {
		return nil, errors.Annotate(err, "cannot create environ")
	}

	t := &Tracker{
		config:           config,
		environ:          environ,
		currentCloudSpec: *spec,
	}
	err = catacomb.Invoke(catacomb.Plan{
		Site: &t.catacomb,
		Work: t.loop,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return t, nil
}

// Environ returns the encapsulated Environ. It will continue to be updated in
// the background for as long as the Tracker continues to run.
func (t *Tracker) Environ() environs.Environ {
	return t.environ
}

// ErrModelRemoved indicates that this worker was operating on the model that is no longer found.
var ErrModelRemoved = errors.New("model has been removed")

func (t *Tracker) loop() error {
	environWatcher, err := t.config.Observer.WatchForModelConfigChanges()
	if err != nil {
		return errors.Annotate(err, "cannot watch environ config")
	}
	if err := t.catacomb.Add(environWatcher); err != nil {
		return errors.Trace(err)
	}

	// Some environs support reacting to changes in the cloud config.
	// Set up a watcher if that's the case.
	var (
		cloudWatcherChanges watcher.NotifyChannel
		cloudSpecSetter     environs.CloudSpecSetter
		ok                  bool
	)
	if cloudSpecSetter, ok = t.environ.(environs.CloudSpecSetter); !ok {
		logger.Warningf("cloud type %v doesn't support dynamic changing of cloud spec", t.environ.Config().Type())
	} else {
		cloudWatcher, err := t.config.Observer.WatchCloudSpecChanges()
		if err != nil {
			return errors.Annotate(err, "cannot watch environ cloud spec")
		}
		if err := t.catacomb.Add(environWatcher); err != nil {
			return errors.Trace(err)
		}
		cloudWatcherChanges = cloudWatcher.Changes()
	}
	for {
		logger.Debugf("waiting for environ watch notification")
		select {
		case <-t.catacomb.Dying():
			return t.catacomb.ErrDying()
		case _, ok := <-environWatcher.Changes():
			if !ok {
				return errors.New("environ config watch closed")
			}
			logger.Debugf("reloading environ config")
			modelConfig, err := t.config.Observer.ModelConfig()
			if err != nil {
				if params.IsCodeNotFound(err) {
					return ErrModelRemoved
				}
				return errors.Annotate(err, "cannot read environ config")
			}
			if err = t.environ.SetConfig(modelConfig); err != nil {
				return errors.Annotate(err, "cannot update environ config")
			}
		case _, ok := <-cloudWatcherChanges:
			if !ok {
				return errors.New("cloud watch closed")
			}
			cloudSpec, err := t.config.Observer.CloudSpec()
			if err != nil {
				return errors.Annotate(err, "cannot read environ config")
			}
			if reflect.DeepEqual(cloudSpec, t.currentCloudSpec) {
				continue
			}
			logger.Debugf("reloading cloud config")
			if err = cloudSpecSetter.SetCloudSpec(cloudSpec); err != nil {
				return errors.Annotate(err, "cannot update environ cloud spec")
			}
			t.currentCloudSpec = cloudSpec
		}
	}
}

// Kill is part of the worker.Worker interface.
func (t *Tracker) Kill() {
	t.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (t *Tracker) Wait() error {
	return t.catacomb.Wait()
}

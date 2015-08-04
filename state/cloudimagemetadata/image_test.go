// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudimagemetadata_test

import (
	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/txn"
	txntesting "github.com/juju/txn/testing"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/mongo"
	"github.com/juju/juju/state/cloudimagemetadata"
)

type cloudImageMetadataSuite struct {
	testing.IsolatedMgoSuite

	runner  txn.Runner
	storage cloudimagemetadata.Storage
}

var _ = gc.Suite(&cloudImageMetadataSuite{})

const (
	envName        = "test-env"
	collectionName = "test-collection"
)

func (s *cloudImageMetadataSuite) SetUpTest(c *gc.C) {
	s.IsolatedMgoSuite.SetUpTest(c)

	db := s.MgoSuite.Session.DB("juju")
	collectionAccessor := func(name string) (_ mongo.Collection, closer func()) {
		return mongo.WrapCollection(db.C(name)), func() {}
	}

	s.runner = txn.NewRunner(txn.RunnerParams{Database: db})
	runTransaction := func(transactions txn.TransactionSource) error {
		return s.runner.Run(transactions)
	}

	s.storage = cloudimagemetadata.NewStorage(envName, collectionName, runTransaction, collectionAccessor)
}

func (s *cloudImageMetadataSuite) TestSaveMetadata(c *gc.C) {
	attrs := cloudimagemetadata.MetadataAttributes{
		Stream:          "stream",
		Region:          "region-test",
		Series:          "series",
		Arch:            "arch",
		VirtualType:     "virtType-test",
		RootStorageType: "rootStorageType-test",
		RootStorageSize: "rootStorageSize-test"}

	added := cloudimagemetadata.Metadata{attrs, "1"}
	s.assertRecordMetadata(c, added)
	s.assertMetadataRecorded(c, attrs, added)

}

func (s *cloudImageMetadataSuite) TestFindMetadataNotFound(c *gc.C) {
	// No metadata is stored yet.
	// So when looking for all and none is found, err.
	found, err := s.storage.FindMetadata(cloudimagemetadata.MetadataAttributes{})
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
	c.Assert(err, gc.ErrorMatches, "matching cloud image metadata not found")
	c.Assert(found, gc.HasLen, 0)

	// insert something...
	attrs := cloudimagemetadata.MetadataAttributes{
		Stream:          "stream",
		Region:          "region",
		Series:          "series",
		Arch:            "arch",
		VirtualType:     "virtualType",
		RootStorageType: "rootStorageType",
		RootStorageSize: "rootStorageSize"}
	m := cloudimagemetadata.Metadata{attrs, "1"}
	s.assertRecordMetadata(c, m)

	// ...but look for something else.
	none, err := s.storage.FindMetadata(cloudimagemetadata.MetadataAttributes{
		Stream: "something else",
	})
	// Make sure that we are explicit that we could not find what we wanted.
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
	c.Assert(err, gc.ErrorMatches, "matching cloud image metadata not found")
	c.Assert(none, gc.HasLen, 0)
}

func (s *cloudImageMetadataSuite) TestFindMetadata(c *gc.C) {
	attrs := cloudimagemetadata.MetadataAttributes{
		Stream:          "stream",
		Region:          "region",
		Series:          "series",
		Arch:            "arch",
		VirtualType:     "virtualType",
		RootStorageType: "rootStorageType",
		RootStorageSize: "rootStorageSize"}

	m := cloudimagemetadata.Metadata{attrs, "1"}

	_, err := s.storage.FindMetadata(attrs)
	c.Assert(err, jc.Satisfies, errors.IsNotFound)

	s.assertRecordMetadata(c, m)
	expected := []cloudimagemetadata.Metadata{m}
	s.assertMetadataRecorded(c, attrs, expected...)

	attrs.Stream = "another_stream"
	m = cloudimagemetadata.Metadata{attrs, "2"}
	s.assertRecordMetadata(c, m)

	expected = append(expected, m)
	// Should find both
	s.assertMetadataRecorded(c, cloudimagemetadata.MetadataAttributes{Region: "region"}, expected...)
}

func (s *cloudImageMetadataSuite) TestSaveMetadataUpdateSameAttrsAndImages(c *gc.C) {
	attrs := cloudimagemetadata.MetadataAttributes{
		Stream: "stream",
		Series: "series",
		Arch:   "arch",
	}
	metadata0 := cloudimagemetadata.Metadata{attrs, "1"}
	metadata1 := cloudimagemetadata.Metadata{attrs, "1"}

	s.assertRecordMetadata(c, metadata0)
	s.assertRecordMetadata(c, metadata1)
	s.assertMetadataRecorded(c, attrs, metadata1)
}

func (s *cloudImageMetadataSuite) TestSaveMetadataUpdateSameAttrsDiffImages(c *gc.C) {
	attrs := cloudimagemetadata.MetadataAttributes{
		Stream: "stream",
		Series: "series",
		Arch:   "arch",
	}
	metadata0 := cloudimagemetadata.Metadata{attrs, "1"}
	metadata1 := cloudimagemetadata.Metadata{attrs, "12"}

	s.assertRecordMetadata(c, metadata0)
	s.assertMetadataRecorded(c, attrs, metadata0)
	s.assertRecordMetadata(c, metadata1)
	s.assertMetadataRecorded(c, attrs, metadata1)
}

func (s *cloudImageMetadataSuite) TestSaveMetadataConcurrently(c *gc.C) {
	attrs := cloudimagemetadata.MetadataAttributes{
		Stream: "stream",
		Series: "series",
		Arch:   "arch",
	}
	metadata0 := cloudimagemetadata.Metadata{attrs, "0"}
	metadata1 := cloudimagemetadata.Metadata{attrs, "1"}
	metadata1.Stream = "scream"
	addMetadata := func() {
		s.assertRecordMetadata(c, metadata0)
	}

	defer txntesting.SetBeforeHooks(c, s.runner, addMetadata).Check()

	s.assertRecordMetadata(c, metadata1)

	all, err := s.storage.FindMetadata(cloudimagemetadata.MetadataAttributes{})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(all, gc.HasLen, 2)
	c.Assert(all, jc.SameContents, []cloudimagemetadata.Metadata{
		metadata0,
		metadata1,
	})
}

func (s *cloudImageMetadataSuite) assertRecordMetadata(c *gc.C, m cloudimagemetadata.Metadata) {
	err := s.storage.SaveMetadata(m)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *cloudImageMetadataSuite) assertMetadataRecorded(c *gc.C, criteria cloudimagemetadata.MetadataAttributes, expected ...cloudimagemetadata.Metadata) {
	metadata, err := s.storage.FindMetadata(criteria)
	c.Assert(err, jc.ErrorIsNil)

	c.Assert(metadata, jc.SameContents, expected)
}

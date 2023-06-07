package boltutil

import (
	"errors"
	"fmt"

	"go.etcd.io/bbolt"
)

var ErrNotFound = errors.New("not found")
var ErrKeyNotFound = fmt.Errorf("key %w", ErrNotFound)
var ErrBucketNotFound = fmt.Errorf("bucket %w", ErrNotFound)
var ErrKeyAlreadyExists = errors.New("key already exists")

func FindBucket(tx *bbolt.Tx, name []byte) (*bbolt.Bucket, error) {
	b := tx.Bucket(name)
	if b == nil {
		return nil, fmt.Errorf("%w: %q", ErrBucketNotFound, name)
	}
	return b, nil
}

func MustGetBucket(tx *bbolt.Tx, name []byte) *bbolt.Bucket {
	b, err := FindBucket(tx, name)
	if err != nil {
		panic(err)
	}
	return b
}

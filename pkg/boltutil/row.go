package boltutil

import (
	"fmt"

	"go.etcd.io/bbolt"
)

type Row struct {
	Key   RowKey
	Value RowValue
}

func (r *Row) Size() int      { return r.KeySize() + r.ValueSize() }
func (r *Row) ValueSize() int { return len(r.Value) }
func (r *Row) KeySize() int   { return len(r.Key) }

type RowKey []byte
type RowValue []byte

// Utilities for using inside of a Go text/template.
func (k RowKey) String() string   { return string(k) }
func (v RowValue) String() string { return string(v) }

// CRUD operations

// Sets the value at the given key only if the key is not defined yet.
func CreateRow(b *bbolt.Bucket, key, value []byte) error {
	err := CheckRowKeyIsAvailable(b, key)
	if err != nil {
		return err
	}
	return b.Put(key, value)
}

// Returns the value associated with the given key,
// if the key is not found, an error is returned.
func ReadRow(b *bbolt.Bucket, key []byte) ([]byte, error) {
	v, err := FindRow(b, key)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// Changes the value associated with the given key,
// if the key is not found, an error is returned.
func UpdateRow(b *bbolt.Bucket, key, value []byte) error {
	_, err := FindRow(b, key)
	if err != nil {
		return err
	}
	return b.Put(key, value)
}

// Removes the key-value pair associated with the given key,
// if the key is not found, an error is returned.
func DeleteRow(b *bbolt.Bucket, key []byte) error {
	_, err := FindRow(b, key)
	if err != nil {
		return err
	}
	return b.Delete(key)
}

func ReadBucketRowPage(b *bbolt.Bucket, page, numRowsPerPage int) ([]*Row, error) {
	out := []*Row{}
	offset := page * numRowsPerPage
	c := b.Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		for i := 0; i < offset; i++ {
			k, v = c.Next()
			if k == nil {
				break
			}
		}
		if len(out) == numRowsPerPage {
			break
		}
		out = append(out, &Row{Key: k, Value: v})
	}
	return out, nil
}

func FindRow(b *bbolt.Bucket, key []byte) ([]byte, error) {
	v := b.Get(key)
	if v == nil {
		return nil, fmt.Errorf("%w: %q", ErrKeyNotFound, key)
	}
	return v, nil
}

func CheckRowKeyIsAvailable(b *bbolt.Bucket, key []byte) error {
	if b.Get(key) != nil {
		return fmt.Errorf("%w: %q", ErrKeyAlreadyExists, key)
	}
	return nil
}

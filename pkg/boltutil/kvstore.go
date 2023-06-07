package boltutil

import (
	"fmt"
	"os"
	"time"

	"github.com/ejuju/boltdb-webgui/pkg/kvstore"
	"go.etcd.io/bbolt"
)

type KeyValueDB struct {
	f *bbolt.DB
}

func NewKeyValueDB(fpath string) *KeyValueDB {
	// Open DB file
	f, err := bbolt.Open(fpath, os.ModePerm, &bbolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		panic(fmt.Errorf("open DB file: %w", err))
	}
	return &KeyValueDB{f: f}
}

func (db *KeyValueDB) Close() error { return db.f.Close() }

func (db *KeyValueDB) Size() (uint64, error) {
	out := uint64(0)
	return out, db.f.View(func(tx *bbolt.Tx) error {
		out = uint64(tx.Size())
		return nil
	})
}

func (db *KeyValueDB) DiskSize() (uint64, error) {
	out := uint64(0)
	return out, db.f.View(func(tx *bbolt.Tx) error {
		fstats, err := os.Stat(tx.DB().Path())
		if err != nil {
			return err
		}
		out = uint64(fstats.Size())
		return nil
	})
}

func (db *KeyValueDB) DiskPath() string { return db.f.Path() }

func (db *KeyValueDB) NumLists() (int, error) {
	out := 0
	return out, db.f.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(_ []byte, _ *bbolt.Bucket) error { out++; return nil })
	})
}

func (db *KeyValueDB) NumRows(list string) (uint64, error) {
	out := uint64(0)
	return out, db.f.View(func(tx *bbolt.Tx) error {
		b, err := findBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		out = uint64(b.Stats().KeyN)
		return nil
	})
}

func (db *KeyValueDB) CreateList(name string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		if tx.Bucket([]byte(name)) != nil {
			return kvstore.NewErrAlreadyExists(name)
		}
		_, err := tx.CreateBucket([]byte(name))
		return err
	})
}

func (db *KeyValueDB) DeleteList(name string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		return tx.DeleteBucket([]byte(name))
	})
}

func (db *KeyValueDB) ReadEachList(callback func(string) error) error {
	return db.f.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			return callback(string(name))
		})
	})
}

func (db *KeyValueDB) CreateRow(list string, row *kvstore.Row) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, err := findBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		if b.Get(row.Key) != nil {
			return kvstore.NewErrAlreadyExists(string(row.Key))
		}
		return b.Put(row.Key, row.Value)
	})
}

func (db *KeyValueDB) ReadRow(list string, key string) (*kvstore.Row, error) {
	out := &kvstore.Row{Key: []byte(key)}
	return out, db.f.View(func(tx *bbolt.Tx) error {
		_, v, err := findBucketRow(tx, []byte(list), []byte(key))
		if err != nil {
			return err
		}
		out.Value = v
		return nil
	})
}

func (db *KeyValueDB) ReadRowPage(list string, pageIndex, numRowsPerPage int) ([]*kvstore.Row, error) {
	var out []*kvstore.Row
	return out, db.f.View(func(tx *bbolt.Tx) error {
		b, err := findBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		offset := pageIndex * numRowsPerPage
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
			out = append(out, &kvstore.Row{Key: k, Value: v})
		}
		return nil
	})
}

func (db *KeyValueDB) ReadEachRow(list string, callback func(*kvstore.Row) error) error {
	return db.f.View(func(tx *bbolt.Tx) error {
		b, err := findBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		return b.ForEach(func(k, v []byte) error {
			return callback(&kvstore.Row{Key: k, Value: v})
		})
	})
}

func (db *KeyValueDB) UpdateRow(list string, key string, newValue string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, _, err := findBucketRow(tx, []byte(list), []byte(key))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), []byte(newValue))
	})
}
func (db *KeyValueDB) DeleteRow(list string, key string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, _, err := findBucketRow(tx, []byte(list), []byte(key))
		if err != nil {
			return err
		}
		return b.Delete([]byte(key))
	})
}

func findBucket(tx *bbolt.Tx, name []byte) (*bbolt.Bucket, error) {
	b := tx.Bucket(name)
	if b == nil {
		return nil, kvstore.NewErrNotFound(string(name))
	}
	return b, nil
}

func findBucketRow(tx *bbolt.Tx, bucketName, key []byte) (*bbolt.Bucket, []byte, error) {
	b, err := findBucket(tx, bucketName)
	if err != nil {
		return nil, nil, err
	}
	v := b.Get([]byte(key))
	if v == nil {
		return b, nil, kvstore.NewErrNotFound(string(key))
	}
	return b, v, nil
}

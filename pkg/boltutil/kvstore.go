package boltutil

import (
	"fmt"
	"os"
	"time"

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

func (db *KeyValueDB) NumLists() (int, error) {
	out := 0
	return out, db.f.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(_ []byte, _ *bbolt.Bucket) error { out++; return nil })
	})
}

func (db *KeyValueDB) NumRows(list string) (uint64, error) {
	out := uint64(0)
	return out, db.f.View(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		out = uint64(b.Stats().KeyN)
		return nil
	})
}

func (db *KeyValueDB) CreateList(name string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucket([]byte(name))
		return err
	})
}

func (db *KeyValueDB) DeleteList(name string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		return tx.DeleteBucket([]byte(name))
	})
}

func (db *KeyValueDB) ForEachList(callback func(string) error) error {
	return db.f.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
			return callback(string(name))
		})
	})
}

func (db *KeyValueDB) CreateRow(list string, row *Row) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		err = CheckRowKeyIsAvailable(b, row.Key)
		if err != nil {
			return err
		}
		return b.Put(row.Key, row.Value)
	})
}

func (db *KeyValueDB) ReadRow(list string, key RowKey) (*Row, error) {
	out := &Row{Key: key}
	return out, db.f.View(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		out.Value, err = FindRow(b, []byte(key))
		if err != nil {
			return err
		}
		return nil
	})
}

func (db *KeyValueDB) UpdateRow(list string, key string, newValue string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		_, err = FindRow(b, []byte(key))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), []byte(newValue))
	})
}
func (db *KeyValueDB) DeleteRow(list string, key string) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		_, err = FindRow(b, []byte(key))
		if err != nil {
			return err
		}
		return b.Delete([]byte(key))
	})
}

func (db *KeyValueDB) ForEachRow(list string, callback func(*Row) error) error {
	return db.f.Update(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, []byte(list))
		if err != nil {
			return err
		}
		return b.ForEach(func(k, v []byte) error {
			return callback(&Row{Key: k, Value: v})
		})
	})
}

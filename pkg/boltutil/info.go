package boltutil

import (
	"os"

	"go.etcd.io/bbolt"
)

type DBInfo struct {
	Size       int64
	FileSize   float64
	NumBuckets int
	Buckets    map[string]*BucketInfo
}

func GetDBInfo(tx *bbolt.Tx) (*DBInfo, error) {
	info := &DBInfo{Buckets: make(map[string]*BucketInfo)}

	// Get file size
	fstats, err := os.Stat(tx.DB().Path())
	if err != nil {
		return nil, err
	}
	info.FileSize = float64(fstats.Size()) / 1_000_000.0 // in GB
	info.Size = tx.Size()

	// Get bucket stats for each bucket
	err = tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
		info.NumBuckets++
		bucketInfo, err := GetBucketInfo(b)
		if err != nil {
			return err
		}
		info.Buckets[string(name)] = bucketInfo
		return nil
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}

func OpenTxAndGetDBInfo(db *bbolt.DB) (*DBInfo, error) {
	var info *DBInfo
	return info, db.View(func(tx *bbolt.Tx) error {
		var err error
		info, err = GetDBInfo(tx)
		return err
	})
}

type BucketInfo struct {
	NumRows      int // Number of keys in bucket
	TotalRowSize int // Sum of size of each row's key and value
	AvgRowSize   int // Average size of a row in the bucket
}

func GetBucketInfo(b *bbolt.Bucket) (*BucketInfo, error) {
	info := &BucketInfo{}

	// Calculate total row size
	err := b.ForEach(func(k, v []byte) error {
		info.TotalRowSize += (&Row{Key: k, Value: v}).Size()
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Set num rows
	info.NumRows = b.Stats().KeyN

	// Calculate average only if rows are present
	if info.NumRows != 0 {
		info.AvgRowSize = info.TotalRowSize / info.NumRows
	}

	return info, nil
}

func OpenTxAndGetBucketInfo(db *bbolt.DB, bucketName []byte) (*BucketInfo, error) {
	var info *BucketInfo
	return info, db.View(func(tx *bbolt.Tx) error {
		b, err := FindBucket(tx, bucketName)
		if err != nil {
			return err
		}
		info, err = GetBucketInfo(b)
		return err
	})
}

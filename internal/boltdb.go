package internal

import "go.etcd.io/bbolt"

type BucketInfo struct {
	Name       string
	NumRows    int
	SubBuckets []*BucketInfo
}

func getBucketInfo(db *bbolt.DB, bucketID []byte) (*BucketInfo, error) {
	out := &BucketInfo{Name: string(bucketID)}
	return out, db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(bucketID)
		// Get num rows
		out.NumRows = b.Stats().KeyN
		// Get sub bucket info
		b.ForEachBucket(func(k []byte) error {
			subBucketInfo, err := getBucketInfo(db, k)
			if err != nil {
				return err
			}
			out.SubBuckets = append(out.SubBuckets, subBucketInfo)
			return nil
		})
		return nil
	})
}

func getAllBucketsInfo(db *bbolt.DB) ([]*BucketInfo, error) {
	out := []*BucketInfo{}
	return out, db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			bucketInfo, err := getBucketInfo(db, name)
			if err != nil {
				return err
			}
			out = append(out, bucketInfo)
			return nil
		})
	})
}

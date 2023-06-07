package kvstore

import (
	"errors"
	"fmt"
)

type DB interface {
	// General information
	Size() (uint64, error)     // size of DB according to DB
	DiskSize() (uint64, error) // size of disk file(s)
	DiskPath() string
	NumLists() (int, error)
	NumRows(list string) (uint64, error)

	// List operations
	CreateList(name string) error
	ReadEachList(callback func(string) error) error
	DeleteList(name string) error

	// List row operations
	CreateRow(list string, row *Row) error
	ReadRow(list string, key string) (*Row, error)
	ReadRowPage(list string, pageIndex, numRowsPerPage int) ([]*Row, error)
	ReadEachRow(list string, callback func(*Row) error) error
	UpdateRow(list string, key string, newValue string) error
	DeleteRow(list string, name string) error
}

var ErrAlreadyExists = errors.New("already exists")
var ErrNotFound = errors.New("not found")

func NewErrNotFound(id string) error      { return fmt.Errorf("%q %w", id, ErrNotFound) }
func NewErrAlreadyExists(id string) error { return fmt.Errorf("%q %w", id, ErrAlreadyExists) }

// Row represents a key-value pair in a list.
type Row struct {
	Key   RowKey
	Value RowValue
}

func (r *Row) Size() uint64      { return r.KeySize() + r.ValueSize() }
func (r *Row) ValueSize() uint64 { return uint64(len(r.Value)) }
func (r *Row) KeySize() uint64   { return uint64(len(r.Key)) }

type RowKey []byte
type RowValue []byte

// Utilities for using inside of a Go text/template.
func (k RowKey) String() string   { return string(k) }
func (v RowValue) String() string { return string(v) }

type DBInfo struct {
	Size     uint64
	DiskSize uint64
	NumLists int
	Lists    map[string]*ListInfo
}

type ListInfo struct {
	NumRows      uint64 // Number of keys in bucket
	TotalRowSize uint64 // Sum of size of each row's key and value
	AvgRowSize   uint64 // Average size of a row in the bucket
}

func GetDBInfo(db DB) (*DBInfo, error) {
	var err error

	info := &DBInfo{
		Lists: make(map[string]*ListInfo),
	}

	info.DiskSize, err = db.DiskSize()
	if err != nil {
		return nil, err
	}
	info.Size, err = db.Size()
	if err != nil {
		return nil, err
	}

	// Get bucket stats for each bucket
	err = db.ReadEachList(func(listName string) error {
		info.NumLists++
		listInfo, err := GetListInfo(db, listName)
		if err != nil {
			return err
		}
		info.Lists[listName] = listInfo
		return nil
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}

func GetListInfo(db DB, listName string) (*ListInfo, error) {
	info := &ListInfo{}

	// Calculate total row size
	err := db.ReadEachRow(listName, func(r *Row) error {
		info.TotalRowSize += r.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Set num rows
	info.NumRows, err = db.NumRows(listName)
	if err != nil {
		return nil, err
	}

	// Calculate average only if rows are present
	if info.NumRows != 0 {
		info.AvgRowSize = info.TotalRowSize / info.NumRows
	}

	return info, nil
}

# Web GUI for BoltDB files

## Installation

1. Install Go
2. Run `go install github.com/ejuju/boltdb-webgui`

## Usage

1. Run `boltdb-webgui ./your_file 8080`
2. Open web browser on http://localhost:8080/

## Features

- [x] Bucket CRUD
- [x] Bucket row CRUD
- [x] DB stats (file size, rows per bucket, etc.)
- [ ] List buckets and number of associated rows
- [ ] Support nested-buckets
- [ ] Search regex in bucket
- [ ] Detect different data formats (plain text, JSON, image, etc.) and display accordingly in GUI
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
- [ ] DB stats (file size, rows per bucket, etc.)
- [ ] List buckets and number of associated rows
- [ ] Support nested-buckets

- [ ] Search string / regex in bucket
- [ ] Handle nested buckets (display on home page and bucket page, allow creation, update and deletion)
- [ ] Handle different data formats (plain text, JSON, etc.)
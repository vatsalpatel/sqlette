package journal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
)

const (
	magic      = "SQLETTEJ"
	headerSize = 16
)

type Journal struct {
	file     *os.File
	path     string
	pageSize uint32
}

func Create(path string, pages, pageSize uint32) (*Journal, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil, err
	}
	header := make([]byte, headerSize)
	copy(header, magic)
	binary.BigEndian.PutUint32(header[8:], pages)
	binary.BigEndian.PutUint32(header[12:], pageSize)
	if _, err := f.Write(header); err != nil {
		f.Close()
		return nil, err
	}

	return &Journal{file: f, path: path, pageSize: pageSize}, nil
}

func (j *Journal) Append(id uint32, data []byte) error {
	if uint32(len(data)) != j.pageSize {
		return fmt.Errorf("journal: page is %d bytes, want %d", len(data), j.pageSize)
	}
	rec := make([]byte, 4+len(data)+4)
	binary.BigEndian.PutUint32(rec[:4], id)
	copy(rec[4:], data)
	binary.BigEndian.PutUint32(rec[4+len(data):], crc32.ChecksumIEEE(rec[:4+len(data)]))
	_, err := j.file.Write(rec)
	return err
}

func (j *Journal) Sync() error  { return j.file.Sync() }
func (j *Journal) Close() error { return j.file.Close() }

func (j *Journal) Delete() error {
	if err := j.file.Close(); err != nil {
		return err
	}
	return Remove(j.path)
}

func Remove(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func Replay(path string, db *os.File) (dbPages uint32, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	header := make([]byte, headerSize)
	if _, err := io.ReadFull(f, header); err != nil {
		return 0, fmt.Errorf("journal: failed to read header: %v", err)
	}
	if string(header[:len(magic)]) != magic {
		return 0, fmt.Errorf("journal: invalid magic ")
	}
	dbPages = binary.BigEndian.Uint32(header[8:])
	pageSize := binary.BigEndian.Uint32(header[12:])
	if pageSize == 0 {
		return 0, fmt.Errorf("journal: invalid page size")
	}

	rec := make([]byte, 4+pageSize+4)
	for {
		_, err := io.ReadFull(f, rec)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("journal: failed to read record: %v", err)
		}
		id := binary.BigEndian.Uint32(rec[:4])
		data := rec[4 : 4+pageSize]
		if crc32.ChecksumIEEE(rec[:4+pageSize]) != binary.BigEndian.Uint32(rec[4+pageSize:]) {
			break
		}
		if _, err := db.WriteAt(data, int64(id)*int64(pageSize)); err != nil {
			return 0, fmt.Errorf("journal: failed to write page: %v", err)
		}
	}

	if err := db.Truncate(int64(dbPages) * int64(pageSize)); err != nil {
		return 0, fmt.Errorf("journal: failed to truncate database: %v", err)
	}
	if err := db.Sync(); err != nil {
		return 0, fmt.Errorf("journal: failed to sync database: %v", err)
	}
	return dbPages, nil
}

package pager

import (
	"os"
)

const (
	PageSize = 4096
)

type PageID uint32

type Page struct {
	ID    PageID
	Data  [PageSize]byte
	dirty bool
}

type Pager struct {
	file  *os.File
	cache map[PageID]*Page
	Count PageID
}

func Open(path string) (*Pager, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	return &Pager{
		file:  file,
		cache: make(map[PageID]*Page),
		Count: PageID(info.Size() / int64(PageSize)),
	}, nil
}

func (p *Pager) Get(id PageID) (*Page, error) {
	if p.cache[id] != nil {
		return p.cache[id], nil
	}
	page := new(Page)
	_, err := p.file.ReadAt(page.Data[:], int64(id)*int64(PageSize))
	if err != nil {
		return nil, err
	}
	p.cache[id] = page
	return page, nil
}

func (p *Pager) Allocate() (*Page, error) {
	p.Count++
	page := new(Page)
	page.ID = p.Count
	_, err := p.file.WriteAt(page.Data[:], int64(page.ID)*int64(PageSize))
	if err != nil {
		return nil, err
	}
	p.cache[page.ID] = page
	return page, nil
}

func (p *Pager) Flush() error {
	for _, page := range p.cache {
		if page.dirty {
			_, err := p.file.WriteAt(page.Data[:], int64(page.ID)*int64(PageSize))
			if err != nil {
				return err
			}
			page.dirty = false
		}
	}
	err := p.file.Sync()
	if err != nil {
		return err
	}
	return nil
}

func (p *Pager) Close() error {
	err := p.Flush()
	if err != nil {
		return err
	}
	return p.file.Close()
}

func (p *Page) MarkDirty() {
	p.dirty = true
}

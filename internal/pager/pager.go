package pager

import (
	"os"

	"github.com/vatsalpatel/sqlette/internal/journal"
)

const (
	PageSize      = 4096
	journalSuffix = "-journal"
)

type PageID uint32

type Page struct {
	ID    PageID
	Data  [PageSize]byte
	dirty bool
}

type Pager struct {
	file       *os.File
	cache      map[PageID]*Page
	Count      PageID
	path       string
	journal    *journal.Journal
	inTxn      bool
	startCount PageID
}

func Open(path string) (*Pager, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}

	jpath := path + journalSuffix
	if _, err := os.Stat(jpath); err == nil {
		if _, err := journal.Replay(jpath, file); err != nil {
			return nil, err
		}
		if err := journal.Remove(jpath); err != nil {
			return nil, err
		}
	}

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	pager := &Pager{
		file:  file,
		cache: make(map[PageID]*Page),
		Count: PageID(info.Size() / int64(PageSize)),
		path:  path,
	}
	if pager.Count == 0 {
		if _, err := pager.Allocate(); err != nil {
			return nil, err
		}
	}
	return pager, nil
}

func (p *Pager) Get(id PageID) (*Page, error) {
	if p.cache[id] != nil {
		return p.cache[id], nil
	}
	page := new(Page)
	page.ID = id
	_, err := p.file.ReadAt(page.Data[:], int64(id)*int64(PageSize))
	if err != nil {
		return nil, err
	}
	p.cache[id] = page
	return page, nil
}

func (p *Pager) Allocate() (*Page, error) {
	page := &Page{ID: p.Count, dirty: true}
	p.cache[page.ID] = page
	p.Count++
	return page, nil
}

func (p *Pager) Write(page *Page) error {
	if page.dirty {
		return nil // already captured this txn (or freshly allocated → born dirty)
	}
	if p.inTxn && page.ID < p.startCount {
		if p.journal == nil {
			j, err := journal.Create(p.path+journalSuffix, uint32(p.startCount), PageSize)
			if err != nil {
				return err
			}
			p.journal = j
		}
		if err := p.journal.Append(uint32(page.ID), page.Data[:]); err != nil {
			return err
		}
	}
	page.dirty = true
	return nil
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
	if err := p.file.Truncate(int64(p.Count) * int64(PageSize)); err != nil {
		return err
	}
	return p.file.Sync()
}

func (p *Pager) Close() error {
	err := p.Flush()
	if err != nil {
		return err
	}
	return p.file.Close()
}

func (p *Pager) Begin() error {
	p.inTxn = true
	p.startCount = p.Count
	return nil
}

func (p *Pager) Commit() error {
	if p.journal != nil {
		if err := p.journal.Sync(); err != nil { // 1. pre-images durable FIRST
			return err
		}
	}
	if err := p.Flush(); err != nil { // 2. write dirty pages, truncate, fsync db
		return err
	}
	if p.journal != nil {
		if err := p.journal.Delete(); err != nil { // 3. THE commit point
			return err
		}
		p.journal = nil
	}
	p.inTxn = false
	return nil
}

func (p *Pager) Rollback() error {
	if p.journal != nil {
		dbPages, err := journal.Replay(p.path+journalSuffix, p.file)
		if err != nil {
			return err
		}
		if err := p.journal.Delete(); err != nil {
			return err
		}
		p.journal = nil
		p.Count = PageID(dbPages)
	} else {
		p.Count = p.startCount
		if err := p.file.Truncate(int64(p.Count) * int64(PageSize)); err != nil {
			return err
		}
	}
	clear(p.cache)
	p.inTxn = false
	return nil
}

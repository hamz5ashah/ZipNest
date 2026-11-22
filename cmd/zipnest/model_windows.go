//go:build windows

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lxn/walk"
)

// row is one line of the file list (a file system item or an archive member).
type row struct {
	name      string
	dir       bool
	up        bool // the ".." row
	size      int64
	packed    int64
	mtime     time.Time
	typ       string
	icon      string // path used to look up the shell icon
	full      string // file system path, or member path inside the archive
	isArchive bool
	encrypted bool
}

type listModel struct {
	walk.TableModelBase
	walk.SorterBase
	rows    []row // visible rows (after the filter)
	all     []row // every row of the current location
	filter  string
	sortCol int
	order   walk.SortOrder
}

func (m *listModel) RowCount() int { return len(m.rows) }

func humanSize(n int64) string {
	if n < 0 {
		return "?"
	}
	// Group thousands like Explorer does.
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func shortSize(n int64) string {
	switch {
	case n < 0:
		return "?"
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
}

func (m *listModel) Value(r, c int) interface{} {
	x := m.rows[r]
	switch c {
	case 0:
		if x.encrypted {
			return x.name + " *"
		}
		return x.name
	case 1:
		if x.dir || x.up {
			return ""
		}
		return humanSize(x.size)
	case 2:
		if x.dir || x.up || x.packed < 0 {
			return ""
		}
		return humanSize(x.packed)
	case 3:
		if x.dir || x.up || x.packed < 0 || x.size <= 0 {
			return ""
		}
		return fmt.Sprintf("%d%%", x.packed*100/x.size)
	case 4:
		return x.typ
	case 5:
		if x.mtime.IsZero() || x.up {
			return ""
		}
		return x.mtime.Format("2006-01-02 15:04")
	}
	return ""
}

func (m *listModel) Image(r int) interface{} { return m.rows[r].icon }

func (m *listModel) Sort(col int, order walk.SortOrder) error {
	m.sortCol, m.order = col, order
	m.sortRows()
	m.PublishRowsReset()
	return m.SorterBase.Sort(col, order)
}

func (m *listModel) sortRows() {
	sort.SliceStable(m.rows, func(i, j int) bool {
		a, b := m.rows[i], m.rows[j]
		if a.up != b.up {
			return a.up
		}
		if a.dir != b.dir {
			return a.dir
		}
		var less bool
		switch m.sortCol {
		case 1:
			less = a.size < b.size
		case 2:
			less = a.packed < b.packed
		case 3:
			less = ratio(a) < ratio(b)
		case 4:
			less = strings.ToLower(a.typ) < strings.ToLower(b.typ)
		case 5:
			less = a.mtime.Before(b.mtime)
		default:
			less = strings.ToLower(a.name) < strings.ToLower(b.name)
		}
		if m.order == walk.SortDescending {
			return !less
		}
		return less
	})
}

func ratio(r row) int64 {
	if r.size <= 0 || r.packed < 0 {
		return 1 << 40
	}
	return r.packed * 1000 / r.size
}

func (m *listModel) setRows(rows []row) {
	m.all = rows
	m.applyFilter()
}

// setFilter shows only rows whose name contains text (case-insensitive).
func (m *listModel) setFilter(text string) {
	m.filter = strings.ToLower(strings.TrimSpace(text))
	m.applyFilter()
}

func (m *listModel) applyFilter() {
	if m.filter == "" {
		m.rows = append([]row(nil), m.all...)
	} else {
		m.rows = m.rows[:0]
		for _, r := range m.all {
			if r.up || strings.Contains(strings.ToLower(r.name), m.filter) {
				m.rows = append(m.rows, r)
			}
		}
	}
	m.sortRows()
	m.PublishRowsReset()
}
